package main

import (
	"fmt"
	"log"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/database"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("get sql db: %v", err)
	}
	defer sqlDB.Close()

	var tables []string
	err = db.Raw(`
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = 'public'
		  AND tablename NOT IN ('users', 'schema_migrations')
		ORDER BY tablename
	`).Scan(&tables).Error
	if err != nil {
		log.Fatalf("list tables: %v", err)
	}

	if len(tables) == 0 {
		fmt.Println("No tables to truncate.")
		return
	}

	fmt.Printf("Truncating %d tables (keeping users + schema_migrations):\n", len(tables))
	for _, t := range tables {
		fmt.Printf("  - %s\n", t)
	}

	query := "TRUNCATE TABLE " + joinQuoted(tables) + " RESTART IDENTITY CASCADE"
	if err := db.Exec(query).Error; err != nil {
		log.Fatalf("truncate tables: %v", err)
	}

	var userCount int64
	if err := db.Raw("SELECT COUNT(*) FROM users").Scan(&userCount).Error; err != nil {
		log.Fatalf("count users: %v", err)
	}

	fmt.Printf("Database cleaned successfully. %d user(s) preserved.\n", userCount)
}

func joinQuoted(tables []string) string {
	out := make([]byte, 0, len(tables)*16)
	for i, t := range tables {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '"')
		out = append(out, t...)
		out = append(out, '"')
	}
	return string(out)
}
