package product

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) Store {
	return &PostgresStore{db: db}
}

const activeProductStatus = "active"
const unlistedProductStatus = "unlisted"

func scopeListableProducts(query *gorm.DB) *gorm.DB {
	return query.Where("LOWER(products.status) IN ?", []string{activeProductStatus, unlistedProductStatus})
}

func (s *PostgresStore) GetProducts(ctx context.Context, q ProductQuery) ([]Product, int64, error) {
	var products []Product
	var total int64

	query := s.db.WithContext(ctx).Model(&Product{})
	query = scopeListableProducts(query)

	if status := strings.ToLower(strings.TrimSpace(q.Status)); status != "" {
		switch status {
		case activeProductStatus, unlistedProductStatus:
			query = query.Where("LOWER(products.status) = ?", status)
		}
	}

	if q.Search != "" {
		searchPattern := "%" + q.Search + "%"
		query = query.Where("products.title ILIKE ? OR products.description ILIKE ?", searchPattern, searchPattern)
	}

	switch q.FulfillmentType {
	case "unlisted":
		// Client sends unlisted via the same fulfillment_type param as ship_ready/pre_order.
		query = query.Where("LOWER(products.status) = ?", unlistedProductStatus)
	case "mixed":
		query = query.Where(`products.id IN (
			SELECT product_id
			FROM product_variants
			GROUP BY product_id
			HAVING COUNT(DISTINCT fulfillment_type) > 1
		)`)
	case "":
		// no fulfillment filter
	default:
		query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").
			Where("product_variants.fulfillment_type = ?", q.FulfillmentType).
			Group("products.id")
	}

	if q.ExcludeInactive {
		query = query.Where("products.id NOT IN (SELECT product_id FROM product_variants WHERE fulfillment_type = ?)", "inactive")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	limit := q.Limit
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	} // Fix 2D: Cap limit to 100
	offset := (page - 1) * limit

	orderStr := "products.created_at DESC" // Fix 1C: Default sorting
	needsVariantJoin := q.FulfillmentType == "" || q.FulfillmentType == "mixed" || q.FulfillmentType == "unlisted"
	switch q.Sort {
	case "price_asc":
		if needsVariantJoin {
			query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").Group("products.id")
		}
		orderStr = "MIN(COALESCE(product_variants.ws_price, product_variants.price)) ASC"
	case "price_desc":
		if needsVariantJoin {
			query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").Group("products.id")
		}
		orderStr = "MAX(COALESCE(product_variants.ws_price, product_variants.price)) DESC"
	case "name_asc":
		orderStr = "products.title ASC"
	case "created_at":
		orderStr = "products.created_at DESC"
	case "stock_asc":
		if needsVariantJoin {
			query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").Group("products.id")
		}
		orderStr = "SUM(product_variants.inventory_quantity) ASC"
	case "stock_desc":
		if needsVariantJoin {
			query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").Group("products.id")
		}
		orderStr = "SUM(product_variants.inventory_quantity) DESC"
	}

	err := query.Preload("Images").Preload("Variants").Order(orderStr).Offset(offset).Limit(limit).Find(&products).Error
	return products, total, err
}

func (s *PostgresStore) ListShopifyProductIDs(ctx context.Context) ([]string, error) {
	var ids []string
	if err := s.db.WithContext(ctx).Model(&Product{}).
		Where("LOWER(status) NOT IN ?", []string{ProductStatusDeleted, ProductStatusCreating, ProductStatusFailed}).
		Where("shopify_id NOT LIKE ?", "pending:%").
		Pluck("shopify_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *PostgresStore) GetVariantByShopifyID(ctx context.Context, shopifyVariantID string) (*ProductVariant, error) {
	var variant ProductVariant
	err := s.db.WithContext(ctx).Where("shopify_variant_id = ?", shopifyVariantID).First(&variant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &variant, nil
}

func (s *PostgresStore) GetVariantByInventoryItemID(ctx context.Context, itemID string) (*ProductVariant, error) {
	var variant ProductVariant
	if err := s.db.WithContext(ctx).Where("shopify_inventory_item_id = ?", itemID).First(&variant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &variant, nil
}

func (s *PostgresStore) UpsertProduct(ctx context.Context, product *Product) error {
	// Ensure ID is not set — let Postgres generate the UUID on insert.
	// If ID were an empty string, GORM would try INSERT with id='' which fails on UUID column.
	product.ID = ""
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "shopify_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"title", "description", "status",
			"handle", "vendor", "product_type", "tags", "body_html",
			"updated_at",
		}),
	}).Create(product).Error
}

