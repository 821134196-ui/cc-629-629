package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"mealsubsidy/internal/model"
	"mealsubsidy/internal/service"
)

func setupRouter(t *testing.T) (*gin.Engine, *service.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&model.Senior{}, &model.EligRecord{}, &model.Meal{},
		&model.Settlement{}, &model.AdjustmentVoucher{}, &model.VoucherItem{}, &model.Ledger{},
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	svc := service.New(db)
	r := gin.New()
	New(svc).Register(r)
	return r, svc
}

func doJSON(t *testing.T, r http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestHTTPEligibilityUpdateAndVerifyFlow(t *testing.T) {
	r, _ := setupRouter(t)

	code, body := doJSON(t, r, "POST", "/api/seniors", map[string]string{"name": "王秀兰", "card_no": "C1", "community": "阳光"})
	if code != 201 {
		t.Fatalf("create senior: %d %v", code, body)
	}
	sid := int(body["id"].(float64))

	// 初始资格 8 元
	code, body = doJSON(t, r, "POST", fmt.Sprintf("/api/seniors/%d/eligibility", sid),
		map[string]any{"effective_date": "2026-10-01", "status": "active", "level_name": "普通", "subsidy_fen": 800})
	if code != 201 {
		t.Fatalf("add elig: %d %v", code, body)
	}
	rid := int(body["id"].(float64))

	// 核销 10-15 → 800
	code, body = doJSON(t, r, "POST", "/api/canteen/verify",
		map[string]any{"senior_id": sid, "meal_date": "2026-10-15", "meal_type": "午餐", "price_fen": 1500})
	if code != 200 || int(body["meal"].(map[string]any)["subsidy_fen"].(float64)) != 800 {
		t.Fatalf("verify: %d %v", code, body)
	}

	// 更正资格为 10 元
	code, body = doJSON(t, r, "PUT", fmt.Sprintf("/api/eligibility/%d", rid),
		map[string]any{"effective_date": "2026-10-01", "status": "active", "level_name": "高龄", "subsidy_fen": 1000})
	if code != 200 {
		t.Fatalf("update elig: %d %v (PUT 参数路由必须可用)", code, body)
	}

	// 删除接口返回 200
	code, _ = doJSON(t, r, "DELETE", fmt.Sprintf("/api/eligibility/%d", rid), nil)
	if code != 200 {
		t.Fatalf("delete elig 应为 200, 实际 %d", code)
	}
	// 再删一次 → 404
	code, _ = doJSON(t, r, "DELETE", fmt.Sprintf("/api/eligibility/%d", rid), nil)
	if code != 404 {
		t.Fatalf("重复删除应为 404, 实际 %d", code)
	}
}

func TestHTTPVerifyIdempotent(t *testing.T) {
	r, _ := setupRouter(t)
	_, body := doJSON(t, r, "POST", "/api/seniors", map[string]string{"name": "李", "card_no": "C2", "community": "和平"})
	sid := int(body["id"].(float64))
	doJSON(t, r, "POST", fmt.Sprintf("/api/seniors/%d/eligibility", sid),
		map[string]any{"effective_date": "2026-10-01", "status": "active", "level_name": "普通", "subsidy_fen": 800})

	payload := map[string]any{"card_no": "C2", "meal_date": "2026-10-15", "meal_type": "午餐", "price_fen": 1500}
	c1, b1 := doJSON(t, r, "POST", "/api/canteen/verify", payload)
	c2, b2 := doJSON(t, r, "POST", "/api/canteen/verify", payload)
	if c1 != 200 || c2 != 200 {
		t.Fatalf("核销请求失败 %d %d", c1, c2)
	}
	if b1["duplicate"].(bool) {
		t.Fatal("首次不应判重")
	}
	if !b2["duplicate"].(bool) || b1["meal"].(map[string]any)["id"] != b2["meal"].(map[string]any)["id"] {
		t.Fatalf("重复扫码必须幂等返回同一餐次")
	}
}
