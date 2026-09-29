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
	ListOpenDraftOrders(ctx context.Context) ([]OpenDraftOrder, error)
	GetDraftOrder(ctx context.Context, id string) (*DraftOrderDetail, error)
	UpdateDraftOrder(ctx context.Context, id string, input DraftOrderInput, clearShippingLine bool) (*DraftOrderResponse, error)
	DeleteDraftOrder(ctx context.Context, draftOrderID string) error
	SendDraftOrderInvoice(ctx context.Context, draftOrderID string, email *DraftOrderInvoiceEmailInput) error
	CreateStorefrontCart(ctx context.Context, input CartCreateInput) (*CartCreateResponse, error)
	CreateRefund(ctx context.Context, shopifyOrderID string, amount float64, currency string, reason string) error
	GetVariantsInventory(ctx context.Context, variantIDs []string) (map[string]int, error)
	CreateFulfillment(ctx context.Context, shopifyOrderID string) error
	FetchFulfillmentOrders(ctx context.Context, shopifyOrderID string) ([]FulfillmentOrderData, error)
	CreateFulfillmentV2(ctx context.Context, input CreateFulfillmentV2Input) (*CreateFulfillmentV2Result, error)
	CreateFulfillmentEvent(ctx context.Context, shopifyFulfillmentID, status string) error
	CreateUnlistedProduct(ctx context.Context, input CreateUnlistedProductInput) (*CreatedProduct, error)
	AddProductVariants(ctx context.Context, input AddProductVariantsInput) ([]CreatedVariant, error)
	ListProductVariantIDs(ctx context.Context, productID string) ([]string, error)
	AttachProductMediaFromURL(ctx context.Context, productID, imageURL, alt string) (*CreatedProductMedia, error)
	AttachProductMediaFromBytes(ctx context.Context, productID string, filename, contentType string, data []byte, alt string) (*CreatedProductMedia, error)
	LinkVariantSKU(ctx context.Context, inventoryItemID, sku string) error
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
	url := fmt.Sprintf("https://%s/admin/api/2025-10/graphql.json", c.StoreDomain)

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
	Note             string               `json:"note,omitempty"`
	TaxExempt        bool                 `json:"taxExempt"`
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
	// Website draft invoices are tax exempt. Store tax settings and the main
	// online store checkout are unchanged.
	input.TaxExempt = true

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

func (c *clientImpl) UpdateDraftOrder(ctx context.Context, id string, input DraftOrderInput, clearShippingLine bool) (*DraftOrderResponse, error) {
	input.TaxExempt = true

	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var inputMap map[string]interface{}
	if err := json.Unmarshal(raw, &inputMap); err != nil {
		return nil, err
	}
	if clearShippingLine {
		inputMap["shippingLine"] = nil
	}

	query := `
		mutation draftOrderUpdate($id: ID!, $input: DraftOrderInput!) {
		  draftOrderUpdate(id: $id, input: $input) {
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
	vars := map[string]interface{}{"id": id, "input": inputMap}

	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data struct {
			DraftOrderUpdate struct {
				DraftOrder *DraftOrderResponse `json:"draftOrder"`
				UserErrors []struct {
					Message string `json:"message"`
				} `json:"userErrors"`
			} `json:"draftOrderUpdate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal shopify response (body: %s): %w", string(resBytes), err)
	}
	if len(res.Data.DraftOrderUpdate.UserErrors) > 0 {
		return nil, fmt.Errorf("shopify draft order error: %s", res.Data.DraftOrderUpdate.UserErrors[0].Message)
	}
	if res.Data.DraftOrderUpdate.DraftOrder == nil {
		return nil, fmt.Errorf("failed to update draft order, raw response: %s", string(resBytes))
	}
	return res.Data.DraftOrderUpdate.DraftOrder, nil
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

type OpenDraftOrder struct {
	ID               string
	Name             string
	Status           string
	Email            string
	InvoiceURL       string
	CreatedAt        string
	TotalAmount      string
	CurrencyCode     string
	CustomAttributes []AttributeInput
}

func (c *clientImpl) ListOpenDraftOrders(ctx context.Context) ([]OpenDraftOrder, error) {
	var all []OpenDraftOrder
	for _, search := range []string{"status:open", "status:invoice_sent", "status:completed"} {
		batch, err := c.listDraftOrdersByQuery(ctx, search)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
	}
	return all, nil
}

