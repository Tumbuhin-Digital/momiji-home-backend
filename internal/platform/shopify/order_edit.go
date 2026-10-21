package shopify

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// OrderEditor edits an existing order. It is used to bill a pre-order balance on the
// deposit order itself instead of creating a second order. The real client implements it;
// it is kept apart from Client so the many Client test doubles do not have to grow.
type OrderEditor interface {
	GetOrderBalanceState(ctx context.Context, orderID string) (*OrderBalanceState, error)
	BeginOrderEdit(ctx context.Context, orderID string) (*CalculatedOrder, error)
	SetOrderEditQuantity(ctx context.Context, calculatedOrderID, lineItemID string, quantity int) (*CalculatedOrderTotals, error)
	AddOrderEditVariant(ctx context.Context, calculatedOrderID, variantID, locationID string, quantity int) (*CalculatedLineItem, *CalculatedOrderTotals, error)
	AddOrderEditLineDiscount(ctx context.Context, calculatedOrderID, lineItemID, description string, amount float64) (*CalculatedLineItem, *CalculatedOrderTotals, error)
	AddOrderEditShippingLine(ctx context.Context, calculatedOrderID, title string, amount float64) (*CalculatedShippingLine, *CalculatedOrderTotals, error)
	RemoveOrderEditShippingLine(ctx context.Context, calculatedOrderID, shippingLineID string) (*CalculatedOrderTotals, error)
	CommitOrderEdit(ctx context.Context, calculatedOrderID string, notifyCustomer bool, staffNote string) error
	GetPrimaryLocation(ctx context.Context) (*LocationRef, error)
	GetVariantInventoryPolicies(ctx context.Context, variantIDs []string) (map[string]VariantInventoryPolicy, error)
	SetVariantsInventoryPolicy(ctx context.Context, productID string, variantIDs []string, policy string) error
	GetInventoryAtLocation(ctx context.Context, inventoryItemIDs []string, locationID string) (map[string]InventoryAtLocation, error)
	MoveFulfillmentOrderLines(ctx context.Context, fulfillmentOrderID, locationID string, lines []FulfillmentOrderLineQuantity) error
	SendOrderInvoice(ctx context.Context, orderID string) error
}

// CalculatedOrderTotals is what an order edit would leave the customer owing if committed now.
type CalculatedOrderTotals struct {
	Outstanding float64
}

// CalculatedOrder is an order edit session: the order as it would be after the staged changes.
type CalculatedOrder struct {
	ID            string
	Outstanding   float64
	LineItems     []CalculatedLineItem // lines that existed before the edit began
	ShippingLines []CalculatedShippingLine
}

type CalculatedLineItem struct {
	ID                  string
	Title               string
	SKU                 string
	VariantID           string // empty for custom line items
	Quantity            int
	EditableQuantity    int
	CustomAttributes    []AttributeInput
	OriginalUnitPrice   float64 // catalog price, before any discount
	DiscountedUnitPrice float64 // what one unit actually costs on this line
}

type CalculatedShippingLine struct {
	ID           string
	Title        string
	Price        float64
	StagedStatus string
}

// OrderBalanceState is the committed order as far as balance billing cares.
type OrderBalanceState struct {
	ID                   string
	FinancialStatus      string
	Outstanding          float64
	PaymentCollectionURL string
	LineItems            []OrderLineState
	ShippingLines        []OrderShippingLineState
}

type OrderLineState struct {
	ID              string
	VariantID       string
	Quantity        int
	CurrentQuantity int // excludes removed and refunded units
}

type OrderShippingLineState struct {
	ID    string
	Title string
	Price float64
}

type InventoryAtLocation struct {
	Tracked   bool
	Available int
}

// LocationRef names a Shopify location.
type LocationRef struct {
	ID   string
	Name string
}

// Inventory policies: whether a variant can be bought once its stock runs out
// ("Sell when out of stock" in the Shopify admin).
const (
	InventoryPolicyContinue = "CONTINUE"
	InventoryPolicyDeny     = "DENY"
)

// VariantInventoryPolicy is a variant's "Sell when out of stock" setting and its product,
// which Shopify needs to change the setting.
type VariantInventoryPolicy struct {
	ProductID string
	Policy    string
}

