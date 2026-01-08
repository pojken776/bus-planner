package journeyplanner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type TripsResponse struct {
	SystemMessages []SystemMessage `json:"systemMessages"`
	Journeys       []Journey       `json:"journeys"`
}

type Journey struct {
	TripID         string `json:"tripId"`
	TripDuration   int    `json:"tripDuration"`   // seconds
	TripRtDuration int    `json:"tripRtDuration"` // seconds
	Interchanges   int    `json:"interchanges"`
	Legs           []Leg  `json:"legs"`
}

type Leg struct {
	Duration             int             `json:"duration"` // seconds
	Origin               LegPoint        `json:"origin"`
	Destination          LegPoint        `json:"destination"`
	Transportation       *Transportation `json:"transportation,omitempty"`
	RealtimeStatus       []string        `json:"realtimeStatus,omitempty"`
	IsRealtimeControlled bool            `json:"isRealtimeControlled,omitempty"`
	Type                 string          `json:"type,omitempty"`
}

type LegPoint struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	Type                   string    `json:"type"`
	Coord                  []float64 `json:"coord,omitempty"`
	DepartureTimePlanned   string    `json:"departureTimePlanned,omitempty"`
	DepartureTimeEstimated string    `json:"departureTimeEstimated,omitempty"`
	ArrivalTimePlanned     string    `json:"arrivalTimePlanned,omitempty"`
	ArrivalTimeEstimated   string    `json:"arrivalTimeEstimated,omitempty"`
	Parent                 *Parent   `json:"parent,omitempty"`
}

type Parent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Transportation struct {
	Name             string  `json:"name"`
	Number           string  `json:"number"`
	DisassembledName string  `json:"disassembledName,omitempty"`
	Product          Product `json:"product"`
	Destination      *Ref    `json:"destination,omitempty"`
}

type Product struct {
	ID    int    `json:"id"`
	Class int    `json:"class"`
	Name  string `json:"name"`
}

type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Trips fetches the next journeys from origin to destination.
// origin/destination can be a stop name, address, or an ID from stop-finder.
// routeType can be "leastinterchange", "leasttime", or "leastwalking" (empty string = no preference).
func (c *Client) Trips(ctx context.Context, origin string, destination string, count int, routeType string) ([]Journey, error) {
	if c.dryRun {
		return dryRunTrips(origin, destination, count), nil
	}

	if count <= 0 {
		count = 3
	}
	if count > 3 {
		count = 3
	}

	q := url.Values{}
	q.Set("type_origin", "any")
	q.Set("type_destination", "any")
	q.Set("name_origin", origin)
	q.Set("name_destination", destination)
	q.Set("calc_number_of_trips", fmt.Sprintf("%d", count))
	q.Set("calc_one_direction", "true")
	q.Set("language", "en")

	if routeType != "" {
		q.Set("routeType", routeType)
	}

	b, err := c.get(ctx, "/trips", q)
	if err != nil {
		return nil, err
	}

	var resp TripsResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal trips: %w", err)
	}

	return resp.Journeys, nil
}

func dryRunTrips(origin string, destination string, count int) []Journey {
	journeys := []Journey{
		{
			TripID:         "dry:1",
			TripDuration:   18 * 60,
			TripRtDuration: 19 * 60,
			Interchanges:   0,
			Legs: []Leg{
				{Duration: 4 * 60, Origin: LegPoint{Name: origin}, Destination: LegPoint{Name: "Storgatan"}, Type: "walk"},
				{Duration: 15 * 60, Origin: LegPoint{Name: "Storgatan", DepartureTimePlanned: "2026-01-03T08:00:00Z", DepartureTimeEstimated: "2026-01-03T08:02:00Z"}, Destination: LegPoint{Name: destination, ArrivalTimePlanned: "2026-01-03T08:15:00Z", ArrivalTimeEstimated: "2026-01-03T08:17:00Z"}, Transportation: &Transportation{Number: "515", Product: Product{Name: "Bus"}}},
			},
		},
		{
			TripID:         "dry:2",
			TripDuration:   22 * 60,
			TripRtDuration: 22 * 60,
			Interchanges:   1,
			Legs: []Leg{
				{Duration: 3 * 60, Origin: LegPoint{Name: origin}, Destination: LegPoint{Name: "Stop A"}, Type: "walk"},
				{Duration: 10 * 60, Origin: LegPoint{Name: "Stop A", DepartureTimePlanned: "2026-01-03T08:10:00Z", DepartureTimeEstimated: "2026-01-03T08:10:00Z"}, Destination: LegPoint{Name: "Stop B", ArrivalTimePlanned: "2026-01-03T08:20:00Z", ArrivalTimeEstimated: "2026-01-03T08:20:00Z"}, Transportation: &Transportation{Number: "11", Product: Product{Name: "Metro"}}},
				{Duration: 9 * 60, Origin: LegPoint{Name: "Stop B"}, Destination: LegPoint{Name: destination}, Type: "walk"},
			},
		},
		{
			TripID:         "dry:3",
			TripDuration:   25 * 60,
			TripRtDuration: 24 * 60,
			Interchanges:   0,
			Legs: []Leg{
				{Duration: 25 * 60, Origin: LegPoint{Name: origin, DepartureTimePlanned: "2026-01-03T08:30:00Z"}, Destination: LegPoint{Name: destination, ArrivalTimePlanned: "2026-01-03T08:55:00Z"}, Transportation: &Transportation{Number: "515", Product: Product{Name: "Bus"}}},
			},
		},
	}

	if count <= 0 || count >= len(journeys) {
		return journeys
	}
	return journeys[:count]
}
