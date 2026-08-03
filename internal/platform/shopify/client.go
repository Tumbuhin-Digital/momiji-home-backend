package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

type Client interface {
	QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error)
	CreateDraftOrder(ctx context.Context, input DraftOrderInput) (*DraftOrderResponse, error)
	DeleteDraftOrder(ctx context.Context, draftOrderID string) error
	SendDraftOrderInvoice(ctx context.Context, draftOrderID string, email *DraftOrderInvoiceEmailInput) error
	CreateStorefrontCart(ctx context.Context, input CartCreateInput) (*CartCreateResponse, error)
	CreateRefund(ctx context.Context, shopifyOrderID string, amount float64, currency string, reason string) error
	GetVariantsInventory(ctx context.Context, variantIDs []string) (map[string]int, error)
	CreateFulfillment(ctx context.Context, shopifyOrderID string) error
	FetchFulfillmentOrders(ctx context.Context, shopifyOrderID string) ([]FulfillmentOrderData, error)
	CreateFulfillmentV2(ctx context.Context, input CreateFulfillmentV2Input) (*CreateFulfillmentV2Result, error)
	CreateFulfillmentEvent(ctx context.Context, shopifyFulfillmentID, status string) error
}

type DraftOrderInvoiceEmailInput struct {
	To            string `json:"to,omitempty"`
	Subject       string `json:"subject,omitempty"`
	CustomMessage string `json:"customMessage,omitempty"`
}

type clientImpl struct {
	StoreDomain     string
	AdminToken      string
	StorefrontToken string
	HTTPClient      *http.Client
}

func NewClient(storeDomain, adminToken, storefrontToken string) Client {
	return &clientImpl{
		StoreDomain:     storeDomain,
		AdminToken:      adminToken,
		StorefrontToken: storefrontToken,
		HTTPClient:      &http.Client{},
	}
}

func (c *clientImpl) QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	url := fmt.Sprintf("https://%s/admin/api/2025-01/graphql.json", c.StoreDomain)

	payload := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", c.AdminToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("shopify admin graphql error: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

type DraftOrderInput struct {
	LineItems        []DraftOrderLineItem `json:"lineItems"`
	Email            string               `json:"email,omitempty"`
	CustomerID       string               `json:"customerId,omitempty"`
	ShippingAddress  *AddressInput        `json:"shippingAddress,omitempty"`
	BillingAddress   *AddressInput        `json:"billingAddress,omitempty"`
	ShippingLine     *ShippingLineInput   `json:"shippingLine,omitempty"`
	CustomAttributes []AttributeInput     `json:"customAttributes,omitempty"`
}

type MoneyInput struct {
	Amount       string `json:"amount"`
	CurrencyCode string `json:"currencyCode"`
}

type ShippingLineInput struct {
	Price              string      `json:"price,omitempty"`
	PriceWithCurrency  *MoneyInput `json:"priceWithCurrency,omitempty"`
	ShippingRateHandle string      `json:"shippingRateHandle,omitempty"`
	Title              string      `json:"title"`
}

func NewShippingLineInput(title, price, currencyCode string) *ShippingLineInput {
	if currencyCode == "" {
		currencyCode = "USD"
	}
	return &ShippingLineInput{
		Title: title,
		Price: price,
		PriceWithCurrency: &MoneyInput{
			Amount:       price,
			CurrencyCode: currencyCode,
		},
	}
}

type DraftOrderLineItem struct {
	VariantID         string                          `json:"variantId,omitempty"`
	Title             string                          `json:"title,omitempty"`
	OriginalUnitPrice string                          `json:"originalUnitPrice,omitempty"`
	PriceOverride     *MoneyInput                     `json:"priceOverride,omitempty"`
	Quantity          int                             `json:"quantity"`
	RequiresShipping  *bool                           `json:"requiresShipping,omitempty"`
	CustomAttributes  []AttributeInput                `json:"customAttributes,omitempty"`
	Weight            *DraftOrderLineItemWeightInput  `json:"weight,omitempty"`
	AppliedDiscount   *DraftOrderAppliedDiscountInput `json:"appliedDiscount,omitempty"`
}

