package providers

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

const promCallTimeout = time.Second * 30

type metricsMap map[int32]int64

type PrometheusMP struct {
	apis []*promAPI
	log  logr.Logger

	productionQuery           string
	productionPartitionLabel  model.LabelName
	consumptionQuery          string
	consumptionPartitionLabel model.LabelName
	offsetQuery               string
	offsetPartitionLabel      model.LabelName

	messagesBehind  metricsMap
	productionRate  metricsMap
	consumptionRate metricsMap
}

type promAPI struct {
	addr   string
	client promv1.API
}

var allConsFailedErr = errors.New("unable to reach any prometheus address")

var once sync.Once

// TODO: make spec passed by value
func NewPrometheusMP(log logr.Logger, spec *konsumeratorv1alpha1.PrometheusAutoscalerSpec) (*PrometheusMP, error) {
	once.Do(initMetrics)

	ctrlLogger := log.WithName("prometheusMP")
	var apis []*promAPI
	for _, addr := range spec.Address {
		c, err := api.NewClient(api.Config{Address: addr})
		if err != nil {
			log.Error(err, "unable to construct prometheus api", "address", addr)
			continue
		}
		apis = append(apis, &promAPI{
			addr:   addr,
			client: promv1.NewAPI(c),
		})
	}
	if len(apis) == 0 {
		return nil, allConsFailedErr
	}

	return &PrometheusMP{
		apis:                      apis,
		log:                       ctrlLogger,
		productionQuery:           spec.Production.Query,
		productionPartitionLabel:  model.LabelName(spec.Production.PartitionLabel),
		consumptionQuery:          spec.Consumption.Query,
		consumptionPartitionLabel: model.LabelName(spec.Consumption.PartitionLabel),
		offsetQuery:               spec.Offset.Query,
		offsetPartitionLabel:      model.LabelName(spec.Offset.PartitionLabel),
	}, nil
}

// GetProductionRate returns production rate.
// not thread-safe
func (l *PrometheusMP) GetProductionRate(partition int32) int64 {
	production, ok := l.productionRate[partition]
	if !ok {
		return 0
	}
	return production
}

// GetConsumptionRate returns consumption rate.
// not thread-safe
func (l *PrometheusMP) GetConsumptionRate(partition int32) int64 {
	consumption, ok := l.consumptionRate[partition]
	if !ok {
		l.log.V(1).Info("consumption value not found", "partition", partition)
		return 0
	}
	return consumption
}

// GetMessagesBehind returns how many messages we're behind.
// not thread-safe
func (l *PrometheusMP) GetMessagesBehind(partition int32) int64 {
	behind, ok := l.messagesBehind[partition]
	if !ok {
		l.log.V(1).Info("lag value not found", "partition", partition)
		return 0
	}
	l.log.V(1).Info("current lag", "partition", partition, "messagesBehind", behind)
	return behind
}

// GetLagByPartition calculates lag based on ProductionRate, ConsumptionRate and
// the number of not processed messages for partition
// not thread-safe
func (l *PrometheusMP) GetLagByPartition(partition int32) time.Duration {
	behind := l.GetMessagesBehind(partition)
	production := l.GetProductionRate(partition)
	l.log.V(1).Info("lag estimation", "production", production, "behind", behind)
	if production == 0 {
		l.log.V(1).Info("production rate is 0", "partition", partition)
		return 0
	}
	lag := float64(behind) / float64(production)
	l.log.V(1).Info("lag per partition", "partition", partition, "lag", lag, "lagSec", time.Duration(lag)*time.Second)
	return time.Duration(lag) * time.Second
}

// Update updates metrics values by querying Prometheus
// not thread-safe
// TODO: may lead to partial update
// TODO: might be queried in parallel
func (l *PrometheusMP) Update() error {
	var err error
	l.productionRate, err = l.queryProductionRate()
	if err != nil {
		return err
	}
	l.consumptionRate, err = l.queryConsumptionRate()
	if err != nil {
		return err
	}
	l.messagesBehind, err = l.queryOffset()
	if err != nil {
		return err
	}
	return nil
}

// Load loads given metrics into object
// not thread-safe
func (l *PrometheusMP) Load(production, consumption, offset map[int32]int64) {
	l.productionRate = production
	l.consumptionRate = consumption
	l.messagesBehind = offset
}

