# 社区老人助餐资格变更与餐补结算系统

社区登记老人助餐资格的**生效 / 暂停 / 恢复日期**，食堂逐餐扫码核销。
**补贴按实际就餐当天的资格计算，不在月底统一套用老人当前状态**；每次核销保存当时适用的资格快照。

- 后端：Go 1.23 + Gin + GORM（SQLite，纯 Go 驱动，无需 CGO）
- 前端：Vue 3 + Vite（构建产物由 Go 服务在 `:8080` 一并托管）
- 身份核验、支付、补差打款均为**本地模拟**（生成模拟流水号），无外部依赖

## 一条命令启动

```bash
./run.sh
```

自动完成：检测/安装 Go → 编译后端 → 安装并构建前端 → SQLite 迁移与演示数据播种 → 启动服务。
浏览器打开 http://localhost:8080/ 。

其他命令：

```bash
./run.sh verify   # 运行 40 项端到端业务规则验证（独立临时库）
./run.sh reset    # 清空演示数据后重启
```

演示老人（卡号即可在食堂端"扫码"）：

| 卡号 | 姓名 | 资格轨迹（播种时按月动态生成） |
|---|---|---|
| CARD-0001 | 张桂兰 | 上月 1 日生效 10 元/餐 → 上月 18 日暂停 → 本月 2 日恢复 |
| CARD-0002 | 李建国 | 上月 1 日生效 8 元/餐 → 本月 15 日暂停 |
| CARD-0003 | 王秀珍 | 本月 5 日才生效 6 元/餐 |

## 核心业务规则

1. **资格按就餐日期判定**：取该日期当天及之前的最后一条资格事件
   （activate / suspend / resume）。例：9/20 暂停、10/3 恢复，10/1 就餐仍按暂停算 0 补贴。
2. **核销即快照**：`MealRecord` 保存资格状态、补贴标准、补贴金额、自付、计算依据、
   模拟身份核验流水（VRF-…）与支付流水（PAY-…）。
3. **同一餐次幂等**：唯一键 `卡号|就餐日期|餐次`。重复扫码返回原记录（`duplicate=true`），
   不产生第二笔补贴、不重复扣款。
4. **月结前可重算**：发现资格记录错误时，先修正/删除记录，再对**尚未结算**的餐次重算；
   重算只改补贴快照与依据，幂等键、核验/支付流水不变（同一笔就餐），`recalc_seq` +1。
5. **封账后只追加补差单**：封账把餐次挂到结算单并冻结快照；之后发现错误，
   原结算结果与原流水保留不动，生成 `Adjustment`：列清受影响日期、新旧补贴、差额
   （正=补发 / 负=追回）与模拟补差流水（ADJ-…）。未封账月份拒绝出补差单。
6. **对账**：逐月把餐次补贴流水逐笔相加，与结算单金额核对；
   最终应付补贴 = 原结算补贴 + 补差单净额。

## 页面

- **社区端 · 资格登记**：建档；登记/覆盖/删除生效、暂停、恢复记录；任意日期资格预览。
- **食堂端 · 扫码核销**：输入卡号+就餐日期（可补录历史）+餐次+餐费；返回当餐补贴、
  自付、计算依据与模拟核验/支付结果；重复扫码明确提示；近期流水带结算状态。
- **老人查询 · 每餐补贴**：按月查看每餐日期、餐次、补贴、自付、当日资格与计算依据。
- **结算 · 月结/封账/补差**：重算未结算餐次、生成月结单、封账、
  封账后预览并出具补差单、流水↔结算单对账。

## API 摘要

```
POST   /api/elders                                 建档
GET    /api/elders?q=
GET/POST/DELETE /api/elders/:id/qualifications[/:qid]   资格记录
GET    /api/elders/:id/eligibility?date=YYYY-MM-DD      某日资格预览
GET    /api/elders/:id/meals?from=&to=                  老人就餐流水
POST   /api/checkin                                扫码核销（幂等）
GET    /api/meals?period=&unsettled=1
POST   /api/recalc            {"elderly_id": 0}        重算未结算餐次
POST   /api/settlements/:period                     生成/刷新月结单
POST   /api/settlements/:period/close               封账
GET    /api/settlements/:period/meals               结算餐次明细
POST   /api/settlements/:period/adjustments/preview 封账后差异预览
POST   /api/settlements/:period/adjustments         出具补差单
GET    /api/settlements/:period/reconcile           对账报告
```

## 数据模型（SQLite，GORM AutoMigrate）

- `elderly` 老人档案（卡号唯一）
- `qual_events` 资格事件（老人+日期唯一；activate 带补贴标准，resume 沿用最近生效标准）
- `meal_records` 餐次核销：资格快照 + 幂等键 + 模拟流水 + `settlement_id`（NULL=未结算）
- `settlements` 月度结算单：`open`（月结中）→ `closed`（已封账）
- `adjustments` 封账后补差单（受影响日期 JSON、新旧补贴、差额、补差流水）

## 端到端验证说明

`./run.sh verify` 通过 httptest 起真实路由，用 HTTP 请求跑完整生命周期（共 40 项断言）：

跨月资格（9/20 暂停、10/3 恢复）下 9/15、9/25、10/1、10/5 四个日期的补贴；
重复扫码幂等；月结前改暂停日并重算；封账与快照冻结；封账后改补贴标准只出 +2 元补差单、
原结果不变；9 月与 10 月流水↔结算单对账平衡、最终应付 = 原结算 + 补差净额。
