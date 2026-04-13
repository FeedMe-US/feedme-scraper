package parse

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/FeedMe-US/feedme-scraper/internal/models"
)

// nutritionCalcURL is UCLA's AJAX endpoint for computing composite item nutrition.
const nutritionCalcURL = "https://dining.ucla.edu/wp-content/plugins/custom-short-code/nutrition_calc_ajax.php"

// numericRegex extracts numeric values from strings like "3.7g" or "15"
var numericRegex = regexp.MustCompile(`([\d.]+)`)

// ParseRecipe parses a recipe detail page and extracts nutrition data.
func ParseRecipe(html, recipeID string) (*models.Nutrition, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	nutrition := &models.Nutrition{
		RecipeID: recipeID,
	}

	// Get recipe name from h2.single-name
	nutrition.Name = strings.TrimSpace(doc.Find("h2.single-name").First().Text())
	if nutrition.Name == "" {
		// Try alternative selector
		nutrition.Name = strings.TrimSpace(doc.Find(".single-name").First().Text())
	}

	// Get serving size from HTML (more reliable)
	doc.Find("#nutrition").Each(func(i int, sel *goquery.Selection) {
		html, _ := sel.Html()
		if strings.Contains(html, "Serving Size:") {
			// Parse out serving size from HTML like "<strong>Serving Size:</strong> 1oz<hr"
			idx := strings.Index(html, "Serving Size:</strong>")
			if idx >= 0 {
				rest := html[idx+len("Serving Size:</strong>"):]
				endIdx := strings.Index(rest, "<")
				if endIdx > 0 {
					nutrition.ServingSize = strings.TrimSpace(rest[:endIdx])
				}
			}
		}
	})

	// Get calories from p.single-calories
	caloriesText := doc.Find("p.single-calories").First().Text()
	if caloriesText != "" {
		// Text is like "Calories15" - extract the number
		cal := extractNumeric(caloriesText)
		nutrition.Calories = cal
	}

	// Parse nutrition table
	doc.Find("table.nutritive-table tr").Each(func(i int, row *goquery.Selection) {
		cells := row.Find("td")
		if cells.Length() < 1 {
			return
		}

		// Get the first cell text (contains nutrient name and value)
		firstCell := cells.First()
		label := strings.TrimSpace(firstCell.Find("span").First().Text())
		valueText := strings.TrimSpace(firstCell.Text())
		// Remove the label from value text
		valueText = strings.TrimPrefix(valueText, label)

		value := extractNumeric(valueText)

		// Map label to nutrition field
		switch strings.ToLower(label) {
		case "total fat":
			nutrition.FatG = value
		case "saturated fat":
			nutrition.SaturatedFatG = value
		case "trans fat":
			nutrition.TransFatG = value
		case "cholesterol":
			nutrition.CholesterolMG = value
		case "sodium":
			nutrition.SodiumMG = value
		case "total carbohydrate":
			nutrition.CarbsG = value
		case "dietary fiber":
			nutrition.FiberG = value
		case "sugars":
			nutrition.SugarG = value
		case "includes added sugars":
			nutrition.AddedSugarsG = value
		case "protein":
			nutrition.ProteinG = value
		case "calcium":
			nutrition.CalciumMG = value
		case "iron":
			nutrition.IronMG = value
		case "potassium":
			nutrition.PotassiumMG = value
		case "vitamin d":
			nutrition.VitaminDMCG = value
		case "vitamin a":
			nutrition.VitaminAMCG = value
		case "vitamin b6":
			nutrition.VitaminB6MG = value
		case "vitamin b12":
			nutrition.VitaminB12MCG = value
		case "vitamin c":
			nutrition.VitaminCMG = value
		}
	})

	// Parse two-column nutrition table for vitamins/minerals
	doc.Find("table.nutritive-table-two-column tr").Each(func(i int, row *goquery.Selection) {
		cells := row.Find("td")
		cells.Each(func(j int, cell *goquery.Selection) {
			label := strings.TrimSpace(cell.Find("span").First().Text())
			valueText := strings.TrimSpace(cell.Text())
			valueText = strings.TrimPrefix(valueText, label)
			value := extractNumeric(valueText)

			switch strings.ToLower(label) {
			case "calcium":
				nutrition.CalciumMG = value
			case "iron":
				nutrition.IronMG = value
			case "potassium":
				nutrition.PotassiumMG = value
			case "vitamin d":
				nutrition.VitaminDMCG = value
			case "vitamin a":
				nutrition.VitaminAMCG = value
			case "vitamin b6":
				nutrition.VitaminB6MG = value
			case "vitamin b12":
				nutrition.VitaminB12MCG = value
			case "vitamin c":
				nutrition.VitaminCMG = value
			}
		})
	})

	// Extract allergens from ingredients tab
	nutrition.Allergens = extractAllergens(doc)

	// Extract ingredients text
	nutrition.IngredientsText = extractIngredients(doc)

	return nutrition, nil
}

