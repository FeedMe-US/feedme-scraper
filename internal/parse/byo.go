package parse

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/FeedMe-US/feedme-scraper/internal/models"
)

// BYO item name patterns - these items have customizable components
var byoPatterns = []string{
	"build-your-own",
	"build your own",
	"craft your own",
	"create your own",
	"make your own",
}

// Category mapping from UCLA's display names to normalized categories
var categoryMapping = map[string]string{
	"tortilla":  "base",
	"base":      "base",
	"bread":     "base",
	"wrap":      "base",
	"shell":     "base",
	"pasta":     "base",
	"rice":      "base",
	"greens":    "base",
	"protein":   "protein",
	"meat":      "protein",
	"proteins":  "protein",
	"filling":   "filling",
	"fillings":  "filling",
	"side":      "filling",
	"sides":     "filling",
	"topping":   "topping",
	"toppings":  "topping",
	"sauce":     "sauce",
	"sauces":    "sauce",
	"dressing":  "sauce",
	"dressings": "sauce",
	"salsa":     "sauce",
	"extras":    "topping",
}

// IsBYOItem checks if a menu item is a Build-Your-Own customizable item.
func IsBYOItem(name string) bool {
	nameLower := strings.ToLower(name)
	for _, pattern := range byoPatterns {
		if strings.Contains(nameLower, pattern) {
			return true
		}
	}
	return false
}

// ParseBYODetailPage extracts component information from a BYO item's detail page.
// The HTML structure shows component categories with individual items that have recipe links.
func ParseBYODetailPage(html, parentRecipeID string) ([]models.RecipeComponent, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	var components []models.RecipeComponent
	categoryOrder := 0

	// Build a map of recipe IDs to names from the page for name-based categorization
	componentNames := make(map[string]string)
	doc.Find("a[href*='recipe=']").Each(func(i int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		recipeID := extractRecipeID(href)
		if recipeID != "" && recipeID != parentRecipeID {
			name := strings.TrimSpace(link.Text())
			if name != "" {
				componentNames[recipeID] = name
			}
		}
	})

	// UCLA BYO pages typically have component sections with headers and recipe links
	// Structure varies but generally follows:
	// - Category headers (e.g., "Tortilla", "Protein Options", "Toppings")
	// - Component items with recipe links

	// Strategy 1: Look for component sections in the nutrition calculator area
	doc.Find(".byo-component-section, .component-category, .meal-component").Each(func(i int, section *goquery.Selection) {
		categoryName := extractCategoryName(section)
		normalizedCategory := normalizeCategory(categoryName)

		displayOrder := 0
		section.Find("a[href*='recipe=']").Each(func(j int, link *goquery.Selection) {
			href, _ := link.Attr("href")
			componentID := extractRecipeID(href)
			if componentID == "" || componentID == parentRecipeID {
				return
			}

			component := models.RecipeComponent{
				ParentRecipeID:      parentRecipeID,
				ComponentRecipeID:   componentID,
				Category:            normalizedCategory,
				CategoryDisplayName: categoryName,
				CategoryOrder:       categoryOrder,
				MinSelections:       0,
				IsDefault:           false,
				DisplayOrder:        displayOrder,
				DefaultQuantity:     1.0,
			}

			// Check if this is a default/pre-selected item
			if isDefaultComponent(link) {
				component.IsDefault = true
			}

			components = append(components, component)
			displayOrder++
		})

		if displayOrder > 0 {
			categoryOrder++
		}
	})

	// Strategy 2: If no components found, look for recipe links in ingredient sections
	if len(components) == 0 {
		components = parseBYOFromIngredientLinks(doc, parentRecipeID)
	}

	// Strategy 3: Parse from structured sections with h3/h4 headers
	if len(components) == 0 {
		components = parseBYOFromHeaderSections(doc, parentRecipeID)
	}

	// Strategy 4 (Fallback): Apply name-based categorization for any components
	// that still have generic "component" category
	if len(components) > 0 {
		hasGenericCategories := false
		for _, c := range components {
			if c.Category == "component" || c.Category == "" {
				hasGenericCategories = true
				break
			}
		}

		if hasGenericCategories {
			// Apply name-based categorization using names extracted from the page
			components = CategorizeComponentsByName(components, func(recipeID string) string {
				return componentNames[recipeID]
			})
		}

		// Apply constraints to all components
		components = ApplyCategoryConstraints(components)
	}

	return components, nil
}

