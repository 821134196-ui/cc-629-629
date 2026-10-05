package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"mealsubsidy/internal/mockpay"
	"mealsubsidy/internal/model"
)

var (
	ErrNotFound = errors.New("记录不存在")
	ErrConflict = errors.New("冲突")
)

const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusInactive  = "inactive" // 从未有过生效登记
)

type Service struct{ DB *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{DB: db} }

// ---------------- 老人 ----------------

func (s *Service) CreateSenior(name, cardNo, community string) (*model.Senior, error) {
	sr := &model.Senior{Name: name, CardNo: cardNo, Community: community}
	if err := s.DB.Create(sr).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("%w: 卡号已存在", ErrConflict)
		}
		return nil, err
	}
	return sr, nil
}

func (s *Service) ListSeniors() ([]model.Senior, error) {
	var out []model.Senior
	err := s.DB.Order("id").Find(&out).Error
	return out, err
}

func (s *Service) getSenior(identifier interface{}) (*model.Senior, error) {
	var sr model.Senior
	q := s.DB
	switch v := identifier.(type) {
	case uint:
		q = q.First(&sr, v)
	case string:
		q = q.Where("card_no = ?", v).First(&sr)
	default:
		return nil, ErrNotFound
	}
	if err := q.Error; err != nil {
		return nil, ErrNotFound
	}
	return &sr, nil
}

// ---------------- 资格登记 ----------------

// AddEligibility 登记一条资格（生效/暂停/恢复），同一天只允许一条。
// 允许补录历史日期——资格本来就是按日期查询，历史登记只影响「尚未固化」的计算。
func (s *Service) AddEligibility(seniorID uint, date, status, levelName string, subsidy int64, reason string) (*model.EligRecord, error) {
	if _, err := s.getSenior(seniorID); err != nil {
		return nil, err
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, fmt.Errorf("日期格式应为 YYYY-MM-DD")
	}
	if status != StatusActive && status != StatusSuspended {
		return nil, fmt.Errorf("status 只能是 active 或 suspended")
	}
	if status == StatusSuspended {
		subsidy = 0 // 暂停期间无补贴
	}
	rec := &model.EligRecord{
		SeniorID: seniorID, EffectiveDate: date, Status: status,
		LevelName: levelName, SubsidyFen: subsidy, Reason: reason,
	}
	if err := s.DB.Create(rec).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("%w: 该日期已有资格登记", ErrConflict)
		}
		return nil, err
	}
	return rec, nil
}

func (s *Service) ListEligibility(seniorID uint) ([]model.EligRecord, error) {
	var out []model.EligRecord
	err := s.DB.Where("senior_id = ?", seniorID).Order("effective_date, id").Find(&out).Error
	return out, err
}

func validateElig(date, status string) error {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return fmt.Errorf("日期格式应为 YYYY-MM-DD")
	}
	if status != StatusActive && status != StatusSuspended {
		return fmt.Errorf("status 只能是 active 或 suspended")
	}
	return nil
}

// UpdateEligibility 更正一条资格登记（含生效日期/状态/等级/标准）。
// 已核销餐次保存的是快照，更正不会静默改写历史；需通过封账前重算或封账后补差体现。
func (s *Service) UpdateEligibility(recordID uint, date, status, levelName string, subsidy int64, reason string) (*model.EligRecord, error) {
	var rec model.EligRecord
	if err := s.DB.First(&rec, recordID).Error; err != nil {
		return nil, ErrNotFound
	}
	if err := validateElig(date, status); err != nil {
		return nil, err
	}
	if status == StatusSuspended {
		subsidy = 0
	}
	rec.EffectiveDate = date
	rec.Status = status
	rec.LevelName = levelName
	rec.SubsidyFen = subsidy
	rec.Reason = reason
	if err := s.DB.Save(&rec).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("%w: 该日期已有资格登记", ErrConflict)
		}
		return nil, err
	}
	return &rec, nil
}