// ParseRecipeWithHTTP parses a recipe detail page, handling both simple and composite formats.
// Composite items (e.g. sandwiches, freestyle bowls) have no inline nutrition data; their
// nutrition is fetched via UCLA's AJAX calculator using all listed components at qty 1.
// If httpClient is nil, falls back to the standard HTML-only parse.
func ParseRecipeWithHTTP(html, recipeID string, httpClient *http.Client) (*models.Nutrition, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	if isCompositePage(doc) && httpClient != nil {
		nutrition, err := parseCompositeNutrition(doc, httpClient, recipeID)
		if err != nil {
			slog.Warn("composite nutrition AJAX failed, falling back to HTML parse",
				"recipe_id", recipeID, "error", err)
		} else {
			return nutrition, nil
		}
	}

	// Standard HTML parse (simple items, or fallback when AJAX fails)
	return ParseRecipe(html, recipeID)
}

// isCompositePage checks whether a recipe page uses the composite/build-your-own format
// where nutrition is calculated from selected components via AJAX, not inline HTML.
func isCompositePage(doc *goquery.Document) bool {
	return doc.Find("div.single-complex-ingredients").Length() > 0
}

// compositeNutritionResp matches UCLA's nutrition_calc_ajax.php JSON response.
type compositeNutritionResp struct {
	Calories      json.Number `json:"calories"`
	Protein       string      `json:"protein"`
	Carbohydrates string      `json:"carbohydrates"`
	Fat           string      `json:"fat"`
	Fibre         string      `json:"fibre"` // British spelling in UCLA's API
	Sugars        string      `json:"sugars"`
	AddedSugars   string      `json:"added_sugars"`
	Sodium        string      `json:"sodium"`
	SaturatedFat  string      `json:"saturated_fat"`
	TransFat      string      `json:"trans_fat"`
	Cholesterol   string      `json:"cholesterol"`
	Calcium       string      `json:"calcium"`
	Iron          string      `json:"iron"`
	Potassium     string      `json:"potassium"`
	VitaminD      string      `json:"vitaminD"` // camelCase in UCLA's API
}

// parseCompositeNutrition fetches nutrition for a composite recipe page by calling
// UCLA's AJAX endpoint with all listed components at quantity 1.
func parseCompositeNutrition(doc *goquery.Document, httpClient *http.Client, recipeID string) (*models.Nutrition, error) {
	// Extract component IDs from checkbox inputs on the page
	var items []string
	doc.Find("input.toggle_nutrition_value").Each(func(i int, sel *goquery.Selection) {
		id, _ := sel.Attr("value")
		ingredientType, _ := sel.Attr("ingredient_type")
		if id != "" && ingredientType != "" {
			items = append(items, fmt.Sprintf("%s,%s,1", id, ingredientType))
		}
	})

	if len(items) == 0 {
		return nil, fmt.Errorf("no components found on composite page")
	}

	// Call the AJAX endpoint with all components
	reqURL := nutritionCalcURL + "?items=" + strings.Join(items, ";") + ";"

	resp, err := httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("AJAX request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AJAX status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read AJAX response: %w", err)
	}

	var result compositeNutritionResp
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode AJAX response: %w", err)
	}

	// Build nutrition from AJAX response
	nutrition := &models.Nutrition{
		RecipeID: recipeID,
		Name:     strings.TrimSpace(doc.Find("h2.single-name").First().Text()),
	}

	if cal, err := result.Calories.Float64(); err == nil && cal > 0 {
		nutrition.Calories = &cal
	}
	nutrition.ProteinG = extractNumeric(result.Protein)
	nutrition.CarbsG = extractNumeric(result.Carbohydrates)
	nutrition.FatG = extractNumeric(result.Fat)
	nutrition.FiberG = extractNumeric(result.Fibre)
	nutrition.SugarG = extractNumeric(result.Sugars)
	nutrition.AddedSugarsG = extractNumeric(result.AddedSugars)
	nutrition.SodiumMG = extractNumeric(result.Sodium)
	nutrition.SaturatedFatG = extractNumeric(result.SaturatedFat)
	nutrition.TransFatG = extractNumeric(result.TransFat)
	nutrition.CholesterolMG = extractNumeric(result.Cholesterol)
	nutrition.CalciumMG = extractNumeric(result.Calcium)
	nutrition.IronMG = extractNumeric(result.Iron)
	nutrition.PotassiumMG = extractNumeric(result.Potassium)
	nutrition.VitaminDMCG = extractNumeric(result.VitaminD)

	// Allergens and ingredients come from the HTML, not the AJAX response
	nutrition.Allergens = extractAllergens(doc)
	nutrition.IngredientsText = extractIngredients(doc)

	if nutrition.Calories == nil {
		return nil, fmt.Errorf("AJAX returned zero or invalid calories for %d components", len(items))
	}

	slog.Info("composite recipe parsed via AJAX",
		"recipe_id", recipeID,
		"calories", *nutrition.Calories,
		"components", len(items),
	)

	return nutrition, nil
}