// parseBYOFromIngredientLinks extracts components from ingredient list links
func parseBYOFromIngredientLinks(doc *goquery.Document, parentRecipeID string) []models.RecipeComponent {
	var components []models.RecipeComponent
	currentCategory := "component"
	categoryOrder := 0
	displayOrder := 0

	// Look for sections that might contain component links
	doc.Find("#ingredient_list, .recipe-ingredients, .byo-options").Each(func(i int, section *goquery.Selection) {
		section.Find("a[href*='recipe=']").Each(func(j int, link *goquery.Selection) {
			href, _ := link.Attr("href")
			componentID := extractRecipeID(href)
			if componentID == "" || componentID == parentRecipeID {
				return
			}

			// Try to determine category from surrounding context
			parent := link.Parent()
			if headerText := findNearestHeader(parent); headerText != "" {
				newCategory := normalizeCategory(headerText)
				if newCategory != currentCategory {
					currentCategory = newCategory
					categoryOrder++
					displayOrder = 0
				}
			}

			component := models.RecipeComponent{
				ParentRecipeID:      parentRecipeID,
				ComponentRecipeID:   componentID,
				Category:            currentCategory,
				CategoryDisplayName: currentCategory,
				CategoryOrder:       categoryOrder,
				MinSelections:       0,
				IsDefault:           false,
				DisplayOrder:        displayOrder,
				DefaultQuantity:     1.0,
			}

			components = append(components, component)
			displayOrder++
		})
	})

	return components
}

// parseBYOFromHeaderSections extracts components from h3/h4 header sections
func parseBYOFromHeaderSections(doc *goquery.Document, parentRecipeID string) []models.RecipeComponent {
	var components []models.RecipeComponent
	categoryOrder := 0

	// Find all header elements that might indicate component categories
	doc.Find("h3, h4, .category-header").Each(func(i int, header *goquery.Selection) {
		headerText := strings.TrimSpace(header.Text())
		if headerText == "" {
			return
		}

		normalizedCategory := normalizeCategory(headerText)
		if normalizedCategory == "" {
			return
		}

		// Look for recipe links in the following sibling elements
		displayOrder := 0
		nextEl := header.Next()
		for nextEl.Length() > 0 {
			// Stop if we hit another header
			if nextEl.Is("h3, h4, .category-header") {
				break
			}

			nextEl.Find("a[href*='recipe=']").Each(func(j int, link *goquery.Selection) {
				href, _ := link.Attr("href")
				componentID := extractRecipeID(href)
				if componentID == "" || componentID == parentRecipeID {
					return
				}

				component := models.RecipeComponent{
					ParentRecipeID:      parentRecipeID,
					ComponentRecipeID:   componentID,
					Category:            normalizedCategory,
					CategoryDisplayName: headerText,
					CategoryOrder:       categoryOrder,
					MinSelections:       0,
					IsDefault:           isDefaultComponent(link),
					DisplayOrder:        displayOrder,
					DefaultQuantity:     1.0,
				}

				components = append(components, component)
				displayOrder++
			})

			nextEl = nextEl.Next()
		}

		if displayOrder > 0 {
			categoryOrder++
		}
	})

	return components
}

// extractCategoryName gets the category name from a section element
func extractCategoryName(section *goquery.Selection) string {
	// Try various selector patterns for category headers
	selectors := []string{
		"h3",
		"h4",
		".category-name",
		".section-header",
		"strong",
	}

	for _, selector := range selectors {
		if header := section.Find(selector).First(); header.Length() > 0 {
			text := strings.TrimSpace(header.Text())
			if text != "" {
				return text
			}
		}
	}

	// Fall back to section's class or id for category name
	if class, exists := section.Attr("class"); exists {
		return extractCategoryFromClass(class)
	}

	return "component"
}

// normalizeCategory converts display names to normalized category values
func normalizeCategory(name string) string {
	nameLower := strings.ToLower(strings.TrimSpace(name))

	// Direct mapping
	if category, ok := categoryMapping[nameLower]; ok {
		return category
	}

	// Partial matching
	for keyword, category := range categoryMapping {
		if strings.Contains(nameLower, keyword) {
			return category
		}
	}

	// Default fallback
	return "component"
}

