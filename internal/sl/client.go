package sl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client wraps the SL Transport API.
// In Go, we use simple structs to bundle related data and methods.
// This is called a "receiver type" or "struct with methods".
type Client struct {
	httpClient *http.Client
	dryRun     bool
	baseURL    string
}

// NewClient is a constructor.
// Go doesn't have explicit constructors, but returning a named type from a New* function is the convention.
// This ensures the Client is always properly initialized.
func NewClient(httpClient *http.Client, dryRun bool) *Client {
	return &Client{
		httpClient: httpClient,
		dryRun:     dryRun,
		baseURL:    "https://transport.integration.sl.se/v1",
	}
}

// Departure represents a single bus departure.
// struct tags like `json:"expected"` tell the JSON decoder which JSON field maps to this struct field.
// Lowercase fields are unexported (private); PascalCase are exported (public).
type Departure struct {
	Scheduled   time.Time `json:"scheduled"`
	Expected    time.Time `json:"expected"`
	Line        string    `json:"line"`
	Direction   string    `json:"direction"`
	DisplayText string    `json:"displayText"`
	StopArea    StopArea  `json:"stopArea"`
	Deviations  []string  `json:"deviations"`
}

// StopArea holds minimal stop metadata.
type StopArea struct {
	Name   string `json:"name"`
	SiteID int    `json:"siteId"`
}

// Site represents a bus stop or station.
type Site struct {
	Name   string `json:"name"`
	SiteID int    `json:"siteId"`
	Type   string `json:"type"` // "STATION", "STOP_AREA", etc.
}

// DeparturesResponse is the full response from SL's /departures endpoint.
type DeparturesResponse struct {
	Departures []Departure `json:"departures"`
}

// SitesResponse is the response from SL's /sites endpoint.
type SitesResponse struct {
	Sites []Site `json:"sites"`
}

// GetDepartures fetches departures for a site.
// It respects the context timeout and implements dry-run mode.
func (c *Client) GetDepartures(ctx context.Context, siteID string) ([]Departure, error) {
	if c.dryRun {
		return c.loadFixture(siteID)
	}

	url := fmt.Sprintf("%s/sites/%s/departures", c.baseURL, siteID)

	// http.NewRequestWithContext attaches the context to the HTTP request.
	// If the context is cancelled (e.g., timeout), the request will be interrupted.
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	// Always close response body to avoid leaking connections.
	// defer ensures this happens even if we return early on error.
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code: %d", resp.StatusCode)
	}

	// io.ReadAll reads the entire response into memory.
	// For small responses (like SL departures), this is fine.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// json.Unmarshal decodes JSON bytes into a Go struct.
	// The type must match the JSON structure.
	var respData DeparturesResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}

	return respData.Departures, nil
}

// GetSites fetches all SL sites (bus stops, stations).
// This is called once for fuzzy matching; the result is cached by the handler.
func (c *Client) GetSites(ctx context.Context) ([]Site, error) {
	if c.dryRun {
		// For dry-run, return a hardcoded list of common sites.
		return []Site{
			{Name: "Storgatan", SiteID: 3484, Type: "STOP_AREA"},
			{Name: "Frösunda torg", SiteID: 3455, Type: "STOP_AREA"},
			{Name: "Solna centrum norra", SiteID: 3472, Type: "STOP_AREA"},
			{Name: "Solna centrum", SiteID: 9305, Type: "STOP_AREA"},
		}, nil
	}

	url := fmt.Sprintf("%s/sites", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var respData SitesResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}

	return respData.Sites, nil
}

// FuzzyMatch finds the top `count` sites matching a query string.
// It does simple case-insensitive substring matching.
// For a production app, use a proper fuzzy library (e.g., https://github.com/sahilm/fuzzy).
func FuzzyMatch(query string, sites []Site, count int) []Site {
	query = strings.ToLower(query)
	var matches []Site

	for _, site := range sites {
		if strings.Contains(strings.ToLower(site.Name), query) {
			matches = append(matches, site)
			if len(matches) >= count {
				break
			}
		}
	}

	return matches
}

// loadFixture loads test data from a JSON file instead of calling the real API.
// This is used when SL_DRY_RUN=1, allowing you to develop offline.
func (c *Client) loadFixture(siteID string) ([]Departure, error) {
	// Construct the fixture path: fixtures/{siteID}.json
	fixtureFile := fmt.Sprintf("fixtures/%s.json", siteID)

	data, err := os.ReadFile(fixtureFile)
	if err != nil {
		return nil, fmt.Errorf("read fixture %s: %w", fixtureFile, err)
	}

	var respData DeparturesResponse
	if err := json.Unmarshal(data, &respData); err != nil {
		return nil, fmt.Errorf("unmarshal fixture: %w", err)
	}

	return respData.Departures, nil
}

// FormatDeparture formats a single departure for display.
// This is a pure function (no I/O, no side effects).
// Pure functions are easy to test.
func FormatDeparture(dep Departure) string {
	// Determine if the bus is early, late, or on time.
	delta := dep.Expected.Sub(dep.Scheduled)

	status := ""
	if delta < -30*time.Second { // more than 30s early
		status = fmt.Sprintf("EARLY −%dm", int((-delta).Minutes()))
	} else if delta > 30*time.Second { // more than 30s late
		status = fmt.Sprintf("+%dm", int(delta.Minutes()))
	} else {
		status = "on time"
	}

	// Format: "HH:mm Direction (status)"
	timeStr := dep.Expected.Format("15:04")
	return fmt.Sprintf("%s %s (%s)", timeStr, dep.Direction, status)
}

// FormatDepartures formats a list of departures.
// We'll show only the next 3 departures to keep the Telegram message concise.
func FormatDepartures(departures []Departure, count int) string {
	if count > len(departures) {
		count = len(departures)
	}

	result := ""
	for i := 0; i < count; i++ {
		result += FormatDeparture(departures[i]) + "\n"
	}

	return result
}
