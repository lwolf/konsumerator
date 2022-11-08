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
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
	"github.com/lwolf/konsumerator/pkg/errors"
)

const (
	consumerOwnerKey = ".metadata.controller"
)

// ConsumerReconciler reconciles a Consumer object
type ConsumerReconciler struct {
	client.Client

	Log      logr.Logger
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme
	Clock    clock.Clock
}

func (r *ConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Clock == nil {
		r.Clock = clock.RealClock{}
	}

	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &appsv1.Deployment{}, consumerOwnerKey, func(rawObj client.Object) []string {
			d := rawObj.(*appsv1.Deployment)
			owner := metav1.GetControllerOf(d)
			if owner == nil {
				return nil
			}
			if owner.APIVersion != apiGVStr || owner.Kind != "Consumer" {
				return nil
			}
			return []string{owner.Name}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&konsumeratorv1.Consumer{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=konsumerator.lwolf.org,resources=consumers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konsumerator.lwolf.org,resources=consumers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=watch;create;get;update;patch;delete;list
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get

func (r *ConsumerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reconcileTotal.WithLabelValues(req.Name).Inc()
	start := time.Now()
	defer func() {
		reconcileDuration.WithLabelValues(req.Name).Observe(time.Since(start).Seconds())
	}()

	log := r.Log.WithValues("consumer", req.NamespacedName)
	result := ctrl.Result{RequeueAfter: defaultMinSyncPeriod}

	var consumer konsumeratorv1.Consumer
	if err := r.Get(ctx, req.NamespacedName, &consumer); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		reconcileErrors.WithLabelValues(req.Name).Inc()
		return ctrl.Result{}, errors.IgnoreNotFound(err)
	}
	var managedDeploys appsv1.DeploymentList
	if err := r.List(ctx, &managedDeploys, client.InNamespace(req.Namespace), client.MatchingFields{consumerOwnerKey: req.Name}); err != nil {
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
		owner:    &consumer,
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
	o.log.Info(
		"deployments count",
		"metricsUpdated", o.metricsUpdated,
		"expected", consumer.Status.Expected,
		"running", consumer.Status.Running,
		"paused", consumer.Status.Paused,
		"missing", consumer.Status.Missing,
		"lagging", consumer.Status.Lagging,
		"toUpdate", consumer.Status.Outdated,
		"redundant", consumer.Status.Redundant,
		"toEstimate", len(o.toEstimateInstances),
	)
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