// DeleteEligibility 删除一条登记错误的资格记录。
func (s *Service) DeleteEligibility(recordID uint) error {
	res := s.DB.Delete(&model.EligRecord{}, recordID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// eligibilityAt 返回某老人在「就餐日」当天适用的资格登记。
// 规则：EffectiveDate <= mealDate 的最新一条；一条都没有则视为 inactive。
func (s *Service) eligibilityAt(tx *gorm.DB, seniorID uint, mealDate string) (*model.EligRecord, error) {
	var rec model.EligRecord
	err := tx.Where("senior_id = ? AND effective_date <= ?", seniorID, mealDate).
		Order("effective_date DESC, id DESC").First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// CurrentStatus 返回老人「今天」的资格状态（仅用于展示，绝不参与历史餐次计算）。
func (s *Service) CurrentStatus(seniorID uint) (string, error) {
	today := time.Now().Format("2006-01-02")
	rec, err := s.eligibilityAt(s.DB, seniorID, today)
	if err != nil {
		return "", err
	}
	if rec == nil {
		return StatusInactive, nil
	}
	return rec.Status, nil
}

func basisDescribe(rec *model.EligRecord, mealDate string) string {
	if rec == nil {
		return fmt.Sprintf("%s 就餐：此前无任何生效资格登记，按全自付处理，不享补贴", mealDate)
	}
	if rec.Status == StatusSuspended {
		return fmt.Sprintf("%s 就餐：适用自 %s 起的「暂停」登记（%s），当日不享补贴",
			mealDate, rec.EffectiveDate, nonEmpty(rec.Reason, "暂停"))
	}
	return fmt.Sprintf("%s 就餐：适用 %s 生效的「%s」资格，每餐补贴标准 %d 分（依据登记#%d，%s）",
		mealDate, rec.EffectiveDate, nonEmpty(rec.LevelName, "补贴对象"),
		rec.SubsidyFen, rec.ID, nonEmpty(rec.Reason, "生效"))
}

func nonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// applyBasis 依据资格快照计算当餐补贴。
func applyBasis(price int64, rec *model.EligRecord, mealDate string) model.Meal {
	m := model.Meal{PriceFen: price, MealDate: mealDate}
	if rec != nil {
		m.EliStatus = rec.Status
		m.EliLevel = rec.LevelName
		m.SubsidyRateFen = rec.SubsidyFen
		m.BasisRecordID = rec.ID
		m.BasisEffectiveDate = rec.EffectiveDate
		m.BasisReason = rec.Reason
	} else {
		m.EliStatus = StatusInactive
	}
	m.BasisDescribe = basisDescribe(rec, mealDate)
	if rec != nil && rec.Status == StatusActive {
		m.SubsidyFen = min64(rec.SubsidyFen, price)
	}
	m.SelfPayFen = price - m.SubsidyFen
	return m
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// ---------------- 食堂核销 ----------------

type VerifyRequest struct {
	CardNo   string `json:"card_no"`
	SeniorID uint   `json:"senior_id"`
	MealDate string `json:"meal_date"`
	MealType string `json:"meal_type"`
	PriceFen int64  `json:"price_fen"`
}

type VerifyResult struct {
	Meal      model.Meal `json:"meal"`
	Duplicate bool       `json:"duplicate"`
	Message   string     `json:"message"`
}

// VerifyMeal 逐餐核销。同一老人同一天同一餐次只核销一次（唯一索引兜底），
// 重复扫码直接返回首次记录，不产生第二笔补贴。
func (s *Service) VerifyMeal(req VerifyRequest) (*VerifyResult, error) {
	if req.MealDate == "" || req.MealType == "" {
		return nil, fmt.Errorf("meal_date / meal_type 必填")
	}
	if _, err := time.Parse("2006-01-02", req.MealDate); err != nil {
		return nil, fmt.Errorf("meal_date 格式应为 YYYY-MM-DD")
	}
	if req.PriceFen < 0 {
		return nil, fmt.Errorf("餐价不能为负")
	}

	var ident interface{}
	if req.CardNo != "" {
		ident = req.CardNo
	} else {
		ident = req.SeniorID
	}
	sr, err := s.getSenior(ident)
	if err != nil {
		return nil, err
	}

	var result *VerifyResult
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// 幂等：先查是否已核销
		var existing model.Meal
		getErr := tx.Where("senior_id = ? AND meal_date = ? AND meal_type = ?",
			sr.ID, req.MealDate, req.MealType).First(&existing).Error
		if getErr == nil {
			result = &VerifyResult{
				Meal:      existing,
				Duplicate: true,
				Message:   "该餐次已核销，重复扫码不再产生补贴",
			}
			return nil
		}
		if !errors.Is(getErr, gorm.ErrRecordNotFound) {
			return getErr
		}

		// 以「就餐日」资格为准固化快照
		rec, err := s.eligibilityAt(tx, sr.ID, req.MealDate)
		if err != nil {
			return err
		}
		m := applyBasis(req.PriceFen, rec, req.MealDate)
		m.SeniorID = sr.ID
		m.SeniorName = sr.Name
		m.CardNo = sr.CardNo
		m.MealDate = req.MealDate
		m.MealType = req.MealType

		// 本地模拟身份核验与支付
		ok, vtx := mockpay.VerifyIdentity(sr.ID, sr.CardNo, req.MealDate, req.MealType)
		if !ok {
			return fmt.Errorf("身份核验未通过")
		}
		m.VerifyTxID = vtx
		m.PayTxID = mockpay.Pay(sr.ID, m.SelfPayFen, req.MealDate, req.MealType)

		if err := tx.Create(&m).Error; err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				// 并发重复扫码：回读已存在记录
				if e2 := tx.Where("senior_id = ? AND meal_date = ? AND meal_type = ?",
					sr.ID, req.MealDate, req.MealType).First(&existing).Error; e2 == nil {
					result = &VerifyResult{Meal: existing, Duplicate: true,
						Message: "该餐次已核销，重复扫码不再产生补贴"}
					return nil
				}
			}
			return err
		}

		// 补贴流水（追加式）
		if err := tx.Create(&model.Ledger{
			SeniorID: sr.ID, Kind: "meal_subsidy", AmountFen: m.SubsidyFen,
			Period: req.MealDate[:7], MealID: m.ID, Revision: 0,
			Note: basisDescribe(rec, req.MealDate),
		}).Error; err != nil {
			return err
		}

		result = &VerifyResult{Meal: m, Message: "核销成功"}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListMeals(seniorID uint, period string) ([]model.Meal, error) {
	q := s.DB.Where("senior_id = ?", seniorID)
	if period != "" {
		q = q.Where("substr(meal_date,1,7) = ?", period)
	}
	var out []model.Meal
	err := q.Order("meal_date, meal_type, id").Find(&out).Error
	return out, err
}

// ---------------- 封账前重算 ----------------

type RecalcReport struct {
	Checked        int          `json:"checked"`
	Changed        int          `json:"changed"`
	SkippedSettled int64        `json:"skipped_settled"`
	Changes        []RecalcItem `json:"changes"`
}

type RecalcItem struct {
	MealID     uint   `json:"meal_id"`
	MealDate   string `json:"meal_date"`
	MealType   string `json:"meal_type"`
	OldSubsidy int64  `json:"old_subsidy_fen"`
	NewSubsidy int64  `json:"new_subsidy_fen"`
	Delta      int64  `json:"delta_fen"`
	OldBasis   string `json:"old_basis"`
	NewBasis   string `json:"new_basis"`
}

// RecalcUnsettled 月结前发现资格登记有误：按最新登记重算「尚未封账」的餐次。
// 已封账餐次一律跳过（它们只能走补差单）。
func (s *Service) RecalcUnsettled(seniorID uint, startDate, endDate string) (*RecalcReport, error) {
	if _, err := s.getSenior(seniorID); err != nil {
		return nil, err
	}
	rep := &RecalcReport{}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		q := tx.Where("senior_id = ? AND settled = ?", seniorID, false)
		if startDate != "" {
			q = q.Where("meal_date >= ?", startDate)
		}
		if endDate != "" {
			q = q.Where("meal_date <= ?", endDate)
		}
		var meals []model.Meal
		if err := q.Order("meal_date, meal_type").Find(&meals).Error; err != nil {
			return err
		}
		for _, m := range meals {
			rep.Checked++
			rec, err := s.eligibilityAt(tx, seniorID, m.MealDate)
			if err != nil {
				return err
			}
			nm := applyBasis(m.PriceFen, rec, m.MealDate)
			if nm.SubsidyFen == m.SubsidyFen && nm.EliStatus == m.EliStatus &&
				nm.BasisRecordID == m.BasisRecordID {
				continue // 计算结果未变
			}

			oldSubsidy, oldBasis, oldRev := m.SubsidyFen, m.BasisDescribe, m.Revision

			// 旧补贴流水作废（保留留痕），追加新版本流水
			if err := tx.Model(&model.Ledger{}).
				Where("meal_id = ? AND kind = 'meal_subsidy' AND voided = ?", m.ID, false).
				Updates(map[string]interface{}{
					"voided":      true,
					"void_reason": "资格登记更正，封账前重算作废",
				}).Error; err != nil {
				return err
			}

			m.EliStatus = nm.EliStatus
			m.EliLevel = nm.EliLevel
			m.SubsidyRateFen = nm.SubsidyRateFen
			m.BasisRecordID = nm.BasisRecordID
			m.BasisEffectiveDate = nm.BasisEffectiveDate
			m.BasisReason = nm.BasisReason
			m.BasisDescribe = nm.BasisDescribe
			m.SubsidyFen = nm.SubsidyFen
			m.SelfPayFen = nm.SelfPayFen
			m.Revision++
			if err := tx.Save(&m).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.Ledger{
				SeniorID: seniorID, Kind: "meal_subsidy", AmountFen: m.SubsidyFen,
				Period: m.MealDate[:7], MealID: m.ID, Revision: m.Revision,
				Note: nm.BasisDescribe,
			}).Error; err != nil {
				return err
			}

			rep.Changed++
			rep.Changes = append(rep.Changes, RecalcItem{
				MealID: m.ID, MealDate: m.MealDate, MealType: m.MealType,
				OldSubsidy: oldSubsidy, NewSubsidy: m.SubsidyFen,
				Delta: m.SubsidyFen - oldSubsidy, OldBasis: oldBasis,
				NewBasis: m.BasisDescribe,
			})
			_ = oldRev
		}

		// 统计区间内被跳过的已封账餐次
		sq := tx.Model(&model.Meal{}).Where("senior_id = ? AND settled = ?", seniorID, true)
		if startDate != "" {
			sq = sq.Where("meal_date >= ?", startDate)
		}
		if endDate != "" {
			sq = sq.Where("meal_date <= ?", endDate)
		}
		return sq.Count(&rep.SkippedSettled).Error
	})
	if err != nil {
		return nil, err
	}
	return rep, nil
}

