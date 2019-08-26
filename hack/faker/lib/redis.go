package lib

import (
	"fmt"
	"strconv"

	"github.com/go-redis/redis"
)

func NewRedisClient(addr string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	pong, err := client.Ping().Result()
	fmt.Println(pong, err)
	return client, err
}

func GetOffset(client *redis.Client, key string, defaultValue int) (int, error) {
	dbValue, err := client.Get(key).Result()
	if err == redis.Nil {
		return defaultValue, nil
	} else if err != nil {
		return 0, err
	} else {
		return strconv.Atoi(dbValue)
	}
}
