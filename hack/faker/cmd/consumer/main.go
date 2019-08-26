package consumer

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/lwolf/konsumerator/hack/faker/lib"
)

const (
	// request => `/sys/fs/cgroup/cpu/cpu.shares`
	sysCpuRequestFile = "/sys/fs/cgroup/cpu/cpu.shares"
	// limit => `/sys/fs/cgroup/cpu/cpu.cfs_quota_us`
	sysCpuLimitFile = "/sys/fs/cgroup/cpu/cpu.cfs_quota_us"

	PartitionPrefixConsumer = "konsumerator_consumption"
)

var consumptionMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "konsumerator",
	Name:      "messages_consumed_total",
	Help:      "Total number of messages consumed per partition",
}, []string{"partition"})

func runServer(port int) {
	log.Printf("starting web server on port %d", port)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func getCpuRequest(fname string) (float64, error) {
	if runtime.GOOS == "darwin" {
		return 104, nil
	}
	f, err := os.Open(fname)
	if err != nil {
		return 0, fmt.Errorf("failed to open cgroups file: %v", err)
	}
	defer f.Close()
	cpuR, err := ioutil.ReadAll(f)
	if err != nil {
		return 0, fmt.Errorf("failed to read cgroups file: %v", err)
	}
	request, err := strconv.Atoi(strings.TrimSpace(string(cpuR)))
	if err != nil {
		return 0, fmt.Errorf("failed to convert cpu shared to int: %v", err)
	}
	return float64(request), nil
}

func consume(partition int, ratePerCore int) float64 {
	milliCores, err := getCpuRequest(sysCpuRequestFile)
	if err != nil {
		panic(fmt.Sprintf("unable to get cgroups value %v", err))
	}
	rand.Seed(int64(partition))
	consumptionRate := (milliCores / 1000) * float64(ratePerCore)
	fuzz := rand.Float64() * consumptionRate * 0.1
	return fuzz + consumptionRate
}

func runConsumer(client *redis.Client, partition int, ratePerCore int) {
	log.Printf("starting consumer for partition %d", partition)
	key := fmt.Sprintf("%s_%d", PartitionPrefixConsumer, partition)
	currentState, err := lib.GetOffset(client, key, 0)
	if err != nil {
		log.Fatalf("unable to load state from redis %v", err)
	}
	ticker := time.NewTicker(time.Second)
	for _ = range ticker.C {
		value := consume(partition, ratePerCore)
		consumptionMetric.WithLabelValues(strconv.Itoa(partition)).Add(value)
		err = client.Set(key, strconv.Itoa(currentState+int(value)), 0).Err()
	}
}

func RunConsumer(redisClient *redis.Client, partition int, ratePerCore int, port int) {
	prometheus.MustRegister(consumptionMetric)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		runConsumer(redisClient, partition, ratePerCore)
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		runServer(port)
		wg.Done()
	}()
	wg.Wait()
}