type DraftOrderAppliedDiscountInput struct {
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Value       float64 `json:"value"`
	ValueType   string  `json:"valueType"`
	Amount      float64 `json:"amount"`
}

type DraftOrderLineItemWeightInput struct {
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
}

type DraftOrderResponse struct {
	ID         string `json:"id"`
	InvoiceUrl string `json:"invoiceUrl"`
}

func (c *clientImpl) CreateDraftOrder(ctx context.Context, input DraftOrderInput) (*DraftOrderResponse, error) {
	query := `
		mutation draftOrderCreate($input: DraftOrderInput!) {
		  draftOrderCreate(input: $input) {
			draftOrder {
			  id
			  invoiceUrl
			}
			userErrors {
			  field
			  message
			}
		  }
		}
	`
	vars := map[string]interface{}{"input": input}

	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data struct {
			DraftOrderCreate struct {
				DraftOrder *DraftOrderResponse `json:"draftOrder"`
				UserErrors []struct {
					Message string `json:"message"`
				} `json:"userErrors"`
			} `json:"draftOrderCreate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal shopify response (body: %s): %w", string(resBytes), err)
	}
	if len(res.Data.DraftOrderCreate.UserErrors) > 0 {
		return nil, fmt.Errorf("shopify draft order error: %s", res.Data.DraftOrderCreate.UserErrors[0].Message)
	}
	if res.Data.DraftOrderCreate.DraftOrder == nil {
		return nil, fmt.Errorf("failed to create draft order, raw response: %s", string(resBytes))
	}

	return res.Data.DraftOrderCreate.DraftOrder, nil
}

func (c *clientImpl) DeleteDraftOrder(ctx context.Context, draftOrderID string) error {
	if strings.TrimSpace(draftOrderID) == "" {
		return nil
	}

	query := `
		mutation draftOrderDelete($input: DraftOrderDeleteInput!) {
		  draftOrderDelete(input: $input) {
			deletedId
			userErrors {
			  field
			  message
			}
		  }
		}
	`
	vars := map[string]interface{}{
		"input": map[string]interface{}{
			"id": draftOrderID,
		},
	}

	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return err
	}

	var res struct {
		Data struct {
			DraftOrderDelete struct {
				DeletedID  *string `json:"deletedId"`
				UserErrors []struct {
					Message string `json:"message"`
				} `json:"userErrors"`
			} `json:"draftOrderDelete"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return fmt.Errorf("failed to unmarshal shopify draftOrderDelete response (body: %s): %w", string(resBytes), err)
	}

	// Treat missing / already-deleted drafts as success so release stays best-effort.
	for _, e := range res.Errors {
		msg := strings.ToLower(e.Message)
		if strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") {
			return nil
		}
		return fmt.Errorf("shopify draftOrderDelete error: %s", e.Message)
	}
	for _, e := range res.Data.DraftOrderDelete.UserErrors {
		msg := strings.ToLower(e.Message)
		if strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") {
			return nil
		}
		return fmt.Errorf("shopify draftOrderDelete user error: %s", e.Message)
	}

	return nil
}

