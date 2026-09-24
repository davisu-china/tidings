// Package migrations 把版本化的 SQL 迁移文件嵌进二进制，
// 这样 api 启动时就能自动执行，不需要额外的 migrate 容器。
//
// 注意：这里用 golang-migrate 跑版本化 SQL，不用 GORM AutoMigrate。
// AutoMigrate 不处理列删除和类型变更，也没有版本记录无法回滚，不能上生产。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
