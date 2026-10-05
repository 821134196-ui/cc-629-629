package main

import (
	"time"

	"gorm.io/gorm"
)

// Elderly 社区老人档案
type Elderly struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Name      string         `gorm:"not null" json:"name"`
	CardNo    string         `gorm:"uniqueIndex;not null" json:"card_no"` // 助餐卡号 / 扫码凭证
	Phone     string         `json:"phone"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// QualEvent 资格记录：生效 / 暂停 / 恢复，均带日期
// 同一老人同一天只允许一条记录（POST 时按 老人+日期 幂等更新）
type QualEvent struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ElderlyID   uint      `gorm:"uniqueIndex:idx_qual_elder_date;not null" json:"elderly_id"`
	Date        string    `gorm:"uniqueIndex:idx_qual_elder_date;not null" json:"date"` // YYYY-MM-DD
	Action      string    `gorm:"not null" json:"action"`                               // activate | suspend | resume
	SubsidyRate float64   `gorm:"not null;default:0" json:"subsidy_rate"`               // activate 时的每餐补贴标准（元）
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MealRecord 逐餐核销记录，保存核销当时适用的资格快照
type MealRecord struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ElderlyID      uint      `gorm:"not null;index:idx_meal_elder,priority:1" json:"elderly_id"`
	MealDate       string    `gorm:"not null;index:idx_meal_elder,priority:2" json:"meal_date"` // YYYY-MM-DD
	MealType       string    `gorm:"not null;index:idx_meal_elder,priority:3" json:"meal_type"` // lunch | dinner
	IdempotencyKey string    `gorm:"uniqueIndex;not null" json:"idempotency_key"`               // 卡号+就餐日期+餐次，重复扫码只核销一次

	Price float64 `gorm:"not null" json:"price"` // 餐费标价

	// —— 核销当时的资格快照（结算依据），资格记录变更不会自动改写已封账餐次 ——
	Status        string  `gorm:"not null" json:"status"` // active | suspended
	SubsidyRate   float64 `gorm:"not null" json:"subsidy_rate"`
	SubsidyAmount float64 `gorm:"not null" json:"subsidy_amount"`
	SelfPay       float64 `gorm:"not null" json:"self_pay"`
	BasisEventID  *uint   `json:"basis_event_id"`
	BasisDetail   string  `gorm:"not null" json:"basis_detail"` // 人类可读的计算依据

	// 本地模拟的身份核验 / 支付流水号
	VerifyTxnID string `gorm:"not null" json:"verify_txn_id"`
	PayTxnID    string `gorm:"not null" json:"pay_txn_id"`

	// 月结前重算次数（可验证记录被重新计算过，但幂等键/支付流水不变）
	RecalcSeq int `gorm:"not null;default:0" json:"recalc_seq"`

	// 封账后归属结算单；NULL 表示尚未结算，可被重算
	SettlementID *uint      `gorm:"index" json:"settlement_id"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Settlement 月度结算单：open（月结中，可重算餐次）→ closed（已封账，只追加补差单）
type Settlement struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Period      string     `gorm:"uniqueIndex;not null" json:"period"` // YYYY-MM
	Status      string     `gorm:"not null" json:"status"`             // open | closed
	TotalMeals  int        `gorm:"not null" json:"total_meals"`
	TotalPrice  float64    `gorm:"not null" json:"total_price"`
	TotalSubsidy float64   `gorm:"not null" json:"total_subsidy"`
	TotalSelfPay float64   `gorm:"not null" json:"total_self_pay"`
	ClosedAt    *time.Time `json:"closed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Adjustment 封账后补差单：原结算结果保留不动，只追加调整
type Adjustment struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Period        string    `gorm:"not null;index" json:"period"`
	SettlementID  uint      `gorm:"not null" json:"settlement_id"`
	ElderlyID     uint      `gorm:"not null;index" json:"elderly_id"`
	ElderlyName   string    `gorm:"not null" json:"elderly_name"`
	AffectedDates string    `gorm:"not null" json:"affected_dates"` // JSON 数组：受影响就餐日期
	MealCount     int       `gorm:"not null" json:"meal_count"`
	OldSubsidy    float64   `gorm:"not null" json:"old_subsidy"`
	NewSubsidy    float64   `gorm:"not null" json:"new_subsidy"`
	DiffAmount    float64   `gorm:"not null" json:"diff_amount"` // 正为补发，负为追回
	Reason        string    `gorm:"not null" json:"reason"`
	PayTxnID      string    `gorm:"not null" json:"pay_txn_id"` // 模拟补差支付流水
	CreatedAt     time.Time `json:"created_at"`
}
