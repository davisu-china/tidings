package database

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/davisu-china/tidings/backend/migrations"
)

// Migrate 执行版本化迁移。迁移文件已嵌进二进制，容器启动即自动执行。
//
// 刻意不用 GORM AutoMigrate：它不处理列删除和类型变更，
// 也没有版本记录无法回滚，不能上生产。
func Migrate(migrateURL string, log *slog.Logger) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("读取迁移文件失败: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
	if err != nil {
		return fmt.Errorf("初始化 migrate 失败: %w", err)
	}
	defer m.Close()

	before, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("读取当前版本失败: %w", err)
	}
	if dirty {
		return fmt.Errorf("数据库处于 dirty 状态（版本 %d），需要人工介入修复", before)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info("数据库已是最新", "version", before)
			return nil
		}
		return fmt.Errorf("执行迁移失败: %w", err)
	}

	after, _, err := m.Version()
	if err != nil {
		return fmt.Errorf("读取迁移后版本失败: %w", err)
	}
	log.Info("迁移完成", "from", before, "to", after)
	return nil
}
