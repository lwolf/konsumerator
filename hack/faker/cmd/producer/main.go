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

const PartitionPrefixProd = "konsumerator_production"

var productionMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "konsumerator",
	Name:      "messages_produced_total",
	Help:      "Total number of messages produced per partition",
}, []string{"partition"})

func runGenerator(client *redis.Client, i int) {
	log.Printf("starting load generation for partition %d", i)
	key := fmt.Sprintf("%s_%d", PartitionPrefixProd, i)
	currentState, err := lib.GetOffset(client, key, 0)
	if err != nil {
		log.Fatalf("unable to get data from redis %v", err)
	}
	ticker := time.NewTicker(time.Second)
	for _ = range ticker.C {
		value := generatePoint(float64(time.Now().Unix()), i, 3600)
		productionMetric.WithLabelValues(strconv.Itoa(i)).Add(value)
		err = client.Set(key, strconv.Itoa(currentState+int(value)), 0).Err()
	}
}

func runServer(port int) {
	log.Printf("starting web server on port %d", port)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func generatePoint(i float64, partition int, resolution float64) float64 {
	rand.Seed(int64(partition))
	offset := float64(40000 + (partition * 1000))
	fuzz := rand.Float64() * offset * 0.1
	return offset + fuzz + (15000 * math.Sin(i*(math.Pi*2/resolution)))

}
func RunProducer(client *redis.Client, numPartitions int, port int) {
	prometheus.MustRegister(productionMetric)
	wg := sync.WaitGroup{}
	for i := 0; i < numPartitions; i++ {
		part := i
		go func() {
			wg.Add(1)
			runGenerator(client, part)
			wg.Done()
		}()
	}
	go func() {
		wg.Add(1)
		runServer(port)
		wg.Done()
	}()
	wg.Wait()

}