// extractCategoryFromClass extracts a category name from CSS class names
func extractCategoryFromClass(class string) string {
	patterns := []string{"protein", "base", "topping", "filling", "sauce"}
	classLower := strings.ToLower(class)

	for _, pattern := range patterns {
		if strings.Contains(classLower, pattern) {
			return pattern
		}
	}

	return "component"
}

// isDefaultComponent checks if a component link indicates it's a default selection
func isDefaultComponent(link *goquery.Selection) bool {
	// Check for common indicators of default/pre-selected state
	parent := link.Parent()

	// Check for "checked" attribute or class
	if _, exists := link.Attr("checked"); exists {
		return true
	}

	if class, _ := parent.Attr("class"); strings.Contains(strings.ToLower(class), "default") ||
		strings.Contains(strings.ToLower(class), "selected") ||
		strings.Contains(strings.ToLower(class), "active") {
		return true
	}

	return false
}

// findNearestHeader finds the nearest header text above an element
func findNearestHeader(el *goquery.Selection) string {
	// Walk up and backwards to find a header
	for el.Length() > 0 {
		if el.Is("h3, h4, h5, strong, .header") {
			return strings.TrimSpace(el.Text())
		}

		// Check previous siblings
		prev := el.Prev()
		if prev.Is("h3, h4, h5, strong, .header") {
			return strings.TrimSpace(prev.Text())
		}

		el = el.Parent()
	}
	return ""
}

// InferDefaultComponents attempts to set reasonable defaults for a BYO item
// when the HTML doesn't indicate which components are pre-selected.
// Heuristic: First item in each required category becomes default.
// If all components are generic "component" category, set first 3 as defaults.
func InferDefaultComponents(components []models.RecipeComponent) []models.RecipeComponent {
	if len(components) == 0 {
		return components
	}

	// Track which categories have defaults set
	categoryHasDefault := make(map[string]bool)
	allGeneric := true

	// First pass: check if any defaults are already set and if all are generic
	for _, c := range components {
		if c.IsDefault {
			categoryHasDefault[c.Category] = true
		}
		if c.Category != "component" {
			allGeneric = false
		}
	}

	result := make([]models.RecipeComponent, len(components))
	copy(result, components)

	// If all components are generic "component" category, set first 3 as defaults
	// This provides a reasonable "standard build" for nutrition calculation
	if allGeneric && !categoryHasDefault["component"] {
		defaultCount := 3
		if len(result) < defaultCount {
			defaultCount = len(result)
		}
		for i := 0; i < defaultCount; i++ {
			result[i].IsDefault = true
		}
		return result
	}

	// Second pass: for categories without defaults, set first item as default
	// Required categories that typically need a default
	requiredCategories := map[string]bool{
		"base":    true,
		"protein": true,
	}

	for i := range result {
		c := &result[i]
		if requiredCategories[c.Category] && !categoryHasDefault[c.Category] {
			// This is the first item in a required category without a default
			c.IsDefault = true
			categoryHasDefault[c.Category] = true
		}
	}

	return result
}

// componentRecipeIDRegex extracts recipe ID from various URL formats
var componentRecipeIDRegex = regexp.MustCompile(`recipe[=\/](\d+)`)

