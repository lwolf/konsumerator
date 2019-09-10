package controllers

import (
	v1 "k8s.io/api/apps/v1"
	"testing"

	tlog "github.com/go-logr/logr/testing"
	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
