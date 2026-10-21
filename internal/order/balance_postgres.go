package order

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func (s *PostgresStore) GetVariantStockRefs(ctx context.Context, shopifyVariantIDs []string) (map[string]VariantStockRef, error) {
	out := make(map[string]VariantStockRef, len(shopifyVariantIDs))
	if len(shopifyVariantIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ShopifyVariantID       string
		ShopifyInventoryItemID string
		InventoryTracked       bool
	}
	err := s.db.WithContext(ctx).Table("product_variants").
		Select("shopify_variant_id, shopify_inventory_item_id, inventory_tracked").
		Where("shopify_variant_id IN ?", shopifyVariantIDs).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ShopifyVariantID] = VariantStockRef{
			ShopifyVariantID: r.ShopifyVariantID,
			InventoryItemID:  r.ShopifyInventoryItemID,
			Tracked:          r.InventoryTracked,
		}
	}
	return out, nil
}

// ClaimOrderEditBalance marks a group as having a balance edit about to be committed. Only
// a group with no invoice and no claim can be claimed, so two requests racing for the same
// group cannot both commit an edit.
func (s *PostgresStore) ClaimOrderEditBalance(ctx context.Context, shipmentID string, expectedOutstanding float64) (bool, error) {
	res := s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("id = ? AND invoice_sent_at IS NULL AND balance_method IS NULL", shipmentID).
		Updates(map[string]interface{}{
			"balance_method":               BalanceMethodOrderEdit,
			"balance_expected_outstanding": expectedOutstanding,
			"updated_at":                   time.Now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ReleaseOrderEditBalanceClaim undoes a claim whose edit was never committed.
func (s *PostgresStore) ReleaseOrderEditBalanceClaim(ctx context.Context, shipmentID string) error {
	return s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("id = ? AND invoice_sent_at IS NULL AND balance_method = ?", shipmentID, BalanceMethodOrderEdit).
		Updates(map[string]interface{}{
			"balance_method":               nil,
			"balance_expected_outstanding": nil,
			"updated_at":                   time.Now(),
		}).Error
}

// CompleteOrderEditBalance records a committed balance edit: the group is invoiced and the
// real product lines it added are linked to their app line items.
func (s *PostgresStore) CompleteOrderEditBalance(ctx context.Context, shipmentID string, result OrderEditBalanceResult) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"balance_method":               BalanceMethodOrderEdit,
			"invoice_sent_at":              result.SentAt,
			"invoice_url":                  result.PaymentURL,
			"prepaid_applied":              result.PrepaidApplied,
			"balance_expected_outstanding": result.ExpectedOutstanding,
			"shopify_shipping_line_id":     optionalString(result.ShippingLineID),
			"updated_at":                   time.Now(),
		}
		if err := tx.Model(&PreorderShipment{}).Where("id = ?", shipmentID).Updates(updates).Error; err != nil {
			return err
		}
		for i := range result.Lines {
			line := result.Lines[i]
			line.ShipmentID = shipmentID
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "shopify_line_item_id"}},
				DoNothing: true,
			}).Create(&line).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateOrderEditBalanceShipping saves what a re-price edit changed. The paid guard keeps a
// payment that landed in the meantime from being overwritten.
func (s *PostgresStore) UpdateOrderEditBalanceShipping(ctx context.Context, shipmentID string, expectedOutstanding float64, shippingLineID, paymentURL string) error {
	updates := map[string]interface{}{
		"balance_expected_outstanding": expectedOutstanding,
		"shopify_shipping_line_id":     optionalString(shippingLineID),
		"updated_at":                   time.Now(),
	}
	if paymentURL != "" {
		updates["invoice_url"] = paymentURL
	}
	res := s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("id = ? AND invoice_paid_at IS NULL", shipmentID).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPreorderShipmentPaid
	}
	return nil
}

func (s *PostgresStore) GetBalanceLinesByOrderID(ctx context.Context, orderID string) ([]PreorderBalanceLine, error) {
	var lines []PreorderBalanceLine
	err := s.db.WithContext(ctx).
		Joins("JOIN preorder_shipments ON preorder_shipments.id = preorder_balance_lines.shipment_id").
		Where("preorder_shipments.order_id = ?", orderID).
		Order("preorder_balance_lines.created_at ASC").
		Find(&lines).Error
	return lines, err
}

// ListAwaitingOrderEditBalances returns groups whose balance edit is committed but unpaid.
func (s *PostgresStore) ListAwaitingOrderEditBalances(ctx context.Context) ([]PreorderShipment, error) {
	var shipments []PreorderShipment
	err := s.db.WithContext(ctx).
		Where("balance_method = ? AND invoice_sent_at IS NOT NULL AND invoice_paid_at IS NULL", BalanceMethodOrderEdit).
		Order("invoice_sent_at ASC").
		Find(&shipments).Error
	return shipments, err
}
