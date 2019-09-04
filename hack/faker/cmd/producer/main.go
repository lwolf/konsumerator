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

const (
	ProductionOffsetKey = "konsumerator_production_offsets"
	messagesKey         = "konsumerator_messages"

	baseProductionRate = 30000
)

func initRedisKeys(client *redis.Client) error {
	var err error
	if err = client.Exists(ProductionOffsetKey).Err(); err != nil {
		return err
	}
	if err = client.Exists(messagesKey).Err(); err != nil {
		return err
	}
	return nil
}

var (
	productionOffsetMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "konsumerator",
		Name:      "messages_production_offset",
		Help:      "Last seen offset per partition",
	}, []string{"partition"})
)

func runGenerator(client *redis.Client, partition int) {
	log.Printf("starting load generation for partition %d", partition)
	state, err := lib.GetOffset(client, ProductionOffsetKey, partition, 0)
	if err != nil {
		log.Fatalf("unable to get data from redis %v", err)
	}
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		batchSize := generatePoint(float64(time.Now().Unix()), partition, 7200)
		log.Printf("going to create batch %v", batchSize)
		values := make([]interface{}, int(batchSize))
		for i := range values {
			values[i] = byte(1)
		}
		state = state + int(batchSize)
		err = client.LPush(fmt.Sprintf("%s_%d", messagesKey, partition), values...).Err()
		if err != nil {
			log.Printf("unable to push messages to redis %v", err)
			continue
		}
		err = lib.SetOffset(client, ProductionOffsetKey, partition, state)
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

func generatePoint(i float64, partition int, resolution float64) float64 {
	rand.Seed(int64(partition))
	offset := float64(baseProductionRate + (partition * 1000))
	fuzz := rand.Float64() * offset * 0.1
	return offset + fuzz + (baseProductionRate * math.Sin(i*(math.Pi*2/resolution)))

}
func RunProducer(client *redis.Client, numPartitions int, port int) {
	log.Printf("running the producer partitions=%d, port=%d", numPartitions, port)
	err := initRedisKeys(client)
	if err != nil {
		log.Fatalf("failed to setup redis keys: %v", err)
	}
	prometheus.MustRegister(productionOffsetMetric)
	wg := sync.WaitGroup{}
	for i := 0; i < numPartitions; i++ {
		part := i
		go func() {
			wg.Add(1)
			runGenerator(client, part)
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
