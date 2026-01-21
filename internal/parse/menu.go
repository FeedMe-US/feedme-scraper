// Package parse provides HTML parsers for UCLA dining pages.
package parse

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/jackwlutz/feedme/scraper/internal/models"
)

// recipeIDRegex extracts recipe ID from URLs like /menu-item/?recipe=1234
var recipeIDRegex = regexp.MustCompile(`recipe=(\d+)`)

// ParseHallMenu parses a dining hall menu page and extracts menu items.
// The hallName and date are passed in since they may not be in the HTML.
func ParseHallMenu(html, hallName, date string) ([]models.MenuItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var items []models.MenuItem

	// Find all meal stations - they have IDs like "breakfast-The-Sweet-Stop" or "lunch-Harvest-Kitchen"
	doc.Find("div.meal-station").Each(func(i int, station *goquery.Selection) {
		stationID, exists := station.Attr("id")
		if !exists {
			return
		}

		// Parse meal and station name from ID (format: "{meal}-{station-name}")
		meal, stationName := parseMealStationID(stationID)
		if meal == "" {
			return
		}

		// Get station name from heading if available (more accurate)
		if heading := station.Find("div.category-heading h2").First(); heading.Length() > 0 {
			stationName = fixEncoding(strings.TrimSpace(heading.Text()))
		}

		// Find all recipe cards in this station
		station.Find("section.recipe-card").Each(func(j int, card *goquery.Selection) {
			item := parseRecipeCard(card, hallName, date, meal, stationName)
			if item != nil {
				items = append(items, *item)
			}
		})
	})

	return items, nil
}

// parseMealStationID extracts meal type and station name from an ID like "breakfast-The-Sweet-Stop"
func parseMealStationID(id string) (meal, station string) {
	id = strings.ToLower(id)

	// Include "allday" for boutique locations that don't have meal periods
	// Include "snack" for locations like Bruin Plate that have snack periods
	meals := []string{"breakfast", "lunch", "dinner", "late_night", "late-night", "latenight", "allday", "all-day", "all_day", "snack"}
	for _, m := range meals {
		if strings.HasPrefix(id, m+"-") {
			meal = normalizeMeal(m)
			station = strings.TrimPrefix(id, m+"-")
			station = strings.ReplaceAll(station, "-", " ")
			station = strings.TrimSpace(station)
			if station != "" {
				station = safeTitle(station)
			}
			return
		}
	}

	return "", ""
}

// safeTitle converts a string to title case, handling edge cases safely
func safeTitle(s string) string {
	if s == "" {
		return ""
	}
	// First fix any encoding issues (UTF-8 double-encoding or Latin-1 misinterpretation)
	s = fixEncoding(s)
	// Use simple word-by-word title casing to avoid golang.org/x/text edge cases
	words := strings.Fields(s)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	return strings.Join(words, " ")
}

// fixEncoding repairs common UTF-8 encoding issues from HTML scraping.
// This handles cases where UTF-8 bytes are misinterpreted as Latin-1 and then re-encoded.
// For example: "é" (U+00E9) becomes "Ã©" when UTF-8 bytes [0xC3 0xA9] are read as Latin-1.
func fixEncoding(s string) string {
	// Common UTF-8 mojibake patterns (UTF-8 bytes misread as Latin-1)
	// These are ordered pairs to apply sequentially
	replacements := []struct{ bad, good string }{
		{"Ã©", "é"}, // e-acute (most important for "Entrée")
		{"Ã¨", "è"}, // e-grave
		{"Ã ", "à"}, // a-grave
		{"Ã¡", "á"}, // a-acute
		{"Ã¢", "â"}, // a-circumflex
		{"Ã£", "ã"}, // a-tilde
		{"Ã¤", "ä"}, // a-umlaut
		{"Ã§", "ç"}, // c-cedilla
		{"Ã®", "î"}, // i-circumflex
		{"Ã¯", "ï"}, // i-umlaut
		{"Ã±", "ñ"}, // n-tilde
		{"Ã³", "ó"}, // o-acute
		{"Ã´", "ô"}, // o-circumflex
		{"Ã¶", "ö"}, // o-umlaut
		{"Ãº", "ú"}, // u-acute
		{"Ã¼", "ü"}, // u-umlaut
	}

	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.bad, r.good)
	}
	return s
}

// generateGrabAndGoID creates a stable recipe ID for items without UCLA recipe links.
// These are typically pre-packaged items, beverages, or whole produce.
// Format: "gng-{slug}" where slug is lowercase with hyphens.
func generateGrabAndGoID(name string) string {
	// Convert to lowercase and replace spaces with hyphens
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = strings.ReplaceAll(slug, " ", "-")
	// Remove non-alphanumeric characters except hyphens
	var cleaned strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			cleaned.WriteRune(r)
		}
	}
	// Collapse multiple hyphens
	result := cleaned.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	return "gng-" + result
}

