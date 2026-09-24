# 前端镜像：Node 构建 → nginx 只放静态产物。
#
# 生产环境不经过 vite（没有 dev server、没有代理）——SPA 与 API 同域，
# /api 与 /img 由宿主机的共享 nginx 直接转给 api 与 media 容器。
# 所以构建产物与域名无关：代码里的 API_BASE 是写死的 /api/v1。

# ---- build ----
FROM node:20-alpine AS build

WORKDIR /app

# 构建发生在国内服务器上，默认走镜像源；换源只影响下载速度，
# 不影响产物（package-lock.json 锁的是版本）。
ARG NPM_REGISTRY=https://registry.npmmirror.com
RUN npm config set registry "$NPM_REGISTRY"

# 先只拷清单，让 npm ci 这一层能被缓存
COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

# ---- runtime ----
FROM nginx:1.27-alpine

# 同名的 default.conf 顶掉镜像自带那份（它 server_name 是 localhost）
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

# 容器内 80，宿主端口由 compose 绑在 127.0.0.1 上，公网只能经共享 nginx 进来
EXPOSE 80
