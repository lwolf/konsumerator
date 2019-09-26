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
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/lwolf/konsumerator/pkg/errors"
	"github.com/lwolf/konsumerator/pkg/helpers"
)

const (
	ownerKey            = ".metadata.controller"
	AnnotationIsManaged = "konsumerator.lwolf.org/managed"
)

// ConfigMapReconciler reconciles a ConfigMap object
type ConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *ConfigMapReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	fmt.Println("reconciling in configmap", req.NamespacedName)
	ctx := context.Background()
	log := r.Log.WithValues("configmap", req.NamespacedName)

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
	deploy := r.constructDeploy(cm, 0)
	if err := ctrl.SetControllerReference(&cm, deploy, r.Scheme); err != nil {
		deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
		log.Error(err, "unable to set owner reference for the new Deployment", "deployment", deploy)
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, deploy); errors.IgnoreAlreadyExists(err) != nil {
		deploymentsCreateErrors.WithLabelValues(req.Name).Inc()
		log.Error(err, "unable to create new Deployment", "deployment", deploy)
		return ctrl.Result{}, err
	}

	// your logic here

	return ctrl.Result{RequeueAfter: time.Duration(time.Second * 10)}, nil
}

func (r *ConfigMapReconciler) constructDeploy(cm corev1.ConfigMap, partition int32) *appsv1.Deployment {
	deployLabels := make(map[string]string)
	deployAnnotations := make(map[string]string)
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels:      deployLabels,
			Annotations: deployAnnotations,
			Name:        fmt.Sprintf("ctrl-%s", cm.Name),
			Namespace:   cm.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: helpers.Ptr2Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"testkey": "testValue"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cm.Name,
					Namespace: cm.Namespace,
					Labels:    map[string]string{"testkey": "testValue"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name:  "test",
							Image: "busybox",
						},
					},
				},
			},
		},
	}
	deploy.Annotations[PartitionAnnotation] = strconv.Itoa(int(partition))
	return deploy
}

func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(&appsv1.Deployment{}, ownerKey, func(rawObj runtime.Object) []string {
		// grab the object, extract the owner...
		d := rawObj.(*appsv1.Deployment)
		owner := metav1.GetControllerOf(d)
		fmt.Println("deployment reconcile", d.Name, owner)
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
