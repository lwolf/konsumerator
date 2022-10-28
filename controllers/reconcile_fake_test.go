package controllers_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
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
	testclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var fakeClock = testclock.NewFakeClock(time.Now())

func initReconciler(consumer *konsumeratorv1.Consumer) (client.Client, *controllers.ConsumerReconciler) {
	var s = runtime.NewScheme()

	_ = clientgoscheme.AddToScheme(s)
	_ = konsumeratorv1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)

	cl := &fakeClient{fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(consumer).Build()}
	broadcaster := record.NewBroadcasterForTests(time.Second)
	eventSource := corev1.EventSource{Component: "eventTest"}
	recorder := broadcaster.NewRecorder(s, eventSource)
	return cl, &controllers.ConsumerReconciler{
		Client:   cl,
		Log:      logr.Discard(),
		Recorder: recorder,
		Scheme:   s,
		Clock:    fakeClock,
	}
}

type fakeClient struct {
	client.Client
}

func (fc *fakeClient) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	if err := fc.Client.List(ctx, obj, opts...); err != nil {
		return err
	}
	dl, ok := obj.(*appsv1.DeploymentList)
	if !ok {
		return nil
	}
	for i, item := range dl.Items {
		if _, ok := item.Annotations[controllers.DisableAutoscalerAnnotation]; ok {
			item.Status.Replicas = 0
		} else {
			item.Status.Replicas = 1
		}
		dl.Items[i] = item
	}
	return nil
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
	autoscalerSpec := konsumeratorv1.AutoscalerSpec{
		Mode: konsumeratorv1.AutoscalerTypePrometheus,
		Prometheus: &konsumeratorv1.PrometheusAutoscalerSpec{
			Offset:      konsumeratorv1.OffsetQuerySpec{},
			Production:  konsumeratorv1.ProductionQuerySpec{},
			Consumption: konsumeratorv1.ConsumptionQuerySpec{},
			RatePerCore: helpers.Ptr2Int64(5000),
			RamPerCore:  resource.Quantity{},
		},
	}
	testCases := map[string]struct {
		consumer              *konsumeratorv1.Consumer
		expDeployAnnotation   map[string]map[string]string
		expContainerResources map[string]corev1.ResourceRequirements
		expContainerEnv       map[string]map[string]string
	}{
		"deployment should have minimum resources without metrics provider": {
			consumer: &konsumeratorv1.Consumer{
				ObjectMeta: objMeta,
				Spec: konsumeratorv1.ConsumerSpec{
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
					ResourcePolicy: &konsumeratorv1.ResourcePolicy{
						ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
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
					"KONSUMERATOR_PARTITION":      "0",
					"GOMAXPROCS":                  "1",
					"KONSUMERATOR_INSTANCE":       "0",
					"KONSUMERATOR_NUM_INSTANCES":  "1",
					"KONSUMERATOR_NUM_PARTITIONS": "1",
					"testKey":                     "testValue",
				},
			},
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
		},
		"sidecar container should not skew metrics estimation": {
			consumer: &konsumeratorv1.Consumer{
				ObjectMeta: objMeta,
				Spec: konsumeratorv1.ConsumerSpec{
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
					ResourcePolicy: &konsumeratorv1.ResourcePolicy{
						ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
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
					"KONSUMERATOR_PARTITION":      "0",
					"GOMAXPROCS":                  "1",
					"KONSUMERATOR_INSTANCE":       "0",
					"KONSUMERATOR_NUM_INSTANCES":  "1",
					"KONSUMERATOR_NUM_PARTITIONS": "1",
					"TESTKEY":                     "TESTVALUE",
				},
				"sidecar": {
					"KONSUMERATOR_PARTITION":      "0",
					"GOMAXPROCS":                  "1",
					"KONSUMERATOR_INSTANCE":       "0",
					"KONSUMERATOR_NUM_INSTANCES":  "1",
					"KONSUMERATOR_NUM_PARTITIONS": "1",
					"SIDECAR_KEY":                 "SIDECAR_VALUE",
				},
			},
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
		},
		"resource policy should be applied to corresponding containers correctly": {
			consumer: &konsumeratorv1.Consumer{
				ObjectMeta: objMeta,
				Spec: konsumeratorv1.ConsumerSpec{
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
					ResourcePolicy: &konsumeratorv1.ResourcePolicy{
						ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
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
					"KONSUMERATOR_PARTITION":      "0",
					"GOMAXPROCS":                  "1",
					"KONSUMERATOR_INSTANCE":       "0",
					"KONSUMERATOR_NUM_INSTANCES":  "1",
					"KONSUMERATOR_NUM_PARTITIONS": "1",
					"TESTKEY":                     "TESTVALUE",
				},
				"sidecar": {
					"KONSUMERATOR_PARTITION":      "0",
					"GOMAXPROCS":                  "1",
					"KONSUMERATOR_INSTANCE":       "0",
					"KONSUMERATOR_NUM_INSTANCES":  "1",
					"KONSUMERATOR_NUM_PARTITIONS": "1",
					"SIDECAR_KEY":                 "SIDECAR_VALUE",
				},
			},
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("400m", "400M", "400m", "400M"),
				"sidecar": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
		},
		"resource policy should ignore global policy on first run": {
			consumer: &konsumeratorv1.Consumer{
				ObjectMeta: objMeta,
				Spec: konsumeratorv1.ConsumerSpec{
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
					ResourcePolicy: &konsumeratorv1.ResourcePolicy{
						GlobalPolicy: &konsumeratorv1.GlobalResourcePolicy{
							MaxAllowed: *tests.NewResourceList("450m", "450M"),
						},
						ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
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
					"KONSUMERATOR_PARTITION":      "0",
					"GOMAXPROCS":                  "1",
					"KONSUMERATOR_INSTANCE":       "0",
					"KONSUMERATOR_NUM_INSTANCES":  "1",
					"KONSUMERATOR_NUM_PARTITIONS": "1",
					"TESTKEY":                     "TESTVALUE",
				},
				"sidecar": {
					"KONSUMERATOR_PARTITION":      "0",
					"GOMAXPROCS":                  "1",
					"KONSUMERATOR_INSTANCE":       "0",
					"KONSUMERATOR_NUM_INSTANCES":  "1",
					"KONSUMERATOR_NUM_PARTITIONS": "1",
					"SIDECAR_KEY":                 "SIDECAR_VALUE",
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
			_, err := r.Reconcile(context.Background(), req)
			if err != nil {
				t.Fatalf("reconcile err: %v", err)
			}
			deployments := &appsv1.DeploymentList{}
			err = cl.List(context.TODO(), deployments, client.InNamespace(tc.consumer.Namespace))
			if err != nil {
				t.Fatalf("get deployments err: %v", err)
			}
			for _, deploy := range deployments.Items {
				for eKey, eValue := range tc.expDeployAnnotation[deploy.Name] {
					value := deploy.Annotations[eKey]
					if value != eValue {
						t.Fatalf("expected to have annotation %s set to %s, but got %s", eKey, eValue, value)
					}
				}
				for _, container := range deploy.Spec.Template.Spec.Containers {
					env := make(map[string]string)
					for i := range container.Env {
						env[container.Env[i].Name] = container.Env[i].Value
					}
					expectedEnv := tc.expContainerEnv[container.Name]
					if diff := cmp.Diff(expectedEnv, env); diff != "" {
						t.Errorf("%q environment mismatch (-expectedEnv +container.Env):\n%s", tName, diff)
					}
					expectedResources := tc.expContainerResources[container.Name]
					if helpers.CmpResourceRequirements(container.Resources, expectedResources) != 0 {
						t.Fatalf("container resource mismatch: \nwant \n%v, \ngot \n%v", expectedResources, container.Resources)
					}
				}
			}
		})
	}
}

func TestConsumerReconciler_Reconcile(t *testing.T) {
	name, namespace := "ConsumerReconciler_Reconcile", "Test"
	testCases := []struct {
		name                  string
		timePassed            time.Duration
		promResponse          *fakeMetrics
		expDeployAnnotation   map[string]map[string]string
		expContainerResources map[string]corev1.ResourceRequirements
		expContainerEnv       map[string]map[string]string
		expConsumerState      konsumeratorv1.ConsumerStatus
	}{
		{
			name:                "should have one missing deployment on consumer creation",
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "1", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(1),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(0),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "should have a single running deployment after the first reconcile",
			promResponse:        promMetricsForLag(0),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "1", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "lag detected, should set pending scale up status for the lagging deployment",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(6),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusPendingScaleUp),
			expContainerEnv:     containerEnv("busybox", "0", "1", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "should scale the deployment",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(15),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1300m", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "lag still present, pending scale up again",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(20),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusPendingScaleUp),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1300m", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:       "scale up and saturate",
			timePassed: time.Minute * 10,
			promResponse: &fakeMetrics{
				offset:      10e3 * 60 * 10,
				production:  10e3,
				consumption: 5e3,
			},
			expDeployAnnotation: deployAnnotationSaturated(name, "400"),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("2", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:       "lag decreased, saturation as well, no other changes",
			timePassed: time.Minute * 10,
			promResponse: &fakeMetrics{
				offset:      10e3 * 60 * 5,
				production:  10e3,
				consumption: 5e3,
			},
			expDeployAnnotation: deployAnnotationSaturated(name, "200"),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("2", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "lag consumed, pending scale down",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(2),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusPendingScaleDown),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("2", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "lag consumed, back to normal",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(1),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1100m", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:       "should do nothing",
			timePassed: time.Second * 10,
			promResponse: &fakeMetrics{
				offset:      5e3 * 60 * 1,
				production:  5e3,
				consumption: 5e3,
			},
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1100m", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:       "should set pending scale down status",
			timePassed: time.Minute * 10,
			promResponse: &fakeMetrics{
				offset:      0,
				production:  1e3,
				consumption: 1e3,
			},
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusPendingScaleDown),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1100m", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Paused:    helpers.Ptr2Int32(0),
				Redundant: helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "should actually scale down",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(1),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1100m", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "should be running normally",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(1),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1.1", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:       "should be pending scale down",
			timePassed: time.Minute * 10,
			promResponse: &fakeMetrics{
				offset:      0,
				production:  10,
				consumption: 5e3,
			},
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusPendingScaleDown),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1.1", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:       "should be running on minimum resources",
			timePassed: time.Minute * 10,
			promResponse: &fakeMetrics{
				offset:      0,
				production:  10,
				consumption: 5e3,
			},
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "1", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "200M", "1", "200M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		// Test for the case when traffic increases in multiple times,
		// which makes konsumerator set status to saturated immediately without
		// even scaling up
		{
			name:       "should set scaling status to PENDING_SCALE_UP instead of saturated",
			timePassed: time.Minute * 10,
			promResponse: &fakeMetrics{
				offset:      5e3 * 60 * 60 * 24,
				production:  5e3,
				consumption: 10,
			},
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusPendingScaleUp),
			expContainerEnv:     containerEnv("busybox", "0", "1", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "200M", "1", "200M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "should assign maximum resources and set status saturated",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(60 * 3), // 3h lag
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusSaturated),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("2", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "still nothing to do, after all the resources are allocated",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(60 * 3), // 3h lag,
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusSaturated),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("2", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
	}

	promServer := newFakePromServer(t)
	c := newConsumer(name, namespace)
	c.Spec.DeploymentTemplate.Template = corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "busybox", Image: "busybox-image"},
			},
		},
	}
	c.Spec.ResourcePolicy = &konsumeratorv1.ResourcePolicy{
		ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
			tests.NewContainerResourcePolicy("busybox", "100m", "100M", "2", "400M"),
		},
	}
	c.Spec.Autoscaler.Prometheus.Address = []string{promServer.URL}
	tr := newTestReconciler(t, c)

	for step, tc := range testCases {
		t.Run(fmt.Sprintf("step_%d", step+1), func(t *testing.T) {
			t.Log(tc.name)
			fakeClock.Step(tc.timePassed)
			if tc.promResponse != nil {
				promServer.setResponse(tc.promResponse)
				tc.expConsumerState.LastSyncState = map[string]konsumeratorv1.InstanceState{
					"0": {
						ProductionRate:  tc.promResponse.production,
						ConsumptionRate: tc.promResponse.consumption,
						MessagesBehind:  tc.promResponse.offset,
					},
				}
			}
			tr.mustReconcile()
			if err := tr.equalConsumerStatus(tc.expConsumerState); err != nil {
				t.Logf("%s", err)
				t.Fail()
			}
			deployments := tr.fetchDeployments()
			for _, deployment := range deployments {
				if err := deployment.diffAnnotation(tc.expDeployAnnotation); err != nil {
					t.Logf("annotation diff err: %s", err)
					t.Fail()
				}
				if err := deployment.diffEnv(tc.expContainerEnv); err != nil {
					t.Logf("environment diff err: %s", err)
					t.Fail()
				}
				if err := deployment.diffResources(tc.expContainerResources); err != nil {
					t.Logf("resources diff err: %s", err)
					t.Fail()
				}
			}
		})
	}
}

func promMetricsForLag(lagMinutes int64) *fakeMetrics {
	if lagMinutes < 1 {
		lagMinutes = 1
	}
	return &fakeMetrics{
		offset:      5e3 * 60 * lagMinutes,
		production:  5e3,
		consumption: 5e3,
	}
}

func TestConsumerReconciler_CriticalLagTest(t *testing.T) {
	name, namespace := "ConsumerReconciler_Reconcile", "Test"
	testCases := []struct {
		name                  string
		timePassed            time.Duration
		promResponse          *fakeMetrics
		expDeployAnnotation   map[string]map[string]string
		expContainerResources map[string]corev1.ResourceRequirements
		expContainerEnv       map[string]map[string]string
		expConsumerState      konsumeratorv1.ConsumerStatus
	}{
		{
			name:                "should have one missing deployment on consumer creation",
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "1", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(1),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(0),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "should have a single running deployment after the first reconcile",
			promResponse:        promMetricsForLag(0),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "1", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(0),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "lag detected, should set pending scale up status for the lagging deployment",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(6),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusPendingScaleUp),
			expContainerEnv:     containerEnv("busybox", "0", "1", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("100m", "100M", "100m", "100M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "should scale the deployment",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(6),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1100m", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "lag still present, pending scale up again",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(10),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusPendingScaleUp),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("1100m", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
		{
			name:                "lag reached `criticalLag` value, allocate maximum resources",
			timePassed:          time.Minute * 10,
			promResponse:        promMetricsForLag(15),
			expDeployAnnotation: deployAnnotation(name, controllers.InstanceStatusRunning),
			expContainerEnv:     containerEnv("busybox", "0", "2", "0", "1", "1"),
			expContainerResources: map[string]corev1.ResourceRequirements{
				"busybox": *tests.NewResourceRequirements("2", "400M", "2", "400M"),
			},
			expConsumerState: konsumeratorv1.ConsumerStatus{
				Expected:  helpers.Ptr2Int32(1),
				Lagging:   helpers.Ptr2Int32(1),
				Missing:   helpers.Ptr2Int32(0),
				Outdated:  helpers.Ptr2Int32(0),
				Running:   helpers.Ptr2Int32(1),
				Redundant: helpers.Ptr2Int32(0),
				Paused:    helpers.Ptr2Int32(0),
			},
		},
	}
	promServer := newFakePromServer(t)
	c := newConsumer(name, namespace)
	c.Spec.Autoscaler.Prometheus.CriticalLag = &metav1.Duration{Duration: time.Minute * 15}
	c.Spec.DeploymentTemplate.Template = corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "busybox", Image: "busybox-image"},
			},
		},
	}
	c.Spec.ResourcePolicy = &konsumeratorv1.ResourcePolicy{
		ContainerPolicies: []konsumeratorv1.ContainerResourcePolicy{
			tests.NewContainerResourcePolicy("busybox", "100m", "100M", "2", "400M"),
		},
	}
	c.Spec.Autoscaler.Prometheus.Address = []string{promServer.URL}
	tr := newTestReconciler(t, c)

	for step, tc := range testCases {
		t.Run(fmt.Sprintf("step_%d", step+1), func(t *testing.T) {
			t.Log(tc.name)
			fakeClock.Step(tc.timePassed)
			if tc.promResponse != nil {
				promServer.setResponse(tc.promResponse)
				tc.expConsumerState.LastSyncState = map[string]konsumeratorv1.InstanceState{
					"0": {
						ProductionRate:  tc.promResponse.production,
						ConsumptionRate: tc.promResponse.consumption,
						MessagesBehind:  tc.promResponse.offset,
					},
				}
			}
			tr.mustReconcile()
			if err := tr.equalConsumerStatus(tc.expConsumerState); err != nil {
				t.Logf("%s", err)
				t.Fail()
			}
			deployments := tr.fetchDeployments()
			for _, deployment := range deployments {
				if err := deployment.diffAnnotation(tc.expDeployAnnotation); err != nil {
					t.Logf("annotation diff err: %s", err)
					t.Fail()
				}
				if err := deployment.diffEnv(tc.expContainerEnv); err != nil {
					t.Logf("environment diff err: %s", err)
					t.Fail()
				}
				if err := deployment.diffResources(tc.expContainerResources); err != nil {
					t.Logf("resources diff err: %s", err)
					t.Fail()
				}
			}
		})
	}
}

