# Load environment variables from .env if it exists
ifneq (,$(wildcard .env))
    include .env
    export
endif

.PHONY: dev test migrate-up migrate-down seed

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

seed:
	go run cmd/seed/main.go

