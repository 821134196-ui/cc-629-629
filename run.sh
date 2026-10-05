#!/usr/bin/env bash
# 社区老人助餐资格变更与餐补结算系统 —— 一条命令启动
# 用法:
#   ./run.sh           安装依赖、构建、播种演示数据（仅首次）并启动服务
#   ./run.sh verify    构建并运行 40 项端到端业务规则验证
#   ./run.sh reset     清空数据库后重新启动
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
BACKEND="$ROOT/backend"
FRONTEND="$ROOT/frontend"

# 兼容已安装 go 或用户目录下安装的 go
if command -v go >/dev/null 2>&1; then
  GO=go
elif [ -x "$HOME/sdk/go/bin/go" ]; then
  export PATH="$HOME/sdk/go/bin:$PATH"
  GO="$HOME/sdk/go/bin/go"
else
  echo "[1/5] 未找到 Go，自动安装 Go 1.23 到 \$HOME/sdk/go ..."
  mkdir -p "$HOME/sdk"
  curl -fsSL https://go.dev/dl/go1.23.12.linux-amd64.tar.gz | tar xz -C "$HOME/sdk"
  export PATH="$HOME/sdk/go/bin:$PATH"
  GO="$HOME/sdk/go/bin/go"
fi

cd "$BACKEND"
echo "[1/5] 拉取后端依赖并编译 ..."
$GO mod download
$GO build -o "$BACKEND/bin/meals" .

if [ "${1:-}" = "verify" ]; then
  echo "[verify] 运行端到端业务规则验证（独立临时库，不影响演示数据）..."
  GIN_MODE=release "$BACKEND/bin/meals" verify
  exit 0
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "错误：需要 Node.js/npm 构建前端，未检测到 npm。" >&2
  exit 1
fi

echo "[2/5] 构建 Vue 前端（产物由 Go 服务托管）..."
cd "$FRONTEND"
[ -d node_modules ] || npm install --no-audit --no-fund
npm run build

echo "[3/5] 数据库准备（SQLite，位于 backend/data/meals.db）..."
cd "$BACKEND"
if [ "${1:-}" = "reset" ]; then
  rm -f data/meals.db data/meals.db-wal data/meals.db-shm
fi
./bin/meals seed || true

echo "[4/5] 启动服务 ..."
echo "[5/5] 访问地址："
echo "      前端页面  http://localhost:8080/"
echo "      API 列表  http://localhost:8080/api/elders"
echo ""
echo "演示卡号：CARD-0001（张桂兰，跨月暂停/恢复）/ CARD-0002（李建国）/ CARD-0003（王秀珍）"
echo "按 Ctrl+C 停止。"
exec ./bin/meals
