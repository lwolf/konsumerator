package controllers

import (
	"github.com/go-logr/logr"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
)

func TestNewConsumerOperator(t *testing.T) {
	testCases := []struct {
		name           string
		consumer       *konsumeratorv1.Consumer
		deploys        appsv1.DeploymentList
		expectedStatus konsumeratorv1.ConsumerStatus
	}{
		{
			"empty deployments step 1",
			&konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions: testInt32ToPt(10),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "",
						Prometheus: &konsumeratorv1.PrometheusAutoscalerSpec{},
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
			},
			appsv1.DeploymentList{},
			konsumeratorv1.ConsumerStatus{
				Expected:  testInt32ToPt(10),
				Running:   testInt32ToPt(0),
				Paused:    testInt32ToPt(0),
				Lagging:   testInt32ToPt(0),
				Missing:   testInt32ToPt(10),
				Outdated:  testInt32ToPt(0),
				Redundant: testInt32ToPt(0),
			},
		},
		{
			"empty deployments with group of 2",
			&konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions:            testInt32ToPt(10),
					NumPartitionsPerInstance: testInt32ToPt(2),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "",
						Prometheus: &konsumeratorv1.PrometheusAutoscalerSpec{},
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
			},
			appsv1.DeploymentList{},
			konsumeratorv1.ConsumerStatus{
				Expected:  testInt32ToPt(5),
				Running:   testInt32ToPt(0),
				Paused:    testInt32ToPt(0),
				Lagging:   testInt32ToPt(0),
				Missing:   testInt32ToPt(5),
				Outdated:  testInt32ToPt(0),
				Redundant: testInt32ToPt(0),
			},
		},
		{
			"empty deployments step 2",
			&konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions: testInt32ToPt(10),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "",
						Prometheus: &konsumeratorv1.PrometheusAutoscalerSpec{},
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
			},
			appsv1.DeploymentList{
				Items: []appsv1.Deployment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								PartitionAnnotation: "1",
								ConsumerAnnotation:  "1",
							},
						},
					},
				},
			},
			konsumeratorv1.ConsumerStatus{
				Expected:  testInt32ToPt(10),
				Running:   testInt32ToPt(1),
				Paused:    testInt32ToPt(0),
				Lagging:   testInt32ToPt(0),
				Missing:   testInt32ToPt(9),
				Outdated:  testInt32ToPt(1),
				Redundant: testInt32ToPt(0),
			},
		},
		{
			"2 paused deployments",
			&konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions: testInt32ToPt(10),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "",
						Prometheus: &konsumeratorv1.PrometheusAutoscalerSpec{},
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
			},
			appsv1.DeploymentList{
				Items: []appsv1.Deployment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								PartitionAnnotation:         "6",
								ConsumerAnnotation:          "6",
								DisableAutoscalerAnnotation: "true",
							},
						},
						Status: appsv1.DeploymentStatus{Replicas: 1},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								PartitionAnnotation:         "5",
								ConsumerAnnotation:          "5",
								DisableAutoscalerAnnotation: "true",
							},
						},
						Status: appsv1.DeploymentStatus{Replicas: 1},
					},
				},
			},
			konsumeratorv1.ConsumerStatus{
				Expected:  testInt32ToPt(10),
				Running:   testInt32ToPt(0),
				Paused:    testInt32ToPt(2),
				Lagging:   testInt32ToPt(0),
				Missing:   testInt32ToPt(8),
				Outdated:  testInt32ToPt(2),
				Redundant: testInt32ToPt(0),
			},
		},
		{
			"deployment should be recreated if `NumPartitionsPerInstance` change",
			&konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions:            testInt32ToPt(10),
					NumPartitionsPerInstance: testInt32ToPt(3),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "",
						Prometheus: &konsumeratorv1.PrometheusAutoscalerSpec{},
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
			},
			appsv1.DeploymentList{
				Items: []appsv1.Deployment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								PartitionAnnotation: "0",
								ConsumerAnnotation:  "0",
							},
						},
						Status: appsv1.DeploymentStatus{Replicas: 1},
					},
				},
			},
			konsumeratorv1.ConsumerStatus{
				Expected:  testInt32ToPt(4),
				Running:   testInt32ToPt(0),
				Paused:    testInt32ToPt(0),
				Lagging:   testInt32ToPt(0),
				Missing:   testInt32ToPt(3),
				Outdated:  testInt32ToPt(0),
				Redundant: testInt32ToPt(1),
			},
		},
		{
			"deployment with instanceId outside of range should be deleted",
			&konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions:            testInt32ToPt(10),
					NumPartitionsPerInstance: testInt32ToPt(3),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "",
						Prometheus: &konsumeratorv1.PrometheusAutoscalerSpec{},
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
			},
			appsv1.DeploymentList{
				Items: []appsv1.Deployment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								PartitionAnnotation: "10",
								ConsumerAnnotation:  "10",
							},
						},
						Status: appsv1.DeploymentStatus{Replicas: 1},
					},
				},
			},
			konsumeratorv1.ConsumerStatus{
				Expected:  testInt32ToPt(4),
				Running:   testInt32ToPt(0),
				Paused:    testInt32ToPt(0),
				Lagging:   testInt32ToPt(0),
				Missing:   testInt32ToPt(4),
				Outdated:  testInt32ToPt(0),
				Redundant: testInt32ToPt(1),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := &operator{
				clock: clock.RealClock{},
				log:   logr.Discard(),
			}
			err := o.init(tc.consumer, tc.deploys)
			if err != nil {
				t.Fatalf("unexpected err: %s", err)
			}
			testCompareStatus(t, tc.expectedStatus, o.consumer.Status)
		})
	}
}

