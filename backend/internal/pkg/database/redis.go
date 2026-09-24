package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/davisu-china/tidings/backend/internal/config"
)

// NewRedis 建立 Redis 连接。Redis 承担验证码、发送频次计数、
// 静默时段的待发队列和多实例 WS 广播。
func NewRedis(cfg config.RedisConfig, log *slog.Logger) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis 失败: %w", err)
	}

	log.Info("redis 已连接", "addr", cfg.Addr, "db", cfg.DB)
	return client, nil
}
