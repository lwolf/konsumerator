package controllers

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const namespace = "konsumerator"

var (
	reconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "reconcile_total",
			Namespace: namespace,
			Help:      "Total number of processed reconcile events",
		},
		[]string{"name"},
	)
	reconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "reconcile_errors_total",
			Namespace: namespace,
			Help:      "Total number of errors while processing reconcile events",
		},
		[]string{"name"},
	)
	reconcileDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "reconcile_duration_seconds",
			Namespace:  namespace,
			Help:       "Reconcile event duration",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"name"},
	)
	statusUpdateDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "reconcile_status_update_duration_seconds",
			Namespace:  namespace,
			Help:       "Reconcile status update duration",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"name"},
	)
	deploysCreateTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_create_total",
			Namespace: namespace,
			Help:      "Total number of created deploys",
		},
		[]string{"name"},
	)
	deploysCreateErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_create_errors",
			Namespace: namespace,
			Help:      "Total number of errors while creating deploys",
		},
		[]string{"name"},
	)
	deploysDeleteTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_delete_total",
			Namespace: namespace,
			Help:      "Total number of deleted deploys",
		},
		[]string{"name"},
	)
	deploysDeleteErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_delete_errors",
			Namespace: namespace,
			Help:      "Total number of errors while deleting deploys",
		},
		[]string{"name"},
	)
	deploysUpdateTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_update_total",
			Namespace: namespace,
			Help:      "Total number of updated deploys",
		},
		[]string{"name"},
	)
	deploysUpdateErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_update_errors",
			Namespace: namespace,
			Help:      "Total number of errors while updating deploys",
		},
		[]string{"name"},
	)
)

func init() {
	metrics.Registry.MustRegister(reconcileTotal, reconcileErrors,
		reconcileDuration, statusUpdateDuration,
		deploysCreateTotal, deploysCreateErrors,
		deploysDeleteTotal, deploysDeleteErrors,
		deploysUpdateTotal, deploysUpdateErrors)
}
