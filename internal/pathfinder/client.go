package pathfinder

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
			Timeout: 30 * time.Second,
		},
	}
}

type Coord struct {
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
}

type SegmentInput struct {
	Coords []Coord `json:"coords"`
}

type Segment struct {
	Coords      []Coord  `json:"coords"`
	Color       string   `json:"color"`
	Comment     string   `json:"comment,omitempty"`
	StreetNames []string `json:"street_names,omitempty"`
	Distance    int      `json:"distance"`
	Duration    int      `json:"duration"`
}

type RouteResponse struct {
	RouteID       string    `json:"route_id"`
	Segments      []Segment `json:"segments"`
	TotalDistance  string    `json:"total_distance"`
	TotalDuration string    `json:"total_duration"`
}

type WalkResponse struct {
	PassengerLat    float64         `json:"passenger_lat"`
	PassengerLon    float64         `json:"passenger_lon"`
	ClosestLat      float64         `json:"closest_lat"`
	ClosestLon      float64         `json:"closest_lon"`
	Geometry        []Coord         `json:"geometry"`
	WalkDistance    string          `json:"walk_distance"`
	WalkDuration    string          `json:"walk_duration"`
	RawWalkResponse json.RawMessage `json:"raw_walk_response"`
}

func (c *Client) CreateRoute(ctx context.Context, authHeader string, startLat, startLon, finishLat, finishLon float64) (*RouteResponse, error) {
	body, err := json.Marshal(map[string]any{
		"point_a": map[string]float64{"lat": startLat, "lon": startLon},
		"point_b": map[string]float64{"lat": finishLat, "lon": finishLon},
	})
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/routes", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling pathfinder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("pathfinder returned status %d", resp.StatusCode)
	}

	var result RouteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (c *Client) CreateCustomRoute(ctx context.Context, authHeader string, segments []SegmentInput) (*RouteResponse, error) {
	body, err := json.Marshal(map[string]any{"segments": segments})
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/routes/custom", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling pathfinder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("pathfinder returned status %d", resp.StatusCode)
	}

	var result RouteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (c *Client) GetRoute(ctx context.Context, authHeader string, routeID string) (*RouteResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/routes/"+routeID, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling pathfinder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pathfinder returned status %d", resp.StatusCode)
	}

	var result RouteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

func (c *Client) WalkToClosest(ctx context.Context, authHeader string, routeID string, lat, lon float64) (*WalkResponse, error) {
	body, err := json.Marshal(map[string]float64{"lat": lat, "lon": lon})
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/routes/"+routeID+"/walk-to-closest", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling pathfinder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pathfinder returned status %d", resp.StatusCode)
	}

	var result WalkResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}
