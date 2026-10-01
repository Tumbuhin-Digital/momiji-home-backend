package shipstation

import (
	"context"
	"net/http"
)

func (c *client) GetRates(ctx context.Context, req RateRequest) ([]Rate, error) {
	var res RateResponseWrapper
	if err := c.do(ctx, http.MethodPost, "/v2/rates", req, &res); err != nil {
		return nil, err
	}

	if len(res.RateResponse.Errors) > 0 {
		return nil, &RateResponseError{Messages: res.RateResponse.Errors}
	}
	if len(res.RateResponse.Rates) == 0 && len(res.RateResponse.InvalidRates) > 0 {
		return nil, &RateResponseError{Messages: messagesFromInvalidRates(res.RateResponse.InvalidRates)}
	}

	return res.RateResponse.Rates, nil
}

func messagesFromInvalidRates(invalid []InvalidRate) []CarrierMessage {
	var messages []CarrierMessage
	for _, rate := range invalid {
		if len(rate.ErrorMessages) > 0 {
			messages = append(messages, rate.ErrorMessages...)
			continue
		}
		if rate.Message != "" || rate.ServiceCode != "" {
			messages = append(messages, CarrierMessage{
				ErrorCode: rate.ServiceCode,
				Message:   rate.Message,
			})
		}
	}
	if len(messages) == 0 {
		messages = append(messages, CarrierMessage{Message: "no valid rates returned"})
	}
	return messages
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