func (l *PrometheusMP) query(api *promAPI, ctx context.Context, query string) (model.Value, error) {
	subRequestTotal.WithLabelValues(api.addr).Inc()
	start := time.Now()
	value, warnings, err := api.client.Query(ctx, query, time.Now())
	if err != nil {
		if ctx.Err() != context.Canceled {
			subRequestErrors.WithLabelValues(api.addr).Inc()
		}
		return nil, err
	}
	// measure only success requests duration
	// to avoid context.Cancel duration values
	subRequestDuration.WithLabelValues(api.addr).Observe(time.Since(start).Seconds())
	for _, w := range warnings {
		l.log.Info("querying prometheus", "warning", w, "query", query)
	}
	return value, nil
}

func (l *PrometheusMP) queryAll(query string) model.Value {
	ctx, cancel := context.WithTimeout(context.Background(), promCallTimeout)
	defer cancel()

	valueCh := make(chan model.Value)
	doneCh := make(chan interface{})
	defer close(doneCh)

	var wg sync.WaitGroup
	for i := range l.apis {
		wg.Add(1)
		api := l.apis[i]
		go func() {
			defer wg.Done()
			v, err := l.query(api, ctx, query)
			if err != nil {
				if ctx.Err() != context.Canceled {
					l.log.Error(err, "failed to query prometheus")
				}
				return
			}
			select {
			case <-doneCh:
			case valueCh <- v:
			}
		}()
	}
	go func() {
		wg.Wait()
		close(valueCh)
	}()

	var v model.Value
	select {
	case v = <-valueCh:
	case <-ctx.Done():
		l.log.Error(nil, "all requests failed")
	}
	return v
}

func (l *PrometheusMP) queryOffset() (metricsMap, error) {
	start := time.Now()
	defer requestDuration.WithLabelValues("offset").Observe(time.Since(start).Seconds())
	requestsTotal.WithLabelValues("offset").Inc()

	value := l.queryAll(l.offsetQuery)
	if value == nil {
		requestErrors.WithLabelValues("offset").Inc()
		return nil, errors.New("failed to get offset metrics from prometheus")
	}
	metrics := l.parse(value, l.offsetPartitionLabel)
	return metrics, nil
}

// queryProductionRate queries Prometheus for the current production rate
func (l *PrometheusMP) queryProductionRate() (metricsMap, error) {
	start := time.Now()
	defer requestDuration.WithLabelValues("production_rate").Observe(time.Since(start).Seconds())
	requestsTotal.WithLabelValues("production_rate").Inc()

	value := l.queryAll(l.productionQuery)
	if value == nil {
		requestErrors.WithLabelValues("production_rate").Inc()
		return nil, errors.New("failed to get production metrics from prometheus")
	}
	metrics := l.parse(value, l.productionPartitionLabel)
	return metrics, nil
}

func (l *PrometheusMP) queryConsumptionRate() (metricsMap, error) {
	start := time.Now()
	defer requestDuration.WithLabelValues("consumption_rate").Observe(time.Since(start).Seconds())
	requestsTotal.WithLabelValues("consumption_rate").Inc()

	value := l.queryAll(l.consumptionQuery)
	if value == nil {
		requestErrors.WithLabelValues("consumption_rate").Inc()
		return nil, errors.New("failed to get consumption metrics from prometheus")
	}
	metrics := l.parse(value, l.consumptionPartitionLabel)
	return metrics, nil
}

func (l *PrometheusMP) parse(value model.Value, label model.LabelName) metricsMap {
	switch value.Type() {
	case model.ValVector:
		return l.parseVector(value.(model.Vector), label)
	case model.ValMatrix:
		return l.parseMatrix(value.(model.Matrix), label)
	default:
		l.log.Error(nil, "unsupported prometheus response", "type", value.Type())
		return nil
	}
}

func (l *PrometheusMP) parseMatrix(matrix model.Matrix, label model.LabelName) metricsMap {
	metrics := make(metricsMap)
	for _, ss := range matrix {
		partitionNumberStr := string(ss.Metric[label])
		partitionNumber, err := strconv.Atoi(partitionNumberStr)
		if err != nil {
			l.log.Info("unable to parse partition number from the label", "label", partitionNumberStr)
			continue
		}

		// grab the last value only
		sample := ss.Values[len(ss.Values)-1]
		metrics[int32(partitionNumber)] = int64(sample.Value)
	}
	return metrics
}

func (l *PrometheusMP) parseVector(vector model.Vector, label model.LabelName) metricsMap {
	metrics := make(metricsMap)
	for _, v := range vector {
		partitionNumberStr := string(v.Metric[label])
		partitionNumber, err := strconv.Atoi(partitionNumberStr)
		if err != nil {
			l.log.Info("unable to parse partition number from the label", "label", partitionNumberStr)
			continue
		}
		metrics[int32(partitionNumber)] = int64(v.Value)
	}
	return metrics
}
