package shopify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewShippingLineInputSetsPriceWithCurrency(t *testing.T) {
	line := NewShippingLineInput("UPS Ground", "17.50", "USD")
	if line == nil {
		t.Fatal("expected shipping line")
	}
	if line.Price != "17.50" {
		t.Fatalf("unexpected legacy price: %s", line.Price)
	}
	if line.PriceWithCurrency == nil {
		t.Fatal("expected priceWithCurrency")
	}
	if line.PriceWithCurrency.Amount != "17.50" || line.PriceWithCurrency.CurrencyCode != "USD" {
		t.Fatalf("unexpected priceWithCurrency: %+v", line.PriceWithCurrency)
	}
}

func TestCreateDraftOrderForcesTaxExempt(t *testing.T) {
	var got bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		var payload struct {
			Variables struct {
				Input DraftOrderInput `json:"input"`
			} `json:"variables"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("unmarshal body: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		got = payload.Variables.Input.TaxExempt
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"draftOrderCreate":{"draftOrder":{"id":"gid://shopify/DraftOrder/1","invoiceUrl":"https://example.com/invoice"},"userErrors":[]}}}`))
	}))
	defer server.Close()

	client := &clientImpl{
		StoreDomain: "shop.example",
		AdminToken:  "token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = server.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		})},
	}

	_, err := client.CreateDraftOrder(context.Background(), DraftOrderInput{
		Email:     "buyer@example.com",
		TaxExempt: false,
	})
	if err != nil {
		t.Fatalf("CreateDraftOrder: %v", err)
	}
	if !got {
		t.Fatal("expected draft order payload taxExempt true")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
