package dashboard

import (
	"context"

	"gorm.io/gorm"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) Store {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) GetDashboardStats(ctx context.Context) (*DashboardRawStats, error) {
	var stats DashboardRawStats

	var totalProducts int64
	if err := s.db.WithContext(ctx).Table("products").
		Where("LOWER(status) <> ?", "deleted").
		Count(&totalProducts).Error; err != nil {
		return nil, err
	}
	stats.TotalProducts = int(totalProducts)

	// 2. Available Stock (ship_ready variants on non-deleted products only)
	s.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(pv.inventory_quantity), 0)
		FROM product_variants pv
		JOIN products p ON p.id = pv.product_id
		WHERE LOWER(p.status) <> 'deleted'
		  AND pv.fulfillment_type = 'ship_ready'
		  AND pv.inventory_quantity > 0
	`).Scan(&stats.AvailableStockCount)

	// Ship-ready SKUs updated today (not total inventory units)
	s.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM product_variants pv
		JOIN products p ON p.id = pv.product_id
		WHERE LOWER(p.status) <> 'deleted'
		  AND pv.fulfillment_type = 'ship_ready'
		  AND pv.inventory_quantity > 0
		  AND DATE(pv.updated_at) = CURRENT_DATE
	`).Scan(&stats.AvailableStockDelta)

	// 3. Orders in Progress
	s.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM orders
		WHERE aggregate_status = 'on_progress'
	`).Scan(&stats.OrdersInProgress)

	s.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM orders
		WHERE aggregate_status = 'on_progress' AND DATE(created_at) = CURRENT_DATE
	`).Scan(&stats.OrdersInProgressDelta)

	// 4. Pre-Orders
	s.db.WithContext(ctx).Raw(`
		SELECT COUNT(DISTINCT order_id)
		FROM order_line_items
		WHERE type = 'pre_order' AND item_status IN ('pending_deposit', 'pending')
	`).Scan(&stats.PreOrdersCount)

	return &stats, nil
}

func (s *PostgresStore) GetRecentOrders(ctx context.Context) ([]RawRecentOrder, error) {
	var orders []RawRecentOrder

	err := s.db.WithContext(ctx).Raw(`
		SELECT 
			o.order_number, 
			c.first_name as customer_first_name, 
			c.last_name as customer_last_name, 
			o.aggregate_status, 
			o.created_at,
			EXISTS (SELECT 1 FROM order_line_items oli WHERE oli.order_id = o.id AND oli.type = 'pre_order') as has_preorder_items,
			(SELECT title FROM order_line_items oli WHERE oli.order_id = o.id ORDER BY created_at ASC LIMIT 1) as preview_item_title,
			(SELECT quantity FROM order_line_items oli WHERE oli.order_id = o.id ORDER BY created_at ASC LIMIT 1) as preview_item_qty
		FROM orders o
		LEFT JOIN customers c ON o.customer_id = c.id
		ORDER BY o.created_at DESC
		LIMIT 5
	`).Scan(&orders).Error

	return orders, err
}

func (s *PostgresStore) GetTotalRevenueThisMonth(ctx context.Context) (float64, error) {
	var total float64
	err := s.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(total_price), 0)
		FROM orders
		WHERE financial_status = 'paid' 
		  AND DATE_TRUNC('month', created_at) = DATE_TRUNC('month', NOW())
	`).Scan(&total).Error
	return total, err
}

func (s *PostgresStore) GetMonthlyRevenue(ctx context.Context) ([]RawMonthlyRevenue, error) {
	var revenues []RawMonthlyRevenue
	err := s.db.WithContext(ctx).Raw(`
		SELECT EXTRACT(month FROM created_at) as month, SUM(total_price) as revenue
		FROM orders
		WHERE financial_status = 'paid'
		  AND EXTRACT(year FROM created_at) = EXTRACT(year FROM NOW())
		GROUP BY month
		ORDER BY month
	`).Scan(&revenues).Error
	return revenues, err
}
