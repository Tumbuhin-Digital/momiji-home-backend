package shipstation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	defaultBaseURL = "https://api.shipstation.com"
	sandboxBaseURL = "https://ssapi.shipstation.com"
)

type Client interface {
	GetRates(ctx context.Context, req RateRequest) ([]Rate, error)
	ListCarriers(ctx context.Context) ([]Carrier, error)
}

type client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string, sandbox bool) Client {
	// ShipStation API v2 base URL is the same for production and sandbox.
	// Sandbox keys automatically route to test environments.
	base := "https://api.shipstation.com"
	return &client{
		baseURL: base,
		apiKey:  apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *client) do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("shipstation: marshal request: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("shipstation: create request: %w", err)
	}
	req.Header.Set("API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("shipstation: http request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		var errRes struct {
			Message string `json:"message"`
		}
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(res.Body)
		bodyStr := buf.String()
		_ = json.Unmarshal([]byte(bodyStr), &errRes)
		
		msg := errRes.Message
		if msg == "" {
			msg = bodyStr
		}
		return fmt.Errorf("shipstation: api error status %d: %s", res.StatusCode, msg)
	}

	return json.NewDecoder(res.Body).Decode(out)
}
