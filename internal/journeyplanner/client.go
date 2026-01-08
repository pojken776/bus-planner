package journeyplanner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Client calls SL Journey Planner v2.
// Docs: https://journeyplanner.integration.sl.se/v2
//
// The client is safe for concurrent use.
type Client struct {
	httpClient *http.Client
	dryRun     bool
	baseURL    string
}

func NewClient(httpClient *http.Client, dryRun bool) *Client {
	return &Client{
		httpClient: httpClient,
		dryRun:     dryRun,
		baseURL:    "https://journeyplanner.integration.sl.se/v2",
	}
}

func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status code: %d body=%s", resp.StatusCode, string(b))
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return b, nil
}
