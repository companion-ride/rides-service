package location

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type Location struct {
	UserID string  `json:"user_id"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
}

func (c *Client) GetCurrentLocation(ctx context.Context, userID string) (*Location, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/last/"+userID, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling location service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("location service returned status %d", resp.StatusCode)
	}

	var loc Location
	if err := json.NewDecoder(resp.Body).Decode(&loc); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &loc, nil
}

// ProxyRequest executes an arbitrary HTTP request through the client's transport.
func (c *Client) ProxyRequest(req *http.Request) (*http.Response, error) {
	return c.httpClient.Do(req)
}

func (c *Client) SetLocation(ctx context.Context, userID string, lat, lon float64) error {
	body, err := json.Marshal(map[string]any{
		"user_id":   userID,
		"user_type": "driver",
		"latitude":  lat,
		"longitude": lon,
	})
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/update", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling location service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("location service returned status %d", resp.StatusCode)
	}
	return nil
}
