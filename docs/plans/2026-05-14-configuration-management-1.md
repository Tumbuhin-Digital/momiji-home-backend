# 2026-05-14 — Configuration Management (Phase 1)

> **Skill:** `configuration-management` (technical-constitution §Configuration Management Principles)  
> **Phases covered:** Phase 1 (Foundation), Phase 4 (Shopify config addition)  
> **Status:** ✅ Complete

---

## Hybrid Config Approach (YAML structure + .env secrets)

Per constitution: *"YAML for structure, .env for secrets. Fail fast on missing config."*

```
config/
├── auth.yaml        # JWT TTLs, bcrypt cost (non-secret structure)
└── database.yaml    # Pool size, timeouts (non-secret structure)

.env                 # ALL secrets — never committed
.env.template        # Empty keys template — committed as contract
```

### `auth.yaml`
```yaml
jwt:
  access_token_ttl: 15m
  refresh_token_ttl: 168h   # 7 days
  bcrypt_cost: 12
rate_limit:
  max_attempts: 5
  window: 15m
```

### `database.yaml`
```yaml
default: &default
  adapter: postgresql
  pool: 10
  idle_timeout: 5m
  connection_timeout: 10s

development:
  <<: *default

production:
  <<: *default
```

### `.env.template` (source of truth for required secrets)
```env
# Database
DB_HOST=
DB_USERNAME=
DB_PASSWORD=
DB_NAME=
DB_PORT=5432

# JWT
JWT_SECRET=
JWT_REFRESH_SECRET=

# Shopify
SHOPIFY_STORE_DOMAIN=
SHOPIFY_ADMIN_API_TOKEN=     # shpat_... — Admin GraphQL + Draft Orders
SHOPIFY_STOREFRONT_TOKEN=    # shpss... — Storefront Checkout API
SHOPIFY_WEBHOOK_SECRET=      # deferred — needed when webhooks are built

# App
APP_ENV=development
PORT=3000
```

---

## Config Loading (`internal/config/config.go`)

```go
type Config struct {
    App     AppConfig
    Database DatabaseConfig
    Auth    AuthConfig
    Shopify ShopifyConfig   // added Phase 4
}

type ShopifyConfig struct {
    StoreDomain        string
    AdminAPIToken      string
    StorefrontToken    string
    WebhookSecret      string
}
```

**Load strategy:**
1. `godotenv.Load()` loads `.env` into process environment
2. YAML files parsed for structured (non-secret) config
3. `os.Getenv()` for all secrets
4. **Fail fast:** If any required key is empty string → `log.Fatal()` at startup

---

## Makefile Targets

```makefile
dev:           go run cmd/server/main.go
test:          go test -v ./...
test-race:     go test -race -v ./...
migrate-up:    migrate -path migrations -database "..." up
migrate-down:  migrate -path migrations -database "..." down -all
seed:          go run cmd/seed/main.go
db-reset:      migrate-down + migrate-up + seed
```

**Docker note:** `cmd/seed/main.go` is gitignored (`cmd/seed/` in `.gitignore`) and NOT compiled into Docker image. Seeder is local-only.

---

## Gitignore Configuration (docs-relevant)

```gitignore
# Project Specific Documentation
Technical PRD (2).pdf
/docs/*
!/docs/swagger.json      # committed — FE/CI needs it
!/docs/swagger.yaml      # committed
!/docs/docs.go           # committed — Go import required

# Local Scripts
cmd/seed/

# Secrets
.env
```

**Note:** `docs/plans/` falls under `/docs/*` and is currently gitignored. Use `git add -f docs/plans/` to force-add when committing plans.
