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
		Select("preorder_settlements.*, order_line_items.order_id, order_line_items.title, users.email as customer_email, COALESCE(customers.first_name, 'Customer') as customer_name").
		Joins("JOIN order_line_items ON order_line_items.id = preorder_settlements.order_line_item_id").
		Joins("JOIN orders ON orders.id = order_line_items.order_id").
		Joins("JOIN users ON users.id = orders.customer_id").
		Joins("LEFT JOIN customers ON customers.id = orders.customer_id").
		Where("preorder_settlements.id = ?", id).
		First(&settlement).Error; err != nil {
		return nil, err
	}
	return &settlement, nil
}

func (s *postgresStore) ListSettlements(ctx context.Context, filter SettlementFilter) ([]PreorderRow, int64, error) {
	buildQuery := func() *gorm.DB {
		q := s.db.WithContext(ctx).Table("preorder_settlements").
			Joins("JOIN order_line_items ON order_line_items.id = preorder_settlements.order_line_item_id").
			Joins("LEFT JOIN product_variants ON product_variants.shopify_variant_id = order_line_items.shopify_variant_id")

		if filter.Status != "" {
			q = q.Where("preorder_settlements.status = ?", filter.Status)
		}
		if filter.BatchLabel != "" {
			q = q.Where("product_variants.preorder_batch_label = ?", filter.BatchLabel)
		}
		return q
	}

	var total int64
	if err := buildQuery().Select("COUNT(DISTINCT order_line_items.title)").Row().Scan(&total); err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 { page = 1 }
	limit := filter.Limit
	if limit < 1 { limit = 20 }
	offset := (page - 1) * limit

	var titles []string
	if err := buildQuery().Select("DISTINCT order_line_items.title").Order("order_line_items.title ASC").Limit(limit).Offset(offset).Pluck("order_line_items.title", &titles).Error; err != nil {
		return nil, 0, err
	}

	if len(titles) == 0 {
		return nil, total, nil
	}

	var rows []PreorderRow
	query := s.db.WithContext(ctx).Table("preorder_settlements").
		Select(`
			preorder_settlements.id,
			orders.id as order_id,
			orders.order_number,
			users.email as customer_email,
			COALESCE(customers.first_name, 'Customer') as customer_name,
			order_line_items.id as order_line_item_id,
			order_line_items.title,
			order_line_items.quantity,
			preorder_settlements.balance_amount,
			product_variants.preorder_batch_label as batch_label,
			preorder_settlements.status as settlement_status,
			preorder_settlements.due_date,
			orders.shopify_order_id,
			preorder_settlements.created_at
		`).
		Joins("JOIN order_line_items ON order_line_items.id = preorder_settlements.order_line_item_id").
		Joins("JOIN orders ON orders.id = order_line_items.order_id").
		Joins("JOIN users ON users.id = orders.customer_id").
		Joins("LEFT JOIN customers ON customers.id = orders.customer_id").
		Joins("LEFT JOIN product_variants ON product_variants.shopify_variant_id = order_line_items.shopify_variant_id").
		Where("order_line_items.title IN ?", titles)

	if filter.Status != "" {
		query = query.Where("preorder_settlements.status = ?", filter.Status)
	}
	if filter.BatchLabel != "" {
		query = query.Where("product_variants.preorder_batch_label = ?", filter.BatchLabel)
	}

	if err := query.Order("order_line_items.title ASC, preorder_settlements.created_at DESC").Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	return rows, total, nil
}

func (s *postgresStore) GetAllSettlementsForExport(ctx context.Context, filter SettlementFilter) ([]PreorderRow, error) {
	var rows []PreorderRow
	query := s.db.WithContext(ctx).Table("preorder_settlements").
		Select(`
			preorder_settlements.id,
			orders.id as order_id,
			orders.order_number,
			users.email as customer_email,
			COALESCE(customers.first_name, 'Customer') as customer_name,
			order_line_items.id as order_line_item_id,
			order_line_items.title,
			order_line_items.quantity,
			preorder_settlements.balance_amount,
			product_variants.preorder_batch_label as batch_label,
			preorder_settlements.status as settlement_status,
			preorder_settlements.due_date,
			orders.shopify_order_id,
			preorder_settlements.created_at
		`).
		Joins("JOIN order_line_items ON order_line_items.id = preorder_settlements.order_line_item_id").
		Joins("JOIN orders ON orders.id = order_line_items.order_id").
		Joins("JOIN users ON users.id = orders.customer_id").
		Joins("LEFT JOIN customers ON customers.id = orders.customer_id").
		Joins("LEFT JOIN product_variants ON product_variants.shopify_variant_id = order_line_items.shopify_variant_id")

	if filter.Status != "" {
		query = query.Where("preorder_settlements.status = ?", filter.Status)
	}
	if filter.BatchLabel != "" {
		query = query.Where("product_variants.preorder_batch_label = ?", filter.BatchLabel)
	}

	if err := query.Order("order_line_items.title ASC, preorder_settlements.created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}

	return rows, nil
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

func (s *postgresStore) GetSettlementsForReminder(ctx context.Context, daysSinceInvoiced int) ([]PreorderRow, error) {
	var rows []PreorderRow
	
	// Compute the date exactly 'daysSinceInvoiced' days ago
	// We want to match records where invoiced_at was on that exact day.
	targetDate := time.Now().AddDate(0, 0, -daysSinceInvoiced)
	startOfDay := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, targetDate.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)

	query := s.db.WithContext(ctx).Table("preorder_settlements").
		Select(`
			preorder_settlements.id,
			orders.id as order_id,
			orders.order_number,
			users.email as customer_email,
			COALESCE(customers.first_name, 'Customer') as customer_name,
			order_line_items.id as order_line_item_id,
			order_line_items.title,
			order_line_items.quantity,
			preorder_settlements.balance_amount,
			product_variants.preorder_batch_label as batch_label,
			preorder_settlements.status as settlement_status,
			preorder_settlements.due_date,
			orders.shopify_order_id,
			preorder_settlements.created_at
		`).
		Joins("JOIN order_line_items ON order_line_items.id = preorder_settlements.order_line_item_id").
		Joins("JOIN orders ON orders.id = order_line_items.order_id").
		Joins("JOIN users ON users.id = orders.customer_id").
		Joins("LEFT JOIN customers ON customers.id = orders.customer_id").
		Joins("LEFT JOIN product_variants ON product_variants.shopify_variant_id = order_line_items.shopify_variant_id").
		Where("preorder_settlements.status = ?", "invoiced").
		Where("preorder_settlements.invoiced_at >= ? AND preorder_settlements.invoiced_at < ?", startOfDay, endOfDay)

	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
