# ---- build ----
# 基础镜像版本必须 >= go.mod 里的 go 指令（当前 1.26.0，由 golang.org/x/crypto
# 及其拉起的 x/sys、x/text 顶上来的）。容器里 GOTOOLCHAIN=local，
# 不会像本地那样自动下载新工具链，版本对不上会直接构建失败。
FROM golang:1.26-alpine AS build

WORKDIR /src

# 国内服务器直连 proxy.golang.org 不通，构建时用 --build-arg GOPROXY 覆盖，
# 见 deploy/docker-compose.prod.yml。
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

# 先只拷依赖清单，让 go mod download 这层能被缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 构建哪个二进制由 TARGET 决定。M0 只有 api，
# worker 在 M2（引荐生成任务）时加入。
ARG TARGET=api
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/app ./cmd/${TARGET}

# ---- runtime ----
FROM alpine:3.20

# ca-certificates 走外网 HTTPS 要用（短信、Web Push 端点都是 HTTPS）；
# tzdata 是静默时段按 Asia/Shanghai 判断的前提
RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 10001 app

COPY --from=build /out/app /usr/local/bin/app

USER app
ENTRYPOINT ["/usr/local/bin/app"]
