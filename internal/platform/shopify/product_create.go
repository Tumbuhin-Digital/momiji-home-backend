package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
)

type CreateUnlistedProductVariantInput struct {
	Title string
	Price string // decimal string, e.g. "12.50"
}

type CreateUnlistedProductInput struct {
	Title    string
	Variants []CreateUnlistedProductVariantInput
}

type CreatedVariant struct {
	ID                   string
	Title                string
	Price                string
	SKU                  string
	InventoryItemID      string
	InventoryQuantity    int
	InventoryItemTracked bool
}

type CreatedProductMedia struct {
	ID  string
	URL string
	Alt string
}

type CreatedProduct struct {
	ID       string
	Title    string
	Status   string
	Handle   string
	Variants []CreatedVariant
	Media    *CreatedProductMedia
}

type userError struct {
	Field   []string `json:"field"`
	Message string   `json:"message"`
}

func formatUserErrors(errs []userError) string {
	if len(errs) == 0 {
		return "unknown shopify user error"
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, "; ")
}

// CreateUnlistedProduct creates an UNLISTED Shopify product with the given variants.
// It uses productCreate + productVariantsBulkCreate (REMOVE_STANDALONE_VARIANT),
// with inventoryPolicy CONTINUE and inventory tracking disabled on every variant.
func (c *clientImpl) CreateUnlistedProduct(ctx context.Context, input CreateUnlistedProductInput) (*CreatedProduct, error) {
	if strings.TrimSpace(input.Title) == "" {
		return nil, fmt.Errorf("product title is required")
	}
	if len(input.Variants) == 0 {
		return nil, fmt.Errorf("at least one variant is required")
	}
	if len(input.Variants) > 100 {
		return nil, fmt.Errorf("at most 100 variants are allowed")
	}

	optionValues := make([]map[string]interface{}, 0, len(input.Variants))
	seenTitles := make(map[string]struct{}, len(input.Variants))
	for _, v := range input.Variants {
		title := strings.TrimSpace(v.Title)
		if title == "" {
			return nil, fmt.Errorf("variant title is required")
		}
		if _, exists := seenTitles[strings.ToLower(title)]; exists {
			return nil, fmt.Errorf("duplicate variant title: %s", title)
		}
		seenTitles[strings.ToLower(title)] = struct{}{}
		optionValues = append(optionValues, map[string]interface{}{"name": title})
	}

	createQuery := `
		mutation productCreate($product: ProductCreateInput!) {
		  productCreate(product: $product) {
			product {
			  id
			  title
			  status
			  handle
			}
			userErrors { field message }
		  }
		}
	`
	createVars := map[string]interface{}{
		"product": map[string]interface{}{
			"title":  input.Title,
			"status": "UNLISTED",
			"productOptions": []map[string]interface{}{
				{
					"name":   "Title",
					"values": optionValues, // all values; standalone default variant replaced in bulk create
				},
			},
		},
	}

	createBytes, err := c.QueryAdminGraphQL(ctx, createQuery, createVars)
	if err != nil {
		return nil, err
	}

	var createRes struct {
		Data struct {
			ProductCreate struct {
				Product *struct {
					ID     string `json:"id"`
					Title  string `json:"title"`
					Status string `json:"status"`
					Handle string `json:"handle"`
				} `json:"product"`
				UserErrors []userError `json:"userErrors"`
			} `json:"productCreate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(createBytes, &createRes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal productCreate response: %w", err)
	}
	if len(createRes.Errors) > 0 {
		return nil, fmt.Errorf("shopify productCreate error: %s", createRes.Errors[0].Message)
	}
	if len(createRes.Data.ProductCreate.UserErrors) > 0 {
		return nil, fmt.Errorf("shopify productCreate error: %s", formatUserErrors(createRes.Data.ProductCreate.UserErrors))
	}
	if createRes.Data.ProductCreate.Product == nil {
		return nil, fmt.Errorf("shopify productCreate returned no product: %s", string(createBytes))
	}

	productID := createRes.Data.ProductCreate.Product.ID
	status := strings.ToLower(createRes.Data.ProductCreate.Product.Status)

	bulkVariants := make([]map[string]interface{}, 0, len(input.Variants))
	for _, v := range input.Variants {
		title := strings.TrimSpace(v.Title)
		bulkVariants = append(bulkVariants, map[string]interface{}{
			"optionValues": []map[string]interface{}{
				{"optionName": "Title", "name": title},
			},
			"price":           v.Price,
			"inventoryPolicy": "CONTINUE",
			"inventoryItem": map[string]interface{}{
				"tracked": false,
			},
		})
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
		"strategy":  "REMOVE_STANDALONE_VARIANT",
	}

	bulkBytes, err := c.QueryAdminGraphQL(ctx, bulkQuery, bulkVars)
	if err != nil {
		return nil, fmt.Errorf("product created (%s) but variants failed: %w", productID, err)
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
		return nil, fmt.Errorf("product created (%s) but variants failed: %s", productID, bulkRes.Errors[0].Message)
	}
	if len(bulkRes.Data.ProductVariantsBulkCreate.UserErrors) > 0 {
		return nil, fmt.Errorf("product created (%s) but variants failed: %s", productID, formatUserErrors(bulkRes.Data.ProductVariantsBulkCreate.UserErrors))
	}
	if len(bulkRes.Data.ProductVariantsBulkCreate.ProductVariants) == 0 {
		return nil, fmt.Errorf("product created (%s) but no variants returned", productID)
	}

	// Ensure tracking is off even if bulk create ignored inventoryItem.tracked.
	for _, v := range bulkRes.Data.ProductVariantsBulkCreate.ProductVariants {
		if v.InventoryItem.ID == "" || !v.InventoryItem.Tracked {
			continue
		}
		if err := c.setInventoryItemTracked(ctx, v.InventoryItem.ID, false); err != nil {
			return nil, fmt.Errorf("product created (%s) but failed to disable inventory tracking: %w", productID, err)
		}
	}

	created := &CreatedProduct{
		ID:     productID,
		Title:  createRes.Data.ProductCreate.Product.Title,
		Status: status,
		Handle: createRes.Data.ProductCreate.Product.Handle,
	}
	for _, v := range bulkRes.Data.ProductVariantsBulkCreate.ProductVariants {
		created.Variants = append(created.Variants, CreatedVariant{
			ID:                   v.ID,
			Title:                v.Title,
			Price:                v.Price,
			SKU:                  v.SKU,
			InventoryItemID:      v.InventoryItem.ID,
			InventoryQuantity:    v.InventoryQuantity,
			InventoryItemTracked: false,
		})
	}
	return created, nil
}

func (c *clientImpl) setInventoryItemTracked(ctx context.Context, inventoryItemID string, tracked bool) error {
	return c.inventoryItemUpdate(ctx, inventoryItemID, map[string]interface{}{
		"tracked": tracked,
	})
}

// LinkVariantSKU sets the inventory item SKU and enables inventory tracking.
func (c *clientImpl) LinkVariantSKU(ctx context.Context, inventoryItemID, sku string) error {
	sku = strings.TrimSpace(sku)
	if inventoryItemID == "" {
		return fmt.Errorf("inventory item id is required")
	}
	if sku == "" {
		return fmt.Errorf("sku is required")
	}
	return c.inventoryItemUpdate(ctx, inventoryItemID, map[string]interface{}{
		"sku":     sku,
		"tracked": true,
	})
}

func (c *clientImpl) inventoryItemUpdate(ctx context.Context, inventoryItemID string, input map[string]interface{}) error {
	query := `
		mutation inventoryItemUpdate($id: ID!, $input: InventoryItemInput!) {
		  inventoryItemUpdate(id: $id, input: $input) {
			inventoryItem { id tracked sku }
			userErrors { field message }
		  }
		}
	`
	vars := map[string]interface{}{
		"id":    inventoryItemID,
		"input": input,
	}
	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return err
	}
	var res struct {
		Data struct {
			InventoryItemUpdate struct {
				UserErrors []userError `json:"userErrors"`
			} `json:"inventoryItemUpdate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(resBytes, &res); err != nil {
		return fmt.Errorf("failed to unmarshal inventoryItemUpdate response: %w", err)
	}
	if len(res.Errors) > 0 {
		return fmt.Errorf("shopify inventoryItemUpdate error: %s", res.Errors[0].Message)
	}
	if len(res.Data.InventoryItemUpdate.UserErrors) > 0 {
		return fmt.Errorf("shopify inventoryItemUpdate error: %s", formatUserErrors(res.Data.InventoryItemUpdate.UserErrors))
	}
	return nil
}

func (c *clientImpl) AttachProductMediaFromURL(ctx context.Context, productID, imageURL, alt string) (*CreatedProductMedia, error) {
	if strings.TrimSpace(imageURL) == "" {
		return nil, nil
	}
	return c.productCreateMedia(ctx, productID, imageURL, alt)
}

func (c *clientImpl) AttachProductMediaFromBytes(ctx context.Context, productID, filename, contentType string, data []byte, alt string) (*CreatedProductMedia, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if filename == "" {
		filename = "reference.jpg"
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}

	resourceURL, err := c.stagedUploadBytes(ctx, filename, contentType, data)
	if err != nil {
		return nil, err
	}
	return c.productCreateMedia(ctx, productID, resourceURL, alt)
}

func (c *clientImpl) productCreateMedia(ctx context.Context, productID, originalSource, alt string) (*CreatedProductMedia, error) {
	query := `
		mutation productCreateMedia($productId: ID!, $media: [CreateMediaInput!]!) {
		  productCreateMedia(productId: $productId, media: $media) {
			media {
			  ... on MediaImage {
				id
				alt
				image { url }
			  }
			}
			mediaUserErrors { field message }
		  }
		}
	`
	mediaInput := map[string]interface{}{
		"originalSource":   originalSource,
		"mediaContentType": "IMAGE",
	}
	if strings.TrimSpace(alt) != "" {
		mediaInput["alt"] = alt
	}
	vars := map[string]interface{}{
		"productId": productID,
		"media":     []map[string]interface{}{mediaInput},
	}
	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data struct {
			ProductCreateMedia struct {
				Media []struct {
					ID    string `json:"id"`
					Alt   string `json:"alt"`
					Image *struct {
						URL string `json:"url"`
					} `json:"image"`
				} `json:"media"`
				MediaUserErrors []userError `json:"mediaUserErrors"`
			} `json:"productCreateMedia"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal productCreateMedia response: %w", err)
	}
	if len(res.Errors) > 0 {
		return nil, fmt.Errorf("shopify productCreateMedia error: %s", res.Errors[0].Message)
	}
	if len(res.Data.ProductCreateMedia.MediaUserErrors) > 0 {
		return nil, fmt.Errorf("shopify productCreateMedia error: %s", formatUserErrors(res.Data.ProductCreateMedia.MediaUserErrors))
	}
	if len(res.Data.ProductCreateMedia.Media) == 0 {
		return nil, fmt.Errorf("shopify productCreateMedia returned no media")
	}
	m := res.Data.ProductCreateMedia.Media[0]
	url := ""
	if m.Image != nil {
		url = m.Image.URL
	}
	return &CreatedProductMedia{ID: m.ID, URL: url, Alt: m.Alt}, nil
}

func (c *clientImpl) stagedUploadBytes(ctx context.Context, filename, contentType string, data []byte) (string, error) {
	query := `
		mutation stagedUploadsCreate($input: [StagedUploadInput!]!) {
		  stagedUploadsCreate(input: $input) {
			stagedTargets {
			  url
			  resourceUrl
			  parameters { name value }
			}
			userErrors { field message }
		  }
		}
	`
	vars := map[string]interface{}{
		"input": []map[string]interface{}{
			{
				"filename":   filepath.Base(filename),
				"mimeType":   contentType,
				"httpMethod": "POST",
				"resource":   "PRODUCT_IMAGE",
				"fileSize":   fmt.Sprintf("%d", len(data)),
			},
		},
	}
	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return "", err
	}
	var res struct {
		Data struct {
			StagedUploadsCreate struct {
				StagedTargets []struct {
					URL         string `json:"url"`
					ResourceURL string `json:"resourceUrl"`
					Parameters  []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"parameters"`
				} `json:"stagedTargets"`
				UserErrors []userError `json:"userErrors"`
			} `json:"stagedUploadsCreate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(resBytes, &res); err != nil {
		return "", fmt.Errorf("failed to unmarshal stagedUploadsCreate response: %w", err)
	}
	if len(res.Errors) > 0 {
		return "", fmt.Errorf("shopify stagedUploadsCreate error: %s", res.Errors[0].Message)
	}
	if len(res.Data.StagedUploadsCreate.UserErrors) > 0 {
		return "", fmt.Errorf("shopify stagedUploadsCreate error: %s", formatUserErrors(res.Data.StagedUploadsCreate.UserErrors))
	}
	if len(res.Data.StagedUploadsCreate.StagedTargets) == 0 {
		return "", fmt.Errorf("shopify stagedUploadsCreate returned no targets")
	}
	target := res.Data.StagedUploadsCreate.StagedTargets[0]

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, p := range target.Parameters {
		if err := writer.WriteField(p.Name, p.Value); err != nil {
			return "", err
		}
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(filename)))
	h.Set("Content-Type", contentType)
	part, err := writer.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("staged upload failed: status %d body %s", resp.StatusCode, string(respBody))
	}
	return target.ResourceURL, nil
}
