package producer

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/lwolf/konsumerator/hack/faker/lib"
)

var (
	productionOffsetMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "konsumerator",
		Name:      "messages_production_offset",
		Help:      "Last seen offset per partition",
	}, []string{"partition"})
)

func runGenerator(client *redis.Client, baseProductionRate int, fullPeriod int, partition int) {
	log.Printf("starting load generation for partition %d", partition)
	productionOffsetKey := fmt.Sprintf("%s-%d", lib.ProductionOffsetKey, partition)
	state, err := lib.GetOffset(client, productionOffsetKey, 0)
	if err != nil {
		log.Fatalf("unable to get data from redis %v", err)
	}
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		batchSize := generatePoint(float64(time.Now().Unix()), partition, baseProductionRate, float64(fullPeriod))
		log.Printf("going to create batch %v", batchSize)
		values := make([]interface{}, int(batchSize))
		for i := range values {
			values[i] = byte(1)
		}
		state = state + int(batchSize)
		err = lib.SetOffset(client, productionOffsetKey, state)
		if err != nil {
			log.Fatalf("unable to set offset %v", err)
		}
		productionOffsetMetric.WithLabelValues(strconv.Itoa(partition)).Set(float64(state))
		log.Printf("produced %d messages to partition %d, new offset=%d", int(batchSize), partition, state)
	}
}

func runServer(port int) {
	log.Printf("starting web server on port %d", port)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func generatePoint(i float64, partition int, baseProductionRate int, resolution float64) float64 {
	rand.Seed(int64(partition))
	offset := float64(baseProductionRate + (partition * 1000))
	fuzz := rand.Float64() * offset * 0.1
	return offset + fuzz + (float64(baseProductionRate) * math.Sin(i*(math.Pi*2/resolution)))

}
func RunProducer(client *redis.Client, numPartitions int, baseProductionRate int, fullPeriod int, port int) {
	log.Printf("running the producer partitions=%d, productionRate=%d, port=%d", numPartitions, baseProductionRate, port)
	prometheus.MustRegister(productionOffsetMetric)
	wg := sync.WaitGroup{}
	for i := 0; i < numPartitions; i++ {
		part := i
		go func() {
			wg.Add(1)
			runGenerator(client, baseProductionRate, fullPeriod, part)
			wg.Done()
		}()
	}
	wg.Add(1)
	go func() {
		runServer(port)
		wg.Done()
	}()
	wg.Wait()

}