// normalizeMeal converts meal names to canonical form
func normalizeMeal(meal string) string {
	meal = strings.ToLower(strings.TrimSpace(meal))
	switch meal {
	case "breakfast":
		return "breakfast"
	case "lunch":
		return "lunch"
	case "dinner":
		return "dinner"
	case "late_night", "late-night", "latenight", "late night":
		return "late_night"
	case "allday", "all-day", "all_day", "all day":
		return "all_day"
	case "snack":
		return "snack"
	default:
		return meal
	}
}

// parseRecipeCard extracts a MenuItem from a recipe card element
func parseRecipeCard(card *goquery.Selection, hallName, date, meal, section string) *models.MenuItem {
	// Get recipe name
	name := strings.TrimSpace(card.Find("div.menu-item-title h3").First().Text())
	if name == "" {
		return nil
	}

	// Get recipe link and extract ID
	link := card.Find("a.recipe-detail-link").First()
	href, _ := link.Attr("href")
	recipeID := extractRecipeID(href)

	// Build full detail URL
	detailURL := ""
	if recipeID != "" {
		detailURL = fmt.Sprintf("https://dining.ucla.edu/menu-item/?recipe=%s", recipeID)
	} else {
		// Generate stable ID for grab-and-go items without UCLA recipe IDs
		recipeID = generateGrabAndGoID(name)
	}

	// Extract dietary tags from icons
	tags := extractDietaryTags(card)

	return &models.MenuItem{
		Date:      date,
		Meal:      meal,
		Location:  hallName,
		Section:   section,
		Name:      name,
		RecipeID:  recipeID,
		DetailURL: detailURL,
		Tags:      tags,
	}
}

// extractRecipeID gets the recipe ID from a URL like /menu-item/?recipe=1234
func extractRecipeID(href string) string {
	matches := recipeIDRegex.FindStringSubmatch(href)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// extractDietaryTags extracts dietary tags from icon elements
func extractDietaryTags(card *goquery.Selection) []string {
	var tags []string
	seen := make(map[string]bool)

	// Diet icons (vegan, vegetarian, etc.)
	card.Find("img.de-diet-icon").Each(func(i int, img *goquery.Selection) {
		if title, exists := img.Attr("title"); exists {
			tag := normalizeDietTag(title)
			if tag != "" && !seen[tag] {
				tags = append(tags, tag)
				seen[tag] = true
			}
		}
	})

	// Allergen icons (contains: wheat, milk, eggs, etc.)
	card.Find("img.de-allergen-icon").Each(func(i int, img *goquery.Selection) {
		if title, exists := img.Attr("title"); exists {
			tag := normalizeAllergenTag(title)
			if tag != "" && !seen[tag] {
				tags = append(tags, tag)
				seen[tag] = true
			}
		}
	})

	return tags
}

// normalizeAllergenTag normalizes allergen tag names with "contains_" prefix
func normalizeAllergenTag(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return ""
	}
	// Prefix allergen tags to distinguish from dietary preferences
	return "contains_" + strings.ReplaceAll(tag, " ", "_")
}

// normalizeDietTag normalizes dietary tag names
func normalizeDietTag(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	switch tag {
	case "vegan":
		return "vegan"
	case "vegetarian":
		return "vegetarian"
	case "halal":
		return "halal"
	case "low-carbon", "low carbon", "low-carbon-footprint":
		return "low_carbon"
	default:
		return tag
	}
}

// ExtractRecipeIDs gets all unique recipe IDs from menu items
func ExtractRecipeIDs(items []models.MenuItem) []string {
	seen := make(map[string]bool)
	var ids []string

	for _, item := range items {
		if item.RecipeID != "" && !seen[item.RecipeID] {
			ids = append(ids, item.RecipeID)
			seen[item.RecipeID] = true
		}
	}

	return ids
}

// CountItemsWithoutRecipeID returns the number of menu items missing recipe_id
func CountItemsWithoutRecipeID(items []models.MenuItem) int {
	count := 0
	for _, item := range items {
		if item.RecipeID == "" {
			count++
		}
	}
	return count
}

// GetItemsWithoutRecipeID returns menu items that are missing recipe_id
func GetItemsWithoutRecipeID(items []models.MenuItem) []models.MenuItem {
	var missing []models.MenuItem
	for _, item := range items {
		if item.RecipeID == "" {
			missing = append(missing, item)
		}
	}
	return missing
}
