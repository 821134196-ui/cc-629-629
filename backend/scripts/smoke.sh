#!/usr/bin/env bash
# 端到端冒烟脚本：临时库启动 → 走通跨月资格、幂等核销、封账前重算、
# 封账后补差单与对账 → 关闭服务并清理。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"   # backend/
PORT="${PORT:-18099}"
BASE="http://127.0.0.1:$PORT"
DBDIR="$(mktemp -d)"
DB="$DBDIR/smoke.db"
BIN="$ROOT/bin/smoke-server"

if [ -x "$HOME/.local/go/bin/go" ]; then export PATH="$HOME/.local/go/bin:$PATH"; fi
export CGO_ENABLED=1 NO_SEED=1 ADDR=":$PORT" DB_PATH="$DB"

cd "$ROOT"
go build -o "$BIN" ./cmd/server
"$BIN" >/tmp/smoke-server.log 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null || true; rm -rf "$DBDIR" "$BIN"' EXIT

for _ in $(seq 1 50); do
  curl -sf "$BASE/api/seniors" >/dev/null && break
  sleep 0.2
done

# 用 Python 统一发请求/断言，规避 shell 引号问题
python3 - "$BASE" <<'PY'
import json, sys, urllib.request, urllib.error

BASE = sys.argv[1]

def call(method, path, body=None, expect=(200, 201)):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method,
                                 headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req) as r:
            code, payload = r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        code, payload = e.code, json.loads(e.read() or b"{}")
    assert code in expect, f"{method} {path} -> {code} {payload} (期望 {expect})"
    return payload

print("--- 登记老人与跨月资格")
sid = call("POST", "/api/seniors", {"name": "王秀兰", "card_no": "C100", "community": "阳光社区"})["id"]
call("POST", f"/api/seniors/{sid}/eligibility",
     {"effective_date": "2026-09-01", "status": "active", "level_name": "普通", "subsidy_fen": 800})
rid_suspend = call("POST", f"/api/seniors/{sid}/eligibility",
     {"effective_date": "2026-10-10", "status": "suspended"})["id"]
call("POST", f"/api/seniors/{sid}/eligibility",
     {"effective_date": "2026-10-20", "status": "active", "level_name": "高龄", "subsidy_fen": 1000})

print("--- 按就餐日期核销（跨暂停/恢复）")
def verify(date, typ="午餐", price=1500):
    return call("POST", "/api/canteen/verify",
                {"senior_id": sid, "meal_date": date, "meal_type": typ, "price_fen": price})
a = verify("2026-10-09")["meal"]["subsidy_fen"]
b = verify("2026-10-15")["meal"]["subsidy_fen"]
c = verify("2026-10-21")["meal"]["subsidy_fen"]
print(f"  10-09={a} (期望800) / 10-15={b} (期望0) / 10-21={c} (期望1000)")
assert (a, b, c) == (800, 0, 1000)

print("--- 重复扫码幂等（同餐次再扫，且餐价改成别的值）")
dup = verify("2026-10-15", price=9999)
assert dup["duplicate"] is True and dup["meal"]["subsidy_fen"] == 0, dup
# 同天不同餐次各自核销
assert verify("2026-10-15", typ="晚餐")["duplicate"] is False
meals = call("GET", f"/api/seniors/{sid}/meals?period=2026-10")
assert len(meals) == 4, f"10月应有4条餐次, 实际 {len(meals)}"

print("--- 9月餐次 + 封账（9月无餐，也允许封账0元单）")
st9 = call("POST", f"/api/seniors/{sid}/settlements", {"period": "2026-09"})
assert st9["total_subsidy_fen"] == 0 and st9["meal_count"] == 0

print("--- 10月月结前更正资格：高龄待遇实际 10-01 起就适用 → 封账前重算")
call("PUT", f"/api/eligibility/{rid_suspend}",
     {"effective_date": "2026-10-10", "status": "suspended", "subsidy_fen": 0, "reason": "保留暂停"})
