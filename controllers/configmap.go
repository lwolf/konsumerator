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
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/lwolf/konsumerator/pkg/errors"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

const (
	cfgMapOwnerKey      = ".metadata.controller"
	AnnotationIsManaged = "konsumerator.lwolf.org/managed"
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
	if err := mgr.GetFieldIndexer().
		IndexField(&appsv1.Deployment{}, cfgMapOwnerKey, func(rawObj runtime.Object) []string {
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

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *ConfigMapReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	reconcileTotal.WithLabelValues(req.Name).Inc()
	start := time.Now()
	defer func() {
		reconcileDuration.WithLabelValues(req.Name).Observe(time.Since(start).Seconds())
	}()

	ctx := context.Background()
	log := r.Log.WithValues("configmap", req.NamespacedName)
	result := ctrl.Result{RequeueAfter: defaultMinSyncPeriod}

	var cm corev1.ConfigMap
	if err := r.Get(ctx, req.NamespacedName, &cm); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, errors.IgnoreNotFound(err)
	}
	_, ok := cm.Annotations[AnnotationIsManaged]
	if !ok {
		return ctrl.Result{}, nil
	}

	var consumer konsumeratorv1alpha1.Consumer
	if err := r.Get(ctx, req.NamespacedName, &consumer); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, errors.IgnoreNotFound(err)
	}

	var managedDeploys appsv1.DeploymentList
	if err := r.List(ctx, &managedDeploys, client.InNamespace(req.Namespace), client.MatchingField(cfgMapOwnerKey, req.Name)); err != nil {
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
	}
	err := o.init(consumer.DeepCopy(), managedDeploys)
	if err != nil {
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, err
	}

	if cmp.Equal(consumer.Status, o.consumer.Status) {
		log.V(1).Info("no change detected...")
		return result, nil
	}

	start = time.Now()
	if err := r.Status().Update(ctx, o.consumer); err != nil {
		properError := errors.IgnoreConflict(err)
		if properError != nil {
			eMsg := "unable to update Consumer status"
			log.Error(err, eMsg)
			o.Recorder.Event(o.consumer, corev1.EventTypeWarning, "UpdateConsumerStatus", eMsg)
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
