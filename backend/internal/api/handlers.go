package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"mealsubsidy/internal/service"
)

type Handler struct{ Svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{Svc: svc} }

func (h *Handler) Register(r *gin.Engine) {
	api := r.Group("/api")
	{
		api.GET("/seniors", h.listSeniors)
		api.POST("/seniors", h.createSenior)
		api.GET("/seniors/:id/status", h.currentStatus)

		api.GET("/seniors/:id/eligibility", h.listEligibility)
		api.POST("/seniors/:id/eligibility", h.addEligibility)
		api.PUT("/eligibility/:rid", h.updateEligibility)
		api.DELETE("/eligibility/:rid", h.deleteEligibility)

		api.POST("/canteen/verify", h.verifyMeal)
		api.GET("/seniors/:id/meals", h.listMeals)

		api.POST("/seniors/:id/recalc", h.recalc)
		api.POST("/seniors/:id/settlements", h.closeMonth)
		api.GET("/settlements", h.listSettlements)
		api.GET("/settlements/:id", h.getSettlement)
		api.POST("/seniors/:id/adjustments", h.createAdjustment)
		api.GET("/adjustments", h.listAdjustments)
		api.GET("/seniors/:id/reconcile", h.reconcile)
	}
}

func fail(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"error": msg})
}

func idParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "id 必须是数字")
		return 0, false
	}
	return uint(id), true
}

func errStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func (h *Handler) listSeniors(c *gin.Context) {
	out, err := h.Svc.ListSeniors()
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	c.JSON(200, out)
}

type seniorReq struct {
	Name      string `json:"name"`
	CardNo    string `json:"card_no"`
	Community string `json:"community"`
}

func (h *Handler) createSenior(c *gin.Context) {
	var req seniorReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, err.Error())
		return
	}
	sr, err := h.Svc.CreateSenior(req.Name, req.CardNo, req.Community)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(201, sr)
}

func (h *Handler) currentStatus(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	st, err := h.Svc.CurrentStatus(id)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(200, gin.H{"senior_id": id, "current_status": st})
}

func (h *Handler) listEligibility(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	out, err := h.Svc.ListEligibility(id)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(200, out)
}

type eligReq struct {
	EffectiveDate string `json:"effective_date"`
	Status        string `json:"status"`
	LevelName     string `json:"level_name"`
	SubsidyFen    int64  `json:"subsidy_fen"`
	Reason        string `json:"reason"`
}

func (h *Handler) addEligibility(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req eligReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, err.Error())
		return
	}
	rec, err := h.Svc.AddEligibility(id, req.EffectiveDate, req.Status, req.LevelName, req.SubsidyFen, req.Reason)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(201, rec)
}

func (h *Handler) updateEligibility(c *gin.Context) {
	rid, err := strconv.ParseUint(c.Param("rid"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "id 必须是数字")
		return
	}
	var req eligReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, err.Error())
		return
	}
	rec, err := h.Svc.UpdateEligibility(uint(rid), req.EffectiveDate, req.Status, req.LevelName, req.SubsidyFen, req.Reason)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(200, rec)
}

func (h *Handler) deleteEligibility(c *gin.Context) {
	rid, err := strconv.ParseUint(c.Param("rid"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "id 必须是数字")
		return
	}
	if err := h.Svc.DeleteEligibility(uint(rid)); err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) verifyMeal(c *gin.Context) {
	var req service.VerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, err.Error())
		return
	}
	res, err := h.Svc.VerifyMeal(req)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(200, res)
}

func (h *Handler) listMeals(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	out, err := h.Svc.ListMeals(id, c.Query("period"))
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(200, out)
}

type recalcReq struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func (h *Handler) recalc(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req recalcReq
	_ = c.ShouldBindJSON(&req)
	rep, err := h.Svc.RecalcUnsettled(id, req.StartDate, req.EndDate)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(200, rep)
}

type closeReq struct {
	Period string `json:"period"`
}

func (h *Handler) closeMonth(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req closeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, err.Error())
		return
	}
	st, err := h.Svc.CloseMonth(id, req.Period)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(201, st)
}

func (h *Handler) listSettlements(c *gin.Context) {
	var seniorID uint
	if v := c.Query("senior_id"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			fail(c, 400, "senior_id 必须是数字")
			return
		}
		seniorID = uint(n)
	}
	out, err := h.Svc.ListSettlements(seniorID, c.Query("period"))
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	c.JSON(200, out)
}

func (h *Handler) getSettlement(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	st, meals, vouchers, err := h.Svc.GetSettlement(id)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(200, gin.H{"settlement": st, "meals": meals, "adjustments": vouchers})
}

type adjReq struct {
	Period string `json:"period"`
	Reason string `json:"reason"`
}

func (h *Handler) createAdjustment(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req adjReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, err.Error())
		return
	}
	v, err := h.Svc.CreateAdjustment(id, req.Period, req.Reason)
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(201, v)
}

func (h *Handler) listAdjustments(c *gin.Context) {
	var seniorID uint
	if v := c.Query("senior_id"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			fail(c, 400, "senior_id 必须是数字")
			return
		}
		seniorID = uint(n)
	}
	out, err := h.Svc.ListAdjustments(seniorID, c.Query("period"))
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	c.JSON(200, out)
}

func (h *Handler) reconcile(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	rep, err := h.Svc.Reconcile(id, c.Query("period"))
	if err != nil {
		fail(c, errStatus(err), err.Error())
		return
	}
	c.JSON(200, rep)
}
