package parse

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/FeedMe-US/feedme-scraper/internal/models"
)

// timeRegex matches times like "7am", "7:30am", "10pm", "10:30pm", "7:00 a.m.", "10:00 p.m."
var timeRegex = regexp.MustCompile(`(\d{1,2}):?(\d{2})?\s*(a\.?m\.?|p\.?m\.?|AM|PM)`)

// closedVariants lists common ways "closed" might appear in the HTML
var closedVariants = []string{"closed", "n/a", "—", "-", "–"}

// isClosed checks if the text indicates a closed status
func isClosed(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, variant := range closedVariants {
		if lower == variant {
			return true
		}
	}
	return false
}

// ParseHours parses the hours page and extracts operating hours for all locations.
func ParseHours(html string, date time.Time) ([]models.LocationHours, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var hours []models.LocationHours
	dateStr := date.Format("2006-01-02")

	// The hours page structure may vary - we'll try common patterns
	// Look for location names and their associated hours

	// Pattern 1: Look for headings with location names followed by hours
	doc.Find("h2, h3, h4").Each(func(i int, heading *goquery.Selection) {
		locationName := strings.TrimSpace(heading.Text())

		// Skip if not a known dining location
		if !isKnownLocation(locationName) {
			return
		}

		locHours := models.LocationHours{
			LocationName: locationName,
			Date:         dateStr,
		}

		// Look for hours in the next siblings or parent container
		nextSibling := heading.Next()
		for nextSibling.Length() > 0 {
			text := strings.TrimSpace(nextSibling.Text())
			if text == "" {
				nextSibling = nextSibling.Next()
				continue
			}

			// Check if this is another heading (stop)
			if nextSibling.Is("h2, h3, h4") {
				break
			}

			// Try to parse meal hours from this text
			parseHoursText(text, &locHours, date)

			nextSibling = nextSibling.Next()
		}

		if locHours.BreakfastStart != nil || locHours.LunchStart != nil ||
			locHours.DinnerStart != nil || locHours.LateNightStart != nil {
			hours = append(hours, locHours)
		}
	})

	// Pattern 2: Look for table-based hours (UCLA dining hours table format)
	// Table structure: Location | Breakfast | Lunch | Dinner | Extended Dinner
	doc.Find("table").Each(func(i int, table *goquery.Selection) {
		table.Find("tbody tr").Each(func(j int, row *goquery.Selection) {
			cells := row.Find("td")
			if cells.Length() < 2 {
				return
			}

			// Location name may be inside an <a> tag
			firstCell := cells.First()
			locationName := strings.TrimSpace(firstCell.Text())
			if link := firstCell.Find("a"); link.Length() > 0 {
				locationName = strings.TrimSpace(link.Text())
			}

			if !isKnownLocation(locationName) {
				return
			}

			// Normalize the location name
			locationName = NormalizeLocationName(locationName)

			locHours := models.LocationHours{
				LocationName: locationName,
				Date:         dateStr,
			}

			// Parse hours from remaining cells by column index
			// Column 1: Breakfast, Column 2: Lunch, Column 3: Dinner, Column 4: Extended Dinner
			cells.Each(func(k int, cell *goquery.Selection) {
				text := strings.TrimSpace(cell.Text())
				if text == "" || isClosed(text) {
					return
				}

				times := extractTimeRange(text)
				if len(times) < 2 {
					return
				}

				switch k {
				case 1: // Breakfast
					locHours.BreakfastStart = makeTime(date, times[0])
					locHours.BreakfastEnd = makeTime(date, times[1])
				case 2: // Lunch
					locHours.LunchStart = makeTime(date, times[0])
					locHours.LunchEnd = makeTime(date, times[1])
				case 3: // Dinner
					locHours.DinnerStart = makeTime(date, times[0])
					locHours.DinnerEnd = makeTime(date, times[1])
				case 4: // Extended Dinner / Late Night
					locHours.LateNightStart = makeTime(date, times[0])
					locHours.LateNightEnd = makeTime(date, times[1])
				}
			})

			if locHours.BreakfastStart != nil || locHours.LunchStart != nil ||
				locHours.DinnerStart != nil || locHours.LateNightStart != nil {
				hours = append(hours, locHours)
			}
		})
	})

	// Log parsing results for debugging
	if len(hours) == 0 {
		slog.Warn("No hours found from either parsing pattern",
			"date", dateStr,
			"html_length", len(html))
	} else {
		// Log overnight hours for visibility (end time < start time indicates overnight)
		for _, h := range hours {
			if h.LateNightStart != nil && h.LateNightEnd != nil {
				if h.LateNightEnd.Before(*h.LateNightStart) {
					slog.Debug("Detected overnight late night hours",
						"location", h.LocationName,
						"start", h.LateNightStart.Format("15:04"),
						"end", h.LateNightEnd.Format("15:04"))
				}
			}
		}
		slog.Info("Parsed hours successfully", "locations", len(hours), "date", dateStr)
	}

	return hours, nil
}

// knownLocationPatterns lists substrings that identify known dining locations
// NOTE: Bruin Bowl removed - limited hours make it unreliable for meal planning
var knownLocationPatterns = []string{
	"de neve", "deneve",
	"bruin plate",
	"epicuria", "covel",
	"bruin cafe", "bruin café",
	"cafe 1919", "café 1919",
	"feast", "rieber", "spice kitchen",
	"rendezvous",
	"the drey", "drey",
	"the study", "hedrick",
}

