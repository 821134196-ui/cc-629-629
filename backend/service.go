package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
)

const (
	dateLayout = "2006-01-02"
	monthLay   = "2006-01"

	StatusActive    = "active"
	StatusSuspended = "suspended"

	ActionActivate = "activate"
	ActionSuspend  = "suspend"
	ActionResume   = "resume"

	SettleOpen   = "open"
	SettleClosed = "closed"

	MealLunch = "lunch"
	MealDinner = "dinner"
)

// Eligibility 某就餐日期适用的资格（按该日期当天已生效的最后一条资格记录判定）
type Eligibility struct {
	Status        string
	SubsidyRate   float64
	BasisEventID  *uint
	BasisDetail   string
}

var errNoQualification = errors.New("老人无生效资格记录，尚未开通助餐资格")

// eligibilityOnDate 关键规则：按就餐日期判定，而不是按当前状态。
// 取 mealDate 当天及之前的最后一条资格事件；没有任何记录则视为未开通。
func eligibilityOnDate(db *gorm.DB, elderlyID uint, mealDate string) (Eligibility, error) {
	var ev QualEvent
	err := db.Where("elderly_id = ? AND date <= ?", elderlyID, mealDate).
		Order("date DESC, id DESC").First(&ev).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Eligibility{}, errNoQualification
	}
	if err != nil {
		return Eligibility{}, err
	}

	e := Eligibility{BasisEventID: &ev.ID}
	switch ev.Action {
	case ActionActivate:
		e.Status = StatusActive
		e.SubsidyRate = ev.SubsidyRate
		e.BasisDetail = fmt.Sprintf("依据 %s 的【资格生效】记录（补贴标准 %.2f 元/餐，自 %s 起）",
			ev.Date, ev.SubsidyRate, ev.Date)
	case ActionSuspend:
		e.Status = StatusSuspended
		e.SubsidyRate = 0
		e.BasisDetail = fmt.Sprintf("依据 %s 的【资格暂停】记录（自 %s 起暂停，当日就餐不享受补贴）",
			ev.Date, ev.Date)
	case ActionResume:
		// 恢复沿用暂停前的补贴标准：向前找最近一条 activate
		var act QualEvent
		if aerr := db.Where("elderly_id = ? AND date <= ? AND action = ?", elderlyID, ev.Date, ActionActivate).
			Order("date DESC, id DESC").First(&act).Error; aerr == nil {
			e.SubsidyRate = act.SubsidyRate
		}
		e.Status = StatusActive
		e.BasisDetail = fmt.Sprintf("依据 %s 的【资格恢复】记录（恢复生效，补贴标准 %.2f 元/餐）",
			ev.Date, e.SubsidyRate)
	default:
		return Eligibility{}, fmt.Errorf("未知资格动作: %s", ev.Action)
	}
	return e, nil
}

// computeSubsidy 按资格快照口径计算一餐
func computeSubsidy(price float64, e Eligibility) (subsidy, selfPay float64) {
	if e.Status != StatusActive {
		return 0, round2(price)
	}
	subsidy = round2(min2(price, e.SubsidyRate))
	selfPay = round2(price - subsidy)
	return
}

// recomputeMeal 用当前资格记录重新计算一餐（月结前）。
// 仅重写补贴快照字段与计算依据；幂等键、身份核验/支付流水号保持不变（同一笔核销）。
func recomputeMeal(db *gorm.DB, m *MealRecord) error {
	e, err := eligibilityOnDate(db, m.ElderlyID, m.MealDate)
	if err != nil {
		return err
	}
	subsidy, selfPay := computeSubsidy(m.Price, e)
	m.Status = e.Status
	m.SubsidyRate = e.SubsidyRate
	m.SubsidyAmount = subsidy
	m.SelfPay = selfPay
	m.BasisEventID = e.BasisEventID
	m.BasisDetail = e.BasisDetail
	m.RecalcSeq++
	return db.Model(&MealRecord{}).Where("id = ?", m.ID).Select(
		"status", "subsidy_rate", "subsidy_amount", "self_pay",
		"basis_event_id", "basis_detail", "recalc_seq",
	).Updates(m).Error
}

