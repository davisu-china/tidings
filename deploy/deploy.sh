#!/usr/bin/env bash
#
# 把《有信》部署到 tidings.jianjiange.site。
#
# 从开发机上跑，它做四件事：
#   1. rsync 源码到 /opt/tidings（服务器上那份 .env 不动）
#   2. 首次生成 deploy/.env（随机密钥 + 现场生成的 VAPID 密钥对）
#   3. docker compose -f docker-compose.prod.yml up -d --build，等 api 健康
#   4. 签证书 + 把 deploy/nginx/tidings.conf 那段 vhost 合进共享 nginx 并 reload
#
# 第 4 步"签证书"依赖 DNS 已经指过来（tidings.jianjiange.site → 这台机器）。
# 没指过来时证书签不出来，脚本**不会**把 vhost 合进去 —— 那段配置引用了
# 证书文件，合进去 nginx -t 直接失败，reload 会把所有站点一起弄挂。
# 这时站点停在 HTTP，等 DNS 生效后重跑一次这个脚本即可。
#
# 服务器上的家法见 /opt/jianjian 与 /opt/bujiangjiu：共享 nginx 容器
# （jianjian-nginx，80/443）按子域分发，各站容器挂在外部网络
# dev-ops_my-network 上让它按容器名反代。
#
# 用法：
#   deploy/deploy.sh                 # 全套
#   SKIP_BUILD=1 deploy/deploy.sh    # 只补证书与 vhost，不重建镜像、不动 .env
#
# 可覆盖：REMOTE DIR DOMAIN CERTBOT_DIR NGINX_CONF NGINX_CONTAINER
#         LE_EMAIL SKIP_BUILD
set -euo pipefail

REMOTE="${REMOTE:-jdcloud}"                # ~/.ssh/config 里的别名
DIR="${DIR:-/opt/tidings}"
DOMAIN="${DOMAIN:-tidings.jianjiange.site}"
CERTBOT_DIR="${CERTBOT_DIR:-/opt/jianjian/deploy/certbot}"
NGINX_CONF="${NGINX_CONF:-/opt/jianjian/deploy/nginx/nginx.conf}"
NGINX_CONTAINER="${NGINX_CONTAINER:-jianjian-nginx}"
LE_EMAIL="${LE_EMAIL:-898168605@qq.com}"
SKIP_BUILD="${SKIP_BUILD:-0}"

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

