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
	if err := s.db.WithContext(ctx).
		Select("preorder_settlements.*, order_line_items.order_id").
		Joins("LEFT JOIN order_line_items ON order_line_items.id = preorder_settlements.order_line_item_id").
		Where("preorder_settlements.id = ?", id).
		First(&settlement).Error; err != nil {
		return nil, err
	}
	return &settlement, nil
}

func (s *postgresStore) ListSettlements(ctx context.Context, filter SettlementFilter) ([]PreorderRow, int64, error) {
	var rows []PreorderRow
	var total int64

	query := s.db.WithContext(ctx).Table("preorder_settlements").
		Select(`
			preorder_settlements.id,
			orders.id as order_id,
			orders.order_number,
			users.email as customer_email,
			order_line_items.id as order_line_item_id,
			order_line_items.title,
			order_line_items.quantity,
			preorder_settlements.balance_amount,
			product_variants.preorder_batch_label as batch_label,
			preorder_settlements.status as settlement_status,
			preorder_settlements.due_date
		`).
		Joins("JOIN order_line_items ON order_line_items.id = preorder_settlements.order_line_item_id").
		Joins("JOIN orders ON orders.id = order_line_items.order_id").
		Joins("JOIN users ON users.id = orders.customer_id").
		Joins("LEFT JOIN product_variants ON product_variants.shopify_variant_id = order_line_items.shopify_variant_id")

	if filter.Status != "" {
		query = query.Where("preorder_settlements.status = ?", filter.Status)
	}
	if filter.BatchLabel != "" {
		query = query.Where("product_variants.preorder_batch_label = ?", filter.BatchLabel)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 { page = 1 }
	limit := filter.Limit
	if limit < 1 { limit = 20 }
	offset := (page - 1) * limit

	if err := query.Order("preorder_settlements.created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	return rows, total, nil
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
		Table("preorder_settlements").
		Joins("JOIN order_line_items ON order_line_items.id = preorder_settlements.order_line_item_id").
		Where("order_line_items.order_id = ? AND preorder_settlements.status != ?", orderID, "paid").
		Count(&unpaidCount).Error
	if err != nil {
		return false, err
	}
	return unpaidCount == 0, nil
}
