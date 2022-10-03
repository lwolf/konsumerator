/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"github.com/google/go-cmp/cmp"
	"k8s.io/client-go/tools/record"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/lwolf/konsumerator/pkg/errors"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
)

const (
	cfgMapOwnerKey      = ".metadata.controller"
	AnnotationIsManaged = "konsumerator.lwolf.org/managed"

	annotationStatusExpected     = "konsumerator.lwolf.org/status.expected"
	annotationStatusRunning      = "konsumerator.lwolf.org/status.running"
	annotationStatusPaused       = "konsumerator.lwolf.org/status.paused"
	annotationStatusMissing      = "konsumerator.lwolf.org/status.missing"
	annotationStatusLagging      = "konsumerator.lwolf.org/status.lagging"
	annotationStatusOutdated     = "konsumerator.lwolf.org/status.outdated"
	annotationStatusLastSyncTime = "konsumerator.lwolf.org/status.last-sync-time"
	annotationStatusLastState    = "konsumerator.lwolf.org/status.last-sync-state"
)

// ConfigMapReconciler reconciles a ConfigMap object
type ConfigMapReconciler struct {
	client.Client

	Log      logr.Logger
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme
	Clock    clock.Clock
}

func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Clock == nil {
		r.Clock = clock.RealClock{}
	}
	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &appsv1.Deployment{}, cfgMapOwnerKey, func(rawObj client.Object) []string {
			d := rawObj.(*appsv1.Deployment)
			owner := metav1.GetControllerOf(d)
			if owner == nil {
				return nil
			}
			if owner.Kind != "ConfigMap" {
				return nil
			}
			return []string{owner.Name}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		Watches(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &corev1.ConfigMap{},
		}).
		Complete(r)
}

func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reconcileTotal.WithLabelValues(req.Name).Inc()
	start := time.Now()
	defer func() {
		reconcileDuration.WithLabelValues(req.Name).Observe(time.Since(start).Seconds())
	}()

	log := r.Log.WithValues("configmap", req.NamespacedName)
	result := ctrl.Result{RequeueAfter: defaultMinSyncPeriod}

	var cfgMap corev1.ConfigMap
	if err := r.Get(ctx, req.NamespacedName, &cfgMap); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, errors.IgnoreNotFound(err)
	}
	cm := cfgMap.DeepCopy()
	_, ok := cm.Annotations[AnnotationIsManaged]
	if !ok {
		return ctrl.Result{}, nil
	}

	var consumer konsumeratorv1.Consumer
	var data string
	for i := range cm.Data {
		data = cm.Data[i]
		break
	}
	br := strings.NewReader(data)
	d := yaml.NewYAMLToJSONDecoder(br)
	if err := d.Decode(&consumer.Spec); err != nil {
		log.Error(err, "failed to decode consumer")
		return ctrl.Result{}, nil
	}
	consumer.Namespace = cm.Namespace
	consumer.Name = cm.Name
	var managedDeploys appsv1.DeploymentList
	if err := r.List(ctx, &managedDeploys, client.InNamespace(req.Namespace), client.MatchingFields{cfgMapOwnerKey: req.Name}); err != nil {
		eMsg := "unable to list managed deployments"
		log.Error(err, eMsg)
		r.Recorder.Event(&consumer, corev1.EventTypeWarning, "ListDeployFailure", eMsg)
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, err
	}

	o := &operator{
		Recorder: r.Recorder,
		clock:    r.Clock,
		log:      log,
		Scheme:   r.Scheme,
		owner:    cm,
	}
	PopulateStatusFromAnnotation(cm.Annotations, &consumer.Status)
	initialStatus := consumer.Status.DeepCopy()
	err := o.init(&consumer, managedDeploys)
	if err != nil {
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, err
	}
	consumer.Status.ObservedGeneration = o.consumer.Status.ObservedGeneration
	if cmp.Equal(consumer.Status, initialStatus) {
		log.V(1).Info("no change detected...")
		return result, nil
	}

	start = time.Now()
	err = UpdateStatusAnnotations(cm, &consumer.Status)
	if err != nil {
		log.Error(err, "unable to populate status annotation")
		return ctrl.Result{}, err
	}
	if err := r.Update(ctx, cm); err != nil {
		properError := errors.IgnoreConflict(err)
		if properError != nil {
			eMsg := "unable to update ConfigMap annotation status"
			log.Error(err, eMsg)
			o.Recorder.Event(o.consumer, corev1.EventTypeWarning, "UpdateConfigMapStatus", eMsg)
			reconcileErrors.WithLabelValues(req.Name).Inc()
		}
		return result, properError
	}

	statusUpdateDuration.WithLabelValues(req.Name).Observe(time.Since(start).Seconds())

	if err := o.reconcile(r.Client, req); err != nil {
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return result, err
	}

	return result, nil
}
