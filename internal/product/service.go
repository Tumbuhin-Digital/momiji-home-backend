package product

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preordercustomtext"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type ProductService interface {
	GetVariantByID(ctx context.Context, variantID string) (*VariantDTO, error)
	GetProducts(ctx context.Context, query ProductQuery) ([]ProductDTO, int64, error)
	SyncFromShopify(ctx context.Context) error
	GetProductByID(ctx context.Context, id string) (*ProductDTO, error)
	GetVariantsByProductID(ctx context.Context, productID string) ([]VariantDTO, error)
	UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) (*ProductDTO, error)
	UpdateVariantStatus(ctx context.Context, variantID string, fulfillmentType string) (*VariantDTO, error)
	UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string, expectedShipDate *string) (*ProductDTO, error)
	UpdateVariantBatchLabelByVariantID(ctx context.Context, variantID string, batchLabel string) (*VariantDTO, error)
	UpdateVariantPrice(ctx context.Context, variantID string, wsPrice *float64) error
	UpdateVariantLtl(ctx context.Context, variantID string, isLtl bool) (*VariantDTO, error)
	LinkCustomVariantSKU(ctx context.Context, variantID, sku string) (*VariantDTO, error)
	GetAllVariants(ctx context.Context) ([]ProductVariant, error)
	BulkUpdateDimensions(ctx context.Context, rows []DimensionUpdateInput) (BulkUpdateDimensionsResult, error)
	ValidateVariantActive(ctx context.Context, variantID string) error
	CreateCustomProduct(ctx context.Context, input CreateCustomProductInput) (*ProductDTO, error)
	AddProductVariants(ctx context.Context, input AddProductVariantsInput) (*ProductDTO, error)
	SyncInventoryQuantities(ctx context.Context, quantities map[string]int) error
}

type service struct {
	store             Store
	client            shopify.Client
	customTextService preordercustomtext.Service
}

func NewProductService(store Store, client shopify.Client, customTextService preordercustomtext.Service) ProductService {
	return &service{store: store, client: client, customTextService: customTextService}
}

