#!/usr/bin/env bash
# 一条命令启动：构建 Vue → 构建 Go（内嵌前端）→ 启动服务。
# 用法：./run.sh            监听 :8080，SQLite 存于 backend/data/
#       ADDR=:9090 ./run.sh
#       NO_SEED=1 ./run.sh  不写入演示数据
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND="$ROOT/backend"
WEB="$BACKEND/web-ui"

# 兼容 Go 装在用户目录的情况
if ! command -v go >/dev/null 2>&1 && [ -x "$HOME/.local/go/bin/go" ]; then
  export PATH="$HOME/.local/go/bin:$PATH"
fi

echo "==> [1/4] 检查工具链"
command -v go >/dev/null || { echo "未找到 Go（需要 1.22+）"; exit 1; }
command -v node >/dev/null || { echo "未找到 Node.js（需要 18+）"; exit 1; }
go version

echo "==> [2/4] 构建前端（Vue 3 + Vite）"
cd "$WEB"
[ -d node_modules ] || npm install
npm run build          # 产物输出到 backend/cmd/server/web，随 Go 二进制内嵌

echo "==> [3/4] 构建后端（Go + Gin，CGO 启用 SQLite）"
cd "$BACKEND"
export CGO_ENABLED=1
mkdir -p bin data
go build -o bin/server ./cmd/server

echo "==> [4/4] 启动服务"
cd "$ROOT"
ADDR="${ADDR:-:8080}" DB_PATH="${DB_PATH:-$BACKEND/data/mealsubsidy.db}" exec "$BACKEND/bin/server"