// isKnownLocation checks if the name matches a known dining location.
// Returns true if matched, false otherwise. Logs unmatched non-empty names for debugging.
func isKnownLocation(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}

	lower := strings.ToLower(name)

	// Skip common non-location strings
	skipPatterns := []string{"breakfast", "lunch", "dinner", "extended", "hours", "location"}
	for _, skip := range skipPatterns {
		if lower == skip {
			return false
		}
	}

	for _, loc := range knownLocationPatterns {
		if strings.Contains(lower, loc) {
			return true
		}
	}

	// Log unmatched names that look like potential locations (contain letters, reasonable length)
	if len(name) > 3 && len(name) < 50 {
		slog.Debug("Unmatched potential location name", "name", name)
	}

	return false
}

// parseHoursText tries to extract meal times from text
func parseHoursText(text string, locHours *models.LocationHours, date time.Time) {
	text = strings.ToLower(text)

	// Look for patterns like "Breakfast: 7am - 10am" or "7am-10am"
	if strings.Contains(text, "breakfast") {
		times := extractTimeRange(text)
		if len(times) >= 2 {
			locHours.BreakfastStart = makeTime(date, times[0])
			locHours.BreakfastEnd = makeTime(date, times[1])
		}
	}

	if strings.Contains(text, "lunch") {
		times := extractTimeRange(text)
		if len(times) >= 2 {
			locHours.LunchStart = makeTime(date, times[0])
			locHours.LunchEnd = makeTime(date, times[1])
		}
	}

	if strings.Contains(text, "dinner") {
		times := extractTimeRange(text)
		if len(times) >= 2 {
			locHours.DinnerStart = makeTime(date, times[0])
			locHours.DinnerEnd = makeTime(date, times[1])
		}
	}

	if strings.Contains(text, "late night") || strings.Contains(text, "late-night") {
		times := extractTimeRange(text)
		if len(times) >= 2 {
			locHours.LateNightStart = makeTime(date, times[0])
			locHours.LateNightEnd = makeTime(date, times[1])
		}
	}
}

// extractTimeRange extracts time pairs from text like "7am - 10am" or "7:30am-10:30pm"
func extractTimeRange(text string) []string {
	matches := timeRegex.FindAllString(text, -1)
	return matches
}

// makeTime creates a time.Time from a date and a time string like "7am", "7:30pm", "7:00 a.m."
// Returns nil if the time string is invalid or unparseable.
func makeTime(date time.Time, timeStr string) *time.Time {
	timeStr = strings.ToLower(strings.TrimSpace(timeStr))

	matches := timeRegex.FindStringSubmatch(timeStr)
	if len(matches) < 4 {
		return nil
	}

	hour, err := strconv.Atoi(matches[1])
	if err != nil {
		slog.Warn("Invalid hour in time string", "timeStr", timeStr, "error", err)
		return nil
	}

	minute := 0
	if matches[2] != "" {
		minute, err = strconv.Atoi(matches[2])
		if err != nil {
			slog.Warn("Invalid minute in time string", "timeStr", timeStr, "error", err)
			return nil
		}
	}

	// Validate hour range (1-12 for 12-hour format)
	if hour < 1 || hour > 12 {
		slog.Warn("Hour out of range for 12-hour format", "timeStr", timeStr, "hour", hour)
		return nil
	}

	// Validate minute range (0-59)
	if minute < 0 || minute > 59 {
		slog.Warn("Minute out of range", "timeStr", timeStr, "minute", minute)
		return nil
	}

	// Check for PM - handle both "pm" and "p.m." formats
	period := strings.ToLower(matches[3])
	isPM := strings.HasPrefix(period, "p")
	if isPM && hour != 12 {
		hour += 12
	}
	if !isPM && hour == 12 {
		hour = 0
	}

	result := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, date.Location())
	return &result
}

// NormalizeLocationName converts various location names to canonical form
func NormalizeLocationName(name string) string {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)

	switch {
	case strings.Contains(lower, "de neve") || strings.Contains(lower, "deneve"):
		return "De Neve Dining"
	case strings.Contains(lower, "bruin plate"):
		return "Bruin Plate"
	case strings.Contains(lower, "epicuria") && strings.Contains(lower, "covel"):
		return "Epicuria at Covel"
	case strings.Contains(lower, "epicuria") && strings.Contains(lower, "ackerman"):
		return "Epicuria at Ackerman"
	case strings.Contains(lower, "bruin bowl"):
		return "Bruin Bowl"
	case strings.Contains(lower, "bruin cafe") || strings.Contains(lower, "bruin café"):
		return "Bruin Cafe"
	case strings.Contains(lower, "cafe 1919") || strings.Contains(lower, "café 1919"):
		return "Cafe 1919"
	case strings.Contains(lower, "feast") || strings.Contains(lower, "spice kitchen"):
		return "Feast at Rieber"
	case strings.Contains(lower, "rendezvous"):
		return "Rendezvous"
	case strings.Contains(lower, "drey"):
		return "The Drey"
	case strings.Contains(lower, "study") || strings.Contains(lower, "hedrick"):
		return "The Study at Hedrick"
	default:
		return name
	}
}
