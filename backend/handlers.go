package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct{ db *gorm.DB }

func writeErr(c *gin.Context, code int, format string, a ...any) {
	c.JSON(code, gin.H{"error": fmt.Sprintf(format, a...)})
}

// ---- 老人档案 ----

func (h *Handler) createElder(c *gin.Context) {
	var in struct {
		Name   string `json:"name" binding:"required"`
		CardNo string `json:"card_no" binding:"required"`
		Phone  string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		writeErr(c, 400, "参数错误: %v", err)
		return
	}
	e := Elderly{Name: in.Name, CardNo: in.CardNo, Phone: in.Phone}
	if err := h.db.Create(&e).Error; err != nil {
		writeErr(c, 409, "建档失败（卡号可能重复）: %v", err)
		return
	}
	c.JSON(201, e)
}

func (h *Handler) listElders(c *gin.Context) {
	q := c.Query("q")
	var out []Elderly
	tx := h.db.Model(&Elderly{}).Order("id")
	if q != "" {
		like := "%" + q + "%"
		tx = tx.Where("name LIKE ? OR card_no LIKE ? OR phone LIKE ?", like, like, like)
	}
	tx.Find(&out)
	c.JSON(200, out)
}

func (h *Handler) getElder(c *gin.Context) (*Elderly, bool) {
	id, _ := strconv.Atoi(c.Param("id"))
	var e Elderly
	if err := h.db.First(&e, id).Error; err != nil {
		writeErr(c, 404, "老人不存在")
		return nil, false
	}
	return &e, true
}

// ---- 资格记录 ----

func (h *Handler) addQualification(c *gin.Context) {
	e, ok := h.getElder(c)
	if !ok {
		return
	}
	var in struct {
		Date        string  `json:"date" binding:"required"`
		Action      string  `json:"action" binding:"required"`
		SubsidyRate float64 `json:"subsidy_rate"`
		Note        string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		writeErr(c, 400, "参数错误: %v", err)
		return
	}
	if _, err := time.Parse(dateLayout, in.Date); err != nil {
		writeErr(c, 400, "日期格式应为 YYYY-MM-DD")
		return
	}
	switch in.Action {
	case ActionActivate, ActionSuspend, ActionResume:
	default:
		writeErr(c, 400, "action 只能是 activate / suspend / resume")
		return
	}
	if in.Action == ActionActivate && in.SubsidyRate <= 0 {
		writeErr(c, 400, "资格生效必须填写正的补贴标准 subsidy_rate")
		return
	}

	// 同一老人同一天只保留一条：幂等更新（修正登记错误）
	var ev QualEvent
	err := h.db.Where("elderly_id = ? AND date = ?", e.ID, in.Date).First(&ev).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ev = QualEvent{
			ElderlyID: e.ID, Date: in.Date, Action: in.Action,
			SubsidyRate: in.SubsidyRate, Note: in.Note,
		}
		if err := h.db.Create(&ev).Error; err != nil {
			writeErr(c, 500, "登记失败: %v", err)
			return
		}
	} else if err != nil {
		writeErr(c, 500, "%v", err)
		return
	} else {
		ev.Action = in.Action
		ev.SubsidyRate = in.SubsidyRate
		ev.Note = in.Note
		if err := h.db.Save(&ev).Error; err != nil {
			writeErr(c, 500, "更新失败: %v", err)
			return
		}
	}
	c.JSON(200, gin.H{"event": ev, "message": "资格记录已保存"})
}

func (h *Handler) listQualifications(c *gin.Context) {
	e, ok := h.getElder(c)
	if !ok {
		return
	}
	var evs []QualEvent
	h.db.Where("elderly_id = ?", e.ID).Order("date DESC, id DESC").Find(&evs)
	c.JSON(200, evs)
}

