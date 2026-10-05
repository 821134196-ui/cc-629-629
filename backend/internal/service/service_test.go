package service

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"mealsubsidy/internal/model"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&model.Senior{}, &model.EligRecord{}, &model.Meal{},
		&model.Settlement{}, &model.AdjustmentVoucher{}, &model.VoucherItem{}, &model.Ledger{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func newSenior(t *testing.T, s *Service, card string) uint {
	t.Helper()
	sr, err := s.CreateSenior("测试老人", card, "阳光社区")
	if err != nil {
		t.Fatalf("create senior: %v", err)
	}
	return sr.ID
}

func addElig(t *testing.T, s *Service, id uint, date, status, level string, subsidy int64, reason string) *model.EligRecord {
	t.Helper()
	r, err := s.AddEligibility(id, date, status, level, subsidy, reason)
	if err != nil {
		t.Fatalf("add elig %s: %v", date, err)
	}
	return r
}

func updElig(t *testing.T, s *Service, recID uint, date, status, level string, subsidy int64, reason string) {
	t.Helper()
	if _, err := s.UpdateEligibility(recID, date, status, level, subsidy, reason); err != nil {
		t.Fatalf("update elig #%d: %v", recID, err)
	}
}

func verify(t *testing.T, s *Service, id uint, date, typ string, price int64) *VerifyResult {
	t.Helper()
	r, err := s.VerifyMeal(VerifyRequest{SeniorID: id, MealDate: date, MealType: typ, PriceFen: price})
	if err != nil {
		t.Fatalf("verify %s %s: %v", date, typ, err)
	}
	return r
}

func assertEq(t *testing.T, name string, got, want int64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: 得到 %d, 期望 %d", name, got, want)
	}
}

// 场景一：跨月资格变更严格按「就餐日期」生效。
// 所有餐次都在状态变更之后核销，验证历史餐次不套用当前状态。
func TestCrossMonthEligibilityByMealDate(t *testing.T) {
	s := New(testDB(t))
	id := newSenior(t, s, "C001")

	addElig(t, s, id, "2026-09-01", StatusActive, "普通", 800, "登记生效")
	addElig(t, s, id, "2026-10-10", StatusSuspended, "", 0, "本人申请暂停")
	addElig(t, s, id, "2026-10-20", StatusActive, "高龄", 1000, "复核恢复")

	cases := []struct {
		date, typ   string
		price       int64
		wantSubsidy int64
		wantSelf    int64
		wantStatus  string
		wantLevel   string
	}{
		{"2026-08-20", "午餐", 1500, 0, 1500, StatusInactive, ""},
		{"2026-09-15", "午餐", 1500, 800, 700, StatusActive, "普通"},
		{"2026-10-09", "午餐", 1500, 800, 700, StatusActive, "普通"},
		{"2026-10-10", "午餐", 1500, 0, 1500, StatusSuspended, ""},
		{"2026-10-15", "午餐", 1500, 0, 1500, StatusSuspended, ""},
		{"2026-10-20", "午餐", 1500, 1000, 500, StatusActive, "高龄"},
		{"2026-10-21", "晚餐", 600, 600, 0, StatusActive, "高龄"}, // 补贴封顶为餐价
	}
	for _, c := range cases {
		r := verify(t, s, id, c.date, c.typ, c.price)
		assertEq(t, c.date+" 补贴", r.Meal.SubsidyFen, c.wantSubsidy)
		assertEq(t, c.date+" 自付", r.Meal.SelfPayFen, c.wantSelf)
		if r.Meal.EliStatus != c.wantStatus {
			t.Errorf("%s 资格状态=%s, 期望 %s", c.date, r.Meal.EliStatus, c.wantStatus)
		}
		if r.Meal.EliLevel != c.wantLevel {
			t.Errorf("%s 等级=%q, 期望 %q", c.date, r.Meal.EliLevel, c.wantLevel)
		}
		// 结算页面必须展示就餐日期及计算依据
		if r.Meal.MealDate != c.date || r.Meal.BasisDescribe == "" {
			t.Errorf("%s 快照缺少就餐日期或计算依据", c.date)
		}
	}
}

