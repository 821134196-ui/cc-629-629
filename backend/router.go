package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func newRouter(h *Handler) *gin.Engine {
	r := gin.Default()
	api := r.Group("/api")
	{
		api.POST("/reset", h.reset)

		api.GET("/elders", h.listElders)
		api.POST("/elders", h.createElder)
		api.GET("/elders/:id", func(c *gin.Context) {
			if e, ok := h.getElder(c); ok {
				c.JSON(200, e)
			}
		})
		api.GET("/elders/:id/qualifications", h.listQualifications)
		api.POST("/elders/:id/qualifications", h.addQualification)
		api.DELETE("/elders/:id/qualifications/:qid", h.deleteQualification)
		api.GET("/elders/:id/eligibility", h.eligibilityPreview)
		api.GET("/elders/:id/meals", h.listMeals)

		api.POST("/checkin", h.checkin)
		api.GET("/meals", h.allMeals)

		api.POST("/recalc", h.recalc)

		api.GET("/settlements", h.listSettlements)
		api.POST("/settlements/:period", h.createSettlement)
		api.POST("/settlements/:period/close", h.closeSettlementHandler)
		api.GET("/settlements/:period/meals", h.settlementMeals)
		api.POST("/settlements/:period/adjustments/preview", h.previewAdjustmentHandler)
		api.POST("/settlements/:period/adjustments", h.createAdjustmentHandler)
		api.GET("/adjustments", h.listAdjustments)
		api.GET("/settlements/:period/reconcile", h.reconcileHandler)
	}

	// 托管前端构建产物（不存在时仅提供 API）
	dist := "web/dist"
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err == nil {
		r.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			p := filepath.Join(dist, filepath.Clean("/"+c.Request.URL.Path))
			if _, err := os.Stat(p); err == nil {
				c.File(p)
				return
			}
			c.File(filepath.Join(dist, "index.html")) // SPA 回退
		})
	}
	return r
}
