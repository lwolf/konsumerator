package main

import (
	"github.com/alexflint/go-arg"
	"github.com/lwolf/konsumerator/hack/faker/cmd/consumer"
	"github.com/lwolf/konsumerator/hack/faker/cmd/producer"
	"github.com/lwolf/konsumerator/hack/faker/lib"
	"log"
)

type ConsumerCmd struct {
	Partition   int `arg:"-partition"`
	RatePerCore int `arg:"-rpc"`
}
type ProducerCmd struct {
	NumPartitions int `arg:"-num-partitions"`
}

var args struct {
	Consumer  *ConsumerCmd `arg:"subcommand:consumer"`
	Producer  *ProducerCmd `arg:"subcommand:producer"`
	RedisAddr string       `arg:"-redisAddr"`
	Port      int          `arg:"-port"`
	Quiet     bool         `arg:"-q"` // this flag is global to all subcommands
}

func main() {
	arg.MustParse(&args)

	redisClient, err := lib.NewRedisClient(args.RedisAddr)
	if err != nil {
		log.Fatalf("unable to connect to redis at addres %s: %v", args.RedisAddr, err)
	}

	switch {
	case args.Consumer != nil:
		consumer.RunConsumer(redisClient, args.Consumer.Partition, args.Consumer.RatePerCore, args.Port)
	case args.Producer != nil:
		producer.RunProducer(redisClient, args.Producer.NumPartitions, args.Port)
	}
}
