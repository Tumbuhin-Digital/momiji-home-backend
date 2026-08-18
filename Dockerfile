# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application + seed helper
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/server/main.go && \
    CGO_ENABLED=0 GOOS=linux go build -o seed_zips ./cmd/seed_zips/main.go

# Run stage
FROM alpine:latest

# Install tzdata + curl
RUN apk add --no-cache tzdata curl

# Download migrate binary
RUN curl -L https://github.com/golang-migrate/migrate/releases/download/v4.18.1/migrate.linux-amd64.tar.gz \
    | tar xvz -C /usr/local/bin

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/main .
COPY --from=builder /app/seed_zips .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/config ./config
COPY --from=builder /app/internal/platform/email/templates ./internal/platform/email/templates
COPY entrypoint.sh .
RUN chmod +x /app/entrypoint.sh

EXPOSE 3000

CMD ["./entrypoint.sh"]