// deleteQualification 删除一条登记错误的资格记录（月结前纠错用）
func (h *Handler) deleteQualification(c *gin.Context) {
	e, ok := h.getElder(c)
	if !ok {
		return
	}
	qid, _ := strconv.Atoi(c.Param("qid"))
	res := h.db.Where("id = ? AND elderly_id = ?", qid, e.ID).Delete(&QualEvent{})
	if res.Error != nil {
		writeErr(c, 500, "删除失败: %v", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		writeErr(c, 404, "资格记录不存在")
		return
	}
	// 删除后提示：已封账餐次不受影响（快照冻结），未结算餐次可点“重新计算”
	var settled, unsettled int64
	h.db.Model(&MealRecord{}).Where("elderly_id = ? AND settlement_id IS NOT NULL", e.ID).Count(&settled)
	h.db.Model(&MealRecord{}).Where("elderly_id = ? AND settlement_id IS NULL", e.ID).Count(&unsettled)
	c.JSON(200, gin.H{
		"message": fmt.Sprintf("资格记录已删除；已封账餐次 %d 笔保持原结果，未结算餐次 %d 笔请执行重新计算",
			settled, unsettled),
	})
}

func (h *Handler) eligibilityPreview(c *gin.Context) {
	e, ok := h.getElder(c)
	if !ok {
		return
	}
	d := c.Query("date")
	if d == "" {
		d = time.Now().Format(dateLayout)
	}
	el, err := eligibilityOnDate(h.db, e.ID, d)
	if err != nil {
		c.JSON(200, gin.H{"date": d, "status": "none", "subsidy_rate": 0,
			"basis_detail": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"date": d, "status": el.Status, "subsidy_rate": el.SubsidyRate,
		"basis_event_id": el.BasisEventID, "basis_detail": el.BasisDetail,
	})
}

// ---- 食堂核销 ----

func (h *Handler) checkin(c *gin.Context) {
	var in struct {
		CardNo   string  `json:"card_no" binding:"required"`
		MealDate string  `json:"meal_date"` // 可补录历史日期，默认今天
		MealType string  `json:"meal_type" binding:"required"`
		Price    float64 `json:"price" binding:"required"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		writeErr(c, 400, "参数错误: %v", err)
		return
	}
	if in.MealType != MealLunch && in.MealType != MealDinner {
		writeErr(c, 400, "meal_type 只能是 lunch / dinner")
		return
	}
	if in.MealDate == "" {
		in.MealDate = time.Now().Format(dateLayout)
	}
	if _, err := time.Parse(dateLayout, in.MealDate); err != nil {
		writeErr(c, 400, "就餐日期格式应为 YYYY-MM-DD")
		return
	}
	if in.Price <= 0 {
		writeErr(c, 400, "餐费标价必须大于 0")
		return
	}

	var e Elderly
	if err := h.db.Where("card_no = ?", in.CardNo).First(&e).Error; err != nil {
		writeErr(c, 404, "卡号不存在: %s", in.CardNo)
		return
	}

	key := fmt.Sprintf("%s|%s|%s", in.CardNo, in.MealDate, in.MealType)
	var existing MealRecord
	if err := h.db.Where("idempotency_key = ?", key).First(&existing).Error; err == nil {
		// 同一餐次重复扫码：返回原记录，不再产生第二笔补贴、不重复扣款
		c.JSON(200, gin.H{
			"duplicate": true,
			"meal":      existing,
			"message":   fmt.Sprintf("重复扫码：%s %s 已核销过，只核销一次，不产生第二笔补贴", in.MealDate, mealCN(in.MealType)),
		})
		return
	}

	// 1) 本地模拟身份核验
	vr := verifyIdentity(&e)
	// 2) 按【就餐日期】判定资格（核心：不套用当前状态）
	el, elErr := eligibilityOnDate(h.db, e.ID, in.MealDate)
	if elErr != nil {
		el = Eligibility{
			Status: StatusSuspended,
			BasisDetail: fmt.Sprintf("当日（%s）无生效资格记录，按全额自付处理", in.MealDate),
		}
	}
	subsidy, selfPay := computeSubsidy(in.Price, el)
	// 3) 本地模拟支付（自付部分）
	pr := chargePayment(selfPay)

	m := MealRecord{
		ElderlyID: e.ID, MealDate: in.MealDate, MealType: in.MealType,
		IdempotencyKey: key, Price: in.Price,
		Status: el.Status, SubsidyRate: el.SubsidyRate,
		SubsidyAmount: subsidy, SelfPay: selfPay,
		BasisEventID: el.BasisEventID, BasisDetail: el.BasisDetail,
		VerifyTxnID: vr.TxnID, PayTxnID: pr.TxnID,
	}
	if err := h.db.Create(&m).Error; err != nil {
		// 并发下唯一键冲突：仍按重复扫码处理
		if err := h.db.Where("idempotency_key = ?", key).First(&existing).Error; err == nil {
			c.JSON(200, gin.H{"duplicate": true, "meal": existing,
				"message": "重复扫码（并发请求）：该餐次已核销，只核销一次"})
			return
		}
		writeErr(c, 500, "核销失败: %v", err)
		return
	}
	c.JSON(201, gin.H{
		"duplicate": false, "meal": m,
		"verify": vr, "pay": pr,
		"message": fmt.Sprintf("核销成功：%s %s，餐费 %.2f，补贴 %.2f，自付 %.2f",
			in.MealDate, mealCN(in.MealType), in.Price, subsidy, selfPay),
	})
}

func mealCN(t string) string {
	if t == MealDinner {
		return "晚餐"
	}
	return "午餐"
}

// ---- 老人就餐流水 ----

func (h *Handler) listMeals(c *gin.Context) {
	e, ok := h.getElder(c)
	if !ok {
		return
	}
	q := h.db.Model(&MealRecord{}).Where("elderly_id = ?", e.ID)
	if from := c.Query("from"); from != "" {
		q = q.Where("meal_date >= ?", from)
	}
	if to := c.Query("to"); to != "" {
		q = q.Where("meal_date <= ?", to)
	}
	var meals []MealRecord
	q.Order("meal_date DESC, meal_type DESC, id DESC").Find(&meals)
	c.JSON(200, meals)
}

// ---- 月结前重算（仅未结算餐次）----

func (h *Handler) recalc(c *gin.Context) {
	var in struct {
		ElderlyID uint `json:"elderly_id"`
	}
	_ = c.ShouldBindJSON(&in)
	n, err := recalcUnsettled(h.db, in.ElderlyID)
	if err != nil {
		writeErr(c, 400, "重算失败: %v", err)
		return
	}
	c.JSON(200, gin.H{"recalculated": n,
		"message": fmt.Sprintf("已按当前资格记录重新计算 %d 笔尚未结算的餐次（已封账餐次不受影响）", n)})
}

// ---- 结算单 ----

func (h *Handler) listSettlements(c *gin.Context) {
	var ss []Settlement
	h.db.Order("period DESC").Find(&ss)
	c.JSON(200, ss)
}

func (h *Handler) loadSettlement(c *gin.Context) (*Settlement, bool) {
	period := c.Param("period")
	var s Settlement
	if err := h.db.Where("period = ?", period).First(&s).Error; err != nil {
		writeErr(c, 404, "%s 尚无结算单", period)
		return nil, false
	}
	return &s, true
}

func (h *Handler) createSettlement(c *gin.Context) {
	period := c.Param("period")
	s, err := createOrRefreshOpenSettlement(h.db, period)
	if err != nil {
		writeErr(c, 400, "%v", err)
		return
	}
	var dates []string
	h.db.Model(&MealRecord{}).
		Where("settlement_id IS NULL AND substr(meal_date,1,7) = ?", period).
		Distinct().Pluck("meal_date", &dates)
	c.JSON(200, gin.H{"settlement": s, "unsettled_dates": dates})
}

func (h *Handler) closeSettlementHandler(c *gin.Context) {
	period := c.Param("period")
	s, meals, err := closeSettlement(h.db, period)
	if err != nil {
		writeErr(c, 400, "%v", err)
		return
	}
	c.JSON(200, gin.H{
		"settlement": s, "frozen_meals": len(meals),
		"message": fmt.Sprintf("%s 已封账：%d 笔餐次的补贴快照已冻结，之后只能出具补差单", period, len(meals)),
	})
}

func (h *Handler) settlementMeals(c *gin.Context) {
	s, ok := h.loadSettlement(c)
	if !ok {
		return
	}
	var meals []MealRecord
	h.db.Where("settlement_id = ?", s.ID).Order("meal_date, meal_type, elderly_id").Find(&meals)
	c.JSON(200, meals)
}

func (h *Handler) previewAdjustmentHandler(c *gin.Context) {
	period := c.Param("period")
	var in struct {
		ElderlyID uint `json:"elderly_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		writeErr(c, 400, "参数错误: %v", err)
		return
	}
	var s Settlement
	if err := h.db.Where("period = ? AND status = ?", period, SettleClosed).First(&s).Error; err != nil {
		writeErr(c, 400, "%s 尚未封账，不能预览补差单", period)
		return
	}
	diff, err := previewAdjustment(h.db, period, in.ElderlyID)
	if err != nil {
		writeErr(c, 500, "%v", err)
		return
	}
	out := gin.H{"period": period, "elderly_id": in.ElderlyID, "affected": diff,
		"message": "封账后预览：以下餐次原快照与按当前资格重算结果存在差异（尚未生成补差单）"}
	c.JSON(200, out)
}

func (h *Handler) createAdjustmentHandler(c *gin.Context) {
	period := c.Param("period")
	var in struct {
		ElderlyID uint   `json:"elderly_id" binding:"required"`
		Reason    string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		writeErr(c, 400, "参数错误: %v", err)
		return
	}
	if in.Reason == "" {
		in.Reason = "封账后发现资格记录错误，按当前资格记录重算差额"
	}
	adj, err := createAdjustment(h.db, period, in.ElderlyID, in.Reason)
	if err != nil {
		writeErr(c, 400, "%v", err)
		return
	}
	c.JSON(201, gin.H{"adjustment": adj,
		"message": "补差单已生成（只追加调整，原结算结果保留不变）"})
}

func (h *Handler) listAdjustments(c *gin.Context) {
	q := h.db.Model(&Adjustment{})
	if p := c.Query("period"); p != "" {
		q = q.Where("period = ?", p)
	}
	var adjs []Adjustment
	q.Order("id DESC").Find(&adjs)
	c.JSON(200, adjs)
}

func (h *Handler) reconcileHandler(c *gin.Context) {
	period := c.Param("period")
	r, err := reconcile(h.db, period)
	if err != nil {
		writeErr(c, 404, "%v", err)
		return
	}
	c.JSON(200, r)
}

func (h *Handler) allMeals(c *gin.Context) {
	// 结算页/管理查看全量餐次（含快照）
	var meals []MealRecord
	tx := h.db.Model(&MealRecord{}).Order("meal_date DESC, meal_type DESC, id DESC")
	if p := c.Query("period"); p != "" {
		tx = tx.Where("substr(meal_date,1,7) = ?", p)
	}
	if eid := c.Query("elderly_id"); eid != "" {
		tx = tx.Where("elderly_id = ?", eid)
	}
	if c.Query("unsettled") == "1" {
		tx = tx.Where("settlement_id IS NULL")
	}
	tx.Limit(500).Find(&meals)
	c.JSON(200, meals)
}

func (h *Handler) reset(c *gin.Context) {
	// 演示用：清空全部业务数据
	for _, m := range []any{&Adjustment{}, &MealRecord{}, &Settlement{}, &QualEvent{}, &Elderly{}} {
		if err := h.db.Where("1=1").Delete(m).Error; err != nil {
			writeErr(c, 500, "重置失败: %v", err)
			return
		}
	}
	c.JSON(200, gin.H{"message": "演示数据已清空"})
}

var _ = http.StatusOK