type FulfillmentOrderLineQuantity struct {
	ID       string
	Quantity int
}

const calculatedLineFields = `
	id title sku quantity editableQuantity
	variant { id }
	customAttributes { key value }
	originalUnitPriceSet { shopMoney { amount } }
	discountedUnitPriceSet { shopMoney { amount } }`

const calculatedOrderTotalsFields = `calculatedOrder { totalOutstandingSet { shopMoney { amount } } }`

type moneyNode struct {
	ShopMoney struct {
		Amount string `json:"amount"`
	} `json:"shopMoney"`
}

func (m moneyNode) value() float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(m.ShopMoney.Amount), 64)
	return v
}

type calculatedLineNode struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	SKU              string `json:"sku"`
	Quantity         int    `json:"quantity"`
	EditableQuantity int    `json:"editableQuantity"`
	Variant          *struct {
		ID string `json:"id"`
	} `json:"variant"`
	CustomAttributes       []AttributeInput `json:"customAttributes"`
	OriginalUnitPriceSet   moneyNode        `json:"originalUnitPriceSet"`
	DiscountedUnitPriceSet moneyNode        `json:"discountedUnitPriceSet"`
}

func (n *calculatedLineNode) toLine() *CalculatedLineItem {
	if n == nil {
		return nil
	}
	line := &CalculatedLineItem{
		ID:                  n.ID,
		Title:               n.Title,
		SKU:                 n.SKU,
		Quantity:            n.Quantity,
		EditableQuantity:    n.EditableQuantity,
		CustomAttributes:    n.CustomAttributes,
		OriginalUnitPrice:   n.OriginalUnitPriceSet.value(),
		DiscountedUnitPrice: n.DiscountedUnitPriceSet.value(),
	}
	if n.Variant != nil {
		line.VariantID = n.Variant.ID
	}
	return line
}

type calculatedShippingNode struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	StagedStatus string    `json:"stagedStatus"`
	Price        moneyNode `json:"price"`
}

func (n *calculatedShippingNode) toShippingLine() *CalculatedShippingLine {
	if n == nil {
		return nil
	}
	return &CalculatedShippingLine{ID: n.ID, Title: n.Title, Price: n.Price.value(), StagedStatus: n.StagedStatus}
}

type calculatedTotalsNode struct {
	TotalOutstandingSet moneyNode `json:"totalOutstandingSet"`
}

func (n *calculatedTotalsNode) toTotals() (*CalculatedOrderTotals, error) {
	if n == nil {
		return nil, fmt.Errorf("shopify order edit: no calculated order returned")
	}
	return &CalculatedOrderTotals{Outstanding: n.TotalOutstandingSet.value()}, nil
}

type userErrorNode struct {
	Field   []string `json:"field"`
	Message string   `json:"message"`
}

func firstUserError(op string, errs []userErrorNode) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("shopify %s: %s", op, errs[0].Message)
}

// runAdminGraphQL sends a query and decodes its data into out. Unlike QueryAdminGraphQL it
// treats top-level GraphQL errors as failures: Shopify answers 200 for those, and an edit
// that silently half-applied would bill the customer the wrong amount.
func (c *clientImpl) runAdminGraphQL(ctx context.Context, op, query string, vars map[string]interface{}, out interface{}) error {
	body, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return fmt.Errorf("shopify %s: %w", op, err)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("shopify %s: decode response: %w", op, err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("shopify %s: %s", op, envelope.Errors[0].Message)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("shopify %s: empty response", op)
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("shopify %s: decode data: %w", op, err)
	}
	return nil
}

func moneyInput(amount float64) map[string]interface{} {
	return map[string]interface{}{"amount": fmt.Sprintf("%.2f", amount), "currencyCode": "USD"}
}

