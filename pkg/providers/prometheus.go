package providers

import (
	"context"
	"errors"
	"fmt"
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
	apis []promv1.API
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

var allConsFailedErr = errors.New("unable to reach any prometheus address")

// TODO: make spec passed by value
func NewPrometheusMP(log logr.Logger, spec *konsumeratorv1alpha1.PrometheusAutoscalerSpec) (*PrometheusMP, error) {
	ctrlLogger := log.WithName("prometheusMP")
	var apis []promv1.API
	for a := range spec.Address {
		c, err := api.NewClient(api.Config{Address: spec.Address[0]})
		if err != nil {
			log.Error(err, "unable to construct prometheus api", "address", spec.Address[a])
			continue
		}
		apis = append(apis, promv1.NewAPI(c))
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
		l.log.V(2).Info("consumption value not found", "partition", partition)
		return 0
	}
	return consumption
}

// GetMessagesBehind returns how many messages we're behind.
// not thread-safe
func (l *PrometheusMP) GetMessagesBehind(partition int32) int64 {
	behind, ok := l.messagesBehind[partition]
	if !ok {
		l.log.V(2).Info("lag value not found", "partition", partition)
		return 0
	}
	l.log.V(2).Info("current lag", "partition", partition, "messagesBehind", behind)
	return behind
}

// GetLagByPartition calculates lag based on ProductionRate, ConsumptionRate and
// the number of not processed messages for partition
// not thread-safe
func (l *PrometheusMP) GetLagByPartition(partition int32) time.Duration {
	behind := l.GetMessagesBehind(partition)
	production := l.GetProductionRate(partition)
	l.log.V(2).Info("lag estimation", "production", production, "behind", behind)
	if production == 0 {
		l.log.V(2).Info("production rate is 0", "partition", partition)
		return 0
	}
	lag := float64(behind) / float64(production)
	l.log.V(2).Info("lag per partition", "partition", partition, "lag", lag, "lagSec", time.Duration(lag)*time.Second)
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

func (l *PrometheusMP) query(prom promv1.API, ctx context.Context, query string) (model.Value, error) {
	value, warnings, err := prom.Query(ctx, query, time.Now())
	if err != nil {
		return nil, err
	}
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
		go func() {
			defer wg.Done()
			v, err := l.query(l.apis[i], ctx, query)
			if err != nil {
				l.log.Error(err, "failed to query prometheus")
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
	value := l.queryAll(l.offsetQuery)
	if value == nil {
		return nil, errors.New("failed to get offset metrics from prometheus")
	}
	metrics := l.parse(value, l.offsetPartitionLabel)
	return metrics, nil
}

// queryProductionRate queries Prometheus for the current production rate
func (l *PrometheusMP) queryProductionRate() (metricsMap, error) {
	value := l.queryAll(l.productionQuery)
	if value == nil {
		return nil, errors.New("failed to get production metrics from prometheus")
	}
	metrics := l.parse(value, l.productionPartitionLabel)
	return metrics, nil
}

func (l *PrometheusMP) queryConsumptionRate() (metricsMap, error) {
	value := l.queryAll(l.consumptionQuery)
	if value == nil {
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
		panic(fmt.Sprintf("unsupported prometheus response type %q", value.Type()))
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
