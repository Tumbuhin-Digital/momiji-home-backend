package webhook

// isWholesaleMomijiOrder reports whether the paid Shopify order was created by
// the Momiji website (checkout, manual order, or settlement invoice).
func isWholesaleMomijiOrder(payload ShopifyOrderWebhook) bool {
	for _, note := range payload.NoteAttributes {
		if note.Name != "source" {
			continue
		}
		if val, ok := note.Value.(string); ok && val == "wholesale" {
			return true
		}
	}
	return false
}