func (c *clientImpl) GetOrderBalanceState(ctx context.Context, orderID string) (*OrderBalanceState, error) {
	query := `
		query OrderBalanceState($id: ID!) {
		  order(id: $id) {
		    id
		    displayFinancialStatus
		    totalOutstandingSet { shopMoney { amount } }
		    paymentCollectionDetails { additionalPaymentCollectionUrl }
		    lineItems(first: 100) { nodes { id quantity currentQuantity variant { id } } }
		    shippingLines(first: 50) { nodes { id title originalPriceSet { shopMoney { amount } } } }
		  }
		}`
	var data struct {
		Order *struct {
			ID                       string    `json:"id"`
			DisplayFinancialStatus   string    `json:"displayFinancialStatus"`
			TotalOutstandingSet      moneyNode `json:"totalOutstandingSet"`
			PaymentCollectionDetails struct {
				AdditionalPaymentCollectionURL string `json:"additionalPaymentCollectionUrl"`
			} `json:"paymentCollectionDetails"`
			LineItems struct {
				Nodes []struct {
					ID              string `json:"id"`
					Quantity        int    `json:"quantity"`
					CurrentQuantity int    `json:"currentQuantity"`
					Variant         *struct {
						ID string `json:"id"`
					} `json:"variant"`
				} `json:"nodes"`
			} `json:"lineItems"`
			ShippingLines struct {
				Nodes []struct {
					ID               string    `json:"id"`
					Title            string    `json:"title"`
					OriginalPriceSet moneyNode `json:"originalPriceSet"`
				} `json:"nodes"`
			} `json:"shippingLines"`
		} `json:"order"`
	}
	if err := c.runAdminGraphQL(ctx, "order balance state", query, map[string]interface{}{"id": orderGID(orderID)}, &data); err != nil {
		return nil, err
	}
	if data.Order == nil {
		return nil, fmt.Errorf("shopify order balance state: order %s not found", orderID)
	}
	o := data.Order
	state := &OrderBalanceState{
		ID:                   o.ID,
		FinancialStatus:      o.DisplayFinancialStatus,
		Outstanding:          o.TotalOutstandingSet.value(),
		PaymentCollectionURL: strings.TrimSpace(o.PaymentCollectionDetails.AdditionalPaymentCollectionURL),
	}
	for _, li := range o.LineItems.Nodes {
		line := OrderLineState{ID: li.ID, Quantity: li.Quantity, CurrentQuantity: li.CurrentQuantity}
		if li.Variant != nil {
			line.VariantID = li.Variant.ID
		}
		state.LineItems = append(state.LineItems, line)
	}
	for _, sl := range o.ShippingLines.Nodes {
		state.ShippingLines = append(state.ShippingLines, OrderShippingLineState{ID: sl.ID, Title: sl.Title, Price: sl.OriginalPriceSet.value()})
	}
	return state, nil
}

func (c *clientImpl) BeginOrderEdit(ctx context.Context, orderID string) (*CalculatedOrder, error) {
	query := `
		mutation OrderEditBegin($id: ID!) {
		  orderEditBegin(id: $id) {
		    calculatedOrder {
		      id
		      totalOutstandingSet { shopMoney { amount } }
		      lineItems(first: 100) { nodes {` + calculatedLineFields + ` } }
		      shippingLines { id title stagedStatus price { shopMoney { amount } } }
		    }
		    userErrors { field message }
		  }
		}`
	var data struct {
		OrderEditBegin struct {
			CalculatedOrder *struct {
				ID                  string    `json:"id"`
				TotalOutstandingSet moneyNode `json:"totalOutstandingSet"`
				LineItems           struct {
					Nodes []calculatedLineNode `json:"nodes"`
				} `json:"lineItems"`
				ShippingLines []calculatedShippingNode `json:"shippingLines"`
			} `json:"calculatedOrder"`
			UserErrors []userErrorNode `json:"userErrors"`
		} `json:"orderEditBegin"`
	}
	if err := c.runAdminGraphQL(ctx, "order edit begin", query, map[string]interface{}{"id": orderGID(orderID)}, &data); err != nil {
		return nil, err
	}
	if err := firstUserError("order edit begin", data.OrderEditBegin.UserErrors); err != nil {
		return nil, err
	}
	co := data.OrderEditBegin.CalculatedOrder
	if co == nil {
		return nil, fmt.Errorf("shopify order edit begin: no calculated order returned")
	}
	out := &CalculatedOrder{ID: co.ID, Outstanding: co.TotalOutstandingSet.value()}
	for i := range co.LineItems.Nodes {
		out.LineItems = append(out.LineItems, *co.LineItems.Nodes[i].toLine())
	}
	for i := range co.ShippingLines {
		out.ShippingLines = append(out.ShippingLines, *co.ShippingLines[i].toShippingLine())
	}
	return out, nil
}

