package cart

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

type PostgresCartStore struct {
	db *gorm.DB
}

func NewPostgresCartStore(db *gorm.DB) CartStore {
	return &PostgresCartStore{db: db}
}

func (s *PostgresCartStore) GetCart(ctx context.Context, userID, sessionID *string) (*Cart, error) {
	var cart Cart
	query := s.db.WithContext(ctx).Preload("Items").Where("status = ?", "active")

	if userID != nil && *userID != "" {
		query = query.Where("user_id = ?", *userID)
	} else if sessionID != nil && *sessionID != "" {
		query = query.Where("session_id = ?", *sessionID)
	} else {
		return nil, errors.New("must provide userID or sessionID")
	}

	err := query.First(&cart).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cart, nil
}

func (s *PostgresCartStore) CreateCart(ctx context.Context, cart *Cart) error {
	return s.db.WithContext(ctx).Create(cart).Error
}

func (s *PostgresCartStore) AddItem(ctx context.Context, item *CartItemModel) error {
	// check if item already exists
	var existing CartItemModel
	err := s.db.WithContext(ctx).Where("cart_id = ? AND shopify_variant_id = ? AND fulfillment_type = ?", item.CartID, item.ShopifyVariantID, item.FulfillmentType).First(&existing).Error
	
	if err == nil {
		// Update quantity
		return s.db.WithContext(ctx).Model(&existing).Update("quantity", existing.Quantity + item.Quantity).Error
	}
	
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.db.WithContext(ctx).Create(item).Error
	}
	return err
}

func (s *PostgresCartStore) UpdateItemQuantity(ctx context.Context, itemID string, quantity int) error {
	return s.db.WithContext(ctx).Model(&CartItemModel{}).Where("id = ?", itemID).Update("quantity", quantity).Error
}

func (s *PostgresCartStore) RemoveItem(ctx context.Context, itemID string) error {
	return s.db.WithContext(ctx).Where("id = ?", itemID).Delete(&CartItemModel{}).Error
}

func (s *PostgresCartStore) ClearCart(ctx context.Context, cartID string) error {
	return s.db.WithContext(ctx).Where("cart_id = ?", cartID).Delete(&CartItemModel{}).Error
}

func (s *PostgresCartStore) MergeCarts(ctx context.Context, sourceCartID, targetCartID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sourceItems []CartItemModel
		if err := tx.Where("cart_id = ?", sourceCartID).Find(&sourceItems).Error; err != nil {
			return err
		}

		for _, sItem := range sourceItems {
			var tItem CartItemModel
			err := tx.Where("cart_id = ? AND shopify_variant_id = ? AND fulfillment_type = ?", targetCartID, sItem.ShopifyVariantID, sItem.FulfillmentType).First(&tItem).Error
			if err == nil {
				if err := tx.Model(&tItem).Update("quantity", tItem.Quantity+sItem.Quantity).Error; err != nil {
					return err
				}
			} else if errors.Is(err, gorm.ErrRecordNotFound) {
				sItem.ID = "" // let db generate
				sItem.CartID = targetCartID
				if err := tx.Create(&sItem).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		}

		// Close source cart
		return tx.Model(&Cart{}).Where("id = ?", sourceCartID).Update("status", "merged").Error
	})
}
