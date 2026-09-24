// Package database 负责 PostgreSQL / Redis 客户端与数据库迁移。
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/davisu-china/tidings/backend/internal/config"
)

// NewPostgres 建立 GORM 连接并配置连接池。
//
// logSQL 控制是否逐条打印 SQL。api 在开发环境打开便于调试；
// worker 必须关闭 —— 它每几秒轮询一次 outbox，打开后日志会被
// 重复的查询刷屏，真正有用的信息全被淹没。
func NewPostgres(cfg config.PostgresConfig, logSQL bool, log *slog.Logger) (*gorm.DB, error) {
	gormLogLevel := gormlogger.Warn
	if logSQL {
		gormLogLevel = gormlogger.Info
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormLogLevel),
		// 时间统一用 UTC 存，展示层再按 Asia/Shanghai 转
		NowFunc: func() time.Time { return time.Now().UTC() },
		// 关掉默认事务包裹，单条写入不需要，能省一次往返
		SkipDefaultTransaction: true,
		// 把驱动的唯一约束冲突翻译成 gorm.ErrDuplicatedKey。
		// 注册接口靠它区分「邮箱被抢注」和「真的写失败」——
		// 不翻译的话只能去匹配错误字符串，那是会随驱动升级失效的写法。
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 postgres 失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取 sql.DB 失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres 失败: %w", err)
	}

	log.Info("postgres 已连接", "host", cfg.Host, "db", cfg.DBName)
	return db, nil
}
