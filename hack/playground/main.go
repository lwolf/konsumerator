package main

import (
	"context"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/api/resource"
	"log"
	"time"
)

var cache = make(map[string]float64)
var rangeCache = make(map[string][]float64)

func prom() {
	c, err := api.NewClient(api.Config{Address: "https://prometheus.srvho.me"})
	if err != nil {
		log.Fatalf("failed to setup new client: %v", err)
	}
	query := `max(rate(container_cpu_user_seconds_total{namespace="monitoring"}[24h])) by (pod_name) * 10000`

	ctx := context.Background()
	a := v1.NewAPI(c)
	value, warnings, err := a.Query(ctx, query, time.Now())
	if err != nil {
		log.Fatalf("failed to query: %v", err)
	}
	// v.Metric[model.LabelName("container_name")]
	for _, v := range value.(model.Vector) {
		log.Printf("metric: %v; value=%v", v.Metric, v.Value)
		name := string(v.Metric[model.LabelName("pod_name")])
		cache[name] = float64(v.Value)
	}
	// QueryRange(ctx context.Context, query string, r Range) (model.Value, api.Warnings, error)
	startTime := time.Now().Add(-5 * time.Minute)
	dateRange := v1.Range{Start: startTime, End: time.Now(), Step: time.Minute}
	log.Printf("****************************")
	value, warnings, err = a.QueryRange(ctx, query, dateRange)

	for _, v := range value.(model.Matrix) {
		log.Printf("metric: %v; value=%v", v.Metric, v.Values)
		name := string(v.Metric[model.LabelName("pod_name")])
		var points []float64
		for _, p := range v.Values {
			points = append(points, float64(p.Value))
		}
		rangeCache[name] = points
	}

	// log.Printf("cache: %v", cache)
	// log.Printf("valueString: %v", values.String())

	// values.Type().UnmarshalJSON()
	// values.Type().
	// for _, v := range values {
	//
	// }
	log.Printf("warning: %v", warnings)
}

const buckets = int64(10)

func predictor() {
	minCpu := resource.MustParse("500m")
	maxCpu := resource.MustParse("4.5")
	cp := minCpu.DeepCopy()
	cp.Add(maxCpu)
	step := cp.MilliValue() / buckets
	log.Printf("minCPU: %v, maxCPU: %v, cp %v, step %d", minCpu.MilliValue(), maxCpu.MilliValue(), cp.MilliValue(), step)
	log.Printf("minCPU: %s, maxCPU: %s, cp %s, step %d", minCpu.String(), maxCpu.String(), cp.String(), step)
	for i := int64(0); i < buckets; i++ {
		vcpu := minCpu.DeepCopy()
		vcpu.SetMilli(vcpu.MilliValue() + (step * i))
		log.Printf("%d: assigning CPU value %s", i, vcpu.String())
	}
	// res := corev1.ResourceList{
	// 	corev1.ResourceCPU:    resource.MustParse("100m"),
	// 	corev1.ResourceMemory: resource.MustParse("2Gi"),
	// }
	// minCpu, isOk := res.Cpu().AsInt64()
	// if !isOk {
	// log.Printf("asDec %v", res.Cpu().AsDec())
	// }
	// log.Printf("minCPU: %d, maxCPU: %d", minCpu)

}

func main() {
	predictor()
}