func (s *PostgresStore) MarkProductDeletedByShopifyID(ctx context.Context, shopifyID string) error {
	candidates := []string{shopifyID}
	if _, err := strconv.ParseInt(shopifyID, 10, 64); err == nil {
		candidates = append(candidates, "gid://shopify/Product/"+shopifyID)
	} else if idx := strings.LastIndex(shopifyID, "/"); idx >= 0 && idx < len(shopifyID)-1 {
		numericID := shopifyID[idx+1:]
		candidates = append(candidates, numericID)
	}

	return s.db.WithContext(ctx).Model(&Product{}).
		Where("shopify_id IN ?", candidates).
		Updates(map[string]interface{}{"status": "deleted", "updated_at": gorm.Expr("now()")}).
		Error
}

func (s *PostgresStore) UpsertVariant(ctx context.Context, variant *ProductVariant) error {
	// Clear ID so Postgres generates a fresh UUID on insert (avoids empty-string UUID error).
	variant.ID = ""
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "shopify_variant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"title", "sku", "price", "image_src",
			"inventory_quantity", "shopify_inventory_item_id", "updated_at",
		}),
		// Select("*") ensures inventory_tracked=false is written on INSERT (GORM omits bool zero-values).
	}).Select("*").Create(variant).Error
}

func (s *PostgresStore) DeleteVariantByShopifyID(ctx context.Context, shopifyVariantID string) error {
	return s.db.WithContext(ctx).
		Where("shopify_variant_id = ?", shopifyVariantID).
		Delete(&ProductVariant{}).Error
}

func (s *PostgresStore) DeleteVariantsNotIn(ctx context.Context, productID string, keepShopifyVariantIDs []string) error {
	if productID == "" || len(keepShopifyVariantIDs) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).
		Where("product_id = ? AND shopify_variant_id NOT IN ?", productID, keepShopifyVariantIDs).
		Delete(&ProductVariant{}).Error
}

func (s *PostgresStore) UpdateInventoryQuantity(ctx context.Context, shopifyVariantID string, quantity int) error {
	return s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("shopify_variant_id = ?", shopifyVariantID).
		Updates(map[string]interface{}{
			"inventory_quantity": quantity,
			"updated_at":         gorm.Expr("now()"),
		}).Error
}

