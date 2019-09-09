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
	apis []promv1.API
	log  logr.Logger

	productionQuery           string
	productionPartitionLabel  string
	consumptionQuery          string
	consumptionPartitionLabel string
	offsetQuery               string
	offsetPartitionLabel      string

	messagesBehind  metricsMap
	productionRate  metricsMap
	consumptionRate metricsMap
}

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
		return nil, errors.New("unable to reach any prometheus address")
	}

	return &PrometheusMP{
		apis:                      apis,
		log:                       ctrlLogger,
		productionQuery:           spec.Production.Query,
		productionPartitionLabel:  spec.Production.PartitionLabel,
		consumptionQuery:          spec.Consumption.Query,
		consumptionPartitionLabel: spec.Consumption.PartitionLabel,
		offsetQuery:               spec.Offset.Query,
		offsetPartitionLabel:      spec.Offset.PartitionLabel,
	}, nil
}

func (l *PrometheusMP) GetProductionRate(partition int32) int64 {
	production, ok := l.productionRate[partition]
	if !ok {
		return 0
	}
	return production
}

func (l *PrometheusMP) GetConsumptionRate(partition int32) int64 {
	consumption, ok := l.consumptionRate[partition]
	if !ok {
		l.log.V(2).Info("consumption value not found", "partition", partition)
		return 0
	}
	return consumption
}

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

	var wg sync.WaitGroup
	valueCh := make(chan model.Value)
	doneCh := make(chan interface{})
	for i := range l.apis {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := l.query(l.apis[i], ctx, query)
			if err != nil {
				l.log.Error(err, "failed to query prometheus", "address", l.apis[i])
				return
			}
			select {
			case <-doneCh:
			case valueCh <- v:
			}
		}()
	}

	var v model.Value
	select {
	case v = <-valueCh:
		close(doneCh)
	case <-ctx.Done():
	}

	wg.Wait()
	close(valueCh)
	return v
}

func (l *PrometheusMP) queryOffset() (metricsMap, error) {
	value := l.queryAll(l.offsetQuery)
	if value == nil {
		return nil, errors.New("failed to get offset metrics from prometheus")
	}
	metrics := l.parseVector(value, l.offsetPartitionLabel)
	return metrics, nil
}

// queryProductionRate queries Prometheus for the current production rate
func (l *PrometheusMP) queryProductionRate() (metricsMap, error) {
	value := l.queryAll(l.productionQuery)
	if value == nil {
		return nil, errors.New("failed to get production metrics from prometheus")
	}
	metrics := l.parseVector(value, l.productionPartitionLabel)
	return metrics, nil
}

func (l *PrometheusMP) queryConsumptionRate() (metricsMap, error) {
	value := l.queryAll(l.consumptionQuery)
	if value == nil {
		return nil, errors.New("failed to get consumption metrics from prometheus")
	}
	metrics := l.parseVector(value, l.consumptionPartitionLabel)
	return metrics, nil
}

func (l *PrometheusMP) parseVector(v model.Value, lbl string) metricsMap {
	metrics := make(metricsMap)
	// TODO: cover cases when value is not a vector
	for _, v := range v.(model.Vector) {
		partitionNumberStr := string(v.Metric[model.LabelName(l.productionPartitionLabel)])
		partitionNumber, err := strconv.Atoi(partitionNumberStr)
		if err != nil {
			l.log.Info("unable to parse partition number from the label", "label", partitionNumberStr)
			continue
		}
		metrics[int32(partitionNumber)] = int64(v.Value)
	}
	return metrics
}