func newConsumer(name, namespace string) *konsumeratorv1.Consumer {
	return &konsumeratorv1.Consumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: konsumeratorv1.ConsumerSpec{
			NumPartitions: helpers.Ptr2Int32(1),
			Name:          name,
			Namespace:     namespace,
			Autoscaler: &konsumeratorv1.AutoscalerSpec{
				Mode:                     konsumeratorv1.AutoscalerTypePrometheus,
				PendingScaleUpDuration:   &metav1.Duration{Duration: 5 * time.Minute},
				PendingScaleDownDuration: &metav1.Duration{Duration: 5 * time.Minute},
				Prometheus: &konsumeratorv1.PrometheusAutoscalerSpec{
					TolerableLag:  &metav1.Duration{Duration: 3 * time.Minute},
					MinSyncPeriod: &metav1.Duration{Duration: time.Minute},
					RecoveryTime:  &metav1.Duration{Duration: 60 * time.Minute},
					Offset: konsumeratorv1.OffsetQuerySpec{
						Query:          "offset",
						PartitionLabel: "partition",
					},
					Production: konsumeratorv1.ProductionQuerySpec{
						Query:          "production",
						PartitionLabel: "partition",
					},
					Consumption: konsumeratorv1.ConsumptionQuerySpec{
						Query:          "consumption",
						PartitionLabel: "partition",
					},
					RatePerCore: helpers.Ptr2Int64(5000),
					RamPerCore:  resource.MustParse("200M"),
				},
			},
			DeploymentTemplate: appsv1.DeploymentSpec{
				Replicas: helpers.Ptr2Int32(1),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"key": "value"}, MatchExpressions: nil},
			},
		}}
}

