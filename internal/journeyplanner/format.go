package journeyplanner

import (
	"fmt"
	"strings"
	"time"
)

var stockholmTZ *time.Location

func init() {
	var err error
	stockholmTZ, err = time.LoadLocation("Europe/Stockholm")
	if err != nil {
		stockholmTZ = time.UTC
	}
}

func FormatJourneys(originLabel, destinationLabel string, journeys []Journey, count int) string {
	if count <= 0 {
		count = 3
	}
	if len(journeys) < count {
		count = len(journeys)
	}
	if count == 0 {
		return "No journeys found."
	}

	var b strings.Builder

	for i := 0; i < count; i++ {
		j := journeys[i]

		// Add separator before each option
		b.WriteString("━━━━━━━━━━━━━━━━━━━━━━\n")

		// Calculate total duration and arrival time
		duration := j.TripRtDuration
		if duration == 0 {
			duration = j.TripDuration
		}
		durationMin := (duration + 59) / 60

		arrivalTime := getJourneyArrivalTime(j)

		if arrivalTime != "" {
			fmt.Fprintf(&b, "Option %d (%d min, arrives %s)\n\n", i+1, durationMin, arrivalTime)
		} else {
			fmt.Fprintf(&b, "Option %d (%d min)\n\n", i+1, durationMin)
		}

		for _, leg := range j.Legs {
			b.WriteString(formatLeg(leg))
			b.WriteString("\n")
		}

		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func getJourneyArrivalTime(j Journey) string {
	for i := len(j.Legs) - 1; i >= 0; i-- {
		leg := j.Legs[i]
		if leg.Destination.ArrivalTimeEstimated != "" {
			return parseAndConvertToLocal(leg.Destination.ArrivalTimeEstimated)
		}
		if leg.Destination.ArrivalTimePlanned != "" {
			return parseAndConvertToLocal(leg.Destination.ArrivalTimePlanned)
		}
	}
	return ""
}

func formatLeg(leg Leg) string {
	var b strings.Builder

	productName := ""
	if leg.Transportation != nil {
		productName = strings.ToLower(strings.TrimSpace(leg.Transportation.Product.Name))
	}

	// Determine method and format accordingly
	switch productName {
	case "footpath", "":
		// Walk: 🚶 Walk 18 min
		//       Origin → Destination
		durationMin := (leg.Duration + 59) / 60
		fmt.Fprintf(&b, "🚶 Walk %d min\n", durationMin)
		fmt.Fprintf(&b, "  %s → %s\n", formatName(leg.Origin.Name), formatName(leg.Destination.Name))

	case "bus":
		// Bus: 🚌 Bus 177 → Direction
		//      Depart: 23:50 (on time) or 23:50 → ⚠️ 23:53 (+3 min)
		//      Origin → Destination
		line := ""
		direction := ""
		if leg.Transportation != nil {
			line = leg.Transportation.Number
			if leg.Transportation.Destination != nil {
				direction = leg.Transportation.Destination.Name
			}
		}

		if direction != "" {
			fmt.Fprintf(&b, "🚌 %s → %s\n", line, direction)
		} else {
			fmt.Fprintf(&b, "🚌 %s\n", line)
		}

		b.WriteString(formatDepartureCompact(leg.Origin.DepartureTimePlanned, leg.Origin.DepartureTimeEstimated))
		fmt.Fprintf(&b, "  %s → %s\n", formatName(leg.Origin.Name), formatName(leg.Destination.Name))

	case "metro":
		// Metro: 🚇 Metro Green line 18 → Direction
		line := ""
		direction := ""
		if leg.Transportation != nil {
			line = leg.Transportation.Number
			if leg.Transportation.Destination != nil {
				direction = leg.Transportation.Destination.Name
			}
		}

		if direction != "" {
			fmt.Fprintf(&b, "🚇 %s → %s\n", line, direction)
		} else {
			fmt.Fprintf(&b, "🚇 %s\n", line)
		}

		b.WriteString(formatDepartureCompact(leg.Origin.DepartureTimePlanned, leg.Origin.DepartureTimeEstimated))
		fmt.Fprintf(&b, "  %s → %s\n", formatName(leg.Origin.Name), formatName(leg.Destination.Name))

	case "tram":
		// Tram: 🚊 Tvärbanan 30 → Direction
		line := ""
		direction := ""
		if leg.Transportation != nil {
			line = leg.Transportation.Number
			if leg.Transportation.Destination != nil {
				direction = leg.Transportation.Destination.Name
			}
		}

		if direction != "" {
			fmt.Fprintf(&b, "🚊 %s → %s\n", line, direction)
		} else {
			fmt.Fprintf(&b, "🚊 %s\n", line)
		}

		b.WriteString(formatDepartureCompact(leg.Origin.DepartureTimePlanned, leg.Origin.DepartureTimeEstimated))
		fmt.Fprintf(&b, "  %s → %s\n", formatName(leg.Origin.Name), formatName(leg.Destination.Name))

	default:
		// Fallback for other transport types
		methodName := strings.Title(productName)
		if methodName == "" {
			methodName = "Transit"
		}
		line := ""
		direction := ""
		if leg.Transportation != nil {
			line = leg.Transportation.Number
			if leg.Transportation.Destination != nil {
				direction = leg.Transportation.Destination.Name
			}
		}

		if direction != "" && line != "" {
			fmt.Fprintf(&b, "🚆 %s %s → %s\n", methodName, line, direction)
		} else if line != "" {
			fmt.Fprintf(&b, "🚆 %s %s\n", methodName, line)
		} else {
			fmt.Fprintf(&b, "🚆 %s\n", methodName)
		}

		b.WriteString(formatDepartureCompact(leg.Origin.DepartureTimePlanned, leg.Origin.DepartureTimeEstimated))
		fmt.Fprintf(&b, "  %s → %s\n", formatName(leg.Origin.Name), formatName(leg.Destination.Name))
	}

	return b.String()
}

func formatName(name string) string {
	if name == "" {
		return "(unknown)"
	}
	return name
}

func formatDuration(seconds int) string {
	min := seconds / 60
	sec := seconds % 60
	return fmt.Sprintf("%d min %d sec", min, sec)
}

func formatDepartureCompact(planned, estimated string) string {
	plannedTime := parseAndConvertToLocal(planned)
	estimatedTime := parseAndConvertToLocal(estimated)

	if plannedTime == "" && estimatedTime == "" {
		return ""
	}

	if estimatedTime == "" {
		estimatedTime = plannedTime
	}

	// If on time or no delay info
	if plannedTime == estimatedTime || plannedTime == "" {
		return fmt.Sprintf("  Depart: %s (on time)\n", estimatedTime)
	}

	// Calculate delay in minutes
	plannedT, plannedOK := parseRFC3339ToLocal(planned)
	estimatedT, estimatedOK := parseRFC3339ToLocal(estimated)

	if plannedOK && estimatedOK {
		delta := estimatedT.Sub(plannedT)
		deltaMin := int(delta.Minutes())

		if deltaMin > 0 {
			return fmt.Sprintf("  Depart: %s → ⚠️ %s (+%d min)\n", plannedTime, estimatedTime, deltaMin)
		} else if deltaMin < 0 {
			return fmt.Sprintf("  Depart: %s → %s (%d min early)\n", plannedTime, estimatedTime, deltaMin)
		}
	}

	// Fallback if we can't calculate delta
	return fmt.Sprintf("  Depart: %s → ⚠️ %s\n", plannedTime, estimatedTime)
}

func parseRFC3339ToLocal(timeStr string) (time.Time, bool) {
	if timeStr == "" {
		return time.Time{}, false
	}

	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, timeStr)
		if err != nil {
			return time.Time{}, false
		}
	}

	return t.In(stockholmTZ), true
}

func parseAndConvertToLocal(timeStr string) string {
	if timeStr == "" {
		return ""
	}

	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, timeStr)
		if err != nil {
			return timeStr
		}
	}

	localTime := t.In(stockholmTZ)
	return localTime.Format("15:04")
}