// extractNumeric extracts a float64 value from a string like "3.7g" or "15mg"
func extractNumeric(s string) *float64 {
	matches := numericRegex.FindStringSubmatch(s)
	if len(matches) >= 2 {
		val, err := strconv.ParseFloat(matches[1], 64)
		if err == nil {
			return &val
		}
	}
	return nil
}

// extractAllergens extracts allergens from the recipe page
func extractAllergens(doc *goquery.Document) []string {
	var allergens []string

	// Look for "Allergens*:" text in the ingredients tab
	doc.Find("#ingredient_list").Each(func(i int, content *goquery.Selection) {
		html, _ := content.Html()
		text := content.Text()

		// Check for "Allergens*:" pattern
		if strings.Contains(text, "Allergens") {
			idx := strings.Index(text, "Allergens")
			if idx >= 0 {
				rest := text[idx:]
				// Find the colon
				colonIdx := strings.Index(rest, ":")
				if colonIdx >= 0 {
					rest = rest[colonIdx+1:]
					// Take until next bold/strong tag or double newline
					endIdx := strings.Index(rest, "\n\n")
					if endIdx > 0 {
						rest = rest[:endIdx]
					}
					endIdx = strings.Index(rest, "*")
					if endIdx > 0 {
						rest = rest[:endIdx]
					}
					rest = strings.TrimSpace(rest)

					if rest != "" && rest != "None" {
						// Split by comma and clean up
						parts := strings.Split(rest, ",")
						for _, p := range parts {
							p = strings.TrimSpace(p)
							if p != "" && p != "None" {
								allergens = append(allergens, normalizeAllergen(p))
							}
						}
					}
				}
			}
		}

		// Also check for allergen icons in metadata
		_ = html // Use html if needed for more complex parsing
	})

	// Also extract allergens from the diet/allergen icons on the main page
	doc.Find("div.single-metadata-item-wrapper img").Each(func(i int, img *goquery.Selection) {
		// These are diet icons, not allergens - skip them
	})

	// Look for allergen icons in recipe cards (from menu pages)
	doc.Find("img.de-allergen-icon").Each(func(i int, img *goquery.Selection) {
		if title, exists := img.Attr("title"); exists {
			allergens = append(allergens, normalizeAllergen(title))
		}
	})

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, a := range allergens {
		if !seen[a] {
			unique = append(unique, a)
			seen[a] = true
		}
	}

	return unique
}

// normalizeAllergen normalizes allergen names
func normalizeAllergen(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "wheat", "gluten":
		return name
	case "dairy", "milk":
		return "dairy"
	case "eggs", "egg":
		return "eggs"
	case "soy", "soybean":
		return "soy"
	case "tree-nuts", "tree nuts", "treenuts":
		return "tree_nuts"
	case "peanuts", "peanut":
		return "peanuts"
	case "fish":
		return "fish"
	case "shellfish":
		return "shellfish"
	case "sesame":
		return "sesame"
	case "alcohol":
		return "alcohol"
	default:
		return name
	}
}

// extractIngredients extracts the ingredients text from the recipe page
func extractIngredients(doc *goquery.Document) string {
	var ingredients []string

	doc.Find("#ingredient_list ul.nolispace li").Each(func(i int, li *goquery.Selection) {
		text := strings.TrimSpace(li.Text())
		if text != "" {
			ingredients = append(ingredients, text)
		}
	})

	return strings.Join(ingredients, ", ")
}

// ExtractAllergensFromMenuCard extracts allergens from a menu card's icons
func ExtractAllergensFromMenuCard(card *goquery.Selection) []string {
	var allergens []string
	seen := make(map[string]bool)

	card.Find("img.de-allergen-icon").Each(func(i int, img *goquery.Selection) {
		if title, exists := img.Attr("title"); exists {
			allergen := normalizeAllergen(title)
			if allergen != "" && !seen[allergen] {
				allergens = append(allergens, allergen)
				seen[allergen] = true
			}
		}
	})

	return allergens
}
