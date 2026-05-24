package preorder

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type postgresStore struct {
	db *gorm.DB
}

// NewPostgresPreorderStore creates a new GORM-backed PreorderStore.
func NewPostgresPreorderStore(db *gorm.DB) PreorderStore {
	return &postgresStore{db: db}
}

func (s *postgresStore) CreateSettlement(ctx context.Context, settlement *Settlement) error {
	return s.db.WithContext(ctx).Create(settlement).Error
}

func (s *postgresStore) GetSettlementByID(ctx context.Context, id string) (*Settlement, error) {
	var settlement Settlement
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&settlement).Error; err != nil {
		return nil, err
	}
	return &settlement, nil
}

func (s *postgresStore) ListSettlements(ctx context.Context, filter SettlementFilter) ([]Settlement, int64, error) {
	var settlements []Settlement
	var total int64

	query := s.db.WithContext(ctx).Model(&Settlement{})

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&settlements).Error; err != nil {
		return nil, 0, err
	}

	return settlements, total, nil
}

func (s *postgresStore) UpdateSettlementStatus(ctx context.Context, id, status string, ts *time.Time) error {
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}

	switch status {
	case "invoiced":
		updates["invoiced_at"] = ts
	case "paid":
		updates["paid_at"] = ts
	}

	return s.db.WithContext(ctx).
		Model(&Settlement{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// AllSettlementsPaid returns true if every settlement for the given orderID has status = 'paid'.
func (s *postgresStore) AllSettlementsPaid(ctx context.Context, orderID string) (bool, error) {
	var unpaidCount int64
	err := s.db.WithContext(ctx).
		Model(&Settlement{}).
		Where("order_id = ? AND status != ?", orderID, "paid").
		Count(&unpaidCount).Error
	if err != nil {
		return false, err
	}
	return unpaidCount == 0, nil
}