type testReconciler struct {
	t               *testing.T
	client          client.Client
	cr              *controllers.ConsumerReconciler
	name, namespace string
	deployments     []*testDeployment
}

func newTestReconciler(t *testing.T, c *konsumeratorv1.Consumer) *testReconciler {
	cl, cr := initReconciler(c)
	return &testReconciler{
		t:         t,
		client:    cl,
		cr:        cr,
		name:      c.Spec.Name,
		namespace: c.Spec.Namespace,
	}
}

func (tr *testReconciler) newRequest() reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      tr.name,
			Namespace: tr.namespace,
		},
	}
}
func (tr *testReconciler) mustReconcile() {
	if _, err := tr.cr.Reconcile(context.Background(), tr.newRequest()); err != nil {
		tr.t.Fatal(err)
	}
}

func (tr *testReconciler) fetchConsumer() konsumeratorv1.Consumer {
	var consumer konsumeratorv1.Consumer
	req := tr.newRequest()
	if err := tr.client.Get(context.TODO(), req.NamespacedName, &consumer); err != nil {
		tr.t.Fatalf("%s", err)
	}
	return consumer
}

func (tr *testReconciler) equalConsumerStatus(expected konsumeratorv1.ConsumerStatus) error {
	consumer := tr.fetchConsumer()
	// nullify dynamic status values
	consumerStatus := purgeStatus(consumer.Status)
	expectedStatus := purgeStatus(expected)
	if diff := cmp.Diff(consumerStatus, expectedStatus); diff != "" {
		return fmt.Errorf("status mismatch:\n%s", diff)
	}
	return nil
}

