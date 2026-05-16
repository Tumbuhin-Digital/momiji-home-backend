package database

import (
	"fmt"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewPostgresDB(cfg config.DatabaseConfig) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Jakarta",
		cfg.Host, cfg.Username, cfg.Password, cfg.Database, cfg.Port)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.Pool)
	sqlDB.SetMaxIdleConns(cfg.Pool / 2)

	idleTimeout, _ := time.ParseDuration(cfg.IdleTimeout)
	if idleTimeout > 0 {
		sqlDB.SetConnMaxIdleTime(idleTimeout)
	}

	return db, nil
}