func testInt32ToPt(v int32) *int32 {
	return &v
}

func testCompareStatus(t *testing.T, a, b konsumeratorv1.ConsumerStatus) {
	t.Helper()
	equalInt32 := func(field string, a, b int32) {
		if a != b {
			t.Fatalf("status.%s %d!=%d", field, a, b)
		}
	}
	equalInt32("Expected", *a.Expected, *b.Expected)
	equalInt32("Running", *a.Running, *b.Running)
	equalInt32("Paused", *a.Paused, *b.Paused)
	equalInt32("Lagging", *a.Lagging, *b.Lagging)
	equalInt32("Missing", *a.Missing, *b.Missing)
	equalInt32("Outdated", *a.Outdated, *b.Outdated)
	equalInt32("Redundant", *a.Redundant, *b.Redundant)
}

func newPrometheusAutoscalerSpec(minSyncPeriod time.Duration) *konsumeratorv1.PrometheusAutoscalerSpec {
	return &konsumeratorv1.PrometheusAutoscalerSpec{
		Address:       nil,
		MinSyncPeriod: &metav1.Duration{Duration: minSyncPeriod},
		Offset:        konsumeratorv1.OffsetQuerySpec{},
		Production:    konsumeratorv1.ProductionQuerySpec{},
		Consumption:   konsumeratorv1.ConsumptionQuerySpec{},
		RatePerCore:   nil,
		RamPerCore:    resource.Quantity{},
		TolerableLag:  nil,
		CriticalLag:   nil,
		RecoveryTime:  nil,
	}
}

