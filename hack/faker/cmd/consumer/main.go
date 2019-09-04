package consumer

import (
	"fmt"
	"io/ioutil"
	"log"
	"math"
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

	ConsumptionOffsetKey = "konsumerator_consumption_offsets"
	messagesKey          = "konsumerator_messages"
)

var (
	consumptionOffsetMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "konsumerator",
		Name:      "messages_consumption_offset",
		Help:      "Last seen offset per partition",
	}, []string{"partition"})
)

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
	state, err := lib.GetOffset(client, ConsumptionOffsetKey, partition, 0)
	if err != nil {
		log.Fatalf("unable to load state from redis %v", err)
	}
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		batchSize := consume(partition, ratePerCore)
		recordsCmd := client.LRem(messagesKey, int64(batchSize), byte(1))
		if recordsCmd.Err() != nil {
			log.Printf("failed to get %d messages: %v", int(batchSize), err)
			continue
		}
		state = state + int(math.Min(batchSize, float64(recordsCmd.Val())))
		err = lib.SetOffset(client, ConsumptionOffsetKey, partition, state)
		if err != nil {
			log.Fatalf("unable to set offset %v", err)
		}
		consumptionOffsetMetric.WithLabelValues(strconv.Itoa(partition)).Set(float64(state))
		log.Printf("processed %d messages from partition %d, new offset=%d", int(batchSize), partition, state)
	}
}

func RunConsumer(redisClient *redis.Client, partition int, ratePerCore int, port int) {
	prometheus.MustRegister(consumptionOffsetMetric)
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