func (c *clientImpl) SendDraftOrderInvoice(ctx context.Context, draftOrderID string, email *DraftOrderInvoiceEmailInput) error {
	query := `
		mutation draftOrderInvoiceSend($id: ID!, $email: EmailInput) {
		  draftOrderInvoiceSend(id: $id, email: $email) {
			draftOrder {
			  id
			}
			userErrors {
			  field
			  message
			}
		  }
		}
	`
	vars := map[string]interface{}{"id": draftOrderID}
	if email != nil {
		vars["email"] = email
	}

	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return err
	}

	var res struct {
		Data struct {
			DraftOrderInvoiceSend struct {
				DraftOrder *struct {
					ID string `json:"id"`
				} `json:"draftOrder"`
				UserErrors []struct {
					Message string `json:"message"`
				} `json:"userErrors"`
			} `json:"draftOrderInvoiceSend"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return fmt.Errorf("failed to unmarshal shopify invoice send response (body: %s): %w", string(resBytes), err)
	}
	if len(res.Data.DraftOrderInvoiceSend.UserErrors) > 0 {
		return fmt.Errorf("shopify draft order invoice send error: %s", res.Data.DraftOrderInvoiceSend.UserErrors[0].Message)
	}
	return nil
}

type RefundTransaction struct {
	Kind    string `json:"kind"`
	Gateway string `json:"gateway"`
	Amount  string `json:"amount"`
}

type RefundInput struct {
	Currency     string              `json:"currency"`
	Note         string              `json:"note"`
	Transactions []RefundTransaction `json:"transactions"`
}

type RefundPayload struct {
	Refund RefundInput `json:"refund"`
}

func (c *clientImpl) CreateRefund(ctx context.Context, shopifyOrderID string, amount float64, currency string, reason string) error {
	// The PRD mentions REST API for Refund since GraphQL doesn't fully support gateway transactions for refund simply
	url := fmt.Sprintf("https://%s/admin/api/2024-01/orders/%s/refunds.json", c.StoreDomain, shopifyOrderID)

	amountStr := fmt.Sprintf("%.2f", amount)
	payload := RefundPayload{
		Refund: RefundInput{
			Currency: currency,
			Note:     reason,
			Transactions: []RefundTransaction{
				{
					Kind:    "refund",
					Gateway: "shopify_payments",
					Amount:  amountStr,
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", c.AdminToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		resBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("shopify refund error: status %d body %s", resp.StatusCode, string(resBody))
	}

	return nil
}

type CartCreateInput struct {
	Lines         []CartLineInput         `json:"lines"`
	BuyerIdentity *CartBuyerIdentityInput `json:"buyerIdentity,omitempty"`
}

type CartLineInput struct {
	MerchandiseId string           `json:"merchandiseId"`
	Quantity      int              `json:"quantity"`
	Attributes    []AttributeInput `json:"attributes,omitempty"`
}

type AttributeInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var WholesaleSourceAttribute = AttributeInput{Key: "source", Value: "wholesale"}

type CartBuyerIdentityInput struct {
	Email                      string                     `json:"email,omitempty"`
	Phone                      string                     `json:"phone,omitempty"`
	DeliveryAddressPreferences []CartDeliveryAddressInput `json:"deliveryAddressPreferences,omitempty"`
}

type CartDeliveryAddressInput struct {
	DeliveryAddress AddressInput `json:"deliveryAddress"`
}

type AddressInput struct {
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Company   string `json:"company,omitempty"`
	Address1  string `json:"address1,omitempty"`
	City      string `json:"city,omitempty"`
	Province  string `json:"province,omitempty"`
	Zip       string `json:"zip,omitempty"`
	Country   string `json:"country,omitempty"`
	Phone     string `json:"phone,omitempty"`
}

type CartCreateResponse struct {
	ID          string `json:"id"`
	CheckoutUrl string `json:"checkoutUrl"`
}

func (c *clientImpl) QueryStorefrontGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	url := fmt.Sprintf("https://%s/api/2024-01/graphql.json", c.StoreDomain)

	payload := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Storefront-Access-Token", c.StorefrontToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("shopify storefront graphql error: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (c *clientImpl) CreateStorefrontCart(ctx context.Context, input CartCreateInput) (*CartCreateResponse, error) {
	query := `
		mutation cartCreate($input: CartInput!) {
		  cartCreate(input: $input) {
			cart {
			  id
			  checkoutUrl
			}
			userErrors {
			  message
			}
		  }
		}
	`
	vars := map[string]interface{}{"input": input}

	resBytes, err := c.QueryStorefrontGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data struct {
			CartCreate struct {
				Cart       *CartCreateResponse `json:"cart"`
				UserErrors []struct {
					Message string `json:"message"`
				} `json:"userErrors"`
			} `json:"cartCreate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, err
	}
	if len(res.Data.CartCreate.UserErrors) > 0 {
		return nil, fmt.Errorf("shopify cart error: %s", res.Data.CartCreate.UserErrors[0].Message)
	}
	if res.Data.CartCreate.Cart == nil {
		return nil, fmt.Errorf("failed to create cart")
	}

	return res.Data.CartCreate.Cart, nil
}

