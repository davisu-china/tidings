// Package repo 是数据访问层。
//
// 约定：常规 CRUD 用 GORM 链式 API；匹配打分那条带排序和可空条件的查询
// 必须用 db.Raw(sql, args...).Scan() 写原生 SQL —— 它有多个可空条件、
// 数组包含判断和自定义打分表达式，用链式 API 表达既难读又容易生成低效计划。
// 写 Raw 时一律用 ? 占位符传参，严禁 fmt.Sprintf 拼接。
package repo

import (
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Repo 持有数据访问所需的客户端。各领域仓储以方法或子结构挂在这里。
type Repo struct {
	DB    *gorm.DB
	Redis *redis.Client
}

func New(db *gorm.DB, rdb *redis.Client) *Repo {
	return &Repo{DB: db, Redis: rdb}
}

// Tx 在事务中执行 fn。表态结算必须走这里 —— 写 action、建 match、
// 写 outbox 要么一起成功要么一起失败。
func (r *Repo) Tx(fn func(tx *gorm.DB) error) error {
	return r.DB.Transaction(fn)
}
