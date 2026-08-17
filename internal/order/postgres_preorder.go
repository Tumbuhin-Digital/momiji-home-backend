package order

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/uszip"
	"gorm.io/gorm"
)

type usZipCode struct {
	ZipCode   string `gorm:"primaryKey"`
	StateAbbr string
}

func (usZipCode) TableName() string {
	return "us_zip_codes"
}

func (s *PostgresStore) GetUSZipStateAbbr(ctx context.Context, zip string) (string, bool) {
	normalized, ok := uszip.NormalizeUSZip(zip)
	if !ok {
		return "", false
	}

	var row usZipCode
	if err := s.db.WithContext(ctx).Where("zip_code = ?", normalized).First(&row).Error; err != nil {
		return "", false
	}
	if row.StateAbbr == "" {
		return "", false
	}
	return row.StateAbbr, true
}

func (s *PostgresStore) GetVariantDimensions(ctx context.Context, shopifyVariantIDs []string) (map[string]VariantDimensions, error) {
	result := make(map[string]VariantDimensions)
	if len(shopifyVariantIDs) == 0 {
		return result, nil
	}

	var rows []struct {
		ShopifyVariantID string
		SKU              string
		WeightKg         float64
		WidthCm          float64
		HeightCm         float64
		DepthCm          float64
	}

	err := s.db.WithContext(ctx).Table("product_variants").
		Select("shopify_variant_id, sku, weight_kg, width_cm, height_cm, depth_cm").
		Where("shopify_variant_id IN ?", shopifyVariantIDs).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, r := range rows {
		result[r.ShopifyVariantID] = VariantDimensions{
			ShopifyVariantID: r.ShopifyVariantID,
			SKU:              r.SKU,
			WeightKg:         r.WeightKg,
			WidthCm:          r.WidthCm,
			HeightCm:         r.HeightCm,
			DepthCm:          r.DepthCm,
		}
	}
	return result, nil
}

