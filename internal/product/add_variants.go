package product

import (
	"context"
	"fmt"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type AddProductVariantsInput struct {
	ProductID      string
	IdempotencyKey string
	Variants       []CreateCustomVariantInput
}

func (s *service) AddProductVariants(ctx context.Context, input AddProductVariantsInput) (*ProductDTO, error) {
	productID := strings.TrimSpace(input.ProductID)
	if productID == "" {
		return nil, apierror.New(400, "validation_error", "Product id is required")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, apierror.New(400, "validation_error", "Idempotency key is required")
	}
	if len(input.Variants) == 0 {
		return nil, apierror.New(400, "validation_error", "At least one variant is required")
	}
	if len(input.Variants) > maxCustomVariants {
		return nil, apierror.New(400, "validation_error", fmt.Sprintf("At most %d variants are allowed", maxCustomVariants))
	}

	product, err := s.store.GetProductByID(ctx, productID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if product == nil {
		return nil, apierror.ErrNotFound
	}
	if strings.HasPrefix(product.ShopifyID, "pending:") {
		return nil, apierror.New(409, "create_in_progress", "Product is still being created")
	}

	existingVariants := product.Variants
	if len(existingVariants) == 0 {
		vs, vErr := s.store.GetVariantsByProductID(ctx, product.ID)
		if vErr != nil {
			return nil, apierror.ErrInternal
		}
		existingVariants = vs
	}

	if len(existingVariants)+len(input.Variants) > maxCustomVariants {
		return nil, apierror.New(400, "validation_error", fmt.Sprintf("Product would exceed %d variants", maxCustomVariants))
	}

	existingTitles := make(map[string]struct{}, len(existingVariants))
	for _, ev := range existingVariants {
		existingTitles[strings.ToLower(strings.TrimSpace(ev.Title))] = struct{}{}
	}

	seenTitles := make(map[string]struct{}, len(input.Variants))
	shopifyVariants := make([]shopify.AddProductVariantInput, 0, len(input.Variants))
	wsByTitle := make(map[string]float64, len(input.Variants))
	rppByTitle := make(map[string]float64, len(input.Variants))

	for i, v := range input.Variants {
		vt := strings.TrimSpace(v.Title)
		if vt == "" {
			return nil, apierror.New(400, "validation_error", fmt.Sprintf("Variant %d title is required", i+1))
		}
		key := strings.ToLower(vt)
		if _, ok := seenTitles[key]; ok {
			return nil, apierror.New(400, "validation_error", "Duplicate variant title: "+vt)
		}
		if _, ok := existingTitles[key]; ok {
			return nil, apierror.New(400, "validation_error", "Variant title already exists: "+vt)
		}
		seenTitles[key] = struct{}{}
		if v.RPPPrice < 0 || v.WSPrice < 0 {
			return nil, apierror.New(400, "validation_error", "Prices must be >= 0")
		}
		shopifyVariants = append(shopifyVariants, shopify.AddProductVariantInput{
			Title: vt,
			Price: fmt.Sprintf("%.2f", v.RPPPrice),
		})
		wsByTitle[key] = v.WSPrice
		rppByTitle[key] = v.RPPPrice
	}

	isCustom := strings.EqualFold(product.Origin, ProductOriginCustom)
	created, err := s.client.AddProductVariants(ctx, shopify.AddProductVariantsInput{
		ProductID:        product.ShopifyID,
		Variants:         shopifyVariants,
		InventoryTracked: !isCustom,
	})
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "multi-option") {
			return nil, apierror.New(400, "validation_error", msg)
		}
		if strings.Contains(msg, "already exists") {
			return nil, apierror.New(400, "validation_error", msg)
		}
		return nil, apierror.New(502, "shopify_error", "Failed to add Shopify variants: "+msg)
	}

	for _, cv := range created {
		titleKey := strings.ToLower(strings.TrimSpace(cv.Title))
		ws := wsByTitle[titleKey]
		price := rppByTitle[titleKey]
		if parsed, parseErr := parsePrice(cv.Price); parseErr == nil {
			price = parsed
		}
		wsCopy := ws

		variant := &ProductVariant{
			ProductID:              product.ID,
			ShopifyVariantID:       cv.ID,
			Title:                  cv.Title,
			SKU:                    "",
			Price:                  price,
			WSPrice:                &wsCopy,
			FulfillmentType:        string(FulfillmentTypePreOrder),
			InventoryQuantity:      cv.InventoryQuantity,
			ShopifyInventoryItemID: cv.InventoryItemID,
			InventoryTracked:       cv.InventoryItemTracked,
		}
		if isCustom {
			awaiting := CustomLinkStateAwaitingSKU
			variant.CustomLinkState = &awaiting
			variant.InventoryTracked = false
			variant.SKU = ""
		}

		if err := s.store.UpsertVariant(ctx, variant); err != nil {
			return nil, apierror.New(500, "persist_failed", "Shopify variants created but failed to persist locally")
		}
	}

	// REMOVE_STANDALONE_VARIANT deleted Default Title on Shopify — drop the local row.
	if len(existingVariants) == 1 && strings.EqualFold(strings.TrimSpace(existingVariants[0].Title), "Default Title") {
		_ = s.store.DeleteVariantByShopifyID(ctx, existingVariants[0].ShopifyVariantID)
	}

	persisted, err := s.store.GetProductByID(ctx, product.ID)
	if err != nil || persisted == nil {
		return nil, apierror.ErrInternal
	}
	dto := mapProductToDTO(persisted, "")
	return &dto, nil
}
