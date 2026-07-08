# Load environment variables from .env if it exists
ifneq (,$(wildcard .env))
    include .env
    export
endif

.PHONY: dev test migrate-up migrate-down seed seed-zips db-clean db-clean-catalog

dev:
	go run cmd/server/main.go

test:
	go test -v ./...

test-race:
	go test -race -v ./...

migrate-up:
	migrate -path migrations -database "postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable" up

migrate-down:
	migrate -path migrations -database "postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable" down -all

db-reset: migrate-down migrate-up seed

# General app seed (products, etc.) — not yet implemented.
seed:
	@echo "cmd/seed/main.go is not implemented; use make seed-zips for US ZIP reference data"

# Seed us_zip_codes from data/uszips.csv (or USZIPS_CSV env). Download CSV first; see data/README.md.
seed-zips:
	go run cmd/seed_zips/main.go

db-clean:
	go run cmd/dbclean/main.go

db-clean-catalog:
	go run cmd/dbclean/main.go -scope=products-orders

