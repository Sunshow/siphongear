package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/sunshow/siphongear/internal/config"
	"github.com/sunshow/siphongear/internal/store/models"
)

func Open(cfg config.DatabaseConfig) (*gorm.DB, error) {
	gconf := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Warn)}

	switch cfg.Driver {
	case "sqlite", "":
		if dir := filepath.Dir(cfg.DSN); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create sqlite dir: %w", err)
			}
		}
		dsn := cfg.DSN + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
		return gorm.Open(sqlite.Open(dsn), gconf)
	case "mysql":
		return gorm.Open(mysql.Open(cfg.DSN), gconf)
	case "postgres":
		return gorm.Open(postgres.Open(cfg.DSN), gconf)
	default:
		return nil, fmt.Errorf("unsupported driver: %s", cfg.Driver)
	}
}

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.User{},
		&models.Site{},
		&models.Credential{},
		&models.Collector{},
		&models.Indicator{},
		&models.Run{},
		&models.StepLog{},
		&models.DataPoint{},
		&models.CollectorTemplate{},
	)
}
