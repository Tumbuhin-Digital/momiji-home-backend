package warehouse

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"gorm.io/gorm"
)

const defaultCacheTTL = 24 * time.Hour

// Resolver resolves warehouse origins for shipping rate calculation.
type Resolver interface {
	ResolveOrigin(segment, requested string) string
	GetOrigin(ctx context.Context, code string) (Origin, error)
}

// Service manages warehouse configuration with a cache-backed read path.
type Service interface {
	Resolver
	List(ctx context.Context) (*WarehouseListResponse, error)
	Update(ctx context.Context, code string, req UpdateWarehouseRequest) (*WarehouseDTO, error)
	Warm(ctx context.Context) error
	EnsureFromEnv(ctx context.Context, cfg config.ShipStationConfig) error
}

type service struct {
	store              Store
	envFallback        config.ShipStationConfig
	globalGroundCode   string
	mu                 sync.RWMutex
	byCode             map[string]Warehouse
	loadedAt           time.Time
	ttl                time.Duration
}

func NewService(store Store, envFallback config.ShipStationConfig) Service {
	ground := envFallback.GroundServiceCode
	if ground == "" {
		ground = "ups_ground"
	}
	return &service{
		store:            store,
		envFallback:      envFallback,
		globalGroundCode: ground,
		byCode:           make(map[string]Warehouse),
		ttl:              defaultCacheTTL,
	}
}

func NormalizeCode(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case CodeWest:
		return CodeWest
	default:
		return CodeEast
	}
}

func (s *service) ResolveOrigin(segment, requested string) string {
	if segment == "ship_ready" {
		return CodeEast
	}
	return NormalizeCode(requested)
}

func (s *service) GetOrigin(ctx context.Context, code string) (Origin, error) {
	wh, err := s.getCached(ctx, NormalizeCode(code))
	if err != nil {
		return Origin{}, err
	}
	return wh.ToOrigin(s.globalGroundCode), nil
}

func (s *service) List(ctx context.Context) (*WarehouseListResponse, error) {
	if err := s.refreshIfStale(ctx); err != nil {
		return nil, apierror.ErrInternal
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	dtos := make([]WarehouseDTO, 0, len(s.byCode))
	for _, code := range []string{CodeEast, CodeWest} {
		if wh, ok := s.byCode[code]; ok {
			dtos = append(dtos, toDTO(wh))
		}
	}
	return &WarehouseListResponse{Warehouses: dtos}, nil
}

func (s *service) Update(ctx context.Context, code string, req UpdateWarehouseRequest) (*WarehouseDTO, error) {
	code = NormalizeCode(code)
	if code != CodeEast && code != CodeWest {
		return nil, apierror.NewWithDetails(400, "invalid_request", "warehouse code must be east or west", nil)
	}

	if err := validateWarehouseUpdate(req); err != nil {
		return nil, err
	}

	country := strings.TrimSpace(req.Country)
	if country == "" {
		country = "US"
	}

	updates := map[string]interface{}{
		"name":      strings.TrimSpace(req.Name),
		"phone":     strings.TrimSpace(req.Phone),
		"address1":  strings.TrimSpace(req.Address1),
		"city":      strings.TrimSpace(req.City),
		"state":     strings.TrimSpace(req.State),
		"zip":       strings.TrimSpace(req.Zip),
		"country":   country,
		"shipstation_warehouse_id": trimOptionalPtr(req.ShipstationWarehouseID),
		"ground_service_code":      trimOptionalPtr(req.GroundServiceCode),
	}

	updated, err := s.store.UpdateByCode(ctx, code, updates)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, apierror.ErrNotFound
		}
		return nil, apierror.ErrInternal
	}

	if err := s.refreshFromDB(ctx); err != nil {
		return nil, apierror.ErrInternal
	}

	dto := toDTO(*updated)
	return &dto, nil
}

func (s *service) Warm(ctx context.Context) error {
	return s.refreshFromDB(ctx)
}

func (s *service) EnsureFromEnv(ctx context.Context, cfg config.ShipStationConfig) error {
	east, err := s.store.GetByCode(ctx, CodeEast)
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
	} else if needsEnvBackfill(*east, cfg) {
		updates := envBackfillUpdates(*east, cfg)
		if len(updates) > 0 {
			slog.WarnContext(ctx, "backfilling east warehouse from legacy env vars; edit in Settings to override")
			if _, err := s.store.UpdateByCode(ctx, CodeEast, updates); err != nil {
				return err
			}
		}
	}
	return s.Warm(ctx)
}

