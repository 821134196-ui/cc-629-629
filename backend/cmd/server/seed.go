package main

import (
	"log"

	"gorm.io/gorm"

	"mealsubsidy/internal/model"
	"mealsubsidy/internal/service"
)

// seedIfEmpty 在空库时写入一套跨月演示数据，便于一条命令启动后直接体验。
func seedIfEmpty(db *gorm.DB) {
	var n int64
	db.Model(&model.Senior{}).Count(&n)
	if n > 0 {
		return
	}
	log.Println("空数据库，写入演示数据……")
	svc := service.New(db)

	wang, err := svc.CreateSenior("王秀兰", "C001", "阳光社区")
	mustSeed(err)
	li, err := svc.CreateSenior("李建国", "C002", "和平里社区")
	mustSeed(err)

	// 王秀兰：9 月生效 8 元 → 10-10 暂停 → 10-20 恢复为高龄 10 元
	mustSeed(svc.DB.Create(&model.EligRecord{SeniorID: wang.ID, EffectiveDate: "2026-09-01",
		Status: "active", LevelName: "普通补贴对象", SubsidyFen: 800, Reason: "初始登记生效"}).Error)
	mustSeed(svc.DB.Create(&model.EligRecord{SeniorID: wang.ID, EffectiveDate: "2026-10-10",
		Status: "suspended", Reason: "本人申请暂停"}).Error)
	mustSeed(svc.DB.Create(&model.EligRecord{SeniorID: wang.ID, EffectiveDate: "2026-10-20",
		Status: "active", LevelName: "高龄补贴对象", SubsidyFen: 1000, Reason: "复核通过恢复并调等高龄"}).Error)

	// 李建国：10 月生效 8 元
	mustSeed(svc.DB.Create(&model.EligRecord{SeniorID: li.ID, EffectiveDate: "2026-10-01",
		Status: "active", LevelName: "普通补贴对象", SubsidyFen: 800, Reason: "初始登记生效"}).Error)

	demoMeals := []service.VerifyRequest{
		{SeniorID: wang.ID, MealDate: "2026-09-15", MealType: "午餐", PriceFen: 1500}, // 800
		{SeniorID: wang.ID, MealDate: "2026-10-09", MealType: "午餐", PriceFen: 1500}, // 800（暂停前一天）
		{SeniorID: wang.ID, MealDate: "2026-10-15", MealType: "午餐", PriceFen: 1500}, // 0（暂停期间）
		{SeniorID: wang.ID, MealDate: "2026-10-21", MealType: "午餐", PriceFen: 1500}, // 1000（恢复后）
		{SeniorID: li.ID, MealDate: "2026-10-12", MealType: "午餐", PriceFen: 1200},   // 800
		{SeniorID: li.ID, MealDate: "2026-10-13", MealType: "晚餐", PriceFen: 1200},   // 800
	}
	for _, m := range demoMeals {
		if _, err := svc.VerifyMeal(m); err != nil {
			log.Printf("演示餐次写入失败 %+v: %v", m, err)
		}
	}
	log.Println("演示数据就绪：王秀兰 C001（跨月暂停/恢复）、李建国 C002")
}

func mustSeed(err error) {
	if err != nil {
		log.Fatalf("写入演示数据失败: %v", err)
	}
}