func (c *clientImpl) SetOrderEditQuantity(ctx context.Context, calculatedOrderID, lineItemID string, quantity int) (*CalculatedOrderTotals, error) {
	query := `
		mutation OrderEditSetQuantity($id: ID!, $lineItemId: ID!, $quantity: Int!) {
		  orderEditSetQuantity(id: $id, lineItemId: $lineItemId, quantity: $quantity, restock: false) {
		    ` + calculatedOrderTotalsFields + `
		    userErrors { field message }
		  }
		}`
	var data struct {
		OrderEditSetQuantity struct {
			CalculatedOrder *calculatedTotalsNode `json:"calculatedOrder"`
			UserErrors      []userErrorNode       `json:"userErrors"`
		} `json:"orderEditSetQuantity"`
	}
	vars := map[string]interface{}{"id": calculatedOrderID, "lineItemId": lineItemID, "quantity": quantity}
	if err := c.runAdminGraphQL(ctx, "order edit set quantity", query, vars, &data); err != nil {
		return nil, err
	}
	if err := firstUserError("order edit set quantity", data.OrderEditSetQuantity.UserErrors); err != nil {
		return nil, err
	}
	return data.OrderEditSetQuantity.CalculatedOrder.toTotals()
}

func (c *clientImpl) AddOrderEditVariant(ctx context.Context, calculatedOrderID, variantID, locationID string, quantity int) (*CalculatedLineItem, *CalculatedOrderTotals, error) {
	query := `
		mutation OrderEditAddVariant($id: ID!, $variantId: ID!, $quantity: Int!, $locationId: ID) {
		  orderEditAddVariant(id: $id, variantId: $variantId, quantity: $quantity, locationId: $locationId, allowDuplicates: true) {
		    calculatedLineItem {` + calculatedLineFields + ` }
		    ` + calculatedOrderTotalsFields + `
		    userErrors { field message }
		  }
		}`
	var data struct {
		OrderEditAddVariant struct {
			CalculatedLineItem *calculatedLineNode   `json:"calculatedLineItem"`
			CalculatedOrder    *calculatedTotalsNode `json:"calculatedOrder"`
			UserErrors         []userErrorNode       `json:"userErrors"`
		} `json:"orderEditAddVariant"`
	}
	vars := map[string]interface{}{"id": calculatedOrderID, "variantId": variantID, "quantity": quantity}
	if locationID != "" {
		vars["locationId"] = locationID
	}
	if err := c.runAdminGraphQL(ctx, "order edit add variant", query, vars, &data); err != nil {
		return nil, nil, err
	}
	if err := firstUserError("order edit add variant", data.OrderEditAddVariant.UserErrors); err != nil {
		return nil, nil, err
	}
	line := data.OrderEditAddVariant.CalculatedLineItem.toLine()
	if line == nil {
		return nil, nil, fmt.Errorf("shopify order edit add variant: no line returned")
	}
	totals, err := data.OrderEditAddVariant.CalculatedOrder.toTotals()
	return line, totals, err
}

func (c *clientImpl) AddOrderEditLineDiscount(ctx context.Context, calculatedOrderID, lineItemID, description string, amount float64) (*CalculatedLineItem, *CalculatedOrderTotals, error) {
	query := `
		mutation OrderEditAddLineItemDiscount($id: ID!, $lineItemId: ID!, $discount: OrderEditAppliedDiscountInput!) {
		  orderEditAddLineItemDiscount(id: $id, lineItemId: $lineItemId, discount: $discount) {
		    calculatedLineItem {` + calculatedLineFields + ` }
		    ` + calculatedOrderTotalsFields + `
		    userErrors { field message }
		  }
		}`
	var data struct {
		OrderEditAddLineItemDiscount struct {
			CalculatedLineItem *calculatedLineNode   `json:"calculatedLineItem"`
			CalculatedOrder    *calculatedTotalsNode `json:"calculatedOrder"`
			UserErrors         []userErrorNode       `json:"userErrors"`
		} `json:"orderEditAddLineItemDiscount"`
	}
	vars := map[string]interface{}{
		"id":         calculatedOrderID,
		"lineItemId": lineItemID,
		"discount": map[string]interface{}{
			"description": description,
			"fixedValue":  moneyInput(amount),
		},
	}
	if err := c.runAdminGraphQL(ctx, "order edit add discount", query, vars, &data); err != nil {
		return nil, nil, err
	}
	if err := firstUserError("order edit add discount", data.OrderEditAddLineItemDiscount.UserErrors); err != nil {
		return nil, nil, err
	}
	line := data.OrderEditAddLineItemDiscount.CalculatedLineItem.toLine()
	if line == nil {
		return nil, nil, fmt.Errorf("shopify order edit add discount: no line returned")
	}
	totals, err := data.OrderEditAddLineItemDiscount.CalculatedOrder.toTotals()
	return line, totals, err
}