func (s *service) getCached(ctx context.Context, code string) (Warehouse, error) {
	if err := s.refreshIfStale(ctx); err != nil {
		return Warehouse{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	wh, ok := s.byCode[code]
	if !ok {
		return s.fallbackWarehouse(code), nil
	}
	if isWarehouseEmpty(wh) {
		return s.fallbackWarehouse(code), nil
	}
	return wh, nil
}

func (s *service) refreshIfStale(ctx context.Context) error {
	s.mu.RLock()
	stale := len(s.byCode) == 0 || time.Since(s.loadedAt) > s.ttl
	s.mu.RUnlock()
	if !stale {
		return nil
	}
	return s.refreshFromDB(ctx)
}

func (s *service) refreshFromDB(ctx context.Context) error {
	rows, err := s.store.List(ctx)
	if err != nil {
		return err
	}

	byCode := make(map[string]Warehouse, len(rows))
	for _, row := range rows {
		byCode[row.Code] = row
	}

	s.mu.Lock()
	s.byCode = byCode
	s.loadedAt = time.Now().UTC()
	s.mu.Unlock()
	return nil
}

func (s *service) fallbackWarehouse(code string) Warehouse {
	cfg := s.envFallback
	if code == CodeWest {
		return Warehouse{
			Code:    CodeWest,
			Name:    "West Coast 3PL",
			Country: "US",
		}
	}
	country := cfg.WarehouseCountry
	if country == "" {
		country = "US"
	}
	return Warehouse{
		Code:      CodeEast,
		Name:      cfg.WarehouseName,
		Phone:     cfg.WarehousePhone,
		Address1:  cfg.WarehouseAddress1,
		City:      cfg.WarehouseCity,
		State:     cfg.WarehouseState,
		Zip:       cfg.WarehouseZip,
		Country:   country,
		IsDefault: true,
	}
}

func needsEnvBackfill(east Warehouse, cfg config.ShipStationConfig) bool {
	if strings.TrimSpace(east.Address1) != "" && strings.TrimSpace(east.Zip) != "" {
		return false
	}
	return strings.TrimSpace(cfg.WarehouseAddress1) != "" || strings.TrimSpace(cfg.WarehouseZip) != ""
}

func envBackfillUpdates(east Warehouse, cfg config.ShipStationConfig) map[string]interface{} {
	updates := map[string]interface{}{}
	if strings.TrimSpace(east.Name) == "" && cfg.WarehouseName != "" {
		updates["name"] = cfg.WarehouseName
	}
	if strings.TrimSpace(east.Phone) == "" && cfg.WarehousePhone != "" {
		updates["phone"] = cfg.WarehousePhone
	}
	if strings.TrimSpace(east.Address1) == "" && cfg.WarehouseAddress1 != "" {
		updates["address1"] = cfg.WarehouseAddress1
	}
	if strings.TrimSpace(east.City) == "" && cfg.WarehouseCity != "" {
		updates["city"] = cfg.WarehouseCity
	}
	if strings.TrimSpace(east.State) == "" && cfg.WarehouseState != "" {
		updates["state"] = cfg.WarehouseState
	}
	if strings.TrimSpace(east.Zip) == "" && cfg.WarehouseZip != "" {
		updates["zip"] = cfg.WarehouseZip
	}
	if strings.TrimSpace(east.Country) == "" && cfg.WarehouseCountry != "" {
		updates["country"] = cfg.WarehouseCountry
	}
	return updates
}

func isWarehouseEmpty(wh Warehouse) bool {
	return strings.TrimSpace(wh.Zip) == "" && strings.TrimSpace(wh.Address1) == ""
}

func validateWarehouseUpdate(req UpdateWarehouseRequest) error {
	details := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		details["name"] = "Name is required"
	}
	if strings.TrimSpace(req.Address1) == "" {
		details["address1"] = "Street address is required"
	}
	if strings.TrimSpace(req.City) == "" {
		details["city"] = "City is required"
	}
	if strings.TrimSpace(req.State) == "" {
		details["state"] = "State is required"
	}
	if strings.TrimSpace(req.Zip) == "" {
		details["zip"] = "ZIP code is required"
	}
	if len(details) > 0 {
		return apierror.NewWithDetails(400, "validation_error", "Warehouse address validation failed", details)
	}
	return nil
}

func trimOptionalPtr(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
