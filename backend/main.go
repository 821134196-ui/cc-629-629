package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	dsn := envOr("DB_PATH", "data/meals.db")
	_ = os.MkdirAll(filepath.Dir(dsn), 0o755)

	db, err := gorm.Open(sqlite.Open(dsn+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	if err := db.AutoMigrate(
		&Elderly{}, &QualEvent{}, &MealRecord{}, &Settlement{}, &Adjustment{},
	); err != nil {
		log.Fatalf("迁移失败: %v", err)
	}

	if len(os.Args) > 1 && os.Args[1] == "seed" {
		seedDemo(db)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "verify" {
		if err := runScenarioVerify(ensureVerifyDB()); err != nil {
			log.Fatalf("验证失败: %v", err)
		}
		return
	}

	h := &Handler{db: db}
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := newRouter(h)

	addr := envOr("ADDR", ":8080")
	log.Printf("社区老人助餐结算系统已启动，API 前缀 /api，监听 %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// seedDemo 播种演示数据（跨月资格变更），仅在库为空时执行
func seedDemo(db *gorm.DB) {
	var n int64
	db.Model(&Elderly{}).Count(&n)
	if n > 0 {
		log.Printf("数据库已有 %d 位老人，跳过播种", n)
		return
	}
	now := time.Now()
	prevMonth := now.AddDate(0, -1, 0).Format(monthLay)
	thisMonth := now.Format(monthLay)

	elders := []Elderly{
		{Name: "张桂兰", CardNo: "CARD-0001", Phone: "13800000001"},
		{Name: "李建国", CardNo: "CARD-0002", Phone: "13800000002"},
		{Name: "王秀珍", CardNo: "CARD-0003", Phone: "13800000003"},
	}
	for i := range elders {
		db.Create(&elders[i])
	}

	type qe struct {
		elderIdx int
		date     string
		action   string
		rate     float64
		note     string
	}
	events := []qe{
		// 张桂兰：上月初生效 10 元/餐，上月中暂停，本月初恢复
		{0, prevMonth + "-01", ActionActivate, 10, "社区登记资格生效"},
		{0, prevMonth + "-18", ActionSuspend, 0, "资料复核暂停"},
		{0, thisMonth + "-02", ActionResume, 0, "复核通过恢复"},
		// 李建国：上月初生效 8 元/餐，本月中暂停
		{1, prevMonth + "-01", ActionActivate, 8, "社区登记资格生效"},
		{1, thisMonth + "-15", ActionSuspend, 0, "异地暂住暂停"},
		// 王秀珍：本月初才生效 6 元/餐
		{2, thisMonth + "-05", ActionActivate, 6, "新申请通过"},
	}
	for _, e := range events {
		db.Create(&QualEvent{
			ElderlyID: elders[e.elderIdx].ID, Date: e.date,
			Action: e.action, SubsidyRate: e.rate, Note: e.note,
		})
	}

	// 上月若干餐次（跨越暂停日）
	prevMeals := []struct {
		idx      int
		day      int
		typ      string
		price    float64
	}{
		{0, 10, MealLunch, 15}, {0, 17, MealLunch, 15},
		{0, 19, MealLunch, 15}, // 暂停后：不补贴
		{1, 12, MealDinner, 12}, {1, 20, MealLunch, 12},
	}
	for _, m := range prevMeals {
		d := prevMonth + "-" + pad2(m.day)
		simulateCheckin(db, elders[m.idx], d, m.typ, m.price)
	}
	log.Printf("演示数据已播种：%d 位老人；上月 %s 餐次可直接月结/封账验证", len(elders), prevMonth)
	log.Printf("本月 %s 可在食堂端继续扫码核销", thisMonth)
}

func pad2(d int) string {
	if d < 10 {
		return "0" + itoa(d)
	}
	return itoa(d)
}

func itoa(d int) string {
	if d == 0 {
		return "0"
	}
	b := []byte{}
	for d > 0 {
		b = append([]byte{byte('0' + d%10)}, b...)
		d /= 10
	}
	return string(b)
}

func simulateCheckin(db *gorm.DB, e Elderly, date, typ string, price float64) MealRecord {
	el, err := eligibilityOnDate(db, e.ID, date)
	if err != nil {
		el = Eligibility{Status: StatusSuspended,
			BasisDetail: date + " 无生效资格记录，全额自付"}
	}
	sub, self := computeSubsidy(price, el)
	m := MealRecord{
		ElderlyID: e.ID, MealDate: date, MealType: typ,
		IdempotencyKey: e.CardNo + "|" + date + "|" + typ,
		Price: price, Status: el.Status, SubsidyRate: el.SubsidyRate,
		SubsidyAmount: sub, SelfPay: self,
		BasisEventID: el.BasisEventID, BasisDetail: el.BasisDetail,
		VerifyTxnID: mockVerifyTxn(), PayTxnID: mockPayTxn("PAY", "", 0),
	}
	db.Create(&m)
	return m
}
