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
	ShippingAddress *ShopifyAddress         `json:"shipping_address"`
	NoteAttributes  []ShopifyProperty       `json:"note_attributes"`
	ShippingLines   []ShopifyShippingLine   `json:"shipping_lines"`
}

type ShopifyShippingLine struct {
	Title string `json:"title"`
	Price string `json:"price"`
}

type ShopifyAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2"`
	City      string `json:"city"`
	Province  string `json:"province"`
	Country   string `json:"country"`
	Zip       string `json:"zip"`
	Phone     string `json:"phone"`
}

type ShopifyOrderLineItem struct {
	ID        int64  `json:"id"`
	VariantID int64  `json:"variant_id"`
	Title     string `json:"title"`
	Quantity      int               `json:"quantity"`
	Price         string            `json:"price"`
	TotalDiscount string            `json:"total_discount"`
	SKU           string            `json:"sku"`
	Properties    []ShopifyProperty `json:"properties"`
}

type ShopifyProperty struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
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

type ShopifyFulfillmentWebhook struct {
	ID              int64                  `json:"id"`
	OrderID         int64                  `json:"order_id"`
	Status          string                 `json:"status"`
	TrackingCompany string                 `json:"tracking_company"`
	TrackingNumber  string                 `json:"tracking_number"`
	TrackingNumbers []string               `json:"tracking_numbers"`
	TrackingURLs    []string               `json:"tracking_urls"`
	ShipmentStatus  string                 `json:"shipment_status"`
	LineItems       []ShopifyOrderLineItem `json:"line_items"`
}
