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
	var shipment PreorderShipment
	err := s.db.WithContext(ctx).
		Preload("PackingItems").
		Where("order_id = ?", orderID).
		First(&shipment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &shipment, nil
}

func (s *PostgresStore) UpsertPreorderShipment(ctx context.Context, shipment *PreorderShipment, packing []PreorderPackingItem) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing PreorderShipment
		err := tx.Where("order_id = ?", shipment.OrderID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if shipment.ID == "" {
				shipment.ID = uuid.NewString()
			}
			if err := tx.Create(shipment).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			shipment.ID = existing.ID
			updates := map[string]interface{}{
				"total_boxes":     shipment.TotalBoxes,
				"total_weight_lb": shipment.TotalWeightLb,
				"updated_at":      time.Now(),
			}
			if shipment.EstimatedShipping != nil {
				updates["estimated_shipping"] = shipment.EstimatedShipping
			}
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
			if err := tx.Create(&packing[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PostgresStore) UpdatePreorderShipping(ctx context.Context, orderID string, finalPrice float64, notes string, creditAmount float64) error {
	return s.db.WithContext(ctx).Model(&PreorderShipment{}).
		Where("order_id = ?", orderID).
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
