package shipstation

import (
	"context"
	"fmt"
	"net/http"
)

func (c *client) GetRates(ctx context.Context, req RateRequest) ([]Rate, error) {
	var res RateResponseWrapper
	if err := c.do(ctx, http.MethodPost, "/v2/rates", req, &res); err != nil {
		return nil, err
	}
	
	if len(res.RateResponse.Errors) > 0 {
		return nil, fmt.Errorf("shipstation returned errors: %v", res.RateResponse.Errors)
	}

	return res.RateResponse.Rates, nil
}

func (c *client) ListCarriers(ctx context.Context) ([]Carrier, error) {
	var res struct {
		Carriers []Carrier `json:"carriers"`
	}
	if err := c.do(ctx, http.MethodGet, "/v2/carriers/", nil, &res); err != nil {
		return nil, err
	}
	return res.Carriers, nil
}
