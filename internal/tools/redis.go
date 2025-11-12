package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/abjerry97/go_payment/api"
	"github.com/go-redis/redis/v8"
)

type RedisService struct {
	Client *redis.Client
}

func NewRedisService(redisURL string) (*RedisService, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	opts.PoolSize = 100
	opts.MinIdleConns = 20
	opts.MaxRetries = 3

	Client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := Client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	log.Println("Redis connected successfully")
	return &RedisService{Client: Client}, nil
}

func (r *RedisService) Close() error {
	return r.Client.Close()
}

func (r *RedisService) EnqueuePayment(ctx context.Context, payment *api.PaymentPayload) error {
	data, err := json.Marshal(payment)
	if err != nil {
		return err
	}

	return r.Client.RPush(ctx, "payment_queue", data).Err()
}

func (r *RedisService) DequeuePayment(ctx context.Context, timeout time.Duration) (*api.PaymentPayload, error) {
	result, err := r.Client.BLPop(ctx, timeout, "payment_queue").Result()
	if err != nil {
		return nil, err
	}

	if len(result) < 2 {
		return nil, nil
	}

	var payment api.PaymentPayload
	if err := json.Unmarshal([]byte(result[1]), &payment); err != nil {
		return nil, err
	}

	return &payment, nil
}

func (r *RedisService) IsDuplicate(ctx context.Context, txnRef string) (bool, error) {
	exists, err := r.Client.Exists(ctx, "txn:"+txnRef).Result()
	return exists > 0, err
}

func (r *RedisService) MarkDuplicate(ctx context.Context, txnRef string, ttl time.Duration) error {
	return r.Client.SetEX(ctx, "txn:"+txnRef, "1", ttl).Err()
}

func (r *RedisService) GetCachedBalance(ctx context.Context, customerID string) (*float64, error) {
	result, err := r.Client.Get(ctx, "balance:"+customerID).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var balance float64
	if _, err := fmt.Sscanf(result, "%f", &balance); err != nil {
		return nil, err
	}

	return &balance, nil
}

func (r *RedisService) CacheBalance(ctx context.Context, customerID string, balance float64, ttl time.Duration) error {
	return r.Client.SetEX(ctx, "balance:"+customerID, fmt.Sprintf("%.2f", balance), ttl).Err()
}
