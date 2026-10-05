package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// runScenarioVerify 端到端验证三条核心规则：
//  1. 跨月资格变更按【就餐日期】生效（不套用当前状态）
//  2. 同一餐次重复扫码只核销一次
//  3. 月结前可重算；封账后只追加补差单，原结算保留
//  4. 补贴流水与结算金额能够对账
func runScenarioVerify(db *gorm.DB) error {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(newRouter(&Handler{db: db}))
	defer srv.Close()
	v := &verifier{srv: srv, passed: 0}

	// ---------- 建档 ----------
	a := v.createElder("验证老人甲", "V-A")
	b := v.createElder("验证老人乙", "V-B")

	// ---------- 资格登记：制造跨月暂停/恢复 ----------
	// 甲：2026-09-01 生效(10元/餐) → 09-20 暂停 → 10-03 恢复
	v.addQual(a, "2026-09-01", ActionActivate, 10)
	v.addQual(a, "2026-09-20", ActionSuspend, 0)
	v.addQual(a, "2026-10-03", ActionResume, 0)
	// 乙：09-01 生效(10元/餐)；暂停日期先误登记为 09-20，月结前改为 09-12
	v.addQual(b, "2026-09-01", ActionActivate, 10)
	suspendWrong := v.addQual(b, "2026-09-20", ActionSuspend, 0)

	// ========== 规则1：跨月资格变更按就餐日期生效 ==========
	log.Println("【场景1】跨月资格变更按就餐日期生效")

	mA0915 := v.checkin("V-A", "2026-09-15", MealLunch, 15) // 暂停日前：补贴 10
	v.check("2026-09-15（暂停日前）享受补贴 10 元", mA0915.SubsidyAmount == 10 && mA0915.Status == StatusActive)
	v.check("计算依据记录了就餐日期 2026-09-15 与生效记录",
		contains(mA0915.BasisDetail, "2026-09-01") && contains(mA0915.BasisDetail, "资格生效"))

	mA0925 := v.checkin("V-A", "2026-09-25", MealLunch, 15) // 9月已暂停：0
	v.check("2026-09-25（9月暂停后）补贴为 0、全额自付 15",
		mA0925.SubsidyAmount == 0 && mA0925.SelfPay == 15 && mA0925.Status == StatusSuspended)

	mA1001 := v.checkin("V-A", "2026-10-01", MealLunch, 15) // 跨月！10月初但恢复日前，仍暂停
	v.check("2026-10-01（跨月但恢复日前）仍按暂停处理，补贴为 0 —— 不套用当前状态",
		mA1001.SubsidyAmount == 0 && mA1001.Status == StatusSuspended)

	mA1005first := v.checkin("V-A", "2026-10-05", MealLunch, 15) // 恢复后：补贴 10
	v.check("2026-10-05（恢复日后）恢复补贴 10 元",
		mA1005first.SubsidyAmount == 10 && mA1005first.Status == StatusActive)

	// 资格预览接口：同一天对不同老人给出不同结果，且未来/过去日期按记录判定
	ea := v.getEligibility(a, "2026-10-01")
	v.check("资格预览 2026-10-01 = suspended", ea["status"] == StatusSuspended)
	ea2 := v.getEligibility(a, "2026-10-05")
	v.check("资格预览 2026-10-05 = active", ea2["status"] == StatusActive)

	// ========== 规则2：重复扫码只核销一次 ==========
	log.Println("【场景2】同一餐次重复扫码幂等")
	dup := v.checkinDup("V-A", "2026-10-05", MealLunch, 15)
	v.check("第二次扫码返回 duplicate=true", dup["duplicate"] == true)
	octMealsA := v.listMeals(a, "2026-10-01", "2026-10-31")
	v.check("10月甲只有 2 笔餐次（10-01、10-05），重复扫码未产生第二笔", len(octMealsA) == 2)
	oct1005 := findMeal(octMealsA, "2026-10-05", MealLunch)
	v.check("10-05 补贴仍为单笔 10 元，未产生第二笔补贴",
		oct1005 != nil && oct1005.SubsidyAmount == 10 && oct1005.PayTxnID == mA1005first.PayTxnID)

	// 乙 09-15 晚餐（暂停日 09-20 之后才登记，但按就餐日期 09-15 判定，当时仍生效 → 10）
	mB0915 := v.checkin("V-B", "2026-09-15", MealDinner, 14)
	v.check("乙 2026-09-15 晚餐：按就餐日期判定为生效，补贴 10 元", mB0915.SubsidyAmount == 10)

	// ========== 规则3a：月结前发现资格错误 → 重算未结算餐次 ==========
	log.Println("【场景3】月结前修正资格记录并重算")
	// 发现乙的暂停日应为 09-12（不是 09-20）：删除误登记、补正确记录
	v.delQual(b, suspendWrong["id"].(float64))
	v.addQual(b, "2026-09-12", ActionSuspend, 0)
	recalc := v.post("/api/recalc", map[string]any{"elderly_id": a2id(b)})
	v.check("重算接口返回处理笔数 ≥ 1", recalc["recalculated"].(float64) >= 1)

	bMeals := v.listMeals(b, "2026-09-01", "2026-09-30")
	bm := findMeal(bMeals, "2026-09-15", MealDinner)
	v.check("乙 09-15 重算后变为暂停：补贴 10 → 0", bm != nil && bm.SubsidyAmount == 0 && bm.RecalcSeq == 1)
	v.check("重算不改变原核销/支付流水（同一笔就餐）",
		bm.IdempotencyKey == mB0915.IdempotencyKey && bm.PayTxnID == mB0915.PayTxnID)
	v.check("重算后计算依据已更新为暂停记录",
		contains(bm.BasisDetail, "2026-09-12") && contains(bm.BasisDetail, "暂停"))

	// ---------- 9月月结并封账 ----------
	log.Println("【场景4】2026-09 月结、封账")
	s09 := v.createSettlement("2026-09")
	v.check("9月结算单覆盖 3 笔餐次（甲15/甲25/乙15）", s09.TotalMeals == 3)
	// 甲0915=10、甲0925=0、乙0915重算后=0；餐费 15+15+14=44；自付 5+15+14=34
	v.check("9月结算补贴合计 10 元", s09.TotalSubsidy == 10)
	v.check("9月结算餐费合计 44 元", s09.TotalPrice == 44)
	v.check("9月结算自付合计 34 元", s09.TotalSelfPay == 34)
	closed09 := v.closeSettlement("2026-09")
	v.check("结算单状态 closed", closed09.Status == SettleClosed)

	septMeals := v.settlementMeals("2026-09")
	frozenA0915 := findMeal(septMeals, "2026-09-15", MealLunch)
	v.check("封账后 3 笔餐次均挂载结算单且快照冻结", len(septMeals) == 3 &&
		frozenA0915 != nil && frozenA0915.SettlementID != nil && frozenA0915.SubsidyAmount == 10)
	v.check("10月餐次没有被串入9月结算", len(septMeals) == 3)

	// 封账后再重算：9月餐次不得变化
	v.post("/api/recalc", map[string]any{"elderly_id": a2id(a)})
	septMeals2 := v.settlementMeals("2026-09")
	fa2 := findMeal(septMeals2, "2026-09-15", MealLunch)
	v.check("封账后重算不影响已结算餐次（甲09-15 仍为 10 元）",
		fa2 != nil && fa2.SubsidyAmount == 10 && fa2.SettlementID != nil)

	// ========== 规则3b：封账后发现错误 → 只追加补差单 ==========
	log.Println("【场景5】封账后发现资格错误，出具补差单")
	// 发现甲 09-01 的补贴标准登记错误：应为 12 元/餐（同日修正记录）
	v.addQual(a, "2026-09-01", ActionActivate, 12)

	// 封账餐次不能被普通重算改动（已验证）；预览补差
	prev := v.post(fmt.Sprintf("/api/settlements/2026-09/adjustments/preview"),
		map[string]any{"elderly_id": a2id(a)})
	aff, _ := prev["affected"].([]any)
	v.check("预览识别出 1 个受影响餐次（09-15；09-25 本就暂停不受影响）", len(aff) == 1)

	adjResp := v.post("/api/settlements/2026-09/adjustments",
		map[string]any{"elderly_id": a2id(a), "reason": "补贴标准误登记为10元，应为12元"})
	adjJSON, _ := json.Marshal(adjResp["adjustment"])
	var adj Adjustment
	_ = json.Unmarshal(adjJSON, &adj)
	v.check("补差单差额 +2.00 元（补发）", adj.DiffAmount == 2)
	v.check("补差单列清受影响日期 [2026-09-15]", adj.AffectedDates == `["2026-09-15"]`)
	v.check("补差单 旧补贴10 / 新补贴12 / 1 笔", adj.OldSubsidy == 10 && adj.NewSubsidy == 12 && adj.MealCount == 1)
	v.check("补差单带模拟补差支付流水号", adj.PayTxnID != "")

	// 原结算结果与原餐次快照保留
	s09After := v.getSettlement("2026-09")
	v.check("原结算单金额保留不变（补贴仍 10 元，不被补差单改写）", s09After.TotalSubsidy == 10)
	septMeals3 := v.settlementMeals("2026-09")
	fa3 := findMeal(septMeals3, "2026-09-15", MealLunch)
	v.check("原餐次补贴流水保留 10 元（补差单只追加，不改写历史）", fa3.SubsidyAmount == 10)

	// 不能对未封账月份出补差单
	code, _ := v.raw("POST", "/api/settlements/2026-10/adjustments",
		map[string]any{"elderly_id": a2id(a)})
	v.check("未封账月份拒绝出补差单（应走重算）", code == 400)

	// ========== 未结算的10月餐次按修正后资格重算 ==========
	log.Println("【场景6】修正对尚未结算餐次的影响（10月）")
	v.post("/api/recalc", map[string]any{"elderly_id": a2id(a)})
	octA := v.listMeals(a, "2026-10-01", "2026-10-31")
	o1005 := findMeal(octA, "2026-10-05", MealLunch)
	v.check("10-05 未结算餐次重算为新标准 12 元", o1005 != nil && o1005.SubsidyAmount == 12 && o1005.RecalcSeq >= 1)
	o1001 := findMeal(octA, "2026-10-01", MealLunch)
	v.check("10-01 暂停餐次仍为 0（恢复日前）", o1001 != nil && o1001.SubsidyAmount == 0)

	// ========== 规则4：对账 ==========
	log.Println("【场景7】对账：补贴流水 == 结算金额；封账后含补差净额")
	r09 := v.reconcile("2026-09")
	v.check("9月对账平衡（流水合计 == 结算单）", r09.Balanced)
	v.check("9月流水补贴合计 10 元", r09.SubsidyFromMeals == 10)
	v.check("9月补差单 1 张、净额 +2 元", r09.AdjustmentCount == 1 && r09.AdjustmentDiff == 2)
	v.check("9月最终应付补贴 = 原结算10 + 补差2 = 12 元", r09.FinalSubsidyPayable == 12)

	s10 := v.createSettlement("2026-10")
	v.check("10月结算覆盖 2 笔餐次，补贴合计 12 元", s10.TotalMeals == 2 && s10.TotalSubsidy == 12)
	v.closeSettlement("2026-10")
	r10 := v.reconcile("2026-10")
	v.check("10月对账平衡", r10.Balanced)
	v.check("10月最终应付补贴 12 元", r10.FinalSubsidyPayable == 12)

	if v.failed > 0 {
		return fmt.Errorf("共有 %d 项验证未通过", v.failed)
	}
	log.Printf("✅ 全部 %d 项验证通过：跨月按就餐日期生效 / 重复扫码幂等 / 封账后只追加补差 / 对账平衡", v.passed)
	return nil
}