func (s *PostgresStore) UpsertProductImages(ctx context.Context, productID string, images []ProductImage) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Get all current image Shopify IDs
		var currentIDs []string
		for _, img := range images {
			currentIDs = append(currentIDs, img.ShopifyID)
		}

		// Delete images that are no longer present
		if len(currentIDs) > 0 {
			if err := tx.Where("product_id = ? AND shopify_id NOT IN ?", productID, currentIDs).Delete(&ProductImage{}).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Where("product_id = ?", productID).Delete(&ProductImage{}).Error; err != nil {
				return err
			}
		}

		// Upsert the provided images
		if len(images) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "shopify_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"src", "alt", "position"}),
			}).Create(&images).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (s *PostgresStore) UpdateVariantPrices(ctx context.Context, variantID string, wsPrice *float64) error {
	updates := map[string]interface{}{}
	if wsPrice != nil {
		updates["ws_price"] = *wsPrice
	}
	if len(updates) == 0 {
		return nil
	}
	result := s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("shopify_variant_id = ?", variantID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *PostgresStore) UpdateVariantLtl(ctx context.Context, variantID string, isLtl bool) error {
	result := s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("shopify_variant_id = ?", variantID).
		Updates(map[string]interface{}{
			"is_ltl":     isLtl,
			"updated_at": gorm.Expr("now()"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *PostgresStore) MarkCustomVariantLinked(ctx context.Context, shopifyVariantID, sku string) error {
	result := s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("shopify_variant_id = ?", shopifyVariantID).
		Updates(map[string]interface{}{
			"sku":                sku,
			"custom_link_state":  CustomLinkStateLinked,
			"inventory_tracked":  true,
			"updated_at":         gorm.Expr("now()"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *PostgresStore) GetAllVariants(ctx context.Context) ([]ProductVariant, error) {
	var variants []ProductVariant
	err := s.db.WithContext(ctx).
		Joins("JOIN products ON products.id = product_variants.product_id").
		Where("LOWER(products.status) IN ?", []string{activeProductStatus, unlistedProductStatus}).
		Preload("Product").
		Find(&variants).Error
	if err != nil {
		return nil, err
	}
	return variants, nil
}

func (s *PostgresStore) GetBatchSummariesByVariantIDs(ctx context.Context, variantIDs []string) (map[string]VariantBatchSummary, error) {
	summaries := make(map[string]VariantBatchSummary, len(variantIDs))
	if len(variantIDs) == 0 {
		return summaries, nil
	}

	type countRow struct {
		ShopifyVariantID string `gorm:"column:shopify_variant_id"`
		TotalCount       int    `gorm:"column:total_count"`
		ActiveCount      int    `gorm:"column:active_count"`
		QueuedCount      int    `gorm:"column:queued_count"`
	}

	var countRows []countRow
	err := s.db.WithContext(ctx).
		Table("preorder_batches pb").
		Select(`
			pv.shopify_variant_id AS shopify_variant_id,
			COUNT(*) FILTER (WHERE pb.status <> 'cancelled') AS total_count,
			COUNT(*) FILTER (WHERE pb.status = 'active') AS active_count,
			COUNT(*) FILTER (WHERE pb.status = 'queued') AS queued_count
		`).
		Joins("JOIN product_variants pv ON pv.id = pb.variant_id").
		Where("pv.shopify_variant_id IN ?", variantIDs).
		Group("pv.shopify_variant_id").
		Scan(&countRows).Error
	if err != nil {
		return nil, err
	}

	for _, item := range countRows {
		summaries[item.ShopifyVariantID] = VariantBatchSummary{
			TotalCount:  item.TotalCount,
			ActiveCount: item.ActiveCount,
			QueuedCount: item.QueuedCount,
			HasBatches:  item.TotalCount > 0,
		}
	}

	type detailRow struct {
		ShopifyVariantID string `gorm:"column:shopify_variant_id"`
		BatchID          string `gorm:"column:batch_id"`
		Name             string `gorm:"column:name"`
		Status           string `gorm:"column:status"`
		Sequence         int    `gorm:"column:sequence"`
		QtyAllocated     int    `gorm:"column:qty_allocated"`
		QtySold          int    `gorm:"column:qty_sold"`
		LockedQty        int    `gorm:"column:locked_qty"`
	}

	var detailRows []detailRow
	err = s.db.WithContext(ctx).
		Table("preorder_batches pb").
		Select(`
			pv.shopify_variant_id AS shopify_variant_id,
			pb.id AS batch_id,
			pb.name AS name,
			pb.status AS status,
			pb.sequence AS sequence,
			pb.qty_allocated AS qty_allocated,
			pb.qty_sold AS qty_sold,
			COALESCE((
				SELECT SUM(pbl.quantity)
				FROM preorder_batch_locks pbl
				WHERE pbl.batch_id = pb.id AND pbl.expires_at > now()
			), 0) AS locked_qty
		`).
		Joins("JOIN product_variants pv ON pv.id = pb.variant_id").
		Where("pv.shopify_variant_id IN ? AND pb.status IN ?", variantIDs, []string{"active", "queued"}).
		Order("pv.shopify_variant_id ASC, pb.sequence ASC, pb.created_at ASC").
		Scan(&detailRows).Error
	if err != nil {
		return nil, err
	}

	type openBatch struct {
		id        string
		name      string
		status    string
		remaining int
	}
	grouped := make(map[string][]openBatch)
	for _, row := range detailRows {
		remaining := row.QtyAllocated - row.QtySold - row.LockedQty
		if remaining < 0 {
			remaining = 0
		}
		grouped[row.ShopifyVariantID] = append(grouped[row.ShopifyVariantID], openBatch{
			id:        row.BatchID,
			name:      row.Name,
			status:    row.Status,
			remaining: remaining,
		})
	}

	for variantID, batches := range grouped {
		summary := summaries[variantID]
		maxOrderable := 0
		var activeFound, nextFound bool
		for _, batch := range batches {
			maxOrderable += batch.remaining
			if !activeFound && batch.status == "active" {
				id := batch.id
				name := batch.name
				remaining := batch.remaining
				summary.ActiveBatchID = &id
				summary.ActiveBatchName = &name
				summary.ActiveBatchRemaining = &remaining
				activeFound = true
				continue
			}
			if activeFound && !nextFound && batch.status == "queued" {
				name := batch.name
				remaining := batch.remaining
				summary.NextBatchName = &name
				summary.NextBatchRemaining = &remaining
				nextFound = true
			}
		}
		if !activeFound {
			for _, batch := range batches {
				if batch.status != "queued" {
					continue
				}
				id := batch.id
				name := batch.name
				remaining := batch.remaining
				summary.ActiveBatchID = &id
				summary.ActiveBatchName = &name
				summary.ActiveBatchRemaining = &remaining
				break
			}
		}
		summary.MaxBatchOrderableQty = &maxOrderable
		summary.HasBatches = summary.TotalCount > 0
		summaries[variantID] = summary
	}

	return summaries, nil
}

func (s *PostgresStore) BulkUpdateVariantDimensions(ctx context.Context, inputs []DimensionUpdateInput) (BulkUpdateDimensionsResult, error) {
	var result BulkUpdateDimensionsResult
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return result, tx.Error
	}
	defer tx.Rollback()

	for _, input := range inputs {
		updates := map[string]interface{}{}
		if input.UpdateDimensions {
			updates["width_cm"] = input.WidthCm
			updates["height_cm"] = input.HeightCm
			updates["depth_cm"] = input.DepthCm
		}
		if input.WeightKg != nil {
			updates["weight_kg"] = *input.WeightKg
		}
		if len(updates) == 0 {
			continue
		}
		res := tx.Model(&ProductVariant{}).Where("shopify_variant_id = ?", input.ShopifyVariantID).Updates(updates)
		if res.Error != nil {
			return result, res.Error
		}
		if res.RowsAffected == 0 {
			result.NotFoundIDs = append(result.NotFoundIDs, input.ShopifyVariantID)
			continue
		}
		result.UpdatedCount++
		if input.WeightKg != nil {
			result.WeightUpdatedCount++
		}
	}
	if err := tx.Commit().Error; err != nil {
		return result, err
	}
	return result, nil
}

func (s *PostgresStore) GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error) {
	var product Product
	err := s.db.WithContext(ctx).Where("shopify_id = ?", shopifyID).First(&product).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &product, nil
}

func (s *PostgresStore) GetProductByID(ctx context.Context, productID string) (*Product, error) {
	var p Product
	err := s.db.WithContext(ctx).
		Preload("Images").
		Preload("Variants").
		Where("id = ? AND LOWER(status) IN ?", productID, []string{activeProductStatus, unlistedProductStatus}).
		First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (s *PostgresStore) IsVariantFromActiveProduct(ctx context.Context, shopifyVariantID string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&ProductVariant{}).
		Joins("JOIN products ON products.id = product_variants.product_id").
		Where("product_variants.shopify_variant_id = ? AND LOWER(products.status) IN ?", shopifyVariantID, []string{activeProductStatus, unlistedProductStatus}).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *PostgresStore) GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error) {
	var variants []ProductVariant
	err := s.db.WithContext(ctx).Where("product_id = ?", productID).Find(&variants).Error
	return variants, err
}

func (s *PostgresStore) UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) error {
	return s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("product_id = ?", productID).
		Updates(map[string]interface{}{"fulfillment_type": fulfillmentType, "updated_at": gorm.Expr("now()")}).Error
}

func (s *PostgresStore) UpdateVariantFulfillmentType(ctx context.Context, variantID string, fulfillmentType string) error {
	return s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("shopify_variant_id = ?", variantID).
		Updates(map[string]interface{}{"fulfillment_type": fulfillmentType, "updated_at": gorm.Expr("now()")}).Error
}

func (s *PostgresStore) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string, expectedShipDate *string) error {
	updates := map[string]interface{}{
		"preorder_batch_label": batchLabel,
		"updated_at":           gorm.Expr("now()"),
	}
	if expectedShipDate != nil && *expectedShipDate != "" {
		if t, err := time.Parse(time.RFC3339, *expectedShipDate); err == nil {
			updates["expected_ship_date"] = t
		} else if t, err := time.Parse("2006-01-02", *expectedShipDate); err == nil {
			updates["expected_ship_date"] = t
		} else {
			updates["expected_ship_date"] = *expectedShipDate // fallback
		}
	}
	return s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("product_id = ?", productID).
		Updates(updates).Error
}

func (s *PostgresStore) UpdateSingleVariantBatchLabel(ctx context.Context, shopifyVariantID string, batchLabel string) error {
	updates := map[string]interface{}{
		"updated_at": gorm.Expr("now()"),
	}
	if batchLabel == "" {
		updates["preorder_batch_label"] = nil
	} else {
		updates["preorder_batch_label"] = batchLabel
	}
	result := s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("shopify_variant_id = ?", shopifyVariantID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *PostgresStore) GetProductByIdempotencyKey(ctx context.Context, key string) (*Product, error) {
	var p Product
	err := s.db.WithContext(ctx).
		Preload("Images").
		Preload("Variants").
		Where("create_idempotency_key = ?", key).
		First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (s *PostgresStore) CreateCustomProductStub(ctx context.Context, product *Product) error {
	product.ID = ""
	return s.db.WithContext(ctx).Create(product).Error
}

func (s *PostgresStore) FinalizeCustomProduct(ctx context.Context, productID string, product *Product, variants []ProductVariant, images []ProductImage) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"shopify_id": product.ShopifyID,
			"title":      product.Title,
			"status":     product.Status,
			"handle":     product.Handle,
			"updated_at": gorm.Expr("now()"),
		}
		if product.InternalNote != nil {
			updates["internal_note"] = *product.InternalNote
		}
		if err := tx.Model(&Product{}).Where("id = ?", productID).Updates(updates).Error; err != nil {
			return err
		}

		if err := tx.Where("product_id = ?", productID).Delete(&ProductVariant{}).Error; err != nil {
			return err
		}
		for i := range variants {
			variants[i].ID = ""
			variants[i].ProductID = productID
			// Select("*") so inventory_tracked=false is persisted (GORM skips bool zero-values otherwise).
			if err := tx.Select("*").Create(&variants[i]).Error; err != nil {
				return err
			}
		}

		if err := tx.Where("product_id = ?", productID).Delete(&ProductImage{}).Error; err != nil {
			return err
		}
		for i := range images {
			images[i].ID = ""
			images[i].ProductID = productID
			if err := tx.Create(&images[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PostgresStore) MarkCustomProductFailed(ctx context.Context, productID string) error {
	return s.db.WithContext(ctx).Model(&Product{}).
		Where("id = ?", productID).
		Updates(map[string]interface{}{"status": ProductStatusFailed, "updated_at": gorm.Expr("now()")}).
		Error
}

func (s *PostgresStore) DeleteProductByID(ctx context.Context, productID string) error {
	return s.db.WithContext(ctx).Where("id = ?", productID).Delete(&Product{}).Error
}
