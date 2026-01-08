package journeyplanner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type SystemMessage struct {
	Type   string `json:"type"`
	Module string `json:"module"`
	Code   int    `json:"code"`
	Text   string `json:"text"`
}

type StopFinderResponse struct {
	SystemMessages []SystemMessage `json:"systemMessages"`
	Locations      []Location      `json:"locations"`
}

// Location is a stop/address/poi returned by stop-finder.
type Location struct {
	ID           string    `json:"id"`
	IsGlobalID   bool      `json:"isGlobalId"`
	Name         string    `json:"name"`
	Coord        []float64 `json:"coord"`
	Type         string    `json:"type"` // stop, address, poi, platform, ...
	MatchQuality int       `json:"matchQuality"`
	IsBest       bool      `json:"isBest"`
}

// StopFinder searches for stops, addresses, and POIs.
// It returns the best matches for the provided query.
func (c *Client) StopFinder(ctx context.Context, query string, limit int) ([]Location, error) {
	if c.dryRun {
		return dryRunStopFinder(query, limit), nil
	}

	q := url.Values{}
	q.Set("name_sf", query)
	q.Set("type_sf", "any")
	// 46 = stops + streets/addresses + POIs (per docs)
	q.Set("any_obj_filter_sf", "46")

	b, err := c.get(ctx, "/stop-finder", q)
	if err != nil {
		return nil, err
	}

	var resp StopFinderResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal stop-finder: %w", err)
	}

	locs := resp.Locations
	if limit > 0 && len(locs) > limit {
		locs = locs[:limit]
	}

	return locs, nil
}

func dryRunStopFinder(query string, limit int) []Location {
	// Minimal fixtures for local dev.
	locs := []Location{
		{ID: "dry:home", Name: "Storgatan", Type: "address", IsBest: true},
		{ID: "dry:work", Name: "Frösunda torg", Type: "stop", IsBest: false},
		{ID: "dry:alt", Name: "Odenplan", Type: "stop", IsBest: false},
	}
	if limit > 0 && len(locs) > limit {
		locs = locs[:limit]
	}
	return locs
}