func (c *clientImpl) AddOrderEditShippingLine(ctx context.Context, calculatedOrderID, title string, amount float64) (*CalculatedShippingLine, *CalculatedOrderTotals, error) {
	query := `
		mutation OrderEditAddShippingLine($id: ID!, $shippingLine: OrderEditAddShippingLineInput!) {
		  orderEditAddShippingLine(id: $id, shippingLine: $shippingLine) {
		    calculatedShippingLine { id title stagedStatus price { shopMoney { amount } } }
		    ` + calculatedOrderTotalsFields + `
		    userErrors { field message }
		  }
		}`
	var data struct {
		OrderEditAddShippingLine struct {
			CalculatedShippingLine *calculatedShippingNode `json:"calculatedShippingLine"`
			CalculatedOrder        *calculatedTotalsNode   `json:"calculatedOrder"`
			UserErrors             []userErrorNode         `json:"userErrors"`
		} `json:"orderEditAddShippingLine"`
	}
	vars := map[string]interface{}{
		"id":           calculatedOrderID,
		"shippingLine": map[string]interface{}{"title": title, "price": moneyInput(amount)},
	}
	if err := c.runAdminGraphQL(ctx, "order edit add shipping line", query, vars, &data); err != nil {
		return nil, nil, err
	}
	if err := firstUserError("order edit add shipping line", data.OrderEditAddShippingLine.UserErrors); err != nil {
		return nil, nil, err
	}
	line := data.OrderEditAddShippingLine.CalculatedShippingLine.toShippingLine()
	if line == nil {
		return nil, nil, fmt.Errorf("shopify order edit add shipping line: no shipping line returned")
	}
	totals, err := data.OrderEditAddShippingLine.CalculatedOrder.toTotals()
	return line, totals, err
}

func (c *clientImpl) RemoveOrderEditShippingLine(ctx context.Context, calculatedOrderID, shippingLineID string) (*CalculatedOrderTotals, error) {
	query := `
		mutation OrderEditRemoveShippingLine($id: ID!, $shippingLineId: ID!) {
		  orderEditRemoveShippingLine(id: $id, shippingLineId: $shippingLineId) {
		    ` + calculatedOrderTotalsFields + `
		    userErrors { field message }
		  }
		}`
	var data struct {
		OrderEditRemoveShippingLine struct {
			CalculatedOrder *calculatedTotalsNode `json:"calculatedOrder"`
			UserErrors      []userErrorNode       `json:"userErrors"`
		} `json:"orderEditRemoveShippingLine"`
	}
	vars := map[string]interface{}{"id": calculatedOrderID, "shippingLineId": shippingLineID}
	if err := c.runAdminGraphQL(ctx, "order edit remove shipping line", query, vars, &data); err != nil {
		return nil, err
	}
	if err := firstUserError("order edit remove shipping line", data.OrderEditRemoveShippingLine.UserErrors); err != nil {
		return nil, err
	}
	return data.OrderEditRemoveShippingLine.CalculatedOrder.toTotals()
}

