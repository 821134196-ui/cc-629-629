package model

import "time"

// 金额一律以「分」为单位存储，避免浮点误差。

// Senior 社区登记的老人
type Senior struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name"`
	CardNo    string    `json:"card_no" gorm:"uniqueIndex"`
	Community string    `json:"community"`
	CreatedAt time.Time `json:"created_at"`
}

// EligRecord 资格时间线上的一条登记：生效 / 暂停 / 恢复。
// 某就餐日期适用的资格 = EffectiveDate <= 该日期 的最新一条。
type EligRecord struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	SeniorID      uint      `json:"senior_id" gorm:"index;uniqueIndex:idx_eli_senior_date,priority:1"`
	EffectiveDate string    `json:"effective_date" gorm:"type:varchar(10);uniqueIndex:idx_eli_senior_date,priority:2"` // YYYY-MM-DD
	Status        string    `json:"status"`                                                                            // active=生效(享补贴) / suspended=暂停(无补贴)
	LevelName     string    `json:"level_name"`
	SubsidyFen    int64     `json:"subsidy_fen"` // 该等级每餐补贴标准（分）
	Reason        string    `json:"reason"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Meal 一次餐次核销。核销时把「当日适用资格」固化为快照字段，
// 之后无论资格记录如何修改、老人当前状态如何变化，本餐记录不被隐式改写。
type Meal struct {
	ID uint `json:"id" gorm:"primaryKey"`

	SeniorID   uint   `json:"senior_id" gorm:"uniqueIndex:idx_meal_slot,priority:1"`
	SeniorName string `json:"senior_name"`
	CardNo     string `json:"card_no"`

	MealDate string `json:"meal_date" gorm:"type:varchar(10);uniqueIndex:idx_meal_slot,priority:2"`
	MealType string `json:"meal_type" gorm:"uniqueIndex:idx_meal_slot,priority:3"` // 午餐 / 晚餐
	PriceFen int64  `json:"price_fen"`

	// —— 资格快照（核销当时适用的资格）——
	EliStatus          string `json:"eli_status"`       // active / suspended / inactive
	EliLevel           string `json:"eli_level"`        // 快照等级名称
	SubsidyRateFen     int64  `json:"subsidy_rate_fen"` // 快照补贴标准
	BasisRecordID      uint   `json:"basis_record_id"`  // 依据的资格登记（0=无有效登记）
	BasisEffectiveDate string `json:"basis_effective_date"`
	BasisReason        string `json:"basis_reason"`
	BasisDescribe      string `json:"basis_describe"` // 计算依据的文字说明

	SubsidyFen int64 `json:"subsidy_fen"`  // 补贴金额 = min(标准, 餐价)
	SelfPayFen int64 `json:"self_pay_fen"` // 个人自付 = 餐价 - 补贴

	// —— 本地模拟身份核验 / 支付 ——
	VerifyTxID string `json:"verify_tx_id"`
	PayTxID    string `json:"pay_tx_id"`

	Settled      bool      `json:"settled" gorm:"index"`
	SettlementID uint      `json:"settlement_id" gorm:"index"`
	Revision     int       `json:"revision"` // 重算次数（补贴被修订过几次）
	CreatedAt    time.Time `json:"created_at"`
}

// Settlement 月结单：按老人 × 月份封账，金额为封账时固化值，封账后不可变。
type Settlement struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	SeniorID        uint      `json:"senior_id" gorm:"uniqueIndex:idx_settle_senior_period,priority:1"`
	SeniorName      string    `json:"senior_name"`
	Period          string    `json:"period" gorm:"type:varchar(7);uniqueIndex:idx_settle_senior_period,priority:2"` // YYYY-MM
	StartDate       string    `json:"start_date"`
	EndDate         string    `json:"end_date"`
	MealCount       int       `json:"meal_count"`
	TotalPriceFen   int64     `json:"total_price_fen"`
	TotalSubsidyFen int64     `json:"total_subsidy_fen"`
	TotalSelfPayFen int64     `json:"total_self_pay_fen"`
	ClosedAt        time.Time `json:"closed_at"`
}

// AdjustmentVoucher 补差单：封账后发现资格记录错误时，只追加、不改原结算。
type AdjustmentVoucher struct {
	ID            uint          `json:"id" gorm:"primaryKey"`
	SeniorID      uint          `json:"senior_id" gorm:"index"`
	SeniorName    string        `json:"senior_name"`
	SettlementID  uint          `json:"settlement_id" gorm:"index"`
	Period        string        `json:"period" gorm:"type:varchar(7);index"`
	Reason        string        `json:"reason"`
	MealCount     int           `json:"meal_count"`
	TotalDeltaFen int64         `json:"total_delta_fen"`                 // 有符号：正=补拨，负=追回/冲减
	AffectedDates string        `json:"affected_dates" gorm:"type:text"` // 受影响日期清单，逗号分隔
	PayTxID       string        `json:"pay_tx_id"`
	PostedAt      time.Time     `json:"posted_at"`
	Items         []VoucherItem `json:"items" gorm:"foreignKey:VoucherID"`
}

// VoucherItem 补差单内逐餐差异
type VoucherItem struct {
	ID            uint   `json:"id" gorm:"primaryKey"`
	VoucherID     uint   `json:"voucher_id" gorm:"index"`
	MealID        uint   `json:"meal_id"`
	MealDate      string `json:"meal_date"`
	MealType      string `json:"meal_type"`
	OldSubsidyFen int64  `json:"old_subsidy_fen"`
	NewSubsidyFen int64  `json:"new_subsidy_fen"`
	DeltaFen      int64  `json:"delta_fen"`
	OldBasis      string `json:"old_basis"`
	NewBasis      string `json:"new_basis"`
}

// Ledger 补贴流水。所有补贴金额变动只追加：
//   - meal_subsidy：核销/重算产生的当餐补贴（重算时旧分录置 Voided，新增一条）
//   - adjustment ：封账后补差单产生的有符号调整
type Ledger struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	SeniorID     uint      `json:"senior_id" gorm:"index"`
	Kind         string    `json:"kind"` // meal_subsidy / adjustment
	AmountFen    int64     `json:"amount_fen"`
	Period       string    `json:"period" gorm:"type:varchar(7);index"`
	MealID       uint      `json:"meal_id"`
	SettlementID uint      `json:"settlement_id" gorm:"index"`
	VoucherID    uint      `json:"voucher_id"`
	Revision     int       `json:"revision"`
	Voided       bool      `json:"voided"`
	VoidReason   string    `json:"void_reason"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`
}
