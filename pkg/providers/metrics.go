package providers

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const namespace = "konsumerator"
const subsystem = "prometheus_mp"

var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			Name:        "requests_total",
			Help:        "Total number of Prometheus provider requests",
			ConstLabels: nil,
		},
		[]string{"type"},
	)
	requestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			Name:        "requests_errors_total",
			Help:        "Total number of error requests to Prometheus provider",
			ConstLabels: nil,
		},
		[]string{"type"},
	)
	requestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Subsystem:  subsystem,
			Name:       "request_duration_seconds",
			Help:       "Prometheus provider request duration",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"type"},
	)
	subRequestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "sub_requests_total",
			Help:      "Total number of HTTP requests to Prometheus",
		},
		[]string{"addr"},
	)
	subRequestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "sub_requests_errors_total",
			Help:      "Total number of HTTP request errors to Prometheus",
		},
		[]string{"addr"},
	)
	subRequestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Subsystem:  subsystem,
			Name:       "sub_request_duration_seconds",
			Help:       "Prometheus HTTP request duration",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"type"},
	)
)

func initMetrics() {
	metrics.Registry.MustRegister(requestsTotal, requestErrors, requestDuration,
		subRequestTotal, subRequestErrors, subRequestDuration)
}