// 场景二：同一餐次重复扫码只核销一次，不产生第二笔补贴；同天不同餐次各自核销。
func TestDuplicateScanIdempotent(t *testing.T) {
	s := New(testDB(t))
	id := newSenior(t, s, "C002")
	addElig(t, s, id, "2026-10-01", StatusActive, "普通", 800, "")

	first := verify(t, s, id, "2026-10-05", "午餐", 1500)
	if first.Duplicate {
		t.Fatal("首次核销不应判重")
	}
	second := verify(t, s, id, "2026-10-05", "午餐", 9999) // 重复扫码即使餐价不同也不新建
	if !second.Duplicate || second.Meal.ID != first.Meal.ID {
		t.Fatal("重复扫码必须返回同一条记录并标记 duplicate")
	}
	if second.Meal.PriceFen != 1500 {
		t.Fatal("重复扫码不得改写原核销记录")
	}
	dinner := verify(t, s, id, "2026-10-05", "晚餐", 1500)
	if dinner.Duplicate {
		t.Fatal("同天不同餐次应各自核销")
	}

	meals, _ := s.ListMeals(id, "2026-10")
	if len(meals) != 2 {
		t.Fatalf("应只有 2 条餐次, 实际 %d", len(meals))
	}
	var liveLedger int64
	s.DB.Model(&model.Ledger{}).
		Where("senior_id = ? AND kind = 'meal_subsidy' AND voided = ?", id, false).Count(&liveLedger)
	if liveLedger != 2 {
		t.Fatalf("重复扫码不得产生第二笔补贴流水，实际有效流水 %d 条", liveLedger)
	}
}

// 场景三：封账前可重算未封账餐次；已封账餐次一律跳过，原结算不被改写。
func TestRecalcBeforeClose(t *testing.T) {
	s := New(testDB(t))
	id := newSenior(t, s, "C003")
	addElig(t, s, id, "2026-09-01", StatusActive, "普通", 800, "")

	verify(t, s, id, "2026-09-10", "午餐", 1500)
	verify(t, s, id, "2026-09-20", "午餐", 1500)

	st, err := s.CloseMonth(id, "2026-09")
	if err != nil {
		t.Fatalf("close sep: %v", err)
	}
	assertEq(t, "9月封账金额", st.TotalSubsidyFen, 1600)

	verify(t, s, id, "2026-10-08", "午餐", 1500) // 800
	addElig(t, s, id, "2026-10-01", StatusActive, "高龄", 1000, "等级更正")

	rep, err := s.RecalcUnsettled(id, "2026-09-01", "2026-10-31")
	if err != nil {
		t.Fatalf("recalc: %v", err)
	}
	if rep.Checked != 1 {
		t.Fatalf("只有 1 顿未封账餐次可重算, 实际检查 %d", rep.Checked)
	}
	if rep.SkippedSettled != 2 {
		t.Fatalf("应跳过 2 顿已封账餐次, 实际 %d", rep.SkippedSettled)
	}
	if rep.Changed != 1 || rep.Changes[0].Delta != 200 {
		t.Fatalf("应变更 1 顿差额 +200, 实际 changed=%d", rep.Changed)
	}

	var st2 model.Settlement
	s.DB.First(&st2, st.ID)
	assertEq(t, "原结算金额不得被重算改写", st2.TotalSubsidyFen, 1600)

	var voided, live int64
	s.DB.Model(&model.Ledger{}).Where("senior_id=? AND kind='meal_subsidy' AND voided = ?", id, true).Count(&voided)
	s.DB.Model(&model.Ledger{}).Where("senior_id=? AND kind='meal_subsidy' AND voided = ?", id, false).Count(&live)
	if voided != 1 || live != 3 {
		t.Fatalf("期望作废1条/有效3条, 实际 作废%d/有效%d", voided, live)
	}
}

