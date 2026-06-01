package webhook

type ShopifyOrderWebhook struct {
	ID              int64                   `json:"id"`
	OrderNumber     int                     `json:"order_number"`
	Email           string                  `json:"email"`
	TotalPrice      string                  `json:"total_price"`
	Currency        string                  `json:"currency"`
	FinancialStatus string                  `json:"financial_status"`
	LineItems       []ShopifyOrderLineItem  `json:"line_items"`
	Customer        ShopifyCustomer         `json:"customer"`
}

type ShopifyOrderLineItem struct {
	ID        int64  `json:"id"`
	VariantID int64  `json:"variant_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	Price     string `json:"price"`
	SKU       string `json:"sku"`
}

type ShopifyCustomer struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type ShopifyInventoryLevelWebhook struct {
	InventoryItemID int64 `json:"inventory_item_id"`
	Available       int   `json:"available"`
}
