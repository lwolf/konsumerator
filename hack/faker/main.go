package main

import (
	"github.com/alexflint/go-arg"
	"github.com/prometheus/common/log"

	"github.com/lwolf/konsumerator/hack/faker/cmd/consumer"
	"github.com/lwolf/konsumerator/hack/faker/cmd/producer"
	"github.com/lwolf/konsumerator/hack/faker/lib"
)

type ConsumerCmd struct {
	Partition   int `arg:"env:KONSUMERATOR_PARTITION"`
	RatePerCore int `arg:"--rpc"`
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
		log.Info("running consumer")
		consumer.RunConsumer(redisClient, args.Consumer.Partition, args.Consumer.RatePerCore, args.Port)
	case args.Producer != nil:
		log.Info("running producer")
		producer.RunProducer(
			redisClient,
			args.Producer.NumPartitions,
			args.Producer.BaseRate,
			args.Producer.FullPeriod,
			args.Port,
		)
	}
}
