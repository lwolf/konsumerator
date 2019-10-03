package controllers

import (
	"github.com/lwolf/konsumerator/pkg/helpers"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPopulateStatusFromAnnotation(t *testing.T) {
	tm, _ := time.Parse(helpers.TimeLayout, "2019-10-03T13:38:03Z")
	mtm := metav1.NewTime(tm)
	testCases := map[string]struct {
		in     map[string]string
		status *konsumeratorv1alpha1.ConsumerStatus
	}{
		"expect to get empty status": {
			in: map[string]string{},
			status: &konsumeratorv1alpha1.ConsumerStatus{
				Expected:      helpers.Ptr2Int32(0),
				Running:       helpers.Ptr2Int32(0),
				Paused:        helpers.Ptr2Int32(0),
				Lagging:       helpers.Ptr2Int32(0),
				Missing:       helpers.Ptr2Int32(0),
				Outdated:      helpers.Ptr2Int32(0),
				LastSyncTime:  nil,
				LastSyncState: nil,
			},
		},
		"expect to get status counters": {
			in: map[string]string{
				annotationStatusExpected: "6",
				annotationStatusRunning:  "1",
				annotationStatusPaused:   "2",
				annotationStatusLagging:  "3",
				annotationStatusMissing:  "4",
				annotationStatusOutdated: "5",
			},
			status: &konsumeratorv1alpha1.ConsumerStatus{
				Expected:      helpers.Ptr2Int32(6),
				Running:       helpers.Ptr2Int32(1),
				Paused:        helpers.Ptr2Int32(2),
				Lagging:       helpers.Ptr2Int32(3),
				Missing:       helpers.Ptr2Int32(4),
				Outdated:      helpers.Ptr2Int32(5),
				LastSyncTime:  nil,
				LastSyncState: nil,
			},
		},
		"expect to get lastsync timestamp and empty status": {
			in: map[string]string{
				annotationStatusExpected:     "6",
				annotationStatusRunning:      "1",
				annotationStatusPaused:       "2",
				annotationStatusLagging:      "3",
				annotationStatusMissing:      "4",
				annotationStatusOutdated:     "5",
				annotationStatusLastSyncTime: "2019-10-03T13:38:03Z",
				annotationStatusLastState:    "bad state",
			},
			status: &konsumeratorv1alpha1.ConsumerStatus{
				Expected:      helpers.Ptr2Int32(6),
				Running:       helpers.Ptr2Int32(1),
				Paused:        helpers.Ptr2Int32(2),
				Lagging:       helpers.Ptr2Int32(3),
				Missing:       helpers.Ptr2Int32(4),
				Outdated:      helpers.Ptr2Int32(5),
				LastSyncTime:  &mtm,
				LastSyncState: nil,
			},
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			status := &konsumeratorv1alpha1.ConsumerStatus{}
			PopulateStatusFromAnnotation(tc.in, status)
			if diff := cmp.Diff(status, tc.status); diff != "" {
				t.Fatalf("status mismatch (-status +tc.status):\n%s", diff)
			}
		})
	}
}

func TestUpdateStatusAnnotations(t *testing.T) {
	tm, _ := time.Parse(helpers.TimeLayout, "2019-10-03T13:38:03Z")
	mtm := metav1.NewTime(tm)
	testCases := map[string]struct {
		status    *konsumeratorv1alpha1.ConsumerStatus
		configMap *corev1.ConfigMap
		expErr    bool
	}{
		"sanity check": {
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationStatusExpected: "6",
						annotationStatusRunning:  "1",
						annotationStatusPaused:   "2",
						annotationStatusLagging:  "3",
						annotationStatusMissing:  "4",
						annotationStatusOutdated: "5",
					},
				},
			},
			status: &konsumeratorv1alpha1.ConsumerStatus{
				Expected:     helpers.Ptr2Int32(6),
				Running:      helpers.Ptr2Int32(1),
				Paused:       helpers.Ptr2Int32(2),
				Lagging:      helpers.Ptr2Int32(3),
				Missing:      helpers.Ptr2Int32(4),
				Outdated:     helpers.Ptr2Int32(5),
				LastSyncTime: &mtm,
				LastSyncState: map[string]konsumeratorv1alpha1.InstanceState{
					"0": konsumeratorv1alpha1.InstanceState{ProductionRate: 20, ConsumptionRate: 10, MessagesBehind: 1000},
				},
			},
		},
	}
	for testName, tc := range testCases {
		t.Run(testName, func(t *testing.T) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			}
			err := UpdateStatusAnnotations(cm, tc.status)
			if (tc.expErr && err == nil) || (!tc.expErr && err != nil) {
				t.Fatalf("error expectation failed, expected to get error=%v, got=%v", tc.expErr, err)
			}
		})
	}

}
