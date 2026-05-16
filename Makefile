.PHONY: dev test migrate-up migrate-down

dev:
	go run cmd/server/main.go

test:
	go test -v ./...

test-race:
	go test -race -v ./...

migrate-up:
	migrate -path migrations -database "postgres://postgres:postgres@localhost:5432/fiber_db?sslmode=disable" up

migrate-down:
	migrate -path migrations -database "postgres://postgres:postgres@localhost:5432/fiber_db?sslmode=disable" down
