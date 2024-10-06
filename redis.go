package main

import (
    "context"
    "log"

    "github.com/go-redis/redis/v8"
)

var redisClient *redis.Client

func initRedis() {
    redisClient = redis.NewClient(&redis.Options{
        Addr:     config.GetString("redis.addr"),
        Password: config.GetString("redis.password"),
        DB:       config.GetInt("redis.db"),
    })

    ctx := context.Background()
    _, err := redisClient.Ping(ctx).Result()
    if err != nil {
        log.Fatalf("Failed to connect to Redis: %v", err)
    }
    
    log.Println("Connected to Redis successfully")
}

func checkRedis() error {
    ctx := context.Background()
    return redisClient.Ping(ctx).Err()
}