func TestShouldUpdateMetrics(t *testing.T) {
	tests := map[string]struct {
		consumer  *konsumeratorv1.Consumer
		expResult bool
		expError  bool
	}{
		"should update if time to sync": {
			consumer: &konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions: testInt32ToPt(1),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "prometheus",
						Prometheus: newPrometheusAutoscalerSpec(time.Duration(5 * time.Minute)),
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
				Status: konsumeratorv1.ConsumerStatus{
					LastSyncTime:  &metav1.Time{Time: time.Now().Add(time.Minute * -6)},
					LastSyncState: map[string]konsumeratorv1.InstanceState{},
				},
			},
			expResult: true,
			expError:  false,
		},
		"false if not the time to sync": {
			consumer: &konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions: testInt32ToPt(1),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "prometheus",
						Prometheus: newPrometheusAutoscalerSpec(5 * time.Minute),
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
				Status: konsumeratorv1.ConsumerStatus{
					LastSyncTime:  &metav1.Time{Time: time.Now().Add(time.Minute * -1)},
					LastSyncState: map[string]konsumeratorv1.InstanceState{},
				},
			},
			expResult: false,
			expError:  false,
		},
		"should update if no LastSyncTime": {
			consumer: &konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions: testInt32ToPt(1),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "prometheus",
						Prometheus: newPrometheusAutoscalerSpec(5 * time.Minute),
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
				Status: konsumeratorv1.ConsumerStatus{
					LastSyncTime:  nil,
					LastSyncState: map[string]konsumeratorv1.InstanceState{},
				},
			},
			expResult: true,
			expError:  false,
		},
		"should update if no LastSyncState ": {
			consumer: &konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions: testInt32ToPt(1),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "prometheus",
						Prometheus: newPrometheusAutoscalerSpec(5 * time.Minute),
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
				Status: konsumeratorv1.ConsumerStatus{
					LastSyncTime:  &metav1.Time{Time: time.Now().Add(time.Minute * -1)},
					LastSyncState: nil,
				},
			},
			expResult: true,
			expError:  false,
		},
		"should fail if no autoscaler setup": {
			consumer: &konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions:      testInt32ToPt(1),
					Autoscaler:         nil,
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
				Status: konsumeratorv1.ConsumerStatus{
					LastSyncTime:  &metav1.Time{Time: time.Now().Add(time.Minute * -6)},
					LastSyncState: map[string]konsumeratorv1.InstanceState{},
				},
			},
			expResult: false,
			expError:  true,
		},
		"should fail if prometheus config is missing": {
			consumer: &konsumeratorv1.Consumer{
				Spec: konsumeratorv1.ConsumerSpec{
					NumPartitions: testInt32ToPt(1),
					Autoscaler: &konsumeratorv1.AutoscalerSpec{
						Mode:       "prometheus",
						Prometheus: nil,
					},
					DeploymentTemplate: appsv1.DeploymentSpec{},
				},
				Status: konsumeratorv1.ConsumerStatus{
					LastSyncTime:  &metav1.Time{Time: time.Now().Add(time.Minute * -6)},
					LastSyncState: map[string]konsumeratorv1.InstanceState{},
				},
			},
			expResult: false,
			expError:  true,
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			res, err := shouldUpdateMetrics(tt.consumer, time.Now())
			if res != tt.expResult {
				t.Fatalf("expected %v, got %v", tt.expResult, res)
			}
			if (err != nil) != tt.expError {
				t.Fatalf("Error check, expected %v, got %v", tt.expError, err != nil)
			}
		})
	}
}

func TestDeployIsPaused(t *testing.T) {
	testCases := map[string]struct {
		replicas    int32
		annotations map[string]string
		want        bool
	}{
		// "status is paused if scaled to 0": {
		// 	replicas:    0,
		// 	annotations: map[string]string{"key": ":value"},
		// 	want:        true,
		// },
		"status is paused if annotation is set to something": {
			replicas:    1,
			annotations: map[string]string{DisableAutoscalerAnnotation: "true"},
			want:        true,
		},
		"status is paused if annotation is set to empty string": {
			replicas:    1,
			annotations: map[string]string{DisableAutoscalerAnnotation: ""},
			want:        true,
		},
		"status not paused if no scaling annotation preset": {
			replicas:    1,
			annotations: map[string]string{"key": "value"},
			want:        false,
		},
		"status not paused if annotations are missing": {
			replicas:    1,
			annotations: nil,
			want:        false,
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			deploy := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tc.annotations,
					Name:        "test",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &tc.replicas,
				},
				Status: appsv1.DeploymentStatus{
					Replicas: tc.replicas,
				},
			}
			got := deployIsPaused(deploy)
			if got != tc.want {
				t.Fatalf("expected %v, but got %v", tc.want, got)
			}
		})
	}
}
