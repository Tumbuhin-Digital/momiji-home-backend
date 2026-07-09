# Load environment variables from .env if it exists
ifneq (,$(wildcard .env))
    include .env
    export
endif

.PHONY: dev test migrate-up migrate-down seed seed-zips create-admin db-clean db-clean-catalog

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

# Bootstrap first admin without public register.
# Usage: make create-admin EMAIL=admin@example.com PASSWORD=secretpass
create-admin:
	@test -n "$(EMAIL)" || (echo "EMAIL is required" && exit 1)
	@test -n "$(PASSWORD)" || (echo "PASSWORD is required" && exit 1)
	go run cmd/create_admin/main.go -email="$(EMAIL)" -password="$(PASSWORD)"

db-clean:
	go run cmd/dbclean/main.go

db-clean-catalog:
	go run cmd/dbclean/main.go -scope=products-orders

