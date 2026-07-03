package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/database"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/seedzips"
	"gorm.io/gorm/clause"
)

const defaultCSVPath = "data/uszips.csv"

func main() {
	csvPath := flag.String("csv", envOrDefault("USZIPS_CSV", defaultCSVPath), "path to US ZIP codes CSV")
	batchSize := flag.Int("batch", 1000, "rows per insert batch")
	flag.Parse()

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

	rows, err := seedzips.LoadZipRowsFromCSV(*csvPath)
	if err != nil {
		log.Fatalf("load csv: %v", err)
	}
	if len(rows) == 0 {
		log.Fatal("no ZIP rows parsed from CSV")
	}

	fmt.Printf("Seeding %d unique ZIP codes from %s...\n", len(rows), *csvPath)

	for i := 0; i < len(rows); i += *batchSize {
		end := i + *batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&batch).Error; err != nil {
			log.Fatalf("insert batch starting at %d: %v", i, err)
		}
		fmt.Printf("  inserted batch %d-%d\n", i+1, end)
	}

	fmt.Println("ZIP seed complete.")
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
