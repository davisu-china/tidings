package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type healthReport struct {
	Status   string            `json:"status"`
	Checks   map[string]string `json:"checks"`
	Duration string            `json:"duration"`
}

// Healthz 逐项探活 Postgres / Redis / MinIO。
// 任一项不通就返回 503，让编排层能正确判断容器是否就绪。
func (h *Handler) Healthz(c *gin.Context) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{
		"postgres": check(func() error {
			sqlDB, err := h.svc.Repo.DB.DB()
			if err != nil {
				return err
			}
			return sqlDB.PingContext(ctx)
		}),
		"redis": check(func() error {
			return h.svc.Repo.Redis.Ping(ctx).Err()
		}),
		"minio": check(func() error {
			return h.svc.Storage.Ping(ctx)
		}),
	}

	status := http.StatusOK
	overall := "ok"
	for _, v := range checks {
		if v != "ok" {
			status = http.StatusServiceUnavailable
			overall = "degraded"
			break
		}
	}

	c.JSON(status, healthReport{
		Status:   overall,
		Checks:   checks,
		Duration: time.Since(start).String(),
	})
}

func check(fn func() error) string {
	if err := fn(); err != nil {
		return err.Error()
	}
	return "ok"
}
