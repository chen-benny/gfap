package storage

import (
	"context"

	"github.com/redis/go-redis/v9"
)

const (
	bloomKey    = "crawler:bloom"
	overflowKey = "crawler:overflow"

	bloomCapacity  = 100_000_000 // ~120MB under 0.1% FPR
	bloomErrorRate = 0.001
)

type Redis struct {
	client *redis.Client
}

func NewRedis(addr string) (*Redis, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if _, err := client.Ping(context.Background()).Result(); err != nil {
		return nil, err
	}
	return &Redis{client: client}, nil
}

// BloomInit reserves the Bloom filter with the config
func (r *Redis) BloomInit(ctx context.Context) error {
	err := r.client.BFReserve(ctx, bloomKey, bloomErrorRate, bloomCapacity).Err()
	if err != nil && err.Error() == "ERR item exists" {
		return nil
	}
	return err
}

// BloomAdd adds url to the Bloom filter
// Returns true if newly inserted, false if already present
func (r *Redis) BloomAdd(ctx context.Context, url string) (bool, error) {
	return r.client.BFAdd(ctx, bloomKey, url).Result()
}

// BloomAddBatch pipelines BF.ADD for a slice of URLs - used by Resume() to re-seed from MongoDB
func (r *Redis) BloomAddBatch(ctx context.Context, urls []string) error {
	pipe := r.client.Pipeline()
	for _, url := range urls {
		pipe.BFAdd(ctx, bloomKey, url)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (r *Redis) PushOverflow(ctx context.Context, url string) error {
	return r.client.LPush(ctx, overflowKey, url).Err()
}
func (r *Redis) PopOverflow(ctx context.Context) (string, error) {
	return r.client.RPop(ctx, overflowKey).Result()
}

func (r *Redis) FlushDB(ctx context.Context) error {
	return r.client.FlushDB(ctx).Err()
}

func (r *Redis) Close() {
	r.client.Close()
}
