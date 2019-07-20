package providers

import (
	"context"
	"math/rand"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

const promCallTimeout = time.Second * 30

type MetricsMap map[int32]int64

type PrometheusMP struct {
	api       v1.API
	addresses []string
	log       logr.Logger

	productionQuery           string
	productionPartitionLabel  string
	consumptinQuery           string
	consumptionPartitionLabel string
	offsetQuery               string
	offsetPartitionLabel      string

	messagesBehind  MetricsMap
	productionRate  MetricsMap
	consumptionRate MetricsMap
}

func NewPrometheusMP(log logr.Logger, spec *konsumeratorv1alpha1.PrometheusAutoscalerSpec) (*PrometheusMP, error) {
	ctrlLogger := log.WithName("prometheusMP")
	c, err := api.NewClient(api.Config{Address: spec.Address[0]})
	if err != nil {
		return nil, err
	}

	return &PrometheusMP{
		api:                       v1.NewAPI(c),
		log:                       ctrlLogger,
		productionQuery:           spec.Production.Query,
		productionPartitionLabel:  spec.Production.PartitionLabel,
		consumptinQuery:           spec.Consumption.Query,
		consumptionPartitionLabel: spec.Consumption.PartitionLabel,
		offsetQuery:               spec.Offset.Query,
		offsetPartitionLabel:      spec.Offset.PartitionLabel,
		addresses:                 spec.Address,
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
		return 0
	}
	return consumption
}

func (l *PrometheusMP) GetMessagesBehind(partition int32) int64 {
	behind, ok := l.messagesBehind[partition]
	if !ok {
		return 0
	}
	return behind
}

// GetLagByPartition calculates lag based on ProductionRate, ConsumptionRate and
// the number of not processed messages for partition
func (l *PrometheusMP) GetLagByPartition(partition int32) time.Duration {
	behind := l.GetMessagesBehind(partition)
	production := l.GetProductionRate(partition)
	if production == 0 {
		return 0
	}
	lag := behind / production
	return time.Duration(lag) * time.Second
}

func (l *PrometheusMP) Update() error {
	var err error
	l.productionRate, err = l.QueryProductionRate()
	if err != nil {
		return err
	}
	l.consumptionRate, err = l.QueryConsumptionRate()
	if err != nil {
		return err
	}
	l.messagesBehind, err = l.QueryOffset()
	if err != nil {
		return err
	}
	return nil
}

func (l *PrometheusMP) QueryOffset() (MetricsMap, error) {
	ctx, cancel := context.WithTimeout(context.Background(), promCallTimeout)
	defer cancel()
	value, warnings, err := l.api.Query(ctx, l.offsetQuery, time.Now())
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		l.log.Info("querying offsets", "warning", w)
	}
	offsets := l.parseVector(value, l.offsetPartitionLabel)
	return offsets, nil

}

// QueryProductionRate queries Prometheus for the current production rate
func (l *PrometheusMP) QueryProductionRate() (MetricsMap, error) {
	ctx, cancel := context.WithTimeout(context.Background(), promCallTimeout)
	defer cancel()
	value, warnings, err := l.api.Query(ctx, l.productionQuery, time.Now())
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		l.log.Info("querying production distribution", "warning", w)
	}
	offsets := l.parseVector(value, l.productionPartitionLabel)
	return offsets, nil
}

func (l *PrometheusMP) QueryConsumptionRate() (MetricsMap, error) {
	ctx, cancel := context.WithTimeout(context.Background(), promCallTimeout)
	defer cancel()
	value, warnings, err := l.api.Query(ctx, l.consumptinQuery, time.Now())
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		l.log.Info("querying consumption distribution", "warning", w)
	}
	offsets := l.parseVector(value, l.consumptionPartitionLabel)
	return offsets, nil
}

func (l *PrometheusMP) parseVector(v model.Value, lbl string) MetricsMap {
	metrics := make(MetricsMap)
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
