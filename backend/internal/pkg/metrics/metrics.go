// Package metrics 定义 Prometheus 指标。
//
// 只定义，不负责采集细节：HTTP 中间件记延迟与状态码，
// worker 与业务侧在关键路径上调用 Record* 记业务量。
//
// 刻意只埋 MVP 判据（见 §1.1）用得上的那几个。完整版 PRD 的指标
// 不埋 —— 对着没有依据的参考线做优化是浪费。
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP 请求。path 必须先归一化（把 id 段换成 :id），否则基数爆炸。
	HTTPRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP 请求总数，按方法与路径",
		},
		[]string{"method", "path", "status"},
	)
	HTTPLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP 请求耗时",
			Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"method", "path"},
	)

	// 引荐生成。kind 为 paired / oneway —— 单向占比长期偏高，
	// 说明池子还没攒起来，处在降级档位上。
	IntroGenerated = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "intro_generated_total",
			Help: "生成的引荐条数，按成对/单向分",
		},
		[]string{"kind"},
	)

	// 通知投递。template 为四类收尾通知之一。
	IntroDelivered = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "intro_delivered_total",
			Help: "通知投递量，按模板分",
		},
		[]string{"template"},
	)

	// 主判据的分子：至少一方表过态的引荐数。
	IntroResponded = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "intro_responded_total",
			Help: "表态数，按动作分（like / pass）",
		},
		[]string{"action"},
	)

	IntroExpired = promauto.NewCounter(
		prometheus.CounterOpts{Name: "intro_expired_total", Help: "超时释放的引荐数"},
	)
	MatchesCreated = promauto.NewCounter(
		prometheus.CounterOpts{Name: "matches_created_total", Help: "新建匹配数"},
	)

	// 推送投递结果。gone 持续上升说明订阅在批量失效
	// （用户清了浏览器数据、卸载了 PWA），那时该回头看安装引导。
	PushSend = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "push_send_total",
			Help: "推送投递结果",
		},
		[]string{"result"}, // ok / gone / failed
	)

	// 候选集查询耗时。池子涨了要先看它。
	MatchQueryLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "match_query_duration_seconds",
			Help:    "候选集查询耗时",
			Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5},
		},
	)
)

// 推送投递结果的三个取值。
const (
	PushResultOK     = "ok"
	PushResultGone   = "gone"   // 订阅已失效（404 / 410），应禁用该订阅
	PushResultFailed = "failed" // 其他失败，可重试
)

// RecordPush 记录一次推送投递结果。
func RecordPush(result string) {
	PushSend.WithLabelValues(result).Inc()
}

// 表态动作的两个取值。
const (
	ActionLike = "like"
	ActionPass = "pass"
)

// RecordResponse 记录一次表态。主判据的分子。
func RecordResponse(action string) {
	IntroResponded.WithLabelValues(action).Inc()
}
