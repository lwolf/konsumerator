package controllers

import (
	v1 "k8s.io/api/apps/v1"
	"testing"
	"time"

	tlog "github.com/go-logr/logr/testing"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

func TestEstimateResources(t *testing.T) {

}

func TestNewConsumerOperator(t *testing.T) {
	testCases := []struct {
		name           string
		consumer       *konsumeratorv1alpha1.Consumer
		deploys        v1.DeploymentList
		expectedStatus konsumeratorv1alpha1.ConsumerStatus
	}{
		{
			"empty deployments",
			&konsumeratorv1alpha1.Consumer{
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions:      testInt32ToPt(10),
					Autoscaler:         nil,
					DeploymentTemplate: v1.DeploymentSpec{},
				},
			},
			v1.DeploymentList{},
			konsumeratorv1alpha1.ConsumerStatus{
				Expected: testInt32ToPt(10),
				Running:  testInt32ToPt(0),
				Paused:   testInt32ToPt(0),
				Lagging:  testInt32ToPt(0),
				Missing:  testInt32ToPt(10),
				Outdated: testInt32ToPt(0),
			},
		},
		{
			"empty deployments",
			&konsumeratorv1alpha1.Consumer{
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions: testInt32ToPt(10),
					Autoscaler: &konsumeratorv1alpha1.AutoscalerSpec{
						Mode:       "",
						Prometheus: &konsumeratorv1alpha1.PrometheusAutoscalerSpec{},
					},
					DeploymentTemplate: v1.DeploymentSpec{},
				},
			},
			v1.DeploymentList{
				Items: []v1.Deployment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								partitionAnnotation: "1",
							},
						},
					},
				},
			},
			konsumeratorv1alpha1.ConsumerStatus{
				Expected: testInt32ToPt(10),
				Running:  testInt32ToPt(0),
				Paused:   testInt32ToPt(1),
				Lagging:  testInt32ToPt(0),
				Missing:  testInt32ToPt(9),
				Outdated: testInt32ToPt(1),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			co, err := newConsumerOperator(tlog.NullLogger{}, tc.consumer, tc.deploys)
			if err != nil {
				t.Fatalf("unexpected err: %s", err)
			}
			testCompareStatus(t, tc.expectedStatus, co.consumer.Status)
		})
	}
}

func testInt32ToPt(v int32) *int32 {
	return &v
}

func testCompareStatus(t *testing.T, a, b konsumeratorv1alpha1.ConsumerStatus) {
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
}

func newPrometheusAutoscalerSpec(minSyncPeriod time.Duration) *konsumeratorv1alpha1.PrometheusAutoscalerSpec {
	return &konsumeratorv1alpha1.PrometheusAutoscalerSpec{
		Address:       nil,
		MinSyncPeriod: &metav1.Duration{Duration: minSyncPeriod},
		Offset:        konsumeratorv1alpha1.OffsetQuerySpec{},
		Production:    konsumeratorv1alpha1.ProductionQuerySpec{},
		Consumption:   konsumeratorv1alpha1.ConsumptionQuerySpec{},
		RatePerCore:   nil,
		RamPerCore:    resource.Quantity{},
		TolerableLag:  nil,
		CriticalLag:   nil,
		RecoveryTime:  nil,
	}
}

func TestShouldUpdateMetrics(t *testing.T) {
	tests := map[string]struct {
		consumer  *konsumeratorv1alpha1.Consumer
		expResult bool
		expError  bool
	}{
		"should update if time to sync": {
			consumer: &konsumeratorv1alpha1.Consumer{
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions: testInt32ToPt(1),
					Autoscaler: &konsumeratorv1alpha1.AutoscalerSpec{
						Mode:       "prometheus",
						Prometheus: newPrometheusAutoscalerSpec(time.Duration(5 * time.Minute)),
					},
					DeploymentTemplate: v1.DeploymentSpec{},
				},
				Status: konsumeratorv1alpha1.ConsumerStatus{
					LastSyncTime:  &metav1.Time{Time: time.Now().Add(time.Minute * -6)},
					LastSyncState: map[string]konsumeratorv1alpha1.InstanceState{},
				},
			},
			expResult: true,
			expError:  false,
		},
		"false if not the time to sync": {
			consumer: &konsumeratorv1alpha1.Consumer{
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions: testInt32ToPt(1),
					Autoscaler: &konsumeratorv1alpha1.AutoscalerSpec{
						Mode:       "prometheus",
						Prometheus: newPrometheusAutoscalerSpec(time.Duration(5 * time.Minute)),
					},
					DeploymentTemplate: v1.DeploymentSpec{},
				},
				Status: konsumeratorv1alpha1.ConsumerStatus{
					LastSyncTime:  &metav1.Time{Time: time.Now().Add(time.Minute * -1)},
					LastSyncState: map[string]konsumeratorv1alpha1.InstanceState{},
				},
			},
			expResult: false,
			expError:  false,
		},
		"should update if no LastSyncTime": {
			consumer: &konsumeratorv1alpha1.Consumer{
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions: testInt32ToPt(1),
					Autoscaler: &konsumeratorv1alpha1.AutoscalerSpec{
						Mode:       "prometheus",
						Prometheus: newPrometheusAutoscalerSpec(time.Duration(5 * time.Minute)),
					},
					DeploymentTemplate: v1.DeploymentSpec{},
				},
				Status: konsumeratorv1alpha1.ConsumerStatus{
					LastSyncTime:  nil,
					LastSyncState: map[string]konsumeratorv1alpha1.InstanceState{},
				},
			},
			expResult: true,
			expError:  false,
		},
		"should update if no LastSyncState ": {
			consumer: &konsumeratorv1alpha1.Consumer{
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions: testInt32ToPt(1),
					Autoscaler: &konsumeratorv1alpha1.AutoscalerSpec{
						Mode:       "prometheus",
						Prometheus: newPrometheusAutoscalerSpec(time.Duration(5 * time.Minute)),
					},
					DeploymentTemplate: v1.DeploymentSpec{},
				},
				Status: konsumeratorv1alpha1.ConsumerStatus{
					LastSyncTime:  &metav1.Time{Time: time.Now().Add(time.Minute * -1)},
					LastSyncState: nil,
				},
			},
			expResult: true,
			expError:  false,
		},
		"should fail if not prometheus autoscaler": {
			consumer: &konsumeratorv1alpha1.Consumer{
				Spec: konsumeratorv1alpha1.ConsumerSpec{
					NumPartitions:      testInt32ToPt(1),
					Autoscaler:         nil,
					DeploymentTemplate: v1.DeploymentSpec{},
				},
				Status: konsumeratorv1alpha1.ConsumerStatus{
					LastSyncTime:  &metav1.Time{Time: time.Now().Add(time.Minute * -6)},
					LastSyncState: map[string]konsumeratorv1alpha1.InstanceState{},
				},
			},
			expResult: false,
			expError:  true,
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			res, err := shouldUpdateMetrics(tt.consumer)
			if res != tt.expResult {
				t.Fatalf("expected %v, got %v", tt.expResult, res)
			}
			if (err != nil) != tt.expError {
				t.Fatalf("Error check, expected %v, got %v", tt.expError, err != nil)
			}
		})
	}
}
