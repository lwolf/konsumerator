package controllers

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const namespace = "konsumerator"

var (
	reconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "consumer",
			Name:      "reconcile_total",
			Help:      "Total number of processed reconcile events",
		},
		[]string{"consumer"},
	)
	reconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "consumer",
			Name:      "reconcile_errors_total",
			Help:      "Total number of errors while processing reconcile events",
		},
		[]string{"consumer"},
	)
	reconcileDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Subsystem:  "consumer",
			Name:       "reconcile_duration_seconds",
			Help:       "Reconcile event duration",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"consumer"},
	)
	deploymentsCreateTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "consumer",
			Name:      "deployments_create_total",
			Help:      "Total number of created deployments",
		},
		[]string{"consumer"},
	)
	deploymentsCreateErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "consumer",
			Name:      "deployments_create_errors",
			Help:      "Total number of errors while creating deployment",
		},
		[]string{"consumer"},
	)
	deploymentsDeleteTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "consumer",
			Name:      "deployments_delete_total",
			Help:      "Total number of deleted deployments",
		},
		[]string{"consumer"},
	)
	deploymentsDeleteErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "consumer",
			Name:      "deployments_delete_errors",
			Help:      "Total number of errors while deleting deployment",
		},
		[]string{"consumer"},
	)
	deploymentsUpdateTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "consumer",
			Name:      "deployments_update_total",
			Help:      "Total number of updated deployments",
		},
		[]string{"consumer"},
	)
	deploymentsUpdateErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "consumer",
			Name:      "deployments_update_errors",
			Help:      "Total number of errors while updating deployments",
		},
		[]string{"consumer"},
	)
	statusUpdateDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Subsystem:  "consumer",
			Name:       "status_update_duration_seconds",
			Help:       "Status update duration",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"consumer"},
	)
	consumerStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "consumer",
			Name:      "status",
			Help:      "Consumer deployments status",
		},
		[]string{"consumer", "type"},
	)
	deploymentSaturation = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "deployment",
			Name:      "saturation",
			Help:      "Current cpu saturation for deployment",
		},
		[]string{"consumer", "deployment"},
	)
	deploymentStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "deployment",
			Name:      "status",
			Help: "Current deployment status. " +
				"0 - RUNNING, 1 - SATURATED, 2 - PENDING_SCALE_UP, 3 - PENDING_SCALE_DOWN, -1 - UNKNOWN",
		},
		[]string{"consumer", "deployment"},
	)
)

func init() {
	metrics.Registry.MustRegister(reconcileTotal, reconcileErrors,
		reconcileDuration, statusUpdateDuration, consumerStatus,
		deploymentsCreateTotal, deploymentsCreateErrors,
		deploymentsDeleteTotal, deploymentsDeleteErrors,
		deploymentsUpdateTotal, deploymentsUpdateErrors,
		deploymentSaturation, deploymentStatus)
}
