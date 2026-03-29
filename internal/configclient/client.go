package configclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type ConfigClient struct {
	baseURL     string
	apiKey      string
	serviceName string
	lastETag    string
	httpClient  *http.Client
	errCount    int
}

type configResponse struct {
	Config   json.RawMessage `json:"config"`
	Checksum string          `json:"checksum"`
}

func New(baseURL, apiKey, serviceName string) *ConfigClient {
	return &ConfigClient{
		baseURL:     baseURL,
		apiKey:      apiKey,
		serviceName: serviceName,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Fetch calls GET /service/:name with If-None-Match.
// Returns (config JSON, changed, error).
// On 304 returns (nil, false, nil).
func (c *ConfigClient) Fetch(ctx context.Context) (json.RawMessage, bool, error) {
	url := fmt.Sprintf("%s/service/%s", c.baseURL, c.serviceName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if c.lastETag != "" {
		req.Header.Set("If-None-Match", c.lastETag)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("fetching config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("config-service returned %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("reading response: %w", err)
	}

	var cr configResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, false, fmt.Errorf("decoding response: %w", err)
	}

	if etag := resp.Header.Get("ETag"); etag != "" {
		c.lastETag = etag
	}

	return cr.Config, true, nil
}

// StartPolling launches a goroutine that calls Fetch every interval.
// On config change, calls onUpdate with the new config JSON.
// Stops when ctx is cancelled.
func (c *ConfigClient) StartPolling(ctx context.Context, interval time.Duration, onUpdate func(json.RawMessage)) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				raw, changed, err := c.Fetch(ctx)
				if err != nil {
					c.errCount++
					if c.errCount <= 5 || c.errCount%2 == 0 {
						slog.Error("config polling fetch failed", "service", c.serviceName, "error", err, "consecutive_errors", c.errCount)
					}
					continue
				}
				if c.errCount > 0 {
					c.errCount = 0
				}
				if changed && raw != nil {
					slog.Info("new config applied", "service", c.serviceName, "etag", c.lastETag)
					onUpdate(raw)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
