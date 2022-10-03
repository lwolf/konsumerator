package main

import (
	"log"
	"strconv"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/lwolf/konsumerator/hack/faker/cmd/consumer"
	"github.com/lwolf/konsumerator/hack/faker/cmd/producer"
	"github.com/lwolf/konsumerator/hack/faker/lib"
)

type ConsumerCmd struct {
	Partition   string `arg:"env:KONSUMERATOR_PARTITION"`
	RatePerCore int    `arg:"--rpc"`
}
type ProducerCmd struct {
	NumPartitions int `arg:"--num-partitions"`
	BaseRate      int `arg:"--base-rate"`
	FullPeriod    int `arg:"--full-period"`
}

var args struct {
	Consumer  *ConsumerCmd `arg:"subcommand:consumer"`
	Producer  *ProducerCmd `arg:"subcommand:producer"`
	RedisAddr string       `arg:"--redisAddr"`
	Port      int          `arg:"--port"`
}

func main() {
	arg.MustParse(&args)

	redisClient, err := lib.NewRedisClient(args.RedisAddr)
	if err != nil {
		log.Fatalf("unable to connect to redis at addres %s: %v", args.RedisAddr, err)
	}
	switch {
	case args.Consumer != nil:
		log.Println("running consumer")
		var partitions []int
		for _, p := range strings.Split(args.Consumer.Partition, ",") {
			partition, err := strconv.Atoi(p)
			if err != nil {
				log.Fatalf("failed to parse partitions ids from %s: %v", args.Consumer.Partition, err)
			}
			partitions = append(partitions, partition)
		}
		consumer.RunConsumer(redisClient, partitions, args.Consumer.RatePerCore, args.Port)
	case args.Producer != nil:
		log.Println("running producer")
		producer.RunProducer(
			redisClient,
			args.Producer.NumPartitions,
			args.Producer.BaseRate,
			args.Producer.FullPeriod,
			args.Port,
		)
	}
}