func purgeStatus(cs konsumeratorv1.ConsumerStatus) *konsumeratorv1.ConsumerStatus {
	s := cs.DeepCopy()
	s.ObservedGeneration = nil
	s.LastSyncTime = nil
	return s
}

func (tr *testReconciler) fetchDeployments() []*testDeployment {
	deployments := &appsv1.DeploymentList{}
	err := tr.client.List(context.TODO(), deployments, client.InNamespace(tr.namespace))
	if err != nil {
		tr.t.Fatalf("get deployments err: %v", err)
	}
	td := make([]*testDeployment, len(deployments.Items))
	for i, d := range deployments.Items {
		d.Status.Replicas = 1
		td[i] = &testDeployment{Deployment: &d}
	}
	return td
}

type testDeployment struct {
	*appsv1.Deployment
}

func (td *testDeployment) diffResources(a map[string]corev1.ResourceRequirements) error {
	for _, container := range td.Spec.Template.Spec.Containers {
		expectedResources := a[container.Name]
		if helpers.CmpResourceRequirements(container.Resources, expectedResources) != 0 {
			want := helpers.PrettyPrintResources(&expectedResources)
			got := helpers.PrettyPrintResources(&container.Resources)
			return fmt.Errorf("container resource mismatch: \nwant \n%v, \ngot \n%v", want, got)
		}
	}
	return nil
}

func (td *testDeployment) diffEnv(a map[string]map[string]string) error {
	for _, container := range td.Spec.Template.Spec.Containers {
		env := make(map[string]string)
		for i := range container.Env {
			env[container.Env[i].Name] = container.Env[i].Value
		}
		expectedEnv := a[container.Name]
		if diff := cmp.Diff(expectedEnv, env); diff != "" {
			return fmt.Errorf("environment mismatch (-expectedEnv +container.Env):\n%s", diff)
		}
	}
	return nil
}

func (td *testDeployment) diffAnnotation(a map[string]map[string]string) error {
	for eKey, eValue := range a[td.Name] {
		value := td.Annotations[eKey]
		if value != eValue {
			return fmt.Errorf("expected to have annotation %s set to %s, but got %s", eKey, eValue, value)
		}
	}
	return nil
}

type fakePromServer struct {
	t      *testing.T
	metric *fakeMetrics
	*httptest.Server
}

func newFakePromServer(t *testing.T) *fakePromServer {
	fs := &fakePromServer{t: t}
	fs.Server = httptest.NewServer(http.HandlerFunc(fs.handler))
	return fs
}

func (fs *fakePromServer) setResponse(fm *fakeMetrics) {
	fs.metric = fm
}

func (fs *fakePromServer) write(w io.Writer, value int64) {
	_, err := fmt.Fprintf(w, vectorResponseFormat, value)
	if err != nil {
		fs.t.Fatalf("err while writing response: %s", err)
	}
}

