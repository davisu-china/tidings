#!/usr/bin/env bash
#
# 往生产环境灌一批合格账号（默认 120 个上海人）。
#
# 为什么要有这个脚本：单 IP 每日注册上限是 10 个
# （maxRegistersPerIPPerDay，见 internal/service/auth.go），而种 120 个人
# 得分 12 批，每批之间必须把限流键清掉 —— 手工做这件事的必然结果是
# 第 2 批开始一路 429，然后对着「池子只有 10 个人」的输出找半天原因。
#
# 走的还是 seed-pool.mjs 那套真接口建号（注册 → 建档 → 头像 + 3 张照片 →
# 偏好），所以这批账号的 status 是产品自己翻成 active 的。
#
# 从开发机跑，打的是公网的 HTTPS 接口；清限流键要 ssh 到服务器上做
# （redis 只绑在私有网络里，外面连不进去）。
#
# 用法：
#   backend/scripts/seed-prod.sh              # 120 个，上海
#   COUNT=20 backend/scripts/seed-prod.sh     # 先试 20 个
#   CITY=310000 SEED_PREFIX=demo backend/scripts/seed-prod.sh
#
# 前置：站点已经部署好，且 api 能连上数据库（迁移是它启动时跑的）。
set -euo pipefail

REMOTE="${REMOTE:-jdcloud}"
BASE="${SEED_BASE:-https://tidings.jianjiange.site}"
CITY="${CITY:-310000}"
COUNT="${COUNT:-120}"
BATCH="${BATCH:-10}"
PREFIX="${SEED_PREFIX:-pool}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 注册限流键。清的是整个 auth:reg:ip:* 前缀 —— 种号期间没有真实用户在
# 注册，清掉不影响任何人；不清就连第二批都过不去。
#
# 清完必须回读确认：这个函数失败的样子（键还在）与成功的样子一模一样，
# 而后果是下一批直接 429，报出来的错却是「注册失败」。
clear_limit() {
  ssh "$REMOTE" "docker exec tidings-redis sh -c \"redis-cli --scan --pattern 'auth:reg:ip:*' | xargs -r redis-cli del\"" >/dev/null
  local left
  left=$(ssh "$REMOTE" "docker exec tidings-redis redis-cli --scan --pattern 'auth:reg:ip:*' | wc -l")
  if [ "$left" != "0" ]; then
    echo "✗ 注册限流键没清干净（还剩 $left 个），下一批会 429" >&2
    exit 1
  fi
}

# 探活走公开的 VAPID 公钥接口而不是 /healthz：/healthz 暴露的是
# 「postgres / redis / minio 各自通不通」，那是内部信息，宿主机上的共享
# nginx 没有把它转出去（前端容器的 SPA 回落会把它变成一张 200 的 HTML）。
# 这一条要登录，也不需要 —— 但它能证明 nginx → api 这一整条链是通的。
if ! curl -fsS -o /dev/null --max-time 10 "$BASE/api/v1/push/public-key"; then
  echo "✗ $BASE/api/v1/push/public-key 不通，先确认站点起来了" >&2
  exit 1
fi

# 变量一律加花括号：$CITY，这种写法里 bash 会把全角逗号的首字节当成变量名的
# 一部分，报 "CITY: unbound variable" 然后当场退出（set -u）。
echo "往 $BASE 种 $COUNT 个账号，城市 ${CITY}，每批 ${BATCH} 个"
done_total=0
for ((offset = 0; offset < COUNT; offset += BATCH)); do
  n=$((COUNT - offset < BATCH ? COUNT - offset : BATCH))
  echo ""
  echo "— 第 $((offset / BATCH + 1)) 批：序号 $offset..$((offset + n - 1))"
  clear_limit
  if ! SEED_BASE="$BASE" SEED_CITY="$CITY" SEED_PREFIX="$PREFIX" SEED_OFFSET="$offset" \
    node "$HERE/seed-pool.mjs" "$n"; then
    echo "  这批有失败（见上），继续下一批" >&2
  fi
  done_total=$((done_total + n))
done

echo ""
echo "共请求 $done_total 个。核对池子规模（必须 ≥ INTRO_POOL_MIN，否则整座城市被跳过）："
ssh "$REMOTE" "docker exec tidings-postgres psql -U tidings -d tidings -tAc \"
  SELECT p.city_code, count(*)
  FROM profiles p JOIN users u ON u.id = p.user_id
  WHERE u.status = 'active' AND p.gender IS NOT NULL
  GROUP BY p.city_code ORDER BY 2 DESC\""
