package exchanger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ExchangeAPIResponse matches the structure of exchangerate-api.com JSON.
type ExchangeAPIResponse struct {
	Base  string             `json:"base"`
	Date  string             `json:"date"`
	Rates map[string]float64 `json:"rates"`
}

// Client wraps an HTTP client for fetching exchange rates from an external API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			// Always set a timeout — external APIs can hang indefinitely.
			// 30 seconds is generous for a simple JSON API.
			Timeout: 30 * time.Second,
		},
	}
}

// FetchRates retrieves the latest exchange rates from the API.
// It returns a map of currency code → rate relative to the base currency.
//
// For exchangerate-api.com, the base is always USD and rates tell you
// "1 USD = X units of this currency". We store the inverse (X units = 1 USD)
// in our DB, so we'll invert these in the updater.
func (c *Client) FetchRates(ctx context.Context) (*ExchangeAPIResponse, error) {
	// The API URL format: https://api.exchangerate-api.com/v4/latest/USD
	// We use the base currency as USD since that's what we normalize to.
	url := c.baseURL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var apiResp ExchangeAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &apiResp, nil
}
