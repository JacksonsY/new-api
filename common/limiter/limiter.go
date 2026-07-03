package limiter

import (
	"context"
	_ "embed"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

//go:embed lua/rate_limit.lua
var rateLimitScript string

// limitScript 使用 go-redis 的 redis.Script：Run 会先尝试 EVALSHA，
// 收到 NOSCRIPT 时自动回退 EVAL 并重新加载脚本，天然处理 Redis
// 重启/SCRIPT FLUSH 后脚本缓存丢失的情况。
var limitScript = redis.NewScript(rateLimitScript)

type RedisLimiter struct {
	client *redis.Client
}

var (
	instance *RedisLimiter
	once     sync.Once
)

func New(ctx context.Context, r *redis.Client) *RedisLimiter {
	once.Do(func() {
		// 预加载脚本（失败不致命，Run 时会自动回退 EVAL）
		if err := limitScript.Load(ctx, r).Err(); err != nil {
			common.SysLog(fmt.Sprintf("Failed to load rate limit script: %v", err))
		}
		instance = &RedisLimiter{client: r}
	})

	return instance
}

func (rl *RedisLimiter) Allow(ctx context.Context, key string, opts ...Option) (bool, error) {
	// 默认配置
	config := &Config{
		Capacity:  10,
		Rate:      1,
		Requested: 1,
	}

	// 应用选项模式
	for _, opt := range opts {
		opt(config)
	}

	// 执行限流（EVALSHA + NOSCRIPT 自动回退）
	result, err := limitScript.Run(
		ctx,
		rl.client,
		[]string{key},
		config.Requested,
		config.Rate,
		config.Capacity,
	).Int()

	if err != nil {
		return false, fmt.Errorf("rate limit failed: %w", err)
	}
	return result == 1, nil
}

// Config 配置选项模式
type Config struct {
	Capacity  int64
	Rate      int64
	Requested int64
}

type Option func(*Config)

func WithCapacity(c int64) Option {
	return func(cfg *Config) { cfg.Capacity = c }
}

func WithRate(r int64) Option {
	return func(cfg *Config) { cfg.Rate = r }
}

func WithRequested(n int64) Option {
	return func(cfg *Config) { cfg.Requested = n }
}