func (s *service) GetVariantByID(ctx context.Context, variantID string) (*VariantDTO, error) {
	variant, err := s.store.GetVariantByShopifyID(ctx, variantID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if variant == nil {
		return nil, apierror.ErrNotFound
	}

	dto := mapVariantToDTO(variant)

	p, err := s.store.GetProductByID(ctx, variant.ProductID)
	if err == nil && p != nil {
		if variant.Title == "" || variant.Title == "Default Title" {
			dto.Title = p.Title
		} else {
			dto.Title = p.Title + " - " + variant.Title
		}
		if dto.ImageSrc == "" && len(p.Images) > 0 {
			dto.ImageSrc = p.Images[0].Src
		}
	}
	if summaries, err := s.store.GetBatchSummariesByVariantIDs(ctx, []string{variant.ShopifyVariantID}); err == nil {
		if summary, ok := summaries[variant.ShopifyVariantID]; ok {
			dto.BatchSummary = mapVariantBatchSummaryDTO(&summary)
		}
	}

	return dto, nil
}

func (s *service) GetProducts(ctx context.Context, query ProductQuery) ([]ProductDTO, int64, error) {
	products, total, err := s.store.GetProducts(ctx, query)
	if err != nil {
		return nil, 0, apierror.ErrInternal
	}

	variantFilter := ""
	if query.FilterVariantsInResponse && query.FulfillmentType != "" && query.FulfillmentType != "unlisted" {
		variantFilter = query.FulfillmentType
	}

	dtos := make([]ProductDTO, len(products))
	variantIDs := make([]string, 0)
	for i := range products {
		for _, variant := range products[i].Variants {
			variantIDs = append(variantIDs, variant.ShopifyVariantID)
		}
	}
	batchSummaries, err := s.store.GetBatchSummariesByVariantIDs(ctx, variantIDs)
	if err != nil {
		return nil, 0, apierror.ErrInternal
	}
	for i, p := range products {
		for variantIdx := range p.Variants {
			if summary, ok := batchSummaries[p.Variants[variantIdx].ShopifyVariantID]; ok {
				p.Variants[variantIdx].BatchSummary = &summary
			}
		}
		dtos[i] = mapProductToDTO(&p, variantFilter)
	}

	if query.FilterVariantsInResponse {
		filtered := make([]ProductDTO, 0, len(dtos))
		for _, dto := range dtos {
			if len(dto.Variants) > 0 {
				filtered = append(filtered, dto)
			}
		}
		dtos = filtered
	}

	return dtos, total, nil
}

func mapProductToDTO(p *Product, fulfillmentTypeFilter string) ProductDTO {
	images := make([]ProductImageDTO, len(p.Images))
	for i, img := range p.Images {
		images[i] = ProductImageDTO{
			ID:       img.ShopifyID,
			Src:      img.Src,
			Alt:      img.Alt,
			Position: img.Position,
		}
	}

	sourceVariants := p.Variants
	if fulfillmentTypeFilter != "" {
		filtered := make([]ProductVariant, 0, len(p.Variants))
		for _, v := range p.Variants {
			if string(v.FulfillmentType) == fulfillmentTypeFilter {
				filtered = append(filtered, v)
			}
		}
		sourceVariants = filtered
	}

	variants := make([]VariantDTO, len(sourceVariants))
	for i, v := range sourceVariants {
		vDto := mapVariantToDTO(&v)
		if vDto.ImageSrc == "" && len(p.Images) > 0 {
			vDto.ImageSrc = p.Images[0].Src
		}
		variants[i] = *vDto
	}

	// Determine product-level preorder_batch_label and expected_ship_date from first variant
	var batchLabel, shipDate *string
	if len(sourceVariants) > 0 {
		if sourceVariants[0].PreorderBatchLabel != nil {
			batchLabel = sourceVariants[0].PreorderBatchLabel
		}
		if sourceVariants[0].ExpectedShipDate != nil {
			dt := sourceVariants[0].ExpectedShipDate.Format("2006-01-02T15:04:05Z07:00")
			shipDate = &dt
		}
	}

	return ProductDTO{
		ID:                 p.ID,
		ShopifyID:          p.ShopifyID,
		Handle:             p.Handle,
		Title:              p.Title,
		Description:        p.Description,
		Vendor:             p.Vendor,
		ProductType:        p.ProductType,
		Tags:               p.Tags,
		Status:             p.Status,
		Origin:             originOrDefault(p.Origin),
		InternalNote:       p.InternalNote,
		PreorderBatchLabel: batchLabel,
		ExpectedShipDate:   shipDate,
		BodyHTML:           p.BodyHTML,
		Variants:           variants,
		Images:             images,
	}
}

func originOrDefault(origin string) string {
	if origin == "" {
		return ProductOriginShopifySync
	}
	return origin
}

func mapVariantToDTO(variant *ProductVariant) *VariantDTO {
	wsPrice := "0.00"
	if variant.WSPrice != nil {
		wsPrice = fmt.Sprintf("%.2f", *variant.WSPrice)
	} else {
		wsPrice = fmt.Sprintf("%.2f", variant.Price)
	}

	retailPrice := fmt.Sprintf("%.2f", variant.Price)

	var sku *string
	if variant.SKU != "" {
		skuStr := variant.SKU
		sku = &skuStr
	}

	ft := string(variant.FulfillmentType)

	return &VariantDTO{
		ID:                 variant.ShopifyVariantID,
		Title:              variant.Title,
		SKU:                sku,
		ImageSrc:           variant.ImageSrc,
		RetailPrice:        retailPrice,
		WSPrice:            wsPrice,
		FulfillmentType:    FulfillmentType(ft),
		InventoryQuantity:  variant.InventoryQuantity,
		InventoryTracked:   variant.InventoryTracked,
		CustomLinkState:    variant.CustomLinkState,
		WeightKg:           variant.WeightKg,
		LengthCm:           variant.DepthCm, // Mapping Depth to Length
		WidthCm:            variant.WidthCm,
		HeightCm:           variant.HeightCm,
		PreorderBatchLabel: variant.PreorderBatchLabel,
		IsLtl:              variant.IsLtl,
		BatchSummary:       mapVariantBatchSummaryDTO(variant.BatchSummary),
	}
}

func mapVariantBatchSummaryDTO(summary *VariantBatchSummary) *VariantBatchSummaryDTO {
	if summary == nil {
		return nil
	}
	return &VariantBatchSummaryDTO{
		TotalCount:           summary.TotalCount,
		ActiveCount:          summary.ActiveCount,
		QueuedCount:          summary.QueuedCount,
		HasBatches:           summary.HasBatches,
		ActiveBatchID:        summary.ActiveBatchID,
		ActiveBatchName:      summary.ActiveBatchName,
		ActiveBatchRemaining: summary.ActiveBatchRemaining,
		NextBatchName:        summary.NextBatchName,
		NextBatchRemaining:   summary.NextBatchRemaining,
		MaxBatchOrderableQty: summary.MaxBatchOrderableQty,
	}
}

const shopifySyncPageCap = 50

func (s *service) SyncFromShopify(ctx context.Context) error {
	shopifyProductIDs := make(map[string]struct{})

	query := `
		query($cursor: String) {
		  products(first: 50, after: $cursor, sortKey: CREATED_AT, reverse: true) {
			pageInfo { hasNextPage endCursor }
			edges {
			  node {
				id title descriptionHtml status
				images(first: 10) {
				  edges {
					node { id url altText }
				  }
				}
				variants(first: 100) {
				  edges {
					node {
						id title sku price inventoryQuantity image { url }
						inventoryItem { id tracked }
					}
				  }
				}
			  }
			}
		  }
		}
	`

	var cursor *string
	for page := 0; page < shopifySyncPageCap; page++ {
		vars := map[string]interface{}{"cursor": cursor}

		resBytes, err := s.client.QueryAdminGraphQL(ctx, query, vars)
		if err != nil {
			return fmt.Errorf("shopify graphql query failed (page %d): %w", page+1, err)
		}

		var res struct {
			Data struct {
				Products struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Edges []struct {
						Node struct {
							ID              string `json:"id"`
							Title           string `json:"title"`
							DescriptionHtml string `json:"descriptionHtml"`
							Status          string `json:"status"`
							Images          struct {
								Edges []struct {
									Node struct {
										ID      string `json:"id"`
										Url     string `json:"url"`
										AltText string `json:"altText"`
									} `json:"node"`
								} `json:"edges"`
							} `json:"images"`
							Variants struct {
								Edges []struct {
									Node struct {
										ID                string `json:"id"`
										Title             string `json:"title"`
										Sku               string `json:"sku"`
										Price             string `json:"price"`
										InventoryQuantity int    `json:"inventoryQuantity"`
										Image             struct {
											Url string `json:"url"`
										} `json:"image"`
										InventoryItem struct {
											ID      string `json:"id"`
											Tracked bool   `json:"tracked"`
										} `json:"inventoryItem"`
									} `json:"node"`
								} `json:"edges"`
							} `json:"variants"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"products"`
			} `json:"data"`
		}

		if err := json.Unmarshal(resBytes, &res); err != nil {
			return fmt.Errorf("failed to parse shopify response (page %d): %w", page+1, err)
		}

		// Also check for GraphQL errors
		var errRes struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(resBytes, &errRes); err == nil && len(errRes.Errors) > 0 {
			return fmt.Errorf("shopify graphql returned error: %s", errRes.Errors[0].Message)
		}

		for _, pEdge := range res.Data.Products.Edges {
			pNode := pEdge.Node
			shopifyProductIDs[pNode.ID] = struct{}{}
			product := &Product{
				ShopifyID:   pNode.ID,
				Title:       pNode.Title,
				Description: pNode.DescriptionHtml,
				Status:      strings.ToLower(pNode.Status),
			}
			if err := s.store.UpsertProduct(ctx, product); err != nil {
				return fmt.Errorf("failed to upsert product %s: %w", product.ShopifyID, err)
			}

			p, err := s.store.GetProductByShopifyID(ctx, product.ShopifyID)
			if err != nil {
				return fmt.Errorf("failed to reload product %s: %w", product.ShopifyID, err)
			}
			if p == nil {
				slog.WarnContext(ctx, "product not found after upsert, skipping variants",
					slog.String("shopify_id", product.ShopifyID))
				continue
			}

			// Store product images
			var productImages []ProductImage
			for i, iEdge := range pNode.Images.Edges {
				iNode := iEdge.Node
				productImages = append(productImages, ProductImage{
					ProductID: p.ID,
					ShopifyID: iNode.ID,
					Src:       iNode.Url,
					Alt:       iNode.AltText,
					Position:  i + 1,
				})
			}
			if err := s.store.UpsertProductImages(ctx, p.ID, productImages); err != nil {
				slog.ErrorContext(ctx, "failed to upsert product images", slog.String("error", err.Error()))
			}

			syncedVariantIDs := make([]string, 0, len(pNode.Variants.Edges))
			for _, vEdge := range pNode.Variants.Edges {
				vNode := vEdge.Node
				syncedVariantIDs = append(syncedVariantIDs, vNode.ID)
				price, _ := strconv.ParseFloat(vNode.Price, 64)

				fulfillmentType := "ship_ready"
				inventoryTracked := true

				// Weight is managed via CSV packaging import — never overwrite from Shopify.
				// Origin/custom fields (ws_price, custom_link_state, inventory_tracked) are Momiji-owned.
				existing, _ := s.store.GetVariantByShopifyID(ctx, vNode.ID)
				if existing != nil && existing.FulfillmentType != "" {
					fulfillmentType = existing.FulfillmentType
				}
				if existing != nil {
					inventoryTracked = existing.InventoryTracked
				} else {
					inventoryTracked = vNode.InventoryItem.Tracked
				}
				// Custom products awaiting SKU must stay untracked and must not auto-flip to pre_order.
				if existing != nil && existing.CustomLinkState != nil &&
					(*existing.CustomLinkState == CustomLinkStateAwaitingSKU || *existing.CustomLinkState == CustomLinkStateLinked) {
					inventoryTracked = existing.InventoryTracked
				}

				if inventoryTracked && vNode.InventoryQuantity <= 0 && fulfillmentType == "ship_ready" {
					fulfillmentType = "pre_order"
					if existing != nil {
						s.store.UpdateVariantFulfillmentType(ctx, vNode.ID, "pre_order")
					}
				}

				variant := &ProductVariant{
					ProductID:              p.ID,
					ShopifyVariantID:       vNode.ID,
					Title:                  vNode.Title,
					SKU:                    vNode.Sku,
					Price:                  price,
					ImageSrc:               vNode.Image.Url,
					InventoryQuantity:      vNode.InventoryQuantity,
					ShopifyInventoryItemID: vNode.InventoryItem.ID,
					FulfillmentType:        fulfillmentType,
					InventoryTracked:       inventoryTracked,
				}
				if existing != nil {
					variant.CustomLinkState = existing.CustomLinkState
					variant.WSPrice = existing.WSPrice
				}
				if err := s.store.UpsertVariant(ctx, variant); err != nil {
					return fmt.Errorf("failed to upsert variant %s: %w", variant.ShopifyVariantID, err)
				}
			}
			// Drop local variants removed from Shopify (e.g. deleted after add-variant).
			if err := s.store.DeleteVariantsNotIn(ctx, p.ID, syncedVariantIDs); err != nil {
				return fmt.Errorf("failed to reconcile variants for product %s: %w", p.ShopifyID, err)
			}
		}

		if !res.Data.Products.PageInfo.HasNextPage {
			break
		}
		endCursor := res.Data.Products.PageInfo.EndCursor
		cursor = &endCursor

		if page == shopifySyncPageCap-1 {
			slog.WarnContext(ctx, "shopify sync page cap reached, some products may be missing",
				slog.Int("cap", shopifySyncPageCap))
		}
	}

	localProductIDs, err := s.store.ListShopifyProductIDs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list local products for reconciliation: %w", err)
	}
	for _, localID := range localProductIDs {
		if _, exists := shopifyProductIDs[localID]; exists {
			continue
		}
		if err := s.store.MarkProductDeletedByShopifyID(ctx, localID); err != nil {
			return fmt.Errorf("failed to mark deleted product %s: %w", localID, err)
		}
		slog.InfoContext(ctx, "marked product as deleted after sync reconciliation",
			slog.String("shopify_product_id", localID))
	}

	return nil
}

func (s *service) GetProductByID(ctx context.Context, id string) (*ProductDTO, error) {
	p, err := s.store.GetProductByID(ctx, id)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if p == nil {
		return nil, apierror.ErrNotFound
	}
	dto := mapProductToDTO(p, "")
	return &dto, nil
}

func (s *service) GetVariantsByProductID(ctx context.Context, productID string) ([]VariantDTO, error) {
	variants, err := s.store.GetVariantsByProductID(ctx, productID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	dtos := make([]VariantDTO, len(variants))
	variantIDs := make([]string, 0, len(variants))
	for _, v := range variants {
		variantIDs = append(variantIDs, v.ShopifyVariantID)
	}
	summaries, err := s.store.GetBatchSummariesByVariantIDs(ctx, variantIDs)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	for i, v := range variants {
		dtos[i] = *mapVariantToDTO(&v)
		if summary, ok := summaries[v.ShopifyVariantID]; ok {
			dtos[i].BatchSummary = mapVariantBatchSummaryDTO(&summary)
		}
	}
	return dtos, nil
}

func (s *service) UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) (*ProductDTO, error) {
	if fulfillmentType != "ship_ready" && fulfillmentType != "pre_order" && fulfillmentType != "inactive" {
		return nil, apierror.New(400, "validation_error", "fulfillment_type must be ship_ready, pre_order, or inactive")
	}

	if fulfillmentType == "ship_ready" {
		variants, err := s.store.GetVariantsByProductID(ctx, productID)
		if err != nil {
			return nil, apierror.ErrInternal
		}
		for _, v := range variants {
			if v.InventoryTracked && v.InventoryQuantity <= 0 {
				return nil, apierror.New(400, "inventory_error", "Cannot set status to ship_ready. Variant '"+v.Title+"' has 0 inventory.")
			}
		}
	}

	if err := s.store.UpdateProductStatus(ctx, productID, fulfillmentType); err != nil {
		return nil, apierror.ErrInternal
	}
	return s.GetProductByID(ctx, productID)
}

func (s *service) UpdateVariantStatus(ctx context.Context, variantID string, fulfillmentType string) (*VariantDTO, error) {
	if fulfillmentType != "ship_ready" && fulfillmentType != "pre_order" && fulfillmentType != "inactive" {
		return nil, apierror.New(400, "validation_error", "fulfillment_type must be ship_ready, pre_order, or inactive")
	}

	variant, err := s.store.GetVariantByShopifyID(ctx, variantID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if variant == nil {
		return nil, apierror.ErrNotFound
	}

	if fulfillmentType == "ship_ready" && variant.InventoryTracked && variant.InventoryQuantity <= 0 {
		return nil, apierror.New(400, "inventory_error", "Cannot set status to ship_ready. Variant '"+variant.Title+"' has 0 inventory.")
	}

	if err := s.store.UpdateVariantFulfillmentType(ctx, variantID, fulfillmentType); err != nil {
		return nil, apierror.ErrInternal
	}

	return s.GetVariantByID(ctx, variantID)
}

func (s *service) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string, expectedShipDate *string) (*ProductDTO, error) {
	if err := s.store.UpdateVariantBatchLabel(ctx, productID, batchLabel, expectedShipDate); err != nil {
		return nil, apierror.ErrInternal
	}
	return s.GetProductByID(ctx, productID)
}

func (s *service) UpdateVariantBatchLabelByVariantID(ctx context.Context, variantID string, batchLabel string) (*VariantDTO, error) {
	batchLabel = strings.TrimSpace(batchLabel)
	if batchLabel != "" {
		if _, err := s.customTextService.EnsureByLabel(ctx, batchLabel); err != nil {
			return nil, apierror.ErrInternal
		}
	}

	if err := s.store.UpdateSingleVariantBatchLabel(ctx, variantID, batchLabel); err != nil {
		return nil, apierror.ErrInternal
	}
	return s.GetVariantByID(ctx, variantID)
}

func (s *service) UpdateVariantPrice(ctx context.Context, variantID string, wsPrice *float64) error {
	if wsPrice == nil {
		return nil // Nothing to update
	}
	if err := s.store.UpdateVariantPrices(ctx, variantID, wsPrice); err != nil {
		return apierror.ErrInternal
	}
	return nil
}

func (s *service) UpdateVariantLtl(ctx context.Context, variantID string, isLtl bool) (*VariantDTO, error) {
	variant, err := s.store.GetVariantByShopifyID(ctx, variantID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if variant == nil {
		return nil, apierror.ErrNotFound
	}

	if err := s.store.UpdateVariantLtl(ctx, variantID, isLtl); err != nil {
		return nil, apierror.ErrInternal
	}

	return s.GetVariantByID(ctx, variantID)
}

func (s *service) LinkCustomVariantSKU(ctx context.Context, variantID, sku string) (*VariantDTO, error) {
	sku = strings.TrimSpace(sku)
	if sku == "" {
		return nil, apierror.New(400, "validation_error", "SKU is required")
	}

	variant, err := s.store.GetVariantByShopifyID(ctx, variantID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if variant == nil {
		return nil, apierror.ErrNotFound
	}

	product, err := s.store.GetProductByID(ctx, variant.ProductID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if product == nil {
		return nil, apierror.ErrNotFound
	}
	if product.Origin != ProductOriginCustom {
		return nil, apierror.New(400, "validation_error", "Only custom products can be linked")
	}

	if variant.CustomLinkState != nil && *variant.CustomLinkState == CustomLinkStateLinked {
		return s.GetVariantByID(ctx, variantID)
	}
	if variant.CustomLinkState == nil || *variant.CustomLinkState != CustomLinkStateAwaitingSKU {
		return nil, apierror.New(400, "validation_error", "Variant is not awaiting SKU")
	}
	if variant.ShopifyInventoryItemID == "" {
		return nil, apierror.New(400, "validation_error", "Variant is missing Shopify inventory item")
	}

	if err := s.client.LinkVariantSKU(ctx, variant.ShopifyInventoryItemID, sku); err != nil {
		return nil, apierror.New(502, "shopify_error", err.Error())
	}

	if err := s.store.MarkCustomVariantLinked(ctx, variantID, sku); err != nil {
		return nil, apierror.ErrInternal
	}

	return s.GetVariantByID(ctx, variantID)
}

func (s *service) GetAllVariants(ctx context.Context) ([]ProductVariant, error) {
	return s.store.GetAllVariants(ctx)
}

func (s *service) BulkUpdateDimensions(ctx context.Context, rows []DimensionUpdateInput) (BulkUpdateDimensionsResult, error) {
	return s.store.BulkUpdateVariantDimensions(ctx, rows)
}

func (s *service) ValidateVariantActive(ctx context.Context, variantID string) error {
	active, err := s.store.IsVariantFromActiveProduct(ctx, variantID)
	if err != nil {
		return apierror.ErrInternal
	}
	if !active {
		return apierror.New(400, "product_unavailable", "One or more cart items are no longer available for purchase")
	}
	return nil
}

// SyncInventoryQuantities writes live Shopify inventory into local variants.
// When available is 0 and the variant is tracked ship_ready, flips fulfillment to pre_order
// (same rule as the inventory webhook).
func (s *service) SyncInventoryQuantities(ctx context.Context, quantities map[string]int) error {
	if len(quantities) == 0 {
		return nil
	}
	for variantID, qty := range quantities {
		if qty < 0 {
			qty = 0
		}
		variant, err := s.store.GetVariantByShopifyID(ctx, variantID)
		if err != nil {
			return apierror.ErrInternal
		}
		if variant == nil {
			continue
		}
		if err := s.store.UpdateInventoryQuantity(ctx, variantID, qty); err != nil {
			return apierror.ErrInternal
		}
		if qty <= 0 && variant.FulfillmentType == string(FulfillmentTypeShipReady) && variant.InventoryTracked {
			if err := s.store.UpdateVariantFulfillmentType(ctx, variantID, string(FulfillmentTypePreOrder)); err != nil {
				slog.WarnContext(ctx, "failed to update fulfillment type to pre_order after inventory sync",
					slog.String("variant_id", variantID),
					slog.Any("error", err))
			}
		}
	}
	return nil
}