// ---------------- 月结封账 ----------------

func periodBounds(period string) (string, string, error) {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return "", "", fmt.Errorf("period 格式应为 YYYY-MM")
	}
	start := t.Format("2006-01-02")
	end := t.AddDate(0, 1, -1).Format("2006-01-02")
	return start, end, nil
}

// CloseMonth 对某老人某月份的餐次封账。封账结果固化，之后资格改动不影响它。
func (s *Service) CloseMonth(seniorID uint, period string) (*model.Settlement, error) {
	sr, err := s.getSenior(seniorID)
	if err != nil {
		return nil, err
	}
	start, end, err := periodBounds(period)
	if err != nil {
		return nil, err
	}

	var st *model.Settlement
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// 已封账则直接拒绝（保持结果不可变）
		var cnt int64
		if err := tx.Model(&model.Settlement{}).
			Where("senior_id = ? AND period = ?", seniorID, period).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			return fmt.Errorf("%w: %s 已封账，不能重复封账；如需更正请出具补差单", ErrConflict, period)
		}

		var meals []model.Meal
		if err := tx.Where("senior_id = ? AND settled = ? AND meal_date >= ? AND meal_date <= ?",
			seniorID, false, start, end).
			Order("meal_date, meal_type").Find(&meals).Error; err != nil {
			return err
		}

		st = &model.Settlement{
			SeniorID: sr.ID, SeniorName: sr.Name, Period: period,
			StartDate: start, EndDate: end, ClosedAt: time.Now(),
		}
		mealIDs := make([]uint, 0, len(meals))
		for _, m := range meals {
			st.MealCount++
			st.TotalPriceFen += m.PriceFen
			st.TotalSubsidyFen += m.SubsidyFen
			st.TotalSelfPayFen += m.SelfPayFen
			mealIDs = append(mealIDs, m.ID)
		}
		if err := tx.Create(st).Error; err != nil {
			return err
		}
		if len(mealIDs) > 0 {
			if err := tx.Model(&model.Meal{}).Where("id IN ?", mealIDs).
				Updates(map[string]interface{}{"settled": true, "settlement_id": st.ID}).Error; err != nil {
				return err
			}
			// 把对应有效补贴流水归入该结算单（作废重算后的旧分录不带入）
			if err := tx.Model(&model.Ledger{}).
				Where("senior_id = ? AND kind = 'meal_subsidy' AND voided = ? AND meal_id IN ?",
					sr.ID, false, mealIDs).
				Update("settlement_id", st.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Service) ListSettlements(seniorID uint, period string) ([]model.Settlement, error) {
	q := s.DB.Model(&model.Settlement{})
	if seniorID != 0 {
		q = q.Where("senior_id = ?", seniorID)
	}
	if period != "" {
		q = q.Where("period = ?", period)
	}
	var out []model.Settlement
	err := q.Order("period DESC, senior_id").Find(&out).Error
	return out, err
}

func (s *Service) GetSettlement(id uint) (*model.Settlement, []model.Meal, []model.AdjustmentVoucher, error) {
	var st model.Settlement
	if err := s.DB.First(&st, id).Error; err != nil {
		return nil, nil, nil, ErrNotFound
	}
	var meals []model.Meal
	if err := s.DB.Where("settlement_id = ?", id).Order("meal_date, meal_type").Find(&meals).Error; err != nil {
		return nil, nil, nil, err
	}
	var vouchers []model.AdjustmentVoucher
	if err := s.DB.Where("settlement_id = ?", id).Order("posted_at, id").Find(&vouchers).Error; err != nil {
		return nil, nil, nil, err
	}
	for i := range vouchers {
		if err := s.DB.Where("voucher_id = ?", vouchers[i].ID).Find(&vouchers[i].Items).Error; err != nil {
			return nil, nil, nil, err
		}
	}
	return &st, meals, vouchers, nil
}

// ---------------- 封账后补差单 ----------------

// CreateAdjustment 封账后发现资格错误：按当前登记重算受影响餐次，
// 但只「追加」补差单与调整流水，原结算单、原餐次快照一律不改。
func (s *Service) CreateAdjustment(seniorID uint, period, reason string) (*model.AdjustmentVoucher, error) {
	sr, err := s.getSenior(seniorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := periodBounds(period); err != nil {
		return nil, err
	}

	var v *model.AdjustmentVoucher
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var st model.Settlement
		if err := tx.Where("senior_id = ? AND period = ?", seniorID, period).First(&st).Error; err != nil {
			return fmt.Errorf("该月份尚未封账，不能出补差单；请先封账或使用封账前重算")
		}

		var meals []model.Meal
		if err := tx.Where("settlement_id = ?", st.ID).Order("meal_date, meal_type").
			Find(&meals).Error; err != nil {
			return err
		}

		// 既往补差单已对各餐次累计调整过的差额（只追加模式下据此做增量，
		// 既防止重复补差，也支持对同一月连续多次更正）。
		priorItems := []model.VoucherItem{}
		if err := tx.Model(&model.VoucherItem{}).
			Joins("JOIN adjustment_vouchers ON adjustment_vouchers.id = voucher_items.voucher_id").
			Where("adjustment_vouchers.settlement_id = ?", st.ID).
			Find(&priorItems).Error; err != nil {
			return err
		}
		priorDelta := map[uint]int64{}
		for _, pi := range priorItems {
			priorDelta[pi.MealID] += pi.DeltaFen
		}

		v = &model.AdjustmentVoucher{
			SeniorID: sr.ID, SeniorName: sr.Name, SettlementID: st.ID, Period: period,
			Reason: reason,
		}
		var items []model.VoucherItem
		dateSet := map[string]bool{}
		var dates []string
		for _, m := range meals {
			rec, err := s.eligibilityAt(tx, seniorID, m.MealDate)
			if err != nil {
				return err
			}
			nm := applyBasis(m.PriceFen, rec, m.MealDate)
			// 已调整后的有效补贴 = 原快照 + 既往补差；本次只需补足剩余差额
			effectiveSubsidy := m.SubsidyFen + priorDelta[m.ID]
			delta := nm.SubsidyFen - effectiveSubsidy
			if delta == 0 {
				continue
			}
			item := model.VoucherItem{
				MealID: m.ID, MealDate: m.MealDate, MealType: m.MealType,
				OldSubsidyFen: effectiveSubsidy, NewSubsidyFen: nm.SubsidyFen,
				DeltaFen: delta, OldBasis: m.BasisDescribe, NewBasis: nm.BasisDescribe,
			}
			items = append(items, item)
			v.TotalDeltaFen += delta
			v.MealCount++
			if !dateSet[m.MealDate] {
				dateSet[m.MealDate] = true
				dates = append(dates, m.MealDate)
			}
		}
		sort.Strings(dates)
		v.AffectedDates = strings.Join(dates, ",")
		if len(items) == 0 {
			return fmt.Errorf("按现行资格登记重算后没有差异，无需补差")
		}
		if err := tx.Create(v).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].VoucherID = v.ID
			if err := tx.Create(&items[i]).Error; err != nil {
				return err
			}
		}
		v.Items = items
		v.PayTxID = mockpay.SettleAdjustment(v.ID, v.TotalDeltaFen)
		if err := tx.Model(v).Update("pay_tx_id", v.PayTxID).Error; err != nil {
			return err
		}

		// 调整流水（有符号、只追加），归入原结算单
		return tx.Create(&model.Ledger{
			SeniorID: sr.ID, Kind: "adjustment", AmountFen: v.TotalDeltaFen,
			Period: period, SettlementID: st.ID, VoucherID: v.ID,
			Note: fmt.Sprintf("补差单#%d：%s；受影响日期 %s", v.ID, reason, v.AffectedDates),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Service) ListAdjustments(seniorID uint, period string) ([]model.AdjustmentVoucher, error) {
	q := s.DB.Model(&model.AdjustmentVoucher{})
	if seniorID != 0 {
		q = q.Where("senior_id = ?", seniorID)
	}
	if period != "" {
		q = q.Where("period = ?", period)
	}
	var out []model.AdjustmentVoucher
	if err := q.Order("period DESC, posted_at DESC, id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.DB.Where("voucher_id = ?", out[i].ID).Find(&out[i].Items).Error; err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---------------- 对账 ----------------

type CheckLine struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type ReconcileReport struct {
	SeniorID          uint        `json:"senior_id"`
	SeniorName        string      `json:"senior_name"`
	Period            string      `json:"period"`
	Settled           bool        `json:"settled"`
	MealCount         int         `json:"meal_count"`
	MealTableSubsidy  int64       `json:"meal_table_subsidy_fen"`
	LiveLedgerSubsidy int64       `json:"live_ledger_subsidy_fen"`
	VoidedLedgerCount int64       `json:"voided_ledger_count"`
	SettlementSubsidy int64       `json:"settlement_subsidy_fen"`
	AdjustmentSubsidy int64       `json:"adjustment_subsidy_fen"`
	VoucherDelta      int64       `json:"voucher_delta_fen"`
	NetPayable        int64       `json:"net_payable_fen"`
	Checks            []CheckLine `json:"checks"`
}

// Reconcile 校验补贴流水与餐次表、结算单、补差单金额是否能对上账。
func (s *Service) Reconcile(seniorID uint, period string) (*ReconcileReport, error) {
	if _, err := s.getSenior(seniorID); err != nil {
		return nil, err
	}
	start, end, err := periodBounds(period)
	if err != nil {
		return nil, err
	}
	r := &ReconcileReport{SeniorID: seniorID, Period: period}

	var sr model.Senior
	_ = s.DB.First(&sr, seniorID)
	r.SeniorName = sr.Name

	// 餐次表（按就餐日期落在本月）
	var meals []model.Meal
	if err := s.DB.Where("senior_id = ? AND meal_date >= ? AND meal_date <= ?",
		seniorID, start, end).Order("meal_date, meal_type").Find(&meals).Error; err != nil {
		return nil, err
	}
	r.MealCount = len(meals)
	for _, m := range meals {
		r.MealTableSubsidy += m.SubsidyFen
	}

	// 补贴流水
	var live, adj int64
	var voided int64
	s.DB.Model(&model.Ledger{}).
		Where("senior_id = ? AND period = ? AND kind = 'meal_subsidy' AND voided = ?", seniorID, period, false).
		Select("COALESCE(SUM(amount_fen),0)").Scan(&live)
	s.DB.Model(&model.Ledger{}).
		Where("senior_id = ? AND period = ? AND kind = 'meal_subsidy' AND voided = ?", seniorID, period, true).
		Count(&voided)
	s.DB.Model(&model.Ledger{}).
		Where("senior_id = ? AND period = ? AND kind = 'adjustment'", seniorID, period).
		Select("COALESCE(SUM(amount_fen),0)").Scan(&adj)
	r.LiveLedgerSubsidy, r.VoidedLedgerCount, r.AdjustmentSubsidy = live, voided, adj

	// 结算单
	var st model.Settlement
	stErr := s.DB.Where("senior_id = ? AND period = ?", seniorID, period).First(&st).Error
	if stErr == nil {
		r.Settled = true
		r.SettlementSubsidy = st.TotalSubsidyFen
	}

	// 补差单
	var vouchers []model.AdjustmentVoucher
	s.DB.Where("senior_id = ? AND period = ?", seniorID, period).Find(&vouchers)
	for _, v := range vouchers {
		r.VoucherDelta += v.TotalDeltaFen
	}

	// 逐笔勾稽：每条有效 meal_subsidy 流水必须对应真实餐次且金额一致
	mismatch := []string{}
	seenMeal := map[uint]int64{}
	for _, m := range meals {
		seenMeal[m.ID] = m.SubsidyFen
	}
	var entries []model.Ledger
	s.DB.Where("senior_id = ? AND period = ? AND kind = 'meal_subsidy' AND voided = ?",
		seniorID, period, false).Find(&entries)
	ledgerByMeal := map[uint]int64{}
	for _, e := range entries {
		ledgerByMeal[e.MealID] += e.AmountFen
	}
	for mealID, amt := range seenMeal {
		if ledgerByMeal[mealID] != amt {
			mismatch = append(mismatch, fmt.Sprintf("餐次#%d 表内补贴 %d 与流水 %d 不符",
				mealID, amt, ledgerByMeal[mealID]))
		}
	}
	for mealID, amt := range ledgerByMeal {
		if _, ok := seenMeal[mealID]; !ok {
			mismatch = append(mismatch, fmt.Sprintf("流水指向不存在餐次#%d（%d 分）", mealID, amt))
		}
	}

	r.Checks = append(r.Checks, CheckLine{
		Name:   "餐次表合计 = 有效补贴流水",
		OK:     r.MealTableSubsidy == r.LiveLedgerSubsidy,
		Detail: fmt.Sprintf("餐次表 %d 分，有效流水 %d 分，作废重算分录 %d 条", r.MealTableSubsidy, r.LiveLedgerSubsidy, r.VoidedLedgerCount),
	})
	r.Checks = append(r.Checks, CheckLine{
		Name:   "逐餐金额勾稽",
		OK:     len(mismatch) == 0,
		Detail: nonEmpty(strings.Join(mismatch, "；"), "每笔有效流水均与餐次快照一一对应"),
	})
	if r.Settled {
		r.Checks = append(r.Checks, CheckLine{
			Name:   "封账金额 = 固化流水（封账后未被改写）",
			OK:     r.SettlementSubsidy == r.LiveLedgerSubsidy,
			Detail: fmt.Sprintf("结算单 %d 分，有效流水 %d 分", r.SettlementSubsidy, r.LiveLedgerSubsidy),
		})
		r.Checks = append(r.Checks, CheckLine{
			Name:   "补差单合计 = 调整流水（只追加）",
			OK:     r.VoucherDelta == r.AdjustmentSubsidy,
			Detail: fmt.Sprintf("补差单合计 %d 分，调整流水 %d 分，补差单 %d 张", r.VoucherDelta, r.AdjustmentSubsidy, len(vouchers)),
		})
		r.NetPayable = r.SettlementSubsidy + r.AdjustmentSubsidy
		r.Checks = append(r.Checks, CheckLine{
			Name:   "本月净应付补贴",
			OK:     r.NetPayable == r.SettlementSubsidy+r.VoucherDelta,
			Detail: fmt.Sprintf("原结算 %d 分 %+d 调整 = %d 分", r.SettlementSubsidy, r.AdjustmentSubsidy, r.NetPayable),
		})
	} else {
		r.Checks = append(r.Checks, CheckLine{
			Name:   "未封账",
			OK:     true,
			Detail: "尚未封账，可继续更正资格登记并重算",
		})
	}
	return r, nil
}
