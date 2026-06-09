# Address Validation Implementation Plan

## Goal Description
Build an address validation API endpoint to prevent Shopify checkout failures by ensuring the city, state, and ZIP codes match for US addresses. This uses a static reference table for US ZIP codes, guaranteeing 100% uptime and zero API costs.

*(Note on External APIs: Services like SmartyStreets are not 100% free. Completely free alternatives like Zippopotam.us exist, but they have no uptime guarantees. If a free API goes down, it would completely block your checkout flow. Therefore, hosting the ZIP code data locally in a database table is the safest, fastest, and 100% free approach).*

## Proposed Changes

### 1. Database Migration
A new migration will be created to add the `us_zip_codes` reference table.

#### [NEW] `migrations/019_create_us_zip_codes.up.sql`
```sql
CREATE TABLE IF NOT EXISTS us_zip_codes (
    zip_code VARCHAR(10) PRIMARY KEY,
    city VARCHAR(100) NOT NULL,
    state_abbr VARCHAR(2) NOT NULL,
    state_name VARCHAR(50) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_us_zip_codes_state_city ON us_zip_codes(state_abbr, city);
```

#### [NEW] `migrations/019_create_us_zip_codes.down.sql`
```sql
DROP TABLE IF EXISTS us_zip_codes;
```

---

### 2. Data Seeding Script
We will add a CLI script that downloads a free US ZIP code database (e.g., from an open-source GitHub repository or Simplemaps free tier) and seeds the `us_zip_codes` table.

#### [NEW] `cmd/seed_zips/main.go`
- A script that connects to Postgres using GORM.
- Reads a provided `uszips.csv` file.
- Uses `gorm.Clauses(clause.OnConflict{DoNothing: true})` to bulk insert the ZIP codes into the DB.

---

### 3. Data Transfer Objects (DTO)

#### [MODIFY] `internal/checkout/dto.go`
Add the request payload struct for address validation.

```go
type ValidateAddressRequest struct {
    Country string `json:"country" validate:"required"`
    State   string `json:"state" validate:"required"`
    City    string `json:"city" validate:"required"`
    Zip     string `json:"zip" validate:"required"`
}
```

---

### 4. Store Layer

#### [MODIFY] `internal/checkout/store.go`
Add the model and repository method to lookup ZIP codes.

```go
type UsZipCode struct {
    ZipCode   string `gorm:"primaryKey"`
    City      string
    StateAbbr string
    StateName string
}

func (UsZipCode) TableName() string {
    return "us_zip_codes"
}

// In the store interface:
GetUSZipCodeDetails(ctx context.Context, zip string) (*UsZipCode, error)
```

#### [MODIFY] `internal/checkout/postgres.go`
Implement the lookup query to fetch the ZIP code row from the DB.

---

### 5. Service Layer

#### [MODIFY] `internal/checkout/service.go`
Add the validation business logic.

```go
// Interface
ValidateAddress(ctx context.Context, req ValidateAddressRequest) map[string]string

// Implementation Details:
// 1. If country != "US", return nil (valid, skip check).
// 2. Fetch GetUSZipCodeDetails(req.Zip).
// 3. If Zip not found -> return error map `{"zip": "Invalid US ZIP code"}`
// 4. If strings.EqualFold(req.City, db.City) is false -> return error map `{"city": "City does not match ZIP"}`
// 5. If req.State matches neither db.StateAbbr nor db.StateName (case insensitive) -> return error map `{"state": "..."}`
// 6. Return the map of errors if any, otherwise return nil.
```

---

### 6. API Handler

#### [MODIFY] `internal/checkout/handler.go`
Expose the endpoint `POST /api/v1/shipping/validate-address`.

```go
// In SetupRoutes:
shipping.Post("/validate-address", h.ValidateAddress)

// Handler Logic:
// 1. Parse and validate JSON body.
// 2. Call h.checkoutService.ValidateAddress
// 3. If errors map is not empty, return HTTP 422 Unprocessable Entity with `errors` object.
// 4. Otherwise, return HTTP 200 OK.
```
