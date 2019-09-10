package controllers

import "github.com/prometheus/client_golang/prometheus"

const namespace = "konsumerator"

var (
	reconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "reconcile_total",
			Namespace: namespace,
			Help:      "Total number of processed reconcile events",
		},
		[]string{"namespace"},
	)
	reconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "reconcile_errors_total",
			Namespace: namespace,
			Help:      "Total number of errors while processing reconcile events",
		},
		[]string{"namespace"},
	)
	reconcileDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "reconcile_duration_seconds",
			Namespace:  namespace,
			Help:       "Reconcile event duration",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"namespace"},
	)
	statusUpdateDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "reconcile_status_update_duration_seconds",
			Namespace:  namespace,
			Help:       "Reconcile status update duration",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"namespace"},
	)
	deploysCreateTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_create_total",
			Namespace: namespace,
			Help:      "Total number of created deploys",
		},
		[]string{"namespace"},
	)
	deploysCreateErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_create_errors",
			Namespace: namespace,
			Help:      "Total number of errors while creating deploys",
		},
		[]string{"namespace"},
	)
	deploysDeleteTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_delete_total",
			Namespace: namespace,
			Help:      "Total number of deleted deploys",
		},
		[]string{"namespace"},
	)
	deploysDeleteErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_delete_errors",
			Namespace: namespace,
			Help:      "Total number of errors while deleting deploys",
		},
		[]string{"namespace"},
	)
	deploysUpdateTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_update_total",
			Namespace: namespace,
			Help:      "Total number of updated deploys",
		},
		[]string{"namespace"},
	)
	deploysUpdateErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "deploys_update_errors",
			Namespace: namespace,
			Help:      "Total number of errors while updating deploys",
		},
		[]string{"namespace"},
	)
)

func init() {
	prometheus.MustRegister(reconcileTotal, reconcileErrors,
		reconcileDuration, statusUpdateDuration,
		deploysCreateTotal, deploysCreateErrors,
		deploysDeleteTotal, deploysDeleteErrors,
		deploysUpdateTotal, deploysUpdateErrors)
}
