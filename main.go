package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis"
	"github.com/kelseyhightower/envconfig"
	"github.com/nats-io/go-nats"
	"github.com/broderickhyman/albiondata-client/lib"
	"flag"
)

type config struct {
	CacheTime int    `default:"500"`
	NatsURL   string
	RedisAddr string
	RedisPass string
	Debug bool
}

var (
	c  config
	rc *redis.Client
	nc *nats.Conn
)

func init() {
	flag.StringVar(
		&c.NatsURL,
		"i",
		"nats://public:thenewalbiondata@localhost:4222",
		"Nats URL.",
	)

	flag.StringVar(
		&c.RedisAddr,
		"r",
		"localhost:6379",
		"Redis URL.",
	)

	flag.StringVar(
		&c.RedisPass,
		"p",
		"",
		"Redis Password.",
	)

	flag.BoolVar(
		&c.Debug,
		"debug",
		false,
		"Enable debug logging.",
	)
}


func main() {
	log.Println("Deduper starting")
	flag.Parse()
	err := envconfig.Process("deduper", &c)
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Printf("Connecting to Nats: %s", c.NatsURL)
	nc, err = nats.Connect(c.NatsURL)
	if err != nil {
		log.Fatalf("Unable to connect to nats server: %v", err)
	}

	defer nc.Close()

	log.Printf("Connecting to Redis: %s", c.RedisAddr)
	rc = redis.NewClient(&redis.Options{
		Addr:     c.RedisAddr,
		Password: c.RedisPass,
		DB:       0,
	})

	// Market Orders
	marketCh := make(chan *nats.Msg, 64)
	marketSub, err := nc.ChanSubscribe(lib.NatsMarketOrdersIngest, marketCh)
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}
	defer marketSub.Unsubscribe()

	// Gold Prices
	goldCh := make(chan *nats.Msg, 64)
	goldSub, err := nc.ChanSubscribe(lib.NatsGoldPricesIngest, goldCh)
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}
	defer goldSub.Unsubscribe()

	// Map Data
	mapCh := make(chan *nats.Msg, 64)
	mapSub, err := nc.ChanSubscribe(lib.NatsMapDataIngest, mapCh)
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}
	defer mapSub.Unsubscribe()

	log.Println("Listening")
	for {
		select {
		case msg := <-marketCh:
			handleMarketOrder(msg)
		case msg := <-goldCh:
			handleGold(msg)
		case msg := <-mapCh:
			handleMapData(msg)
		}
	}
}

func handleGold(msg *nats.Msg) {
	log.Print("Processing gold prices message...")

	hash := md5.Sum(msg.Data)
	key := fmt.Sprintf("%v-%v", msg.Subject, hash)

	if !isDupedMessage(key) {
		nc.Publish(lib.NatsGoldPricesDeduped, msg.Data)
	}
}

func handleMapData(msg *nats.Msg) {
	log.Print("Processing map data message...")

	hash := md5.Sum(msg.Data)
	key := fmt.Sprintf("%v-%v", msg.Subject, hash)

	if !isDupedMessage(key) {
		nc.Publish(lib.NatsMapDataDeduped, msg.Data)
	}
}

func handleMarketOrder(msg *nats.Msg) {
	log.Print("Processing market order message...")

	morders := &lib.MarketUpload{}
	if err := json.Unmarshal(msg.Data, morders); err != nil {
		fmt.Printf("%v\n", err)
	}

	for _, order := range morders.Orders {
		// Hack since albion seems to be multiplying every price by 10000?
		order.Price = order.Price / 10000
		jb, _ := json.Marshal(order)
		hash := md5.Sum(jb)
		key := fmt.Sprintf("%v-%v", msg.Subject, hash)

		if !isDupedMessage(key) {
			nc.Publish(lib.NatsMarketOrdersDeduped, jb)
		}
	}
}

func isDupedMessage(key string) bool {
	_, err := rc.Get(key).Result()
	if err == redis.Nil {
		set(key)

		// It didn't exist so not a duped message
		return false
	} else if err != nil {
		fmt.Println("Error while getting from Redis: %v", err)

		// There was a problem with Redis and since we cannot verify
		// if the message is a dupe lets just say it isn't. Better
		// safe than sorry.
		return false
	} else {
		// There was no problem with Redis and we got a value back.
		// So it was a dupe.
		return true
	}
}

func set(key string) {
	cache_time := time.Duration(c.CacheTime) * time.Second

	_, err := rc.Set(key, 1, cache_time).Result()
	if err != nil {
		fmt.Println("Error while setting redis key: %v", err)
	}
}