func (c *clientImpl) GetVariantsInventory(ctx context.Context, variantIDs []string) (map[string]int, error) {
	if len(variantIDs) == 0 {
		return make(map[string]int), nil
	}

	query := `
		query getVariantsInventory($ids: [ID!]!) {
		  nodes(ids: $ids) {
			... on ProductVariant {
			  id
			  inventoryQuantity
			}
		  }
		}
	`
	vars := map[string]interface{}{"ids": variantIDs}

	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data struct {
			Nodes []struct {
				ID                string `json:"id"`
				InventoryQuantity int    `json:"inventoryQuantity"`
			} `json:"nodes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, err
	}

	inventoryMap := make(map[string]int)
	for _, node := range res.Data.Nodes {
		if node.ID != "" {
			inventoryMap[node.ID] = node.InventoryQuantity
		}
	}

	return inventoryMap, nil
}

func (c *clientImpl) CreateFulfillment(ctx context.Context, shopifyOrderID string) error {
	// Ensure global ID format
	if !strings.HasPrefix(shopifyOrderID, "gid://") {
		shopifyOrderID = "gid://shopify/Order/" + shopifyOrderID
	}

	// Step 1: Get the fulfillment order IDs
	query := `
		query($orderId: ID!) {
		  order(id: $orderId) {
			fulfillmentOrders(first: 10) {
			  edges {
				node {
				  id
				  status
				}
			  }
			}
		  }
		}
	`
	vars := map[string]interface{}{"orderId": shopifyOrderID}

	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return err
	}

	var res struct {
		Data struct {
			Order struct {
				FulfillmentOrders struct {
					Edges []struct {
						Node struct {
							ID     string `json:"id"`
							Status string `json:"status"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"fulfillmentOrders"`
			} `json:"order"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return fmt.Errorf("failed to parse fulfillment orders: %w", err)
	}

	if len(res.Data.Order.FulfillmentOrders.Edges) == 0 {
		return fmt.Errorf("no fulfillment orders found for order %s. raw response: %s", shopifyOrderID, string(resBytes))
	}

	// Step 2: Create fulfillment for OPEN fulfillment orders
	for _, edge := range res.Data.Order.FulfillmentOrders.Edges {
		slog.Info("Shopify FulfillmentOrder found", "id", edge.Node.ID, "status", edge.Node.Status)
		if edge.Node.Status == "OPEN" {
			mut := `
				mutation fulfillmentCreate($fulfillment: FulfillmentInput!) {
				  fulfillmentCreate(fulfillment: $fulfillment) {
					fulfillment {
					  id
					}
					userErrors {
					  message
					}
				  }
				}
			`
			mutVars := map[string]interface{}{
				"fulfillment": map[string]interface{}{
					"lineItemsByFulfillmentOrder": []map[string]interface{}{
						{
							"fulfillmentOrderId": edge.Node.ID,
						},
					},
				},
			}

			mutRes, mutErr := c.QueryAdminGraphQL(ctx, mut, mutVars)
			if mutErr != nil {
				return mutErr
			}

			var mRes struct {
				Data struct {
					FulfillmentCreate struct {
						UserErrors []struct {
							Message string `json:"message"`
						} `json:"userErrors"`
					} `json:"fulfillmentCreate"`
				} `json:"data"`
			}
			if err := json.Unmarshal(mutRes, &mRes); err == nil {
				if len(mRes.Data.FulfillmentCreate.UserErrors) > 0 {
					return fmt.Errorf("shopify fulfillment error: %s", mRes.Data.FulfillmentCreate.UserErrors[0].Message)
				}
			}
		}
	}

	return nil
}