// ---------- 验证辅助 ----------

type verifier struct {
	srv    *httptest.Server
	passed int
	failed int
}

func (v *verifier) check(name string, ok bool) {
	if ok {
		v.passed++
		log.Printf("  ✅ %s", name)
	} else {
		v.failed++
		log.Printf("  ❌ %s", name)
	}
}

func (v *verifier) raw(method, path string, body any) (int, map[string]any) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, v.srv.URL+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		v.failed++
		log.Fatalf("请求失败 %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

func (v *verifier) post(path string, body any) map[string]any {
	code, out := v.raw("POST", path, body)
	if code >= 400 {
		v.failed++
		log.Fatalf("POST %s 返回 %d: %v", path, code, out["error"])
	}
	return out
}

func (v *verifier) get(path string) (int, map[string]any) {
	return v.raw("GET", path, nil)
}

func (v *verifier) createElder(name, card string) map[string]any {
	return v.post("/api/elders", map[string]any{"name": name, "card_no": card})
}

func a2id(m map[string]any) uint { return uint(m["id"].(float64)) }

func (v *verifier) addQual(elder map[string]any, date, action string, rate float64) map[string]any {
	out := v.post(fmt.Sprintf("/api/elders/%d/qualifications", a2id(elder)),
		map[string]any{"date": date, "action": action, "subsidy_rate": rate})
	return out["event"].(map[string]any)
}

func (v *verifier) delQual(elder map[string]any, eventID float64) {
	code, _ := v.raw("DELETE", fmt.Sprintf("/api/elders/%d/qualifications/%d", a2id(elder), int(eventID)), nil)
	if code >= 400 {
		v.failed++
		log.Fatalf("删除资格记录失败: %d", code)
	}
}

func (v *verifier) getEligibility(elder map[string]any, date string) map[string]any {
	code, out := v.get(fmt.Sprintf("/api/elders/%d/eligibility?date=%s", a2id(elder), date))
	if code >= 400 {
		log.Fatalf("资格预览失败: %d %v", code, out)
	}
	return out
}

func parseMeal(m map[string]any) MealRecord {
	b, _ := json.Marshal(m)
	var out MealRecord
	_ = json.Unmarshal(b, &out)
	return out
}

func (v *verifier) checkin(card, date, typ string, price float64) MealRecord {
	out := v.post("/api/checkin", map[string]any{
		"card_no": card, "meal_date": date, "meal_type": typ, "price": price,
	})
	return parseMeal(out["meal"].(map[string]any))
}

func (v *verifier) checkinDup(card, date, typ string, price float64) map[string]any {
	return v.post("/api/checkin", map[string]any{
		"card_no": card, "meal_date": date, "meal_type": typ, "price": price,
	})
}

func (v *verifier) listMeals(elder map[string]any, from, to string) []MealRecord {
	return v.mealsListRaw(fmt.Sprintf("/api/elders/%d/meals?from=%s&to=%s", a2id(elder), from, to))
}

func (v *verifier) mealsListRaw(path string) []MealRecord {
	req, _ := http.NewRequest("GET", v.srv.URL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var ms []MealRecord
	_ = json.Unmarshal(raw, &ms)
	return ms
}

func (v *verifier) settlementMeals(period string) []MealRecord {
	return v.mealsListRaw("/api/settlements/" + period + "/meals")
}

func parseSettlement(m map[string]any) Settlement {
	b, _ := json.Marshal(m)
	var s Settlement
	_ = json.Unmarshal(b, &s)
	return s
}

func (v *verifier) createSettlement(period string) Settlement {
	out := v.post("/api/settlements/"+period, nil)
	return parseSettlement(out["settlement"].(map[string]any))
}

func (v *verifier) getSettlement(period string) Settlement {
	var list []Settlement
	raw := v.getRaw("/api/settlements")
	_ = json.Unmarshal(raw, &list)
	for _, s := range list {
		if s.Period == period {
			return s
		}
	}
	return Settlement{}
}

func (v *verifier) getRaw(path string) []byte {
	req, _ := http.NewRequest("GET", v.srv.URL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw
}

func (v *verifier) closeSettlement(period string) Settlement {
	out := v.post("/api/settlements/"+period+"/close", nil)
	return parseSettlement(out["settlement"].(map[string]any))
}

func (v *verifier) reconcile(period string) ReconcileReport {
	req, _ := http.NewRequest("GET", v.srv.URL+"/api/settlements/"+period+"/reconcile", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("对账请求失败: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r ReconcileReport
	if err := json.Unmarshal(raw, &r); err != nil {
		log.Fatalf("对账结果解析失败: %v (%s)", err, raw)
	}
	return r
}

func findMeal(ms []MealRecord, date, typ string) *MealRecord {
	for i := range ms {
		if ms[i].MealDate == date && ms[i].MealType == typ {
			return &ms[i]
		}
	}
	return nil
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}

// ensureVerifyDB 供 `go run . verify` 使用独立临时库
func ensureVerifyDB() *gorm.DB {
	path := filepath.Join("data", "verify.db")
	_ = os.Remove(path)
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	db, err := gorm.Open(sqlite.Open(path+"?_pragma=busy_timeout(5000)"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatal(err)
	}
	if err := db.AutoMigrate(&Elderly{}, &QualEvent{}, &MealRecord{}, &Settlement{}, &Adjustment{}); err != nil {
		log.Fatal(err)
	}
	return db
}