// recalcUnsettled 重新计算某老人（或全部，elderlyID=0）尚未结算的餐次
func recalcUnsettled(db *gorm.DB, elderlyID uint) (int, error) {
	q := db.Model(&MealRecord{}).Where("settlement_id IS NULL")
	if elderlyID > 0 {
		q = q.Where("elderly_id = ?", elderlyID)
	}
	var meals []MealRecord
	if err := q.Order("meal_date, meal_type, id").Find(&meals).Error; err != nil {
		return 0, err
	}
	n := 0
	for i := range meals {
		if err := recomputeMeal(db, &meals[i]); err != nil {
			return n, fmt.Errorf("餐次 %d 重算失败: %w", meals[i].ID, err)
		}
		n++
	}
	return n, nil
}

// listUnsettledDates 返回某月份尚未封账的就餐日期（用于判断是否还能重算/封账）
func unsettledDatesInPeriod(db *gorm.DB, period string) ([]string, error) {
	var dates []string
	err := db.Model(&MealRecord{}).
		Where("settlement_id IS NULL AND substr(meal_date,1,7) = ?", period).
		Distinct().Order("meal_date").Pluck("meal_date", &dates).Error
	return dates, err
}

// createOrRefreshOpenSettlement 生成/刷新某月 open 结算单（幂等：一个月份只有一单）
func createOrRefreshOpenSettlement(db *gorm.DB, period string) (*Settlement, error) {
	if _, perr := time.Parse(monthLay, period); perr != nil {
		return nil, fmt.Errorf("月份格式应为 YYYY-MM")
	}
	var s Settlement
	err := db.Where("period = ?", period).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s = Settlement{Period: period, Status: SettleOpen}
		if err := db.Create(&s).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if s.Status == SettleClosed {
		return nil, fmt.Errorf("%s 结算单已封账，不能重新汇总；如有错误请出具补差单", period)
	}

	var agg struct {
		N       int
		Price   float64
		Subsidy float64
		Self    float64
	}
	if err := db.Model(&MealRecord{}).
		Where("settlement_id IS NULL AND substr(meal_date,1,7) = ?", period).
		Select("COUNT(*) AS n, COALESCE(SUM(price),0) AS price, "+
			"COALESCE(SUM(subsidy_amount),0) AS subsidy, COALESCE(SUM(self_pay),0) AS self").
		Scan(&agg).Error; err != nil {
		return nil, err
	}

	// open 单保存的是“本次月结覆盖的餐次”汇总；刷新时重算。
	// 已封账的历史餐次 settlement_id 不为 NULL，不会进入汇总。
	s.TotalMeals = agg.N
	s.TotalPrice = round2(agg.Price)
	s.TotalSubsidy = round2(agg.Subsidy)
	s.TotalSelfPay = round2(agg.Self)
	if err := db.Save(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// closeSettlement 封账：把当月未结算餐次挂到结算单并冻结快照
func closeSettlement(db *gorm.DB, period string) (*Settlement, []MealRecord, error) {
	s, err := createOrRefreshOpenSettlement(db, period)
	if err != nil {
		return nil, nil, err
	}
	var meals []MealRecord
	if err := db.Where("settlement_id IS NULL AND substr(meal_date,1,7) = ?", period).
		Order("meal_date, meal_type, elderly_id").Find(&meals).Error; err != nil {
		return nil, nil, err
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		s.Status = SettleClosed
		s.ClosedAt = &now
		if err := tx.Save(s).Error; err != nil {
			return err
		}
		if len(meals) > 0 {
			if err := tx.Model(&MealRecord{}).
				Where("settlement_id IS NULL AND substr(meal_date,1,7) = ?", period).
				Update("settlement_id", s.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return s, meals, nil
}

// affectedMeal 封账后按“当前资格记录”重算一餐的差异（不写回 MealRecord）
type affectedMeal struct {
	Meal       MealRecord
	NewStatus  string
	NewRate    float64
	NewSubsidy float64
	NewSelfPay float64
	Diff       float64
}

// previewAdjustment 预览封账后补差：对比已封账餐次原快照与按当前资格重算的结果
func previewAdjustment(db *gorm.DB, period string, elderlyID uint) ([]affectedMeal, error) {
	var meals []MealRecord
	q := db.Where("settlement_id IS NOT NULL AND substr(meal_date,1,7) = ?", period)
	if elderlyID > 0 {
		q = q.Where("elderly_id = ?", elderlyID)
	}
	if err := q.Order("meal_date, meal_type").Find(&meals).Error; err != nil {
		return nil, err
	}
	var out []affectedMeal
	for _, m := range meals {
		e, err := eligibilityOnDate(db, m.ElderlyID, m.MealDate)
		if err != nil {
			continue // 资格被删空等异常：跳过，原结算保留
		}
		newSub, newSelf := computeSubsidy(m.Price, e)
		diff := round2(newSub - m.SubsidyAmount)
		if diff == 0 {
			continue
		}
		out = append(out, affectedMeal{
			Meal: m, NewStatus: e.Status, NewRate: e.SubsidyRate,
			NewSubsidy: newSub, NewSelfPay: newSelf, Diff: diff,
		})
	}
	return out, nil
}

// createAdjustment 出具补差单：只追加，不修改原结算结果
func createAdjustment(db *gorm.DB, period string, elderlyID uint, reason string) (*Adjustment, error) {
	var s Settlement
	if err := db.Where("period = ? AND status = ?", period, SettleClosed).First(&s).Error; err != nil {
		return nil, fmt.Errorf("%s 尚未封账，月结前的错误应使用“重新计算”而非补差单", period)
	}
	var elder Elderly
	if err := db.First(&elder, elderlyID).Error; err != nil {
		return nil, fmt.Errorf("老人不存在")
	}
	diff, err := previewAdjustment(db, period, elderlyID)
	if err != nil {
		return nil, err
	}
	if len(diff) == 0 {
		return nil, fmt.Errorf("没有发现受影响餐次（原补贴与按当前资格重算结果一致）")
	}

	dateSet := map[string]struct{}{}
	oldSum, newSum := 0.0, 0.0
	for _, a := range diff {
		dateSet[a.Meal.MealDate] = struct{}{}
		oldSum += a.Meal.SubsidyAmount
		newSum += a.NewSubsidy
	}
	dates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	dj, _ := json.Marshal(dates)

	adj := Adjustment{
		Period:        period,
		SettlementID:  s.ID,
		ElderlyID:     elderlyID,
		ElderlyName:   elder.Name,
		AffectedDates: string(dj),
		MealCount:     len(diff),
		OldSubsidy:    round2(oldSum),
		NewSubsidy:    round2(newSum),
		DiffAmount:    round2(newSum - oldSum),
		Reason:        reason,
		PayTxnID:      mockPayTxn("ADJ", period, elderlyID),
	}
	if err := db.Create(&adj).Error; err != nil {
		return nil, err
	}
	return &adj, nil
}

// ReconcileReport 对账报告
type ReconcileReport struct {
	Period                string  `json:"period"`
	SettlementStatus      string  `json:"settlement_status"`
	MealsInSettlement     int     `json:"meals_in_settlement"`
	PriceFromMeals        float64 `json:"price_from_meals"`
	SubsidyFromMeals      float64 `json:"subsidy_from_meals"`
	SelfPayFromMeals      float64 `json:"self_pay_from_meals"`
	SettlementTotalPrice  float64 `json:"settlement_total_price"`
	SettlementTotalSubsidy float64 `json:"settlement_total_subsidy"`
	SettlementTotalSelf   float64 `json:"settlement_total_self"`
	AdjustmentCount       int     `json:"adjustment_count"`
	AdjustmentDiff        float64 `json:"adjustment_diff"`
	FinalSubsidyPayable   float64 `json:"final_subsidy_payable"` // 原结算补贴 + 补差净额
	Balanced              bool    `json:"balanced"`             // 流水合计 == 结算单
	Mismatches            []string `json:"mismatches"`
}

// reconcile 对账：补贴流水(MealRecord 逐条) 汇总 == 结算单金额；封账后加上补差单
func reconcile(db *gorm.DB, period string) (*ReconcileReport, error) {
	var s Settlement
	if err := db.Where("period = ?", period).First(&s).Error; err != nil {
		return nil, fmt.Errorf("%s 尚无结算单", period)
	}
	r := ReconcileReport{Period: period, SettlementStatus: s.Status}

	var meals []MealRecord
	q := db.Model(&MealRecord{})
	if s.Status == SettleOpen {
		// 月结中（未封账）：结算单金额来自当月尚未结算的餐次，对账也按此口径
		q = q.Where("settlement_id IS NULL AND substr(meal_date,1,7) = ?", period)
	} else {
		// 已封账：只统计挂载到本结算单的餐次（快照冻结）
		q = q.Where("settlement_id = ?", s.ID)
	}
	if err := q.Order("id").Find(&meals).Error; err != nil {
		return nil, err
	}
	r.MealsInSettlement = len(meals)
	for _, m := range meals {
		r.PriceFromMeals += m.Price
		r.SubsidyFromMeals += m.SubsidyAmount
		r.SelfPayFromMeals += m.SelfPay
	}
	r.PriceFromMeals = round2(r.PriceFromMeals)
	r.SubsidyFromMeals = round2(r.SubsidyFromMeals)
	r.SelfPayFromMeals = round2(r.SelfPayFromMeals)
	r.SettlementTotalPrice = s.TotalPrice
	r.SettlementTotalSubsidy = s.TotalSubsidy
	r.SettlementTotalSelf = s.TotalSelfPay

	const eps = 0.001
	if !floatEq(r.PriceFromMeals, s.TotalPrice, eps) {
		r.Mismatches = append(r.Mismatches,
			fmt.Sprintf("餐费不一致：餐次流水合计 %.2f ≠ 结算单 %.2f", r.PriceFromMeals, s.TotalPrice))
	}
	if !floatEq(r.SubsidyFromMeals, s.TotalSubsidy, eps) {
		r.Mismatches = append(r.Mismatches,
			fmt.Sprintf("补贴不一致：补贴流水合计 %.2f ≠ 结算单 %.2f", r.SubsidyFromMeals, s.TotalSubsidy))
	}
	if !floatEq(r.SelfPayFromMeals, s.TotalSelfPay, eps) {
		r.Mismatches = append(r.Mismatches,
			fmt.Sprintf("自付不一致：餐次自付合计 %.2f ≠ 结算单 %.2f", r.SelfPayFromMeals, s.TotalSelfPay))
	}
	if r.MealsInSettlement != s.TotalMeals {
		r.Mismatches = append(r.Mismatches,
			fmt.Sprintf("餐次笔数不一致：流水 %d 笔 ≠ 结算单 %d 笔", r.MealsInSettlement, s.TotalMeals))
	}

	var adjs []Adjustment
	db.Where("settlement_id = ?", s.ID).Order("id").Find(&adjs)
	r.AdjustmentCount = len(adjs)
	for _, a := range adjs {
		r.AdjustmentDiff = round2(r.AdjustmentDiff + a.DiffAmount)
	}
	// 每笔补差单自身：new-old == diff
	for _, a := range adjs {
		if !floatEq(round2(a.NewSubsidy-a.OldSubsidy), a.DiffAmount, eps) {
			r.Mismatches = append(r.Mismatches,
				fmt.Sprintf("补差单 #%d 金额不自洽：新 %.2f - 旧 %.2f ≠ 差额 %.2f",
					a.ID, a.NewSubsidy, a.OldSubsidy, a.DiffAmount))
		}
	}

	r.FinalSubsidyPayable = round2(s.TotalSubsidy + r.AdjustmentDiff)
	r.Balanced = len(r.Mismatches) == 0
	return &r, nil
}

func round2(v float64) float64 {
	return float64(int64(v*100+sign(v)*0.5)) / 100
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func min2(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func floatEq(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}