const vectorResponseFormat = `{
   "status": "success",
   "data": {
      "resultType": "vector",
      "result": [{
            "metric": {
               "partition": "0"
            },
            "value": [1435781451.781, "%d"]
         }]
   }
}`

func (fs *fakePromServer) handler(rw http.ResponseWriter, req *http.Request) {
	if fs.metric == nil {
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	if req.Method != http.MethodPost {
		fs.t.Fatalf("expected to receive POST request; got %q", req.Method)
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		fs.t.Fatalf("error while reading body: %s", err)
	}
	params, err := url.ParseQuery(string(b))
	if err != nil {
		fs.t.Fatalf("error while parsing params: %s", err)
	}
	q := params.Get("query")
	switch q {
	case "offset":
		fs.write(rw, fs.metric.offset)
	case "production":
		fs.write(rw, fs.metric.production)
	case "consumption":
		fs.write(rw, fs.metric.consumption)
	default:
		fs.t.Logf("prometheus: unexpected query: %q", q)
	}
}

type fakeMetrics struct {
	offset      int64
	production  int64
	consumption int64
}

func containerEnv(name, partition, gomaxprocs, instance, numP, numI string) map[string]map[string]string {
	return map[string]map[string]string{
		name: {
			"KONSUMERATOR_PARTITION":      partition,
			"KONSUMERATOR_INSTANCE":       instance,
			"KONSUMERATOR_NUM_INSTANCES":  numI,
			"KONSUMERATOR_NUM_PARTITIONS": numP,
			"GOMAXPROCS":                  gomaxprocs,
		},
	}
}

func deployAnnotation(name, scalingStatus string) map[string]map[string]string {
	return map[string]map[string]string{
		fmt.Sprintf("%s-0", name): {
			controllers.PartitionAnnotation:     "0",
			controllers.ScalingStatusAnnotation: scalingStatus,
		},
	}
}

func deployAnnotationSaturated(name, saturation string) map[string]map[string]string {
	a := deployAnnotation(name, controllers.InstanceStatusSaturated)
	a[fmt.Sprintf("%s-0", name)][controllers.CPUSaturationLevel] = saturation
	return a
}