// 场景四：封账后发现错误只能出补差单，原结算与餐次快照不变，调整只追加。
func TestAdjustmentAfterCloseAppendsOnly(t *testing.T) {
	s := New(testDB(t))
	id := newSenior(t, s, "C004")
	addElig(t, s, id, "2026-10-01", StatusActive, "普通", 800, "")

	verify(t, s, id, "2026-10-05", "午餐", 1500)
	verify(t, s, id, "2026-10-06", "午餐", 1500)
	verify(t, s, id, "2026-10-07", "午餐", 1500)

	st, err := s.CloseMonth(id, "2026-10")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	assertEq(t, "原结算 2400", st.TotalSubsidyFen, 2400)

	// 封账后核实：老人 10-06 起即不符合条件
	addElig(t, s, id, "2026-10-06", StatusSuspended, "", 0, "事后核实不符合条件")

	if _, err := s.CloseMonth(id, "2026-10"); err == nil {
		t.Fatal("已封账月份必须拒绝重复封账")
	}
	rep, err := s.RecalcUnsettled(id, "", "")
	if err != nil {
		t.Fatalf("recalc: %v", err)
	}
	if rep.Checked != 0 {
		t.Fatalf("封账后没有可重算餐次, 实际 %d", rep.Checked)
	}

	v, err := s.CreateAdjustment(id, "2026-10", "误发补贴追回")
	if err != nil {
		t.Fatalf("adjustment: %v", err)
	}
	if v.MealCount != 2 || v.TotalDeltaFen != -1600 {
		t.Fatalf("补差单应为 2 餐 -1600, 实际 %d 餐 %d", v.MealCount, v.TotalDeltaFen)
	}
	if v.AffectedDates != "2026-10-06,2026-10-07" {
		t.Fatalf("受影响日期清单错误: %q", v.AffectedDates)
	}
	if len(v.Items) != 2 || v.Items[0].OldBasis == "" || v.Items[0].NewBasis == "" {
		t.Fatal("补差单应逐餐列清新旧依据")
	}

	var st2 model.Settlement
	s.DB.First(&st2, st.ID)
	assertEq(t, "原结算金额保持 2400", st2.TotalSubsidyFen, 2400)
	var m1006 model.Meal
	s.DB.Where("senior_id = ? AND meal_date = '2026-10-06'", id).First(&m1006)
	assertEq(t, "餐次快照补贴仍为 800", m1006.SubsidyFen, 800)
	if m1006.EliStatus != StatusActive {
		t.Fatal("餐次资格快照不得被补差单改写")
	}

	var adj int64
	s.DB.Model(&model.Ledger{}).
		Where("senior_id=? AND kind='adjustment'", id).Select("COALESCE(SUM(amount_fen),0)").Scan(&adj)
	assertEq(t, "调整流水 -1600 只追加", adj, -1600)

	// 无差异时拒绝再出补差单
	if _, err := s.CreateAdjustment(id, "2026-10", "再次核对"); err == nil {
		t.Fatal("无差异时不应出具补差单")
	}
}

// 场景五：跨月完整链路——封账前重算、封账后补差，补贴流水与结算金额全程对账。
func TestReconciliationAcrossMonths(t *testing.T) {
	s := New(testDB(t))
	id := newSenior(t, s, "C005")
	addElig(t, s, id, "2026-09-01", StatusActive, "普通", 800, "")

	// 9 月一顿 → 封账 800
	verify(t, s, id, "2026-09-15", "午餐", 1500)
	if _, err := s.CloseMonth(id, "2026-09"); err != nil {
		t.Fatalf("close sep: %v", err)
	}

	// 10 月：误登记 10-03 暂停；10-02 享 800，10-04 因暂停为 0
	wrong := addElig(t, s, id, "2026-10-03", StatusSuspended, "", 0, "误登记暂停")
	verify(t, s, id, "2026-10-02", "午餐", 1500) // 800
	verify(t, s, id, "2026-10-04", "午餐", 1500) // 0

	// 月结前发现错误：更正该条登记为 10-03 起高龄 1000，重算
	updElig(t, s, wrong.ID, "2026-10-03", StatusActive, "高龄", 1000, "更正：并未暂停")
	rep, err := s.RecalcUnsettled(id, "2026-10-01", "2026-10-31")
	if err != nil {
		t.Fatalf("recalc: %v", err)
	}
	if rep.Changed != 1 || rep.Changes[0].Delta != 1000 {
		t.Fatalf("10-04 应由 0 更正为 1000, 实际 changed=%d", rep.Changed)
	}

	st, err := s.CloseMonth(id, "2026-10")
	if err != nil {
		t.Fatalf("close oct: %v", err)
	}
	assertEq(t, "10月封账 = 800 + 1000", st.TotalSubsidyFen, 1800)

	// 封账后又发现：高龄待遇实际 10-01 起就适用 → 把该条更正提前到 10-01，出补差单
	updElig(t, s, wrong.ID, "2026-10-01", StatusActive, "高龄", 1000, "二次核实：10-01 起适用")
	v, err := s.CreateAdjustment(id, "2026-10", "待遇起算日更正补差")
	if err != nil {
		t.Fatalf("adjustment: %v", err)
	}
	if v.MealCount != 1 || v.TotalDeltaFen != 200 {
		t.Fatalf("补差单应仅 10-02 一餐 +200, 实际 %d 餐 %d", v.MealCount, v.TotalDeltaFen)
	}
	if v.AffectedDates != "2026-10-02" {
		t.Fatalf("受影响日期应为 2026-10-02, 实际 %q", v.AffectedDates)
	}

	// 9 月对账
	rec9, err := s.Reconcile(id, "2026-09")
	if err != nil {
		t.Fatalf("reconcile sep: %v", err)
	}
	for _, c := range rec9.Checks {
		if !c.OK {
			t.Errorf("9月对账不通过[%s]: %s", c.Name, c.Detail)
		}
	}
	assertEq(t, "9月净应付", rec9.NetPayable, 800)

	// 10 月对账
	rec10, err := s.Reconcile(id, "2026-10")
	if err != nil {
		t.Fatalf("reconcile oct: %v", err)
	}
	for _, c := range rec10.Checks {
		if !c.OK {
			t.Errorf("10月对账不通过[%s]: %s", c.Name, c.Detail)
		}
	}
	assertEq(t, "餐次表合计", rec10.MealTableSubsidy, 1800)
	assertEq(t, "有效流水合计", rec10.LiveLedgerSubsidy, 1800)
	assertEq(t, "封账金额固化", rec10.SettlementSubsidy, 1800)
	assertEq(t, "调整流水", rec10.AdjustmentSubsidy, 200)
	assertEq(t, "净应付 = 原结算 + 调整", rec10.NetPayable, 2000)
}