# 新增 10-01 高龄登记（同日无冲突）
call("POST", f"/api/seniors/{sid}/eligibility",
     {"effective_date": "2026-10-01", "status": "active", "level_name": "高龄", "subsidy_fen": 1000})
rep = call("POST", f"/api/seniors/{sid}/recalc", {})
assert rep["skipped_settled"] == 0, rep
changed = {x["meal_date"] + x["meal_type"]: x["delta_fen"] for x in rep["changes"]}
# 10-09 午餐 800→1000 (+200)；10-15 两餐仍处于暂停区间不变；10-21 已是1000不变
assert changed == {"2026-10-09午餐": 200}, changed
print(f"  重算变更 {rep['changed']} 餐: {changed}")

print("--- 10月封账")
st10 = call("POST", f"/api/seniors/{sid}/settlements", {"period": "2026-10"})
# 10-09=1000, 10-15午=0, 10-15晚=0, 10-21=1000 → 2000
assert st10["meal_count"] == 4 and st10["total_subsidy_fen"] == 2000, st10
print(f"  封账 {st10['meal_count']} 餐, 补贴合计 {st10['total_subsidy_fen']} 分")

print("--- 封账后发现暂停登记有误（10-15 实际应享高龄）→ 只能追加补差单")
call("PUT", f"/api/eligibility/{rid_suspend}",
     {"effective_date": "2026-10-16", "status": "suspended", "subsidy_fen": 0,
      "reason": "更正：暂停自10-16起，10-15仍享待遇"})
v = call("POST", f"/api/seniors/{sid}/adjustments",
         {"period": "2026-10", "reason": "暂停起算日登记错误，10-15应享补贴"})
assert v["meal_count"] == 2, v                      # 10-15 午餐 + 晚餐
assert v["total_delta_fen"] == 2000, v              # 各 +1000
assert v["affected_dates"] == "2026-10-15", v
assert v["items"][0]["old_basis"] and v["items"][0]["new_basis"], "补差单须列新旧依据"
print(f"  补差单 #{v['id']}: {v['meal_count']} 餐, 差额 +{v['total_delta_fen']}, 日期 {v['affected_dates']}")

print("--- 原结算与餐次快照保持不变")
detail = call("GET", f"/api/settlements/{st10['id']}")
assert detail["settlement"]["total_subsidy_fen"] == 2000, "原结算金额被改写！"
m1015 = [m for m in detail["meals"] if m["meal_date"] == "2026-10-15" and m["meal_type"] == "午餐"][0]
assert m1015["subsidy_fen"] == 0 and m1015["eli_status"] == "suspended", "餐次快照被改写！"

print("--- 拒绝重复封账；已封账餐次不参与重算")
call("POST", f"/api/seniors/{sid}/settlements", {"period": "2026-10"}, expect=(409,))
rep2 = call("POST", f"/api/seniors/{sid}/recalc", {})
assert rep2["checked"] == 0 and rep2["skipped_settled"] == 4, rep2

print("--- 无差异时拒绝重复补差")
call("POST", f"/api/seniors/{sid}/adjustments",
     {"period": "2026-10", "reason": "再次核对"}, expect=(400,))

print("--- 对账勾稽")
r9 = call("GET", f"/api/seniors/{sid}/reconcile?period=2026-09")
r10 = call("GET", f"/api/seniors/{sid}/reconcile?period=2026-10")
for name, r in [("9月", r9), ("10月", r10)]:
    for ck in r["checks"]:
        mark = "✅" if ck["ok"] else "❌"
        print(f"  {mark} {name} {ck['name']} — {ck['detail']}")
        assert ck["ok"], f"{name} 对账失败: {ck}"
assert r10["settlement_subsidy_fen"] == 2000
assert r10["adjustment_subsidy_fen"] == 2000
assert r10["net_payable_fen"] == 4000, r10   # 原结算 2000 + 补差 2000
assert r10["meal_table_subsidy_fen"] == 2000 == r10["live_ledger_subsidy_fen"]
print(f"  10月净应付补贴 = 原结算 2000 + 调整 2000 = {r10['net_payable_fen']} 分")

print("\n全部端到端检查通过 ✅")
PY
