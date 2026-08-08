package shopify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type AddProductVariantInput struct {
	Title string
	Price string // decimal string, e.g. "12.50"
}

type AddProductVariantsInput struct {
	ProductID        string
	Variants         []AddProductVariantInput
	InventoryTracked bool // when false, disable tracking (custom products)
}

// AddProductVariants appends variants to an existing Shopify product.
// It never updates product status. Supports single-option products only.
// Uses REMOVE_STANDALONE_VARIANT when the product only has a Default Title standalone variant;
// otherwise DEFAULT (append).
func (c *clientImpl) AddProductVariants(ctx context.Context, input AddProductVariantsInput) ([]CreatedVariant, error) {
	productID := strings.TrimSpace(input.ProductID)
	if productID == "" {
		return nil, fmt.Errorf("product id is required")
	}
	if len(input.Variants) == 0 {
		return nil, fmt.Errorf("at least one variant is required")
	}
	if len(input.Variants) > 100 {
		return nil, fmt.Errorf("at most 100 variants are allowed")
	}

	seenTitles := make(map[string]struct{}, len(input.Variants))
	for _, v := range input.Variants {
		title := strings.TrimSpace(v.Title)
		if title == "" {
			return nil, fmt.Errorf("variant title is required")
		}
		key := strings.ToLower(title)
		if _, exists := seenTitles[key]; exists {
			return nil, fmt.Errorf("duplicate variant title: %s", title)
		}
		seenTitles[key] = struct{}{}
	}

	optionName, strategy, err := c.resolveAddVariantsStrategy(ctx, productID, seenTitles)
	if err != nil {
		return nil, err
	}

	bulkVariants := make([]map[string]interface{}, 0, len(input.Variants))
	for _, v := range input.Variants {
		title := strings.TrimSpace(v.Title)
		variantInput := map[string]interface{}{
			"optionValues": []map[string]interface{}{
				{"optionName": optionName, "name": title},
			},
			"price":           v.Price,
			"inventoryPolicy": "CONTINUE",
			"inventoryItem": map[string]interface{}{
				"tracked": input.InventoryTracked,
			},
		}
		bulkVariants = append(bulkVariants, variantInput)
	}

	bulkQuery := `
		mutation productVariantsBulkCreate($productId: ID!, $variants: [ProductVariantsBulkInput!]!, $strategy: ProductVariantsBulkCreateStrategy) {
		  productVariantsBulkCreate(productId: $productId, variants: $variants, strategy: $strategy) {
			productVariants {
			  id
			  title
			  price
			  sku
			  inventoryQuantity
			  inventoryItem { id tracked }
			}
			userErrors { field message }
		  }
		}
	`
	bulkVars := map[string]interface{}{
		"productId": productID,
		"variants":  bulkVariants,
		"strategy":  strategy,
	}

	bulkBytes, err := c.QueryAdminGraphQL(ctx, bulkQuery, bulkVars)
	if err != nil {
		return nil, fmt.Errorf("productVariantsBulkCreate failed: %w", err)
	}

	var bulkRes struct {
		Data struct {
			ProductVariantsBulkCreate struct {
				ProductVariants []struct {
					ID                string `json:"id"`
					Title             string `json:"title"`
					Price             string `json:"price"`
					SKU               string `json:"sku"`
					InventoryQuantity int    `json:"inventoryQuantity"`
					InventoryItem     struct {
						ID      string `json:"id"`
						Tracked bool   `json:"tracked"`
					} `json:"inventoryItem"`
				} `json:"productVariants"`
				UserErrors []userError `json:"userErrors"`
			} `json:"productVariantsBulkCreate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(bulkBytes, &bulkRes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal productVariantsBulkCreate response: %w", err)
	}
	if len(bulkRes.Errors) > 0 {
		return nil, fmt.Errorf("productVariantsBulkCreate error: %s", bulkRes.Errors[0].Message)
	}
	if len(bulkRes.Data.ProductVariantsBulkCreate.UserErrors) > 0 {
		return nil, fmt.Errorf("productVariantsBulkCreate error: %s", formatUserErrors(bulkRes.Data.ProductVariantsBulkCreate.UserErrors))
	}
	if len(bulkRes.Data.ProductVariantsBulkCreate.ProductVariants) == 0 {
		return nil, fmt.Errorf("productVariantsBulkCreate returned no variants")
	}

	created := make([]CreatedVariant, 0, len(bulkRes.Data.ProductVariantsBulkCreate.ProductVariants))
	for _, v := range bulkRes.Data.ProductVariantsBulkCreate.ProductVariants {
		tracked := v.InventoryItem.Tracked
		if !input.InventoryTracked && tracked && v.InventoryItem.ID != "" {
			if err := c.setInventoryItemTracked(ctx, v.InventoryItem.ID, false); err != nil {
				return nil, fmt.Errorf("failed to disable inventory tracking: %w", err)
			}
			tracked = false
		}
		created = append(created, CreatedVariant{
			ID:                   v.ID,
			Title:                v.Title,
			Price:                v.Price,
			SKU:                  v.SKU,
			InventoryItemID:      v.InventoryItem.ID,
			InventoryQuantity:    v.InventoryQuantity,
			InventoryItemTracked: tracked,
		})
	}
	return created, nil
}

// ListProductVariantIDs returns Shopify variant GIDs currently on the product.
func (c *clientImpl) ListProductVariantIDs(ctx context.Context, productID string) ([]string, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return nil, fmt.Errorf("product id is required")
	}
	query := `
		query productVariantIDs($id: ID!) {
		  product(id: $id) {
			variants(first: 100) {
			  nodes { id }
			}
		  }
		}
	`
	resBytes, err := c.QueryAdminGraphQL(ctx, query, map[string]interface{}{"id": productID})
	if err != nil {
		return nil, err
	}
	var res struct {
		Data struct {
			Product *struct {
				Variants struct {
					Nodes []struct {
						ID string `json:"id"`
					} `json:"nodes"`
				} `json:"variants"`
			} `json:"product"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal product variants: %w", err)
	}
	if len(res.Errors) > 0 {
		return nil, fmt.Errorf("shopify product query error: %s", res.Errors[0].Message)
	}
	if res.Data.Product == nil {
		return nil, fmt.Errorf("shopify product not found")
	}
	ids := make([]string, 0, len(res.Data.Product.Variants.Nodes))
	for _, n := range res.Data.Product.Variants.Nodes {
		if n.ID != "" {
			ids = append(ids, n.ID)
		}
	}
	return ids, nil
}

func (c *clientImpl) resolveAddVariantsStrategy(ctx context.Context, productID string, newTitlesLower map[string]struct{}) (optionName string, strategy string, err error) {
	query := `
		query productOptions($id: ID!) {
		  product(id: $id) {
			id
			options { id name values }
			variants(first: 100) {
			  nodes { id title }
			}
		  }
		}
	`
	resBytes, err := c.QueryAdminGraphQL(ctx, query, map[string]interface{}{"id": productID})
	if err != nil {
		return "", "", err
	}

	var res struct {
		Data struct {
			Product *struct {
				ID      string `json:"id"`
				Options []struct {
					ID     string   `json:"id"`
					Name   string   `json:"name"`
					Values []string `json:"values"`
				} `json:"options"`
				Variants struct {
					Nodes []struct {
						ID    string `json:"id"`
						Title string `json:"title"`
					} `json:"nodes"`
				} `json:"variants"`
			} `json:"product"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(resBytes, &res); err != nil {
		return "", "", fmt.Errorf("failed to unmarshal product options: %w", err)
	}
	if len(res.Errors) > 0 {
		return "", "", fmt.Errorf("shopify product query error: %s", res.Errors[0].Message)
	}
	if res.Data.Product == nil {
		return "", "", fmt.Errorf("shopify product not found")
	}

	opts := res.Data.Product.Options
	if len(opts) == 0 {
		return "", "", fmt.Errorf("product has no options")
	}
	if len(opts) > 1 {
		return "", "", fmt.Errorf("multi-option products are not supported; manage variants in Shopify")
	}

	optionName = opts[0].Name
	existingLower := make(map[string]struct{}, len(res.Data.Product.Variants.Nodes))
	for _, v := range res.Data.Product.Variants.Nodes {
		existingLower[strings.ToLower(strings.TrimSpace(v.Title))] = struct{}{}
	}
	for title := range newTitlesLower {
		if _, exists := existingLower[title]; exists {
			return "", "", fmt.Errorf("variant title already exists on product: %s", title)
		}
	}

	strategy = "DEFAULT"
	if len(res.Data.Product.Variants.Nodes) == 1 {
		only := strings.TrimSpace(res.Data.Product.Variants.Nodes[0].Title)
		if strings.EqualFold(only, "Default Title") {
			strategy = "REMOVE_STANDALONE_VARIANT"
		}
	}

	return optionName, strategy, nil
}
