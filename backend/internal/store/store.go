package store

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"mealsubsidy/internal/model"
)

// Open 打开/创建 SQLite 数据库并执行结构迁移。
func Open(dsn string) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Senior{},
		&model.EligRecord{},
		&model.Meal{},
		&model.Settlement{},
		&model.AdjustmentVoucher{},
		&model.VoucherItem{},
		&model.Ledger{},
	); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	return db
}
