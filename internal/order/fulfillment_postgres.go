package order

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

func (s *PostgresStore) UpsertFulfillmentOrder(ctx context.Context, orderID, shopifyFOID, status string, locationName *string, lineItems []SyncedFulfillmentOrderLineItem) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fo FulfillmentOrder
		err := tx.Where("shopify_fulfillment_order_id = ?", shopifyFOID).First(&fo).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fo = FulfillmentOrder{
				OrderID:                   orderID,
				ShopifyFulfillmentOrderID: shopifyFOID,
				Status:                    status,
				AssignedLocationName:      locationName,
			}
			if err := tx.Create(&fo).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			updates := map[string]interface{}{
				"status":     status,
				"updated_at": time.Now(),
			}
			if locationName != nil {
				updates["assigned_location_name"] = *locationName
			}
			if err := tx.Model(&fo).Updates(updates).Error; err != nil {
				return err
			}
		}

		for _, li := range lineItems {
			var foli FulfillmentOrderLineItem
			err := tx.Where("shopify_fulfillment_order_line_item_id = ?", li.ShopifyFulfillmentOrderLineItemID).First(&foli).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				foli = FulfillmentOrderLineItem{
					FulfillmentOrderID:                fo.ID,
					OrderLineItemID:                   li.OrderLineItemID,
					ShopifyFulfillmentOrderLineItemID: li.ShopifyFulfillmentOrderLineItemID,
					TotalQuantity:                     li.TotalQuantity,
					RemainingQuantity:                 li.RemainingQuantity,
				}
				if err := tx.Create(&foli).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				if err := tx.Model(&foli).Updates(map[string]interface{}{
					"total_quantity":     li.TotalQuantity,
					"remaining_quantity": li.RemainingQuantity,
					"updated_at":         time.Now(),
				}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *PostgresStore) GetFulfillmentOrdersByOrderID(ctx context.Context, orderID string) ([]FulfillmentOrder, error) {
	var fos []FulfillmentOrder
	err := s.db.WithContext(ctx).
		Preload("LineItems").
		Where("order_id = ?", orderID).
		Find(&fos).Error
	return fos, err
}

func (s *PostgresStore) GetFOLIByOrderLineItemIDs(ctx context.Context, orderID string, lineItemIDs []string) ([]FulfillmentOrderLineItem, error) {
	if len(lineItemIDs) == 0 {
		return nil, nil
	}
	var items []FulfillmentOrderLineItem
	err := s.db.WithContext(ctx).
		Joins("JOIN fulfillment_orders ON fulfillment_orders.id = fulfillment_order_line_items.fulfillment_order_id").
		Where("fulfillment_orders.order_id = ? AND fulfillment_order_line_items.order_line_item_id IN ?", orderID, lineItemIDs).
		Find(&items).Error
	return items, err
}

func (s *PostgresStore) DecrementFOLIRemaining(ctx context.Context, foliID string, qty int) error {
	return s.db.WithContext(ctx).
		Model(&FulfillmentOrderLineItem{}).
		Where("id = ? AND remaining_quantity >= ?", foliID, qty).
		UpdateColumn("remaining_quantity", gorm.Expr("remaining_quantity - ?", qty)).Error
}

func (s *PostgresStore) CreateFulfillment(ctx context.Context, f *Fulfillment) error {
	return s.db.WithContext(ctx).Create(f).Error
}

func (s *PostgresStore) UpsertFulfillmentByShopifyID(ctx context.Context, f *Fulfillment, lineItems []FulfillmentLineItem) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if f.ShopifyFulfillmentID != nil && *f.ShopifyFulfillmentID != "" {
			var existing Fulfillment
			err := tx.Where("shopify_fulfillment_id = ?", *f.ShopifyFulfillmentID).First(&existing).Error
			if err == nil {
				updates := map[string]interface{}{
					"tracking_number":  f.TrackingNumber,
					"tracking_url":     f.TrackingURL,
					"tracking_company": f.TrackingCompany,
					"shipment_status":  f.ShipmentStatus,
					"status":           f.Status,
					"updated_at":       time.Now(),
				}
				if f.BatchID != nil {
					updates["batch_id"] = f.BatchID
				}
				if f.FulfilledAt != nil {
					updates["fulfilled_at"] = f.FulfilledAt
				}
				if f.DeliveredAt != nil {
					updates["delivered_at"] = f.DeliveredAt
				}
				if err := tx.Model(&existing).Updates(updates).Error; err != nil {
					return err
				}
				f.ID = existing.ID
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if err := tx.Create(f).Error; err != nil {
			return err
		}
		for i := range lineItems {
			lineItems[i].FulfillmentID = f.ID
		}
		if len(lineItems) > 0 {
			if err := tx.Create(&lineItems).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PostgresStore) GetFulfillmentsByOrderID(ctx context.Context, orderID string) ([]Fulfillment, error) {
	var fulfillments []Fulfillment
	err := s.db.WithContext(ctx).
		Preload("LineItems").
		Where("order_id = ?", orderID).
		Order("sequence_number ASC").
		Find(&fulfillments).Error
	return fulfillments, err
}

func (s *PostgresStore) GetFulfillmentByID(ctx context.Context, fulfillmentID string) (*Fulfillment, error) {
	var f Fulfillment
	err := s.db.WithContext(ctx).
		Preload("LineItems").
		Where("id = ?", fulfillmentID).
		First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *PostgresStore) GetNextFulfillmentSequence(ctx context.Context, orderID string) (int, error) {
	var maxSeq int
	err := s.db.WithContext(ctx).
		Model(&Fulfillment{}).
		Where("order_id = ?", orderID).
		Select("COALESCE(MAX(sequence_number), 0)").
		Scan(&maxSeq).Error
	return maxSeq + 1, err
}

func (s *PostgresStore) MarkFulfillmentDelivered(ctx context.Context, fulfillmentID string, deliveredAt time.Time) error {
	return s.db.WithContext(ctx).
		Model(&Fulfillment{}).
		Where("id = ?", fulfillmentID).
		Updates(map[string]interface{}{
			"status":           "delivered",
			"shipment_status":  "delivered",
			"delivered_at":     deliveredAt,
			"updated_at":       deliveredAt,
		}).Error
}

func (s *PostgresStore) IsWebhookProcessed(ctx context.Context, webhookID string) (bool, error) {
	if webhookID == "" {
		return false, nil
	}
	var existing ShopifyWebhookEvent
	err := s.db.WithContext(ctx).Where("webhook_id = ?", webhookID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *PostgresStore) SaveWebhookEvent(ctx context.Context, webhookID, topic string) error {
	if webhookID == "" {
		return nil
	}
	ev := ShopifyWebhookEvent{
		WebhookID:   webhookID,
		Topic:       topic,
		ProcessedAt: time.Now(),
	}
	return s.db.WithContext(ctx).Create(&ev).Error
}

func (s *PostgresStore) RecordWebhookEvent(ctx context.Context, webhookID, topic string) (bool, error) {
	already, err := s.IsWebhookProcessed(ctx, webhookID)
	if err != nil || already {
		return already, err
	}
	if err := s.SaveWebhookEvent(ctx, webhookID, topic); err != nil {
		return false, err
	}
	return false, nil
}
