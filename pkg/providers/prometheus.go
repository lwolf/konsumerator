package providers

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
)

type LagSourcePrometheus struct {
	api                       v1.API
	addresses                 []string
	productionQuery           string
	productionPartitionLabel  string
	productionMetricName      string
	consumptinQuery           string
	consumptionPartitionLabel string
	offsetQuery               string
	offsetPartitionLabel      string

	messagesBehind  map[int32]float64
	productionRate  map[int32]float64
	consumptionRate map[int32]float64
}

func NewLagSourcePrometheus(spec *konsumeratorv1alpha1.PrometheusAutoscalerSpec) (*LagSourcePrometheus, error) {
	c, err := api.NewClient(api.Config{Address: spec.Address[0]})
	if err != nil {
		return nil, err
	}

	return &LagSourcePrometheus{
		api:                       v1.NewAPI(c),
		productionQuery:           spec.Production.Query,
		productionPartitionLabel:  spec.Production.PartitionLabel,
		productionMetricName:      spec.Production.MetricName,
		consumptinQuery:           spec.Consumption.Query,
		consumptionPartitionLabel: spec.Consumption.PartitionLabel,
		offsetQuery:               spec.Offset.Query,
		offsetPartitionLabel:      spec.Offset.PartitionLabel,
		addresses:                 spec.Address,
	}, nil
}

// GetLagByPartition calculates lag based on ProductionRate, ConsumptionRate and
// the number of not processed messages for partition
func (l *LagSourcePrometheus) GetLagByPartition(partition int32) time.Duration {
	behind, ok := l.messagesBehind[partition]
	if !ok {
		return 0
	}
	consumption, ok := l.consumptionRate[partition]
	if !ok {
		return 0
	}
	production, ok := l.productionRate[partition]
	if !ok {
		return 0
	}
	if (consumption == production) || (consumption-production == 0) {
		return 0
	}
	lag := behind / (consumption - production)
	return time.Duration(lag) * time.Second
}

func (l *LagSourcePrometheus) QueryProductionRate() (map[int32]float64, error) {
	return nil, nil
}

func (l *LagSourcePrometheus) EstimateLag() error {
	return nil
}

func (l *LagSourcePrometheus) GetLag() map[int32]float64 {
	return nil
}

func (l *LagSourcePrometheus) QueryOffset() (map[int32]float64, error) {
	return nil, nil
}

// QueryProductionDistibution queries Prometheus for the maximum production rate for the
// last 24h to allocate maximum resources required to process the partition
func (l *LagSourcePrometheus) QueryProductionRateDistribution() (map[int32]float64, error) {
	ctx := context.Background()
	query := fmt.Sprintf(`max(rate(%s[24h])) by (%s)`, l.productionMetricName, l.productionPartitionLabel)
	value, warnings, err := l.api.Query(ctx, query, time.Now())
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		log.Printf("WARNING getting production distribution: %s", w)
	}
	offsets := make(map[int32]float64)
	for _, v := range value.(model.Vector) {
		partitionNumberStr := string(v.Metric[model.LabelName(l.productionPartitionLabel)])
		partitionNumber, err := strconv.Atoi(partitionNumberStr)
		if err != nil {
			log.Printf("unable to parse partition number from the label %s", partitionNumberStr)
		}
		offsets[int32(partitionNumber)] = float64(v.Value)
	}
	return offsets, nil
}

func (l *LagSourcePrometheus) QueryConsumptionRate() (map[int32]float64, error) {
	return nil, nil
}