func (c *clientImpl) CommitOrderEdit(ctx context.Context, calculatedOrderID string, notifyCustomer bool, staffNote string) error {
	query := `
		mutation OrderEditCommit($id: ID!, $notifyCustomer: Boolean, $staffNote: String) {
		  orderEditCommit(id: $id, notifyCustomer: $notifyCustomer, staffNote: $staffNote) {
		    order { id }
		    userErrors { field message }
		  }
		}`
	var data struct {
		OrderEditCommit struct {
			Order *struct {
				ID string `json:"id"`
			} `json:"order"`
			UserErrors []userErrorNode `json:"userErrors"`
		} `json:"orderEditCommit"`
	}
	vars := map[string]interface{}{"id": calculatedOrderID, "notifyCustomer": notifyCustomer, "staffNote": staffNote}
	if err := c.runAdminGraphQL(ctx, "order edit commit", query, vars, &data); err != nil {
		return err
	}
	if err := firstUserError("order edit commit", data.OrderEditCommit.UserErrors); err != nil {
		return err
	}
	if data.OrderEditCommit.Order == nil {
		return fmt.Errorf("shopify order edit commit: no order returned")
	}
	return nil
}

// GetPrimaryLocation returns the shop's primary (default) location, which Shopify and apps
// use when no other location is specified.
func (c *clientImpl) GetPrimaryLocation(ctx context.Context) (*LocationRef, error) {
	query := `query PrimaryLocation { location { id name } }`
	var data struct {
		Location *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"location"`
	}
	if err := c.runAdminGraphQL(ctx, "primary location", query, map[string]interface{}{}, &data); err != nil {
		return nil, err
	}
	if data.Location == nil || data.Location.ID == "" {
		return nil, fmt.Errorf("shopify primary location: none returned")
	}
	return &LocationRef{ID: data.Location.ID, Name: data.Location.Name}, nil
}

func (c *clientImpl) GetInventoryAtLocation(ctx context.Context, inventoryItemIDs []string, locationID string) (map[string]InventoryAtLocation, error) {
	out := make(map[string]InventoryAtLocation, len(inventoryItemIDs))
	if len(inventoryItemIDs) == 0 {
		return out, nil
	}
	query := `
		query InventoryAtLocation($ids: [ID!]!, $locationId: ID!) {
		  nodes(ids: $ids) {
		    ... on InventoryItem {
		      id
		      tracked
		      inventoryLevel(locationId: $locationId) { quantities(names: ["available"]) { name quantity } }
		    }
		  }
		}`
	var data struct {
		Nodes []*struct {
			ID             string `json:"id"`
			Tracked        bool   `json:"tracked"`
			InventoryLevel *struct {
				Quantities []struct {
					Name     string `json:"name"`
					Quantity int    `json:"quantity"`
				} `json:"quantities"`
			} `json:"inventoryLevel"`
		} `json:"nodes"`
	}
	vars := map[string]interface{}{"ids": inventoryItemIDs, "locationId": locationID}
	if err := c.runAdminGraphQL(ctx, "inventory at location", query, vars, &data); err != nil {
		return nil, err
	}
	for _, node := range data.Nodes {
		if node == nil || node.ID == "" {
			continue
		}
		inv := InventoryAtLocation{Tracked: node.Tracked}
		if node.InventoryLevel != nil {
			for _, q := range node.InventoryLevel.Quantities {
				if q.Name == "available" {
					inv.Available = q.Quantity
				}
			}
		}
		out[node.ID] = inv
	}
	return out, nil
}

func (c *clientImpl) MoveFulfillmentOrderLines(ctx context.Context, fulfillmentOrderID, locationID string, lines []FulfillmentOrderLineQuantity) error {
	query := `
		mutation FulfillmentOrderMove($id: ID!, $newLocationId: ID!, $lines: [FulfillmentOrderLineItemInput!]) {
		  fulfillmentOrderMove(id: $id, newLocationId: $newLocationId, fulfillmentOrderLineItems: $lines) {
		    movedFulfillmentOrder { id }
		    userErrors { field message }
		  }
		}`
	var data struct {
		FulfillmentOrderMove struct {
			UserErrors []userErrorNode `json:"userErrors"`
		} `json:"fulfillmentOrderMove"`
	}
	vars := map[string]interface{}{"id": fulfillmentOrderID, "newLocationId": locationID}
	if len(lines) > 0 {
		in := make([]map[string]interface{}, 0, len(lines))
		for _, l := range lines {
			in = append(in, map[string]interface{}{"id": l.ID, "quantity": l.Quantity})
		}
		vars["lines"] = in
	}
	if err := c.runAdminGraphQL(ctx, "fulfillment order move", query, vars, &data); err != nil {
		return err
	}
	return firstUserError("fulfillment order move", data.FulfillmentOrderMove.UserErrors)
}

func (c *clientImpl) SendOrderInvoice(ctx context.Context, orderID string) error {
	query := `
		mutation OrderInvoiceSend($id: ID!) {
		  orderInvoiceSend(id: $id) {
		    order { id }
		    userErrors { field message }
		  }
		}`
	var data struct {
		OrderInvoiceSend struct {
			UserErrors []userErrorNode `json:"userErrors"`
		} `json:"orderInvoiceSend"`
	}
	if err := c.runAdminGraphQL(ctx, "order invoice send", query, map[string]interface{}{"id": orderGID(orderID)}, &data); err != nil {
		return err
	}
	return firstUserError("order invoice send", data.OrderInvoiceSend.UserErrors)
}

// GIDNumericID returns the trailing numeric part of a Shopify global ID, so IDs of the same
// record under different types compare equal, e.g. "gid://shopify/CalculatedLineItem/42"
// and "gid://shopify/LineItem/42". Bare numbers pass through.
func GIDNumericID(gid string) string {
	gid = strings.TrimSpace(gid)
	if i := strings.LastIndex(gid, "/"); i >= 0 {
		gid = gid[i+1:]
	}
	if q := strings.Index(gid, "?"); q >= 0 {
		gid = gid[:q]
	}
	return gid
}

// LineItemGID builds an order line item global ID from the numeric ID webhooks send.
func LineItemGID(id int64) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("gid://shopify/LineItem/%d", id)
}

func (c *clientImpl) GetVariantInventoryPolicies(ctx context.Context, variantIDs []string) (map[string]VariantInventoryPolicy, error) {
	out := make(map[string]VariantInventoryPolicy, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	query := `
		query VariantInventoryPolicies($ids: [ID!]!) {
		  nodes(ids: $ids) {
		    ... on ProductVariant { id inventoryPolicy product { id } }
		  }
		}`
	var data struct {
		Nodes []*struct {
			ID              string `json:"id"`
			InventoryPolicy string `json:"inventoryPolicy"`
			Product         *struct {
				ID string `json:"id"`
			} `json:"product"`
		} `json:"nodes"`
	}
	if err := c.runAdminGraphQL(ctx, "variant inventory policies", query, map[string]interface{}{"ids": variantIDs}, &data); err != nil {
		return nil, err
	}
	for _, n := range data.Nodes {
		if n == nil || n.ID == "" || n.Product == nil {
			continue
		}
		out[n.ID] = VariantInventoryPolicy{ProductID: n.Product.ID, Policy: n.InventoryPolicy}
	}
	return out, nil
}

// SetVariantsInventoryPolicy turns "Sell when out of stock" on (CONTINUE) or off (DENY) for
// variants of one product.
func (c *clientImpl) SetVariantsInventoryPolicy(ctx context.Context, productID string, variantIDs []string, policy string) error {
	if len(variantIDs) == 0 {
		return nil
	}
	query := `
		mutation SetInventoryPolicy($productId: ID!, $variants: [ProductVariantsBulkInput!]!) {
		  productVariantsBulkUpdate(productId: $productId, variants: $variants) {
		    userErrors { field message }
		  }
		}`
	variants := make([]map[string]interface{}, 0, len(variantIDs))
	for _, id := range variantIDs {
		variants = append(variants, map[string]interface{}{"id": id, "inventoryPolicy": policy})
	}
	var data struct {
		ProductVariantsBulkUpdate struct {
			UserErrors []userErrorNode `json:"userErrors"`
		} `json:"productVariantsBulkUpdate"`
	}
	vars := map[string]interface{}{"productId": productID, "variants": variants}
	if err := c.runAdminGraphQL(ctx, "set inventory policy", query, vars, &data); err != nil {
		return err
	}
	return firstUserError("set inventory policy", data.ProductVariantsBulkUpdate.UserErrors)
}

var _ OrderEditor = (*clientImpl)(nil)