func (c *clientImpl) listDraftOrdersByQuery(ctx context.Context, search string) ([]OpenDraftOrder, error) {
	const maxPages = 20
	query := `
		query ListDraftOrders($cursor: String, $query: String!) {
		  draftOrders(first: 50, after: $cursor, query: $query, sortKey: UPDATED_AT, reverse: true) {
		    pageInfo { hasNextPage endCursor }
		    nodes {
		      id
		      name
		      status
		      email
		      invoiceUrl
		      createdAt
		      totalPriceSet { shopMoney { amount currencyCode } }
		      customAttributes { key value }
		    }
		  }
		}
	`

	var all []OpenDraftOrder
	var cursor string
	for page := 0; page < maxPages; page++ {
		vars := map[string]interface{}{"query": search}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
		if err != nil {
			return nil, err
		}

		var res struct {
			Data struct {
				DraftOrders struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						ID            string `json:"id"`
						Name          string `json:"name"`
						Status        string `json:"status"`
						Email         string `json:"email"`
						InvoiceURL    string `json:"invoiceUrl"`
						CreatedAt     string `json:"createdAt"`
						TotalPriceSet struct {
							ShopMoney struct {
								Amount       string `json:"amount"`
								CurrencyCode string `json:"currencyCode"`
							} `json:"shopMoney"`
						} `json:"totalPriceSet"`
						CustomAttributes []AttributeInput `json:"customAttributes"`
					} `json:"nodes"`
				} `json:"draftOrders"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(resBytes, &res); err != nil {
			return nil, fmt.Errorf("failed to unmarshal shopify draft orders (body: %s): %w", string(resBytes), err)
		}
		if len(res.Errors) > 0 {
			return nil, fmt.Errorf("shopify draft orders error: %s", res.Errors[0].Message)
		}

		for _, node := range res.Data.DraftOrders.Nodes {
			all = append(all, OpenDraftOrder{
				ID:               node.ID,
				Name:             node.Name,
				Status:           node.Status,
				Email:            node.Email,
				InvoiceURL:       node.InvoiceURL,
				CreatedAt:        node.CreatedAt,
				TotalAmount:      node.TotalPriceSet.ShopMoney.Amount,
				CurrencyCode:     node.TotalPriceSet.ShopMoney.CurrencyCode,
				CustomAttributes: node.CustomAttributes,
			})
		}
		if !res.Data.DraftOrders.PageInfo.HasNextPage {
			break
		}
		cursor = res.Data.DraftOrders.PageInfo.EndCursor
		if cursor == "" {
			break
		}
	}
	return all, nil
}

type DraftOrderAddress struct {
	FirstName string
	LastName  string
	Company   string
	Address1  string
	Address2  string
	City      string
	Province  string
	Country   string
	Zip       string
	Phone     string
}

type DraftOrderLine struct {
	Title            string
	SKU              string
	Quantity         int
	UnitPrice        string
	LineTotal        string
	VariantID        string
	CustomAttributes []AttributeInput
}

type DraftOrderDetail struct {
	ID               string
	Name             string
	Status           string
	Email            string
	InvoiceURL       string
	CreatedAt        string
	Note             string
	CurrencyCode     string
	Subtotal         string
	TotalTax         string
	Total            string
	ShippingTitle    string
	ShippingAmount   string
	OrderName        string
	CustomAttributes []AttributeInput
	ShippingAddress  *DraftOrderAddress
	BillingAddress   *DraftOrderAddress
	LineItems        []DraftOrderLine
}

func (c *clientImpl) GetDraftOrder(ctx context.Context, id string) (*DraftOrderDetail, error) {
	const maxPages = 5
	query := `
		query GetDraftOrder($id: ID!, $cursor: String) {
		  draftOrder(id: $id) {
		    id
		    name
		    status
		    email
		    invoiceUrl
		    createdAt
		    note2
		    customAttributes { key value }
		    shippingAddress {
		      firstName lastName company address1 address2 city province country zip phone
		    }
		    billingAddress {
		      firstName lastName company address1 address2 city province country zip phone
		    }
		    shippingLine {
		      title
		      originalPriceSet { shopMoney { amount currencyCode } }
		    }
		    subtotalPriceSet { shopMoney { amount currencyCode } }
		    totalTaxSet { shopMoney { amount currencyCode } }
		    totalPriceSet { shopMoney { amount currencyCode } }
		    lineItems(first: 50, after: $cursor) {
		      pageInfo { hasNextPage endCursor }
		      nodes {
		        title
		        sku
		        quantity
		        variant { id }
		        customAttributes { key value }
		        originalUnitPriceSet { shopMoney { amount currencyCode } }
		        discountedTotalSet { shopMoney { amount currencyCode } }
		      }
		    }
		    order { name }
		  }
		}
	`

	var detail *DraftOrderDetail
	var cursor string
	for page := 0; page < maxPages; page++ {
		vars := map[string]interface{}{"id": id}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
		if err != nil {
			return nil, err
		}

		var res draftOrderQueryResponse
		if err := json.Unmarshal(resBytes, &res); err != nil {
			return nil, fmt.Errorf("failed to unmarshal shopify draft order (body: %s): %w", string(resBytes), err)
		}
		if len(res.Errors) > 0 {
			return nil, fmt.Errorf("shopify draft order error: %s", res.Errors[0].Message)
		}
		node := res.Data.DraftOrder
		if node == nil {
			return nil, nil
		}
		if detail == nil {
			detail = mapDraftOrderNode(node)
		}
		for _, line := range node.LineItems.Nodes {
			detail.LineItems = append(detail.LineItems, DraftOrderLine{
				Title:            line.Title,
				SKU:              line.SKU,
				Quantity:         line.Quantity,
				UnitPrice:        line.OriginalUnitPriceSet.ShopMoney.Amount,
				LineTotal:        line.DiscountedTotalSet.ShopMoney.Amount,
				VariantID:        line.Variant.ID,
				CustomAttributes: line.CustomAttributes,
			})
		}
		if !node.LineItems.PageInfo.HasNextPage {
			break
		}
		cursor = node.LineItems.PageInfo.EndCursor
		if cursor == "" {
			break
		}
	}
	return detail, nil
}

type shopMoney struct {
	Amount       string `json:"amount"`
	CurrencyCode string `json:"currencyCode"`
}

type draftOrderAddressNode struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Company   string `json:"company"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2"`
	City      string `json:"city"`
	Province  string `json:"province"`
	Country   string `json:"country"`
	Zip       string `json:"zip"`
	Phone     string `json:"phone"`
}

type draftOrderNode struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Status           string                 `json:"status"`
	Email            string                 `json:"email"`
	InvoiceURL       string                 `json:"invoiceUrl"`
	CreatedAt        string                 `json:"createdAt"`
	Note             string                 `json:"note2"`
	CustomAttributes []AttributeInput       `json:"customAttributes"`
	ShippingAddress  *draftOrderAddressNode `json:"shippingAddress"`
	BillingAddress   *draftOrderAddressNode `json:"billingAddress"`
	ShippingLine     *struct {
		Title            string `json:"title"`
		OriginalPriceSet struct {
			ShopMoney shopMoney `json:"shopMoney"`
		} `json:"originalPriceSet"`
	} `json:"shippingLine"`
	SubtotalPriceSet struct {
		ShopMoney shopMoney `json:"shopMoney"`
	} `json:"subtotalPriceSet"`
	TotalTaxSet struct {
		ShopMoney shopMoney `json:"shopMoney"`
	} `json:"totalTaxSet"`
	TotalPriceSet struct {
		ShopMoney shopMoney `json:"shopMoney"`
	} `json:"totalPriceSet"`
	LineItems struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []struct {
			Title    string `json:"title"`
			SKU      string `json:"sku"`
			Quantity int    `json:"quantity"`
			Variant  struct {
				ID string `json:"id"`
			} `json:"variant"`
			CustomAttributes     []AttributeInput `json:"customAttributes"`
			OriginalUnitPriceSet struct {
				ShopMoney shopMoney `json:"shopMoney"`
			} `json:"originalUnitPriceSet"`
			DiscountedTotalSet struct {
				ShopMoney shopMoney `json:"shopMoney"`
			} `json:"discountedTotalSet"`
		} `json:"nodes"`
	} `json:"lineItems"`
	Order *struct {
		Name string `json:"name"`
	} `json:"order"`
}

