package parse

import (
	"io"
	"net/http"
	"testing"
	"time"
)

// httpClient with timeout for integration tests.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// TestParseHours_LiveSite tests hours parsing against the actual UCLA dining hours page.
// This is an integration test that requires network access.
// Run with: go test -run LiveSite -v
// Skip with: go test -short
func TestParseHours_LiveSite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live site test in short mode")
	}

	resp, err := httpClient.Get("https://dining.ucla.edu/hours/")
	if err != nil {
		t.Fatalf("Failed to fetch hours page: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	html := string(body)
	t.Logf("Fetched %d bytes from UCLA dining hours page", len(html))

	hours, err := ParseHours(html, time.Now())
	if err != nil {
		t.Fatalf("ParseHours failed: %v", err)
	}

	t.Logf("Parsed %d locations", len(hours))

	// Should find at least 5 locations (residential + some boutiques)
	if len(hours) < 5 {
		t.Errorf("Expected at least 5 locations, got %d", len(hours))
	}

	// Log all parsed locations for visibility
	for _, h := range hours {
		breakfast := "Closed"
		if h.BreakfastStart != nil {
			breakfast = h.BreakfastStart.Format("15:04") + "-" + h.BreakfastEnd.Format("15:04")
		}
		lunch := "Closed"
		if h.LunchStart != nil {
			lunch = h.LunchStart.Format("15:04") + "-" + h.LunchEnd.Format("15:04")
		}
		dinner := "Closed"
		if h.DinnerStart != nil {
			dinner = h.DinnerStart.Format("15:04") + "-" + h.DinnerEnd.Format("15:04")
		}
		lateNight := "Closed"
		if h.LateNightStart != nil {
			lateNight = h.LateNightStart.Format("15:04") + "-" + h.LateNightEnd.Format("15:04")
		}
		t.Logf("  %-25s B=%-11s L=%-11s D=%-11s LN=%s",
			h.LocationName, breakfast, lunch, dinner, lateNight)
	}

	// Check for expected residential halls (these should always exist)
	expectedLocations := map[string]bool{
		"De Neve Dining":    false,
		"Bruin Plate":       false,
		"Epicuria at Covel": false,
	}

	for _, h := range hours {
		if _, expected := expectedLocations[h.LocationName]; expected {
			expectedLocations[h.LocationName] = true
		}
	}

	for name, found := range expectedLocations {
		if !found {
			t.Errorf("Expected to find %q but it was not parsed", name)
		}
	}

	// Verify time ranges are valid (start before end, except overnight)
	for _, h := range hours {
		validateTimeRange(t, h.LocationName, "breakfast", h.BreakfastStart, h.BreakfastEnd)
		validateTimeRange(t, h.LocationName, "lunch", h.LunchStart, h.LunchEnd)
		validateTimeRange(t, h.LocationName, "dinner", h.DinnerStart, h.DinnerEnd)
		// Late night can be overnight (e.g., 22:00 - 00:30)
	}
}

// validateTimeRange checks that a time range is valid (non-nil times, start before end).
func validateTimeRange(t *testing.T, location, meal string, start, end *time.Time) {
	t.Helper()
	if start == nil && end == nil {
		return // Closed is valid
	}
	if start == nil || end == nil {
		t.Errorf("%s %s: incomplete time range (start=%v, end=%v)", location, meal, start, end)
		return
	}
	// For non-late-night meals, start should be before end
	if start.After(*end) && meal != "late_night" {
		t.Errorf("%s %s: start time (%s) after end time (%s)",
			location, meal, start.Format("15:04"), end.Format("15:04"))
	}
}

// TestParseHours_LiveSite_LocationMapping verifies that all parsed locations
// can be mapped to database IDs via the AllLocations list.
func TestParseHours_LiveSite_LocationMapping(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live site test in short mode")
	}

	resp, err := httpClient.Get("https://dining.ucla.edu/hours/")
	if err != nil {
		t.Fatalf("Failed to fetch hours page: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	hours, err := ParseHours(string(body), time.Now())
	if err != nil {
		t.Fatalf("ParseHours failed: %v", err)
	}

	// Verify all parsed locations have valid normalized names
	// that will map to database IDs in db.locationNameToID()
	for _, h := range hours {
		normalized := NormalizeLocationName(h.LocationName)
		if normalized == "" {
			t.Errorf("Location %q normalized to empty string", h.LocationName)
		}
		t.Logf("  %q -> %q", h.LocationName, normalized)
	}
}
