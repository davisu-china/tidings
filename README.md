# Tidings · 有信

> 不必天天来。合适的人出现时，我们会给你写信。

主动引荐式严肃婚恋 Web 产品。不做划卡信息流：用户填一次档案与偏好，系统在后台持续找人，发现双方互相适配的组合时，用浏览器推送（邮件兜底）通知双方回来看。

## 文档

- [产品需求文档](docs/prd.html) — v0.7。产品定位、成对引荐机制、建档字段字典、偏好双层结构、AI 推荐、通知双通道、安全风控、指标与版本规划。用浏览器打开阅读。
- [MVP 产品需求与技术方案](docs/mvp-prd-and-tech-design.html) — v1.2。完整版 PRD 的可交付子集，加上细化到可开工的技术方案（数据模型 DDL、匹配引擎、引荐生命周期、Web Push、API 清单、里程碑），以及前端设计系统「信纸体系」。

## 已确认的关键决策

| # | 决策 |
| --- | --- |
| 01 | 池子不足时接受**单向引荐**降级；低于 100 人暂停自动引荐并向用户明示 |
| 02 | 用户可自行调节**严格程度**（稳妥 / 均衡 / 多看看），只调阈值不破硬条件 |
| 03 | 暂停接收引荐期间**仍可被别人看到**，相关引荐冻结计时，恢复后重新计时 |
| 04 | **不引入邀请机制**，开放注册是唯一入口 |
| 05 | 首站城市为**上海** |
| 06 | **补邮件**作为第二触达渠道，推送优先、邮件兜底，同一条引荐只计一次频次 |
| 07 | 「不合适」的原因**必填**，三个一键选项；「想认识」路径保持零摩擦 |
| 08 | 品牌名 **Tidings**，中文只用「有信」，英文名不参与中文口语传播 |
| 09 | **早期不设推送次数上限**，频次由够格的组合数决定；用户可自行收紧。次数上限留到商业化阶段作为付费权益引入 |
| 10 | 各节草案数值沿用；指标参考线待 V1 真实数据回归校准 |
| 11 | **年收入对外展示**，且只出区间不出具体数字，同时参与匹配打分 |
| 12 | **删除宗教信仰字段**（中国大陆严肃婚恋场景区分度低）；饮食习惯保留「清真」「素食」 |
| 13 | **成对引荐分阶段**：池子未越过阈值时允许单向降级，越过阈值后必须回到严格成对 |
| 14 | **频次上限与保底都不设**，完全由够格的组合数决定；原「每周保底 1 次单向引荐」删除 |
| 15 | **推荐理由分阶段**：V1 不设理由位、不要求理由；AI 上线后转为必需项 |
| 16 | **完整度只做门槛、不参与打分，且刻意不对称** —— 只入池的用户能收引荐、不会被引荐 |
| 17 | **引荐终结时双方都告知**：未回应方收「你错过了」，等待方收中性收尾；被拒绝时等待方立即收同一套措辞 |
| 18 | **1.3「不做焦虑营销」留一个例外** —— 引荐终结时的事实告知；攀比类提示仍在禁止之列 |
| 19 | **登录标识用邮箱 + 密码**（bcrypt），不收短信、不发验证码。代价认账：邮箱无法验证，批量注册比手机号便宜得多；忘记密码在 MVP 期间只能由运营手动重置 |

## 尚未定稿

产品需求文档把未定事项全部集中在 **第 14 节「待确认」**，当前 **11 项**（原 14.1 的五条阻塞项已全部拍板）：

| 分组 | 条目 | 性质 |
| --- | --- | --- |
| 14.1 阻塞项 | 0 | 已全部确认，移入第 13 节 |
| 14.2 产品立场与范围 | 4 | 文档里替你做、但你还没表态的判断 |
| 14.3 待准备 | 5 | 不是选择题，是开工前必须就位的东西 |
| 14.4 待数据校准 | 2 | 等 V1 真实数据才能定 |

**条目一经确认即从第 14 节删除，并入第 13 节「已确认决策」。** 该节清零后，进入技术方案阶段。页面顶部与第 13 节的计数会随条目增删自动更新。

## MVP 待确认

MVP 文档范围内另有 **2 项**，集中在它的第 23 节。文档已按建议值写就，但**不代表已获同意**：

| # | 待确认 | 建议值 |
| --- | --- | --- |
| 01 | 忘记密码只能找运营，这个代价接不接受 | 接受，M1 提供 `ADMIN_TOKEN` 重置接口；V1.1 有邮件通道后再做自助找回 |
| 02 | 会话形态 | 复用 `stubborn-love` 已验证的 WebSocket hub + Redis Pub/Sub，做站内实时文字会话 |

原第 01 项「短信通道怎么接」已随邮箱 + 密码的决策（决策 19）作废 —— 没有短信，就没有签名与模板报备，排期上最可能卡住的那一项整个消失。

## 状态

- 产品需求：定位、机制、字段字典、待确认清单均已成文
- 技术方案：MVP 版已成文，细化到 DDL 与 API 清单
  - 后端：Go 1.26 + PostgreSQL 16 + Redis 7 + MinIO
  - 前端：React 19 + TypeScript + Tailwind v4 + shadcn/ui，PWA（`vite-plugin-pwa`）
  - 视觉：信纸体系（米白纸感 / 墨绿 / 印章红 / 思源宋体），与 PRD 文档同一套设计 token
