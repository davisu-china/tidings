.DEFAULT_GOAL := help
SHELL := /bin/bash

BACKEND := backend
COMPOSE := docker compose -f deploy/docker-compose.yml

.PHONY: help
help: ## 显示可用命令
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- backend
.PHONY: build
build: ## 构建后端二进制到 backend/bin/
	cd $(BACKEND) && go build -o bin/api ./cmd/api

.PHONY: fmt
fmt: ## 格式化 Go 代码
	cd $(BACKEND) && gofmt -w .

.PHONY: vet
vet: ## 静态检查
	cd $(BACKEND) && go vet ./...

.PHONY: test
test: ## 跑测试
	cd $(BACKEND) && go test ./...

.PHONY: tidy
tidy: ## 整理依赖
	cd $(BACKEND) && go mod tidy

.PHONY: check
check: fmt vet test ## 提交前跑一遍

# ---------------------------------------------------------------- compose
.PHONY: up
up: ## 起本地环境（启动时自动建表）
	$(COMPOSE) up -d --build

.PHONY: down
down: ## 停掉本地环境，保留数据
	$(COMPOSE) down

.PHONY: reset
reset: ## 停掉并清空所有数据卷（不可逆）
	$(COMPOSE) down -v

.PHONY: logs
logs: ## 跟踪 api 日志
	$(COMPOSE) logs -f api

.PHONY: ps
ps: ## 查看容器状态
	$(COMPOSE) ps

.PHONY: health
health: ## 探一次健康检查
	@curl -s localhost:8081/healthz | python3 -m json.tool

.PHONY: psql
psql: ## 进数据库
	$(COMPOSE) exec postgres psql -U tidings -d tidings

.PHONY: tables
tables: ## 列出已建的表
	$(COMPOSE) exec postgres psql -U tidings -d tidings -c '\dt'

# ---------------------------------------------------------------- 验收
#
# 两个脚本都会真注册账号，于是都撞同一个单 IP 每日注册上限
# （maxRegistersPerIPPerDay，见 service/auth.go）。反复跑之前先清掉
# 那个计数，否则第二次就 429。
.PHONY: e2e-m3
e2e-m3: ## M3 验收：表态 → 成匹配 → 互发消息（走真 HTTP 与真 WebSocket）
	@$(COMPOSE) exec -T redis redis-cli DEL auth:reg:ip:142.250.99.141 >/dev/null
	cd $(BACKEND) && node scripts/e2e-m3.mjs

.PHONY: e2e-m4
e2e-m4: ## M4 验收：超时扫描 → 四类收尾通知 → 静默时段与未响应冻结
	cd $(BACKEND) && node scripts/e2e-m4.mjs

.PHONY: e2e-recall
e2e-recall: ## 召回验收：期望城市真的在过滤（种两个同省城市，等 worker 真跑几轮）
	cd $(BACKEND) && node scripts/e2e-recall.mjs

.PHONY: e2e-web
e2e-web: ## 网页验收：注册 → 建档 → 入池（需要 vite dev server 在 5173）
	cd web && node scripts/e2e-onboarding.mjs

.PHONY: e2e-settings
e2e-settings: ## 网页验收：偏好设置的读写与校验、通知与暂停（需要 vite dev server 在 5173）
	cd web && node scripts/e2e-settings.mjs

.PHONY: seed
seed: ## 灌一批合格账号进池子（引荐引擎要有足够的人才会开始配）
	cd $(BACKEND) && node scripts/seed-pool.mjs