func (s *PostgresStore) GetPreorderShipment(ctx context.Context, orderID string) (*PreorderShipment, error) {
	// Prefer unbatched shipment for legacy callers; otherwise return the earliest row.
	var shipment PreorderShipment
	err := s.db.WithContext(ctx).
		Preload("PackingItems").
		Where("order_id = ? AND batch_id IS NULL", orderID).
		First(&shipment).Error
	if err == nil {
		return &shipment, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	err = s.db.WithContext(ctx).
		Preload("PackingItems").
		Where("order_id = ?", orderID).
		Order("created_at ASC").
		First(&shipment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &shipment, nil
}

func (s *PostgresStore) GetPreorderShipments(ctx context.Context, orderID string) ([]PreorderShipment, error) {
	var shipments []PreorderShipment
	err := s.db.WithContext(ctx).
		Preload("PackingItems").
		Where("order_id = ?", orderID).
		Order("created_at ASC").
		Find(&shipments).Error
	if err != nil {
		return nil, err
	}
	return shipments, nil
}

func (s *PostgresStore) GetPreorderShipmentByBatch(ctx context.Context, orderID string, batchID *string) (*PreorderShipment, error) {
	var shipment PreorderShipment
	q := s.db.WithContext(ctx).Preload("PackingItems").Where("order_id = ?", orderID)
	if batchID == nil || *batchID == "" {
		q = q.Where("batch_id IS NULL")
	} else {
		q = q.Where("batch_id = ?", *batchID)
	}
	err := q.First(&shipment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &shipment, nil
}

func (s *PostgresStore) GetPreorderShipmentByID(ctx context.Context, shipmentID string) (*PreorderShipment, error) {
	if shipmentID == "" {
		return nil, nil
	}
	var shipment PreorderShipment
	err := s.db.WithContext(ctx).
		Preload("PackingItems").
		Where("id = ?", shipmentID).
		First(&shipment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &shipment, nil
}

func buildPreorderShipmentUpsertUpdates(shipment *PreorderShipment) map[string]interface{} {
	updates := map[string]interface{}{
		"total_boxes":     shipment.TotalBoxes,
		"total_weight_lb": shipment.TotalWeightLb,
		"updated_at":      time.Now(),
	}
	if shipment.WarehouseOrigin != "" {
		updates["warehouse_origin"] = shipment.WarehouseOrigin
	}
	if shipment.EstimatedShipping != nil {
		updates["estimated_shipping"] = shipment.EstimatedShipping
	}
	if shipment.RateCalculatedAt != nil {
		updates["rate_calculated_at"] = shipment.RateCalculatedAt
	}
	// Only the checkout webhook supplies this; admin re-packing upserts leave it zero
	// and must not wipe what the customer already paid.
	if shipment.PrepaidShipping > 0 {
		updates["prepaid_shipping"] = shipment.PrepaidShipping
	}
	return updates
}

func findExistingPreorderShipment(tx *gorm.DB, shipment *PreorderShipment) (*PreorderShipment, error) {
	var existing PreorderShipment
	q := tx.Where("order_id = ?", shipment.OrderID)
	if shipment.ID != "" {
		err := tx.Where("id = ?", shipment.ID).First(&existing).Error
		if err == nil {
			return &existing, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if shipment.BatchID == nil || *shipment.BatchID == "" {
		q = q.Where("batch_id IS NULL")
	} else {
		q = q.Where("batch_id = ?", *shipment.BatchID)
	}
	err := q.First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &existing, nil
}

func (s *PostgresStore) UpsertPreorderShipment(ctx context.Context, shipment *PreorderShipment, packing []PreorderPackingItem) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := findExistingPreorderShipment(tx, shipment)
		if err != nil {
			return err
		}
		if existing == nil {
			if shipment.ID == "" {
				shipment.ID = uuid.NewString()
			}
			if err := tx.Create(shipment).Error; err != nil {
				return err
			}
		} else {
			shipment.ID = existing.ID
			if shipment.BatchID == nil {
				shipment.BatchID = existing.BatchID
			}
			updates := buildPreorderShipmentUpsertUpdates(shipment)
			if err := tx.Model(&PreorderShipment{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
				return err
			}
		}

		if err := tx.Where("preorder_shipment_id = ?", shipment.ID).Delete(&PreorderPackingItem{}).Error; err != nil {
			return err
		}
		for i := range packing {
			packing[i].PreorderShipmentID = shipment.ID
			if packing[i].ID == "" {
				packing[i].ID = uuid.NewString()
			}
			if packing[i].Quantity <= 0 {
				// Leave as-is; caller should set slice quantity.
			}
			if err := tx.Create(&packing[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PostgresStore) UpdatePreorderShipping(ctx context.Context, orderID string, finalPrice float64, notes string, creditAmount float64) error {
	// Legacy: update unbatched shipment if present, else the single/first shipment.
	res := s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("order_id = ? AND batch_id IS NULL", orderID).
		Updates(map[string]interface{}{
			"final_shipping_price": finalPrice,
			"shipping_notes":       notes,
			"credit_amount":        creditAmount,
			"updated_at":           time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("order_id = ?", orderID).
		Updates(map[string]interface{}{
			"final_shipping_price": finalPrice,
			"shipping_notes":       notes,
			"credit_amount":        creditAmount,
			"updated_at":           time.Now(),
		}).Error
}

func (s *PostgresStore) UpdatePreorderShippingByShipmentID(ctx context.Context, shipmentID string, finalPrice float64, notes string, creditAmount float64) error {
	return s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("id = ?", shipmentID).
		Updates(map[string]interface{}{
			"final_shipping_price": finalPrice,
			"shipping_notes":       notes,
			"credit_amount":        creditAmount,
			"updated_at":           time.Now(),
		}).Error
}

func (s *PostgresStore) MarkPreorderInvoiceSent(ctx context.Context, orderID string, sentAt time.Time) error {
	return s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("order_id = ?", orderID).
		Updates(map[string]interface{}{
			"invoice_sent_at": sentAt,
			"updated_at":      time.Now(),
		}).Error
}

func (s *PostgresStore) MarkPreorderShipmentInvoiceSent(
	ctx context.Context,
	shipmentID string,
	draftOrderID, invoiceURL string,
	sentAt time.Time,
	prepaidApplied float64,
) error {
	return s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("id = ?", shipmentID).
		Updates(map[string]interface{}{
			"invoice_sent_at":        sentAt,
			"shopify_draft_order_id": draftOrderID,
			"invoice_url":            invoiceURL,
			// Recorded with the invoice so the shared prepayment pool is not offered twice.
			"prepaid_applied": prepaidApplied,
			"updated_at":      time.Now(),
		}).Error
}

func (s *PostgresStore) MarkPreorderShipmentInvoicePaid(ctx context.Context, shipmentID string, paidAt time.Time) error {
	return s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("id = ?", shipmentID).
		Updates(map[string]interface{}{
			"invoice_paid_at": paidAt,
			"updated_at":      time.Now(),
		}).Error
}

func (s *PostgresStore) GetPreorderShipmentByDraftOrderID(ctx context.Context, draftOrderID string) (*PreorderShipment, error) {
	if draftOrderID == "" {
		return nil, nil
	}
	var shipment PreorderShipment
	err := s.db.WithContext(ctx).
		Preload("PackingItems").
		Where("shopify_draft_order_id = ?", draftOrderID).
		First(&shipment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &shipment, nil
}

func (s *PostgresStore) HasAnyShipmentInvoiceForOrder(ctx context.Context, orderID string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("order_id = ? AND invoice_sent_at IS NOT NULL", orderID).
		Count(&count).Error
	return count > 0, err
}