- 登录注册：**邮箱 + 密码**（bcrypt 代价因子 12），JWT 双 token。无验证码、无自助找回、无邮箱验证 —— MVP 只有 Web Push 一条外发通道。忘记密码走后台重置
- 代码：**M0（地基）与 M1（能注册能建档）已写完**，`gofmt` / `go vet` / `go build` / `go test` 均通过
  - M0：`make up` 后 `/healthz` 的三个依赖都是 ok
  - M1：真机走完「注册 → 建档 → 传头像与照片 → `status` 翻 `active`」；
    后台重置密码后旧 token 立即失效、新密码能登录（`web/scripts/e2e-*.mjs` 的冒烟断言全通过；
    入池只要求 1 张照片，`status` 在第 1 张落库时就翻）

## 本地启动

```bash
cp deploy/.env.example deploy/.env    # 首次
make up                               # 起 PG / Redis / MinIO / api，启动时自动建表
make health                           # curl localhost:8081/healthz
make logs
make down
```

| 服务 | host 端口 |
| --- | --- |
| api | 8081 |
| PostgreSQL | 5434 |
| Redis | 6380 |
| MinIO S3 API / 控制台 | 9200 / 9201 |
| 图片入口（nginx 反代 MinIO） | 8093 |

> 若 5434 已被别的容器占用，可以只放掉这个宿主机端口映射 —— 容器之间
> 走 compose 内网，冒烟测试不需要它：
> `docker compose -f deploy/docker-compose.yml -f <override> up -d`，
> override 里写 `services.postgres.ports: !reset []`。

端口一律避开 `stubborn-love`（那套占 5432 / 6379 / 8090 / 9100），两套可以同时开。

### 前端待办的第一件事

iOS 上 Safari 普通标签页收不到 Web Push，**只有把站点「添加到主屏幕」之后才行**（iOS 16.4+）。对一个推送即产品的应用，安装引导是触达链路的必需环节 —— 见 MVP 文档 19.5。

## 部署（生产）

站点：**https://tidings.jianjiange.site**（京东云 111.228.14.136，与不将就、jianjiange.site 同一台机器）

```bash
deploy/deploy.sh          # 或 make deploy-prod
make seed-prod            # 灌池子：默认 120 个上海人，分批绕开单 IP 注册上限
```

`deploy.sh` 做四件事，全部幂等：

1. rsync 源码到 `/opt/tidings`（服务器上那份 `deploy/.env` 不动）
2. 首次生成 `.env`：`JWT_SECRET` / `ADMIN_TOKEN` / 数据库与 MinIO 口令一律 `openssl` 级别随机，
   VAPID 密钥对现场生成 —— `.env.example` 里的占位值都公开在仓库里，带着它们上线等于没有鉴权
3. `docker compose -f docker-compose.prod.yml up -d --build`，等 api 健康
4. 签证书（复用宿主机共享 nginx 的 certbot webroot），把 `deploy/nginx/tidings.conf`
   那段 vhost 合进 `/opt/jianjian/deploy/nginx/nginx.conf` 并 reload

生产编排与本地那份是**两份独立文件**（`docker-compose.prod.yml`），不是 override：
compose 的 `ports` 是追加语义，override 去不掉本地那份里已发布的 5434/6380/9200 ——
那等于把数据库和 MinIO 控制台挂到公网。生产这份里：

- 宿主机端口一律绑 `127.0.0.1`（web 8181 / api 8182 / media 8183），公网只能经共享 nginx 进来
- 只有 web / api / media 接外部网络 `dev-ops_my-network`（nginx 按容器名反代），
  postgres / redis / minio **只接本站私有网络** —— 它们的服务名会变成网络别名，
  一旦出现在共享网络上，别的站解析到的 `postgres` / `redis` 就被顶掉了
- 域名相关的四项（`APP_ENV` / `MEDIA_BASE_URL` / `MINIO_PUBLIC_ENDPOINT` / `MINIO_PUBLIC_USE_SSL`）
  写死在编排里，不指望 `.env` 抄对：抄漏一项的症状是「照片传上去了但显示不出来」

改共享 nginx 的两个坑：

- `/opt/jianjian/deploy/nginx/nginx.conf` 在容器里是**只读文件挂载**。必须原地覆盖
  （`cat new > old` / `cp`），`sed -i` 会换 inode，容器读到的还是旧文件 —— 表现成「改了没生效」
- 证书没签出来之前**不要**把 vhost 合进去：那段配置引用证书文件，`nginx -t` 会失败，
  一 reload 就把同机器上所有站点一起弄挂。`deploy.sh` 因此把这一步挡在证书检查之后

DNS 是外部依赖：`tidings.jianjiange.site` 需要一条 A 记录指向 111.228.14.136
（Cloudflare，代理状态 **DNS only**）。没这条记录时证书签不出来，站点停在 HTTP，
`deploy.sh` 会明确提示，DNS 生效后重跑即可。

## 目录结构

```
docs/          产品文档
backend/       Go 服务（cmd/api、cmd/worker，internal/{config,handler,repo,service}，migrations）
deploy/        docker-compose（dev 与 prod 两份）、nginx、deploy.sh、.env.example
web/           前端（React 19 + Tailwind v4 + PWA，Dockerfile 构建静态产物）
Makefile
```