type draftOrderQueryResponse struct {
	Data struct {
		DraftOrder *draftOrderNode `json:"draftOrder"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func mapDraftOrderNode(node *draftOrderNode) *DraftOrderDetail {
	detail := &DraftOrderDetail{
		ID:               node.ID,
		Name:             node.Name,
		Status:           node.Status,
		Email:            node.Email,
		InvoiceURL:       node.InvoiceURL,
		CreatedAt:        node.CreatedAt,
		Note:             node.Note,
		CurrencyCode:     node.TotalPriceSet.ShopMoney.CurrencyCode,
		Subtotal:         node.SubtotalPriceSet.ShopMoney.Amount,
		TotalTax:         node.TotalTaxSet.ShopMoney.Amount,
		Total:            node.TotalPriceSet.ShopMoney.Amount,
		CustomAttributes: node.CustomAttributes,
		ShippingAddress:  mapDraftAddress(node.ShippingAddress),
		BillingAddress:   mapDraftAddress(node.BillingAddress),
	}
	if node.ShippingLine != nil {
		detail.ShippingTitle = node.ShippingLine.Title
		detail.ShippingAmount = node.ShippingLine.OriginalPriceSet.ShopMoney.Amount
	}
	if node.Order != nil {
		detail.OrderName = node.Order.Name
	}
	return detail
}

func mapDraftAddress(node *draftOrderAddressNode) *DraftOrderAddress {
	if node == nil {
		return nil
	}
	return &DraftOrderAddress{
		FirstName: node.FirstName,
		LastName:  node.LastName,
		Company:   node.Company,
		Address1:  node.Address1,
		Address2:  node.Address2,
		City:      node.City,
		Province:  node.Province,
		Country:   node.Country,
		Zip:       node.Zip,
		Phone:     node.Phone,
	}
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