log() { printf '\n\033[36m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[33m! %s\033[0m\n' "$*" >&2; }
die() { printf '\033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- 1. 同步源码

log "同步源码 → $REMOTE:$DIR"
ssh "$REMOTE" "mkdir -p '$DIR'"
rsync -az --delete \
  --exclude '.git' \
  --exclude 'node_modules' \
  --exclude 'web/dist' \
  --exclude 'backend/bin' \
  --exclude 'deploy/.env' \
  --exclude '.DS_Store' \
  --exclude '*.tsbuildinfo' \
  "$REPO/" "$REMOTE:$DIR/"
echo "  ✓ 已同步"

# ---------------------------------------------------------------- 2. 环境与容器

log "准备 .env"
ssh "$REMOTE" "DIR='$DIR' DOMAIN='$DOMAIN' LE_EMAIL='$LE_EMAIL' bash -s" <<'REMOTE_ENV'
set -euo pipefail
ENV="$DIR/deploy/.env"
EXAMPLE="$DIR/deploy/.env.example"
[ -f "$EXAMPLE" ] || { echo "✗ 缺 $EXAMPLE" >&2; exit 1; }

python3 - "$ENV" "$EXAMPLE" "$DOMAIN" "$LE_EMAIL" <<'PY'
import pathlib, re, secrets, subprocess, sys

env_path, example_path, domain, le_email = sys.argv[1:5]
env = pathlib.Path(env_path)
fresh = not env.exists()
text = pathlib.Path(example_path).read_text() if fresh else env.read_text()

def set_var(text, key, value):
    pat = re.compile(rf'(?m)^{re.escape(key)}=.*$')
    if pat.search(text):
        return pat.sub(lambda m: f'{key}={value}', text, count=1)
    return text.rstrip('\n') + f'\n{key}={value}\n'

def get_var(text, key):
    m = re.search(rf'(?m)^{re.escape(key)}=(.*)$', text)
    return None if m is None else m.group(1)

# 域名相关的四项：不管 .env 是新建还是已有，一律按当前部署写死。
# 它们错的样子都跟配置无关 —— MINIO_PUBLIC_ENDPOINT 指向 localhost 时，
# 症状是「照片传上去了但显示不出来」。
forced = {
    'APP_ENV': 'production',
    'MEDIA_BASE_URL': f'https://{domain}',
    'MINIO_PUBLIC_ENDPOINT': domain,
    'MINIO_PUBLIC_USE_SSL': 'true',
    'VAPID_SUBJECT': f'mailto:{le_email}',
}
notes = []
for k, v in forced.items():
    if get_var(text, k) != v:
        notes.append(f'{k}={v}')
        text = set_var(text, k, v)

if fresh:
    # 密钥现场生成。.env.example 里的占位值都公开在仓库里：
    # JWT_SECRET 那个占位串长度是够的、能一路通过校验 —— 带着它上线
    # 等于任何人都能自己签一个管理员 token；ADMIN_TOKEN=dev_admin_token
    # 则等于谁都能重置任何人的密码。
    for k in ('JWT_SECRET', 'POSTGRES_PASSWORD', 'MINIO_SECRET_KEY', 'ADMIN_TOKEN'):
        text = set_var(text, k, secrets.token_urlsafe(48))

    # VAPID 密钥对：推送是唯一的触达通道，缺了它 config.validate 会在
    # APP_ENV=production 时直接拒绝启动（这一步失败就没人能收到信）。
    def gen(cmd):
        # 服务器上是 python 3.6：capture_output / text 这两个关键字要 3.7+，
        # 用等价的旧写法，免得部署脚本自己先死在语法糖上。
        out = subprocess.run(cmd, shell=True,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                             universal_newlines=True)
        blob = out.stdout + out.stderr
        pub = re.search(r'"?publicKey"?\s*:\s*"([^"]+)"', blob)
        priv = re.search(r'"?privateKey"?\s*:\s*"([^"]+)"', blob)
        return (pub.group(1), priv.group(1)) if pub and priv else None

    keys = gen('npx --yes web-push generate-vapid-keys --json') \
        or gen('docker run --rm node:20-alpine npx --yes web-push generate-vapid-keys --json')
    if not keys:
        sys.exit('✗ VAPID 密钥生成失败（npx 与 docker 两条路都不通）')
    text = set_var(text, 'VAPID_PUBLIC_KEY', keys[0])
    text = set_var(text, 'VAPID_PRIVATE_KEY', keys[1])

env.write_text(text)
print(('  新建 ' if fresh else '  沿用 ') + env_path + ('（密钥随机生成）' if fresh else ''))
for n in notes:
    print(f'    {n}')
PY

chmod 600 "$ENV"
echo "  ✓ .env 就绪"
REMOTE_ENV

if [ "$SKIP_BUILD" = "1" ]; then
  log "SKIP_BUILD=1，跳过构建"
else
  log "构建并启动容器（首次比较久：Go 依赖 + npm ci）"
  ssh "$REMOTE" "cd '$DIR/deploy' && docker compose -f docker-compose.prod.yml up -d --build" \
    || die "compose 启动失败：docker compose -f $DIR/deploy/docker-compose.prod.yml logs"
fi

log "等 api 健康"
ssh "$REMOTE" "bash -s" <<'REMOTE_HEALTH'
set -euo pipefail
for _ in $(seq 1 40); do
  if docker exec tidings-api wget -qO- http://localhost:8080/healthz 2>/dev/null | tee /tmp/tidings-health.json; then
    echo "  ✓ api 健康"
    exit 0
  fi
  sleep 3
done
echo "✗ api 40 次探测仍未就绪，最近日志：" >&2
docker logs --tail 40 tidings-api >&2
exit 1
REMOTE_HEALTH

# ---------------------------------------------------------------- 3. 证书

log "证书"
# 签不出来不算失败：DNS 还没指过来时这是预期结果，下一步会给出明确提示。
# 这里要是让 set -e 把脚本带走，看到的就是一段 certbot 的原始报错，
# 而不是「DNS 没生效」这句人话。
CERT_RC=0
ssh "$REMOTE" "DOMAIN='$DOMAIN' CERTBOT_DIR='$CERTBOT_DIR' LE_EMAIL='$LE_EMAIL' bash -s" <<'REMOTE_CERT' || CERT_RC=$?
set -euo pipefail
LIVE="$CERTBOT_DIR/conf/live/$DOMAIN"
if [ -f "$LIVE/fullchain.pem" ]; then
  echo "  ✓ 已有证书：$LIVE"
  exit 0
fi
# 80 端口的 default_server 已经在服务 /.well-known/acme-challenge/，
# 所以不用先把 vhost 加进去就能签。
docker run --rm \
  -v "$CERTBOT_DIR/conf:/etc/letsencrypt" \
  -v "$CERTBOT_DIR/www:/var/www/certbot" \
  certbot/certbot:latest \
  certonly --webroot -w /var/www/certbot \
  --email "$LE_EMAIL" --agree-tos --no-eff-email \
  --keep-until-expiring -d "$DOMAIN"
REMOTE_CERT
# 花括号别省：$CERT_RC）这种写法里 bash 会把全角括号的首字节吞进变量名，
# 而这一行恰恰只在证书签失败时才执行 —— 到那一步它自己先崩，
# 该说的「DNS 还没指过来」就永远说不出来。
[ "$CERT_RC" = "0" ] || warn "证书没签出来（退出码 ${CERT_RC}），多半是 DNS 还没指过来"

# ---------------------------------------------------------------- 4. vhost

log "把 vhost 合进共享 nginx"
NGINX_RC=0
ssh "$REMOTE" "DOMAIN='$DOMAIN' NGINX_CONF='$NGINX_CONF' NGINX_CONTAINER='$NGINX_CONTAINER' DIR='$DIR' CERTBOT_DIR='$CERTBOT_DIR' bash -s" <<'REMOTE_NGINX' || NGINX_RC=$?
set -euo pipefail
LIVE="$CERTBOT_DIR/conf/live/$DOMAIN"
if [ ! -f "$LIVE/fullchain.pem" ]; then
  echo "! 证书还没签出来（多半是 DNS 还没指过来），本次不合并 vhost。" >&2
  echo "  那段配置引用证书文件，合进去 nginx -t 会失败，reload 会连带弄挂其他站点。" >&2
  echo "  DNS 生效后重跑 deploy/deploy.sh 即可。" >&2
  exit 3
fi

STAMP=$(date +%Y%m%d-%H%M%S)
cp "$NGINX_CONF" "$NGINX_CONF.bak-tidings-$STAMP"
echo "  已备份 → $(basename "$NGINX_CONF").bak-tidings-$STAMP"

# 用 python 就地改写：这个文件在容器里是只读**文件**挂载，sed -i 会换
# inode（临时文件 + rename），容器读到的还是旧文件，表现成「改了没生效」。
# open('w') 是截断写回原 inode，容器能看到。
python3 - "$NGINX_CONF" "$DIR/deploy/nginx/tidings.conf" <<'PY'
import pathlib, re, sys
conf_path, block_path = sys.argv[1], sys.argv[2]
conf = pathlib.Path(conf_path)
text = conf.read_text()
block = pathlib.Path(block_path).read_text()
start = '# ===== 有信 tidings.jianjiange.site =====\n'
end = '# ===== /有信 =====\n'
if start in text:
    text = re.sub(re.escape(start) + '.*?' + re.escape(end),
                  lambda m: start + block + end, text, flags=re.S)
    print('  已替换原有 vhost')
else:
    idx = text.rstrip().rfind('\n}')   # http {} 的收尾大括号
    if idx < 0:
        sys.exit('✗ 找不到 http 块的收尾括号')
    text = text[:idx] + '\n' + start + block + end + text[idx:]
    print('  已插入 vhost')
conf.write_text(text)
PY

if ! docker exec "$NGINX_CONTAINER" nginx -t; then
  cp "$NGINX_CONF.bak-tidings-$STAMP" "$NGINX_CONF"
  echo "✗ nginx -t 失败，已回滚到备份（其他站点不受影响）" >&2
  exit 1
fi
docker exec "$NGINX_CONTAINER" nginx -s reload
echo "  ✓ nginx 已 reload"
REMOTE_NGINX

if [ "$NGINX_RC" = "3" ]; then
  warn "站点还停在 HTTP（证书未签发）。DNS 生效后重跑本脚本。"
elif [ "$NGINX_RC" != "0" ]; then
  die "合 vhost 失败，见上面的输出"
else
  log "完成"
  echo "  https://$DOMAIN/"
  echo "  日志：ssh $REMOTE 'docker logs -f tidings-api'"
fi