// 场景六：封账前重算使补贴下降，作废留痕后对账仍然平衡。
func TestRecalcDownwardAndReconcile(t *testing.T) {
	s := New(testDB(t))
	id := newSenior(t, s, "C006")
	addElig(t, s, id, "2026-10-01", StatusActive, "普通", 1000, "")
	verify(t, s, id, "2026-10-11", "午餐", 1500)
	verify(t, s, id, "2026-10-12", "午餐", 1500)

	addElig(t, s, id, "2026-10-10", StatusSuspended, "", 0, "核实暂停")
	rep, err := s.RecalcUnsettled(id, "", "")
	if err != nil {
		t.Fatalf("recalc: %v", err)
	}
	if rep.Changed != 2 {
		t.Fatalf("两餐都应从 1000 变为 0, 实际 %d", rep.Changed)
	}
	for _, c := range rep.Changes {
		assertEq(t, c.MealDate+" 差额", c.Delta, -1000)
	}

	st, err := s.CloseMonth(id, "2026-10")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	assertEq(t, "更正后封账金额为 0", st.TotalSubsidyFen, 0)

	rec, err := s.Reconcile(id, "2026-10")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, c := range rec.Checks {
		if !c.OK {
			t.Errorf("对账不通过[%s]: %s", c.Name, c.Detail)
		}
	}
	assertEq(t, "有效流水归零", rec.LiveLedgerSubsidy, 0)
	if rec.VoidedLedgerCount != 2 {
		t.Errorf("应有 2 条作废留痕, 实际 %d", rec.VoidedLedgerCount)
	}
}

// 场景七：从未登记资格的老人就餐按全自付处理，对账仍平衡。
func TestNoEligibilityFullSelfPay(t *testing.T) {
	s := New(testDB(t))
	id := newSenior(t, s, "C007")
	r := verify(t, s, id, "2026-10-01", "午餐", 1500)
	assertEq(t, "无资格全自付", r.Meal.SubsidyFen, 0)

	st, err := s.CloseMonth(id, "2026-10")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	assertEq(t, "封账补贴为 0", st.TotalSubsidyFen, 0)
	assertEq(t, "封账自付为 1500", st.TotalSelfPayFen, 1500)

	rec, err := s.Reconcile(id, "2026-10")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, c := range rec.Checks {
		if !c.OK {
			t.Errorf("对账不通过[%s]: %s", c.Name, c.Detail)
		}
	}
}