// extractComponentRecipeID extracts recipe ID from a URL (handles multiple formats)
func extractComponentRecipeID(href string) string {
	matches := componentRecipeIDRegex.FindStringSubmatch(href)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// ============================================================================
// NAME-BASED CATEGORIZATION (Fallback when HTML parsing doesn't yield categories)
// ============================================================================

// ingredientPatterns maps ingredient name patterns to categories
// Used as fallback when UCLA's HTML doesn't have recognizable category headers
var ingredientPatterns = map[string][]string{
	"base": {
		"tortilla", "bread", "croissant", "shell", "wrap", "bagel",
		"penne", "rigatoni", "ziti", "rotini", "cavatappi", "farfalle",
		"noodle", "pasta", "pizza dough", "bun", "roll", "pita", "flatbread",
	},
	"protein": {
		"steak", "chicken", "pork", "beef", "turkey", "carnitas",
		"shrimp", "salmon", "fish", "tofu", "lentil", "impossible",
		"meat", "fajita", "egg", "bacon", "sausage", "ham", "prosciutto",
		"chorizo", "pepperoni", "roast beef", "tempeh", "just egg",
		"meatball", "pulled", "grilled", "fried chicken", "ground",
	},
	"filling": {
		"rice", "bean", "refried", "garbanzo", "farro", "quinoa",
		"black bean", "pinto", "chickpea",
	},
	"topping": {
		"lettuce", "tomato", "onion", "corn", "pepper", "olive",
		"vegetable", "cilantro", "jalapeno", "mushroom", "spinach",
		"avocado", "cucumber", "radish", "arugula", "greens", "carrot",
		"kale", "beet", "squash", "zucchini", "artichoke", "brussel",
		"parmesan", "mozzarella", "cheddar", "feta", "provolone",
		"goat cheese", "blue cheese", "crouton", "walnut", "almond",
		"pickle", "caper", "cranberry", "pineapple", "basil",
		"romaine", "cabbage", "slaw", "sprout",
	},
	"sauce": {
		"sauce", "salsa", "cream", "crema", "guacamole", "dressing",
		"pico", "chipotle", "ranch", "mayo", "mustard", "vinaigrette",
		"pesto", "glaze", "bbq", "buffalo", "tajin", "chamoy", "vinegar",
		"aioli", "hummus", "tzatziki", "sriracha", "hot sauce",
	},
}

// categoryConstraints defines min/max selections for each category
var categoryConstraints = map[string]struct {
	min         int
	max         int
	displayName string
	order       int
}{
	"base":    {min: 1, max: 1, displayName: "Choose Your Base", order: 0},
	"protein": {min: 1, max: 2, displayName: "Choose Your Protein", order: 1},
	"filling": {min: 0, max: 2, displayName: "Rice & Beans", order: 2},
	"topping": {min: 0, max: 6, displayName: "Add Toppings", order: 3},
	"sauce":   {min: 0, max: 2, displayName: "Sauces & Dressings", order: 4},
	"extras":  {min: 0, max: 4, displayName: "Extras", order: 5},
}

// categorizeByIngredientName determines component category from its name
// This is used as a fallback when HTML parsing doesn't yield categories
func categorizeByIngredientName(name string) string {
	nameLower := strings.ToLower(name)

	// Check cheese separately - it's a topping but contains "cheese"
	if strings.Contains(nameLower, "cheese") {
		return "topping"
	}

	// Check each category's patterns
	for category, patterns := range ingredientPatterns {
		for _, pattern := range patterns {
			if strings.Contains(nameLower, pattern) {
				return category
			}
		}
	}

	// Default to extras for uncategorized items
	return "extras"
}

// CategorizeComponentsByName applies name-based categorization to components
// that still have the generic "component" category. Also sets proper constraints.
func CategorizeComponentsByName(components []models.RecipeComponent, nameResolver func(recipeID string) string) []models.RecipeComponent {
	if len(components) == 0 {
		return components
	}

	result := make([]models.RecipeComponent, len(components))
	copy(result, components)

	// Categorize each component based on its name
	for i := range result {
		c := &result[i]
		if c.Category == "component" || c.Category == "" {
			// Get the component name from the resolver
			name := ""
			if nameResolver != nil {
				name = nameResolver(c.ComponentRecipeID)
			}

			if name != "" {
				c.Category = categorizeByIngredientName(name)
			} else {
				c.Category = "extras"
			}
		}

		// Apply constraints based on category
		if constraints, ok := categoryConstraints[c.Category]; ok {
			c.MinSelections = constraints.min
			c.MaxSelections = &constraints.max
			c.CategoryDisplayName = constraints.displayName
			c.CategoryOrder = constraints.order
		}
	}

	return result
}

// ApplyCategoryConstraints sets min/max constraints on components based on their category
func ApplyCategoryConstraints(components []models.RecipeComponent) []models.RecipeComponent {
	result := make([]models.RecipeComponent, len(components))
	copy(result, components)

	for i := range result {
		c := &result[i]
		if constraints, ok := categoryConstraints[c.Category]; ok {
			c.MinSelections = constraints.min
			c.MaxSelections = &constraints.max
			if c.CategoryDisplayName == "" || c.CategoryDisplayName == c.Category {
				c.CategoryDisplayName = constraints.displayName
			}
			c.CategoryOrder = constraints.order
		}
	}

	return result
}
