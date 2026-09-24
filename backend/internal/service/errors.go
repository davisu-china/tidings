package service

import (
	"errors"

	"gorm.io/gorm"
)

// isUniqueViolation 判断错误是否为唯一约束冲突。
//
// 靠 GORM 的 TranslateError（见 pkg/database/postgres.go）把驱动的
// SQLSTATE 23505 翻译成哨兵错误，而不是匹配错误字符串 ——
// 字符串匹配会随驱动升级或本地化悄悄失效，而且失效时是静默的：
// 注册接口会把「邮箱被抢注」当成 500 返回，看起来只是偶尔抽风。
func isUniqueViolation(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey)
}
