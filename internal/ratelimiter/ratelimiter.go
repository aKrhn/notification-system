package ratelimiter

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local now = tonumber(ARGV[2])

local bucket = redis.call('HMGET', key, 'tokens', 'last')
local tokens = tonumber(bucket[1])
local last = tonumber(bucket[2])

if tokens == nil then
    tokens = rate
    last = now
end

local elapsed = now - last
local refill = elapsed * rate
tokens = math.min(rate, tokens + refill)

if tokens >= 1 then
    tokens = tokens - 1
    redis.call('HMSET', key, 'tokens', tokens, 'last', now)
    redis.call('EXPIRE', key, 2)
    return 1
else
    redis.call('HMSET', key, 'tokens', tokens, 'last', now)
    redis.call('EXPIRE', key, 2)
    return 0
end
`)

type RateLimiter struct {
	client *redis.Client
	key    string
	rate   int
}

func New(client *redis.Client, channel string, rate int) *RateLimiter {
	return &RateLimiter{
		client: client,
		key:    fmt.Sprintf("ratelimit:%s", channel),
		rate:   rate,
	}
}

func (r *RateLimiter) Allow(ctx context.Context) (bool, error) {
	now := float64(time.Now().UnixMicro()) / 1e6
	result, err := tokenBucketScript.Run(ctx, r.client, []string{r.key}, r.rate, now).Int()
	if err != nil {
		return false, fmt.Errorf("rate limiter script: %w", err)
	}
	return result == 1, nil
}

func (r *RateLimiter) Wait(ctx context.Context) error {
	deadline := time.After(5 * time.Second)
	for {
		allowed, err := r.Allow(ctx)
		if err != nil {
			slog.Warn("rate limiter error, allowing through", "key", r.key, "error", err)
			return nil // fail-open
		}
		if allowed {
			return nil
		}

		select {
		case <-time.After(10 * time.Millisecond):
			continue
		case <-deadline:
			return fmt.Errorf("rate limiter wait timeout for %s", r.key)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
