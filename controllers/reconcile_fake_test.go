package controllers_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	tlog "github.com/go-logr/logr/testing"
	"github.com/google/go-cmp/cmp"
	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	"github.com/lwolf/konsumerator/controllers"
	"github.com/lwolf/konsumerator/pkg/helpers"
	"github.com/lwolf/konsumerator/pkg/helpers/tests"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func initReconciler(consumer *konsumeratorv1alpha1.Consumer) (client.Client, *controllers.ConsumerReconciler) {
	var s = runtime.NewScheme()

	_ = clientgoscheme.AddToScheme(s)
	_ = konsumeratorv1alpha1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)

	cl := fake.NewFakeClientWithScheme(s, consumer)
	broadcaster := record.NewBroadcasterForTests(time.Second)
	eventSource := corev1.EventSource{Component: "eventTest"}
	recorder := broadcaster.NewRecorder(s, eventSource)
	return cl, &controllers.ConsumerReconciler{cl, tlog.NullLogger{}, recorder, s}

}

func TestConsumerReconciliation(t *testing.T) {
	var (
		name      = "testconsumer"
		namespace = "testns"
	)
	objMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	autoscalerSpec := konsumeratorv1alpha1.AutoscalerSpec{
		Mode: konsumeratorv1alpha1.AutoscalerTypePrometheus,
		Prometheus: &konsumeratorv1alpha1.PrometheusAutoscalerSpec{
			Offset:      konsumeratorv1alpha1.OffsetQuerySpec{},
			Production:  konsumeratorv1alpha1.ProductionQuerySpec{},
			Consumption: konsumeratorv1alpha1.ConsumptionQuerySpec{},
			RatePerCore: helpers.Ptr2Int64(5000),
			RamPerCore:  resource.Quantity{},
		},
	}
	testCases := map[string]struct {
		consumer              *konsumeratorv1alpha1.Consumer
		expDeployAnnotation   map[string]map[string]string
		expContainerResources map[string]corev1.ResourceRequirements
		expContainerEnv       map[string]map[string]string
	}{
		"deployment should have minimum resources without metrics provider": {
			consumer: &konsumeratorv1alpha1.Consumer{
				ObjectMeta: objMeta,
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions: helpers.Ptr2Int32(1),
					Name:          name,
					Namespace:     namespace,
					Autoscaler:    &autoscalerSpec,
					DeploymentTemplate: appsv1.DeploymentSpec{
						Replicas: helpers.Ptr2Int32(1),
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}, MatchExpressions: nil},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "busybox", Image: "busybox-image", Env: []corev1.EnvVar{{Name: "testKey", Value: "testValue"}}},
								},
							}},
					},
					ResourcePolicy: &konsumeratorv1alpha1.ResourcePolicy{
						ContainerPolicies: []konsumeratorv1alpha1.ContainerResourcePolicy{
							tests.NewContainerResourcePolicy("busybox", "100m", "100M", "100m", "100M"),
						},
					},
				},
			},
			expDeployAnnotation: map[string]map[string]string{
				fmt.Sprintf("%s-0", name): {
					"konsumerator.lwolf.org/partition":      "0",
					"konsumerator.lwolf.org/scaling-status": "RUNNING",
				},
			},
			expContainerEnv: map[string]map[string]string{
				"busybox": {
					"KONSUMERATOR_PARTITION": "0",
					"GOMAXPROCS":             "1",
					"testKey":                "testValue",
				},
			},
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
		},
		"sidecar container should not skew metrics estimation": {
			consumer: &konsumeratorv1alpha1.Consumer{
				ObjectMeta: objMeta,
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions: helpers.Ptr2Int32(1),
					Name:          name,
					Namespace:     namespace,
					Autoscaler:    &autoscalerSpec,
					DeploymentTemplate: appsv1.DeploymentSpec{
						Replicas: helpers.Ptr2Int32(1),
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}, MatchExpressions: nil},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "busybox", Image: "busybox-image", Env: []corev1.EnvVar{{Name: "TESTKEY", Value: "TESTVALUE"}}},
									{Name: "sidecar", Image: "sidecar-image", Env: []corev1.EnvVar{{Name: "SIDECAR_KEY", Value: "SIDECAR_VALUE"}}},
								},
							}},
					},
					ResourcePolicy: &konsumeratorv1alpha1.ResourcePolicy{
						ContainerPolicies: []konsumeratorv1alpha1.ContainerResourcePolicy{
							tests.NewContainerResourcePolicy("busybox", "100m", "100M", "100m", "100M"),
						},
					},
				},
			},
			expDeployAnnotation: map[string]map[string]string{
				fmt.Sprintf("%s-0", name): {
					"konsumerator.lwolf.org/partition":      "0",
					"konsumerator.lwolf.org/scaling-status": "RUNNING",
				},
			},
			expContainerEnv: map[string]map[string]string{
				"busybox": {
					"KONSUMERATOR_PARTITION": "0",
					"GOMAXPROCS":             "1",
					"TESTKEY":                "TESTVALUE",
				},
				"sidecar": {
					"KONSUMERATOR_PARTITION": "0",
					"GOMAXPROCS":             "1",
					"SIDECAR_KEY":            "SIDECAR_VALUE",
				},
			},
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
		},
		"resource policy should be applied to corresponding containers correctly": {
			consumer: &konsumeratorv1alpha1.Consumer{
				ObjectMeta: objMeta,
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions: helpers.Ptr2Int32(1),
					Name:          name,
					Namespace:     namespace,
					Autoscaler:    &autoscalerSpec,
					DeploymentTemplate: appsv1.DeploymentSpec{
						Replicas: helpers.Ptr2Int32(1),
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}, MatchExpressions: nil},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "busybox",
										Image: "busybox-image",
										Env:   []corev1.EnvVar{{Name: "TESTKEY", Value: "TESTVALUE"}},
									},
									{
										Name:  "sidecar",
										Image: "sidecar-image",
										Env:   []corev1.EnvVar{{Name: "SIDECAR_KEY", Value: "SIDECAR_VALUE"}},
									},
								},
							}},
					},
					ResourcePolicy: &konsumeratorv1alpha1.ResourcePolicy{
						ContainerPolicies: []konsumeratorv1alpha1.ContainerResourcePolicy{
							tests.NewContainerResourcePolicy("busybox", "400m", "400M", "800m", "800M"),
							tests.NewContainerResourcePolicy("sidecar", "100m", "100M", "100m", "100M"),
						},
					},
				},
			},
			expDeployAnnotation: map[string]map[string]string{
				fmt.Sprintf("%s-0", name): {
					"konsumerator.lwolf.org/partition":      "0",
					"konsumerator.lwolf.org/scaling-status": "RUNNING",
				},
			},
			expContainerEnv: map[string]map[string]string{
				"busybox": {
					"KONSUMERATOR_PARTITION": "0",
					"GOMAXPROCS":             "1",
					"TESTKEY":                "TESTVALUE",
				},
				"sidecar": {
					"KONSUMERATOR_PARTITION": "0",
					"GOMAXPROCS":             "1",
					"SIDECAR_KEY":            "SIDECAR_VALUE",
				},
			},
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("400m", "400M", "400m", "400M"),
				"sidecar": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
		},
		"resource policy should ignore global policy on first run": {
			consumer: &konsumeratorv1alpha1.Consumer{
				ObjectMeta: objMeta,
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions: helpers.Ptr2Int32(1),
					Name:          name,
					Namespace:     namespace,
					Autoscaler:    &autoscalerSpec,
					DeploymentTemplate: appsv1.DeploymentSpec{
						Replicas: helpers.Ptr2Int32(1),
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}, MatchExpressions: nil},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "busybox",
										Image: "busybox-image",
										Env:   []corev1.EnvVar{{Name: "TESTKEY", Value: "TESTVALUE"}},
									},
									{
										Name:  "sidecar",
										Image: "sidecar-image",
										Env:   []corev1.EnvVar{{Name: "SIDECAR_KEY", Value: "SIDECAR_VALUE"}},
									},
								},
							}},
					},
					ResourcePolicy: &konsumeratorv1alpha1.ResourcePolicy{
						GlobalPolicy: &konsumeratorv1alpha1.GlobalResourcePolicy{
							MaxAllowed: *tests.NewResourceList("450m", "450M"),
						},
						ContainerPolicies: []konsumeratorv1alpha1.ContainerResourcePolicy{
							tests.NewContainerResourcePolicy("busybox", "400m", "400M", "800m", "800M"),
							tests.NewContainerResourcePolicy("sidecar", "100m", "100M", "100m", "100M"),
						},
					},
				},
			},
			expDeployAnnotation: map[string]map[string]string{
				fmt.Sprintf("%s-0", name): {
					"konsumerator.lwolf.org/partition":      "0",
					"konsumerator.lwolf.org/scaling-status": "RUNNING",
				},
			},
			expContainerEnv: map[string]map[string]string{
				"busybox": {
					"KONSUMERATOR_PARTITION": "0",
					"GOMAXPROCS":             "1",
					"TESTKEY":                "TESTVALUE",
				},
				"sidecar": {
					"KONSUMERATOR_PARTITION": "0",
					"GOMAXPROCS":             "1",
					"SIDECAR_KEY":            "SIDECAR_VALUE",
				},
			},
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("400m", "400M", "400m", "400M"),
				"sidecar": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
		},
		// "disabled policy for container should be respected":                 {},
		// "containers should preserve resources set in the consumer if no other policy is set": {},
	}
	for tName, tc := range testCases {
		t.Run(tName, func(t *testing.T) {
			cl, r := initReconciler(tc.consumer)
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.consumer.Name,
					Namespace: tc.consumer.Namespace,
				},
			}
			_, err := r.Reconcile(req)
			if err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}
			deployments := &appsv1.DeploymentList{}
			err = cl.List(context.TODO(), deployments, client.InNamespace(tc.consumer.Namespace))
			if err != nil {
				t.Fatalf("get deployment: (%v)", err)
			}
			for _, deploy := range deployments.Items {
				for eKey, eValue := range tc.expDeployAnnotation[deploy.Name] {
					value, exists := deploy.Annotations[eKey]
					if !exists || value != eValue {
						t.Fatalf("expected to have annoation %s set to %s, but got %s", eKey, eValue, value)
					}
				}
				for _, container := range deploy.Spec.Template.Spec.Containers {
					env := make(map[string]string)
					for i := range container.Env {
						env[container.Env[i].Name] = container.Env[i].Value
					}
					expectedEnv := tc.expContainerEnv[container.Name]
					if diff := cmp.Diff(expectedEnv, env); diff != "" {
						t.Errorf("%q Environment mismatch (-expectedEnv +container.Env):\n%s", tName, diff)
					}
					expectedResources := tc.expContainerResources[container.Name]
					if helpers.CmpResourceRequirements(container.Resources, expectedResources) != 0 {
						t.Fatalf("Container resource mismatch. \nWant \n%v, \ngot \n%v", expectedResources, container.Resources)
					}
				}
			}
		})
	}
}
