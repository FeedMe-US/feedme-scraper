package parse

import (
	"os"
	"testing"
	"time"

	"github.com/FeedMe-US/feedme-scraper/internal/models"
)

func TestParseHallMenu(t *testing.T) {
	// Read the test fixture
	html, err := os.ReadFile("../../../temp_page.html")
	if err != nil {
		// Try alternate path
		html, err = os.ReadFile("c:/Users/jackw/Desktop/Lutz Consulting Group, LLC/clients/feedme/temp_page.html")
		if err != nil {
			t.Skip("temp_page.html not found, skipping test")
		}
	}

	items, err := ParseHallMenu(string(html), "De Neve Dining", "2025-01-15")
	if err != nil {
		t.Fatalf("ParseHallMenu failed: %v", err)
	}

	t.Logf("Found %d menu items", len(items))

	if len(items) == 0 {
		t.Error("Expected at least some menu items")
	}

	// Check a few items
	for i, item := range items {
		if i >= 5 {
			break
		}
		t.Logf("Item %d: %s (meal=%s, section=%s, recipe=%s, tags=%v)",
			i, item.Name, item.Meal, item.Section, item.RecipeID, item.Tags)
	}

	// Verify we have items from different meals
	meals := make(map[string]int)
	for _, item := range items {
		meals[item.Meal]++
	}
	t.Logf("Items by meal: %v", meals)

	// Verify we extracted recipe IDs
	withRecipeID := 0
	for _, item := range items {
		if item.RecipeID != "" {
			withRecipeID++
		}
	}
	t.Logf("Items with recipe ID: %d/%d", withRecipeID, len(items))
}

func TestExtractRecipeID(t *testing.T) {
	tests := []struct {
		href     string
		expected string
	}{
		{"/menu-item/?recipe=1922", "1922"},
		{"/menu-item/?recipe=813", "813"},
		{"https://dining.ucla.edu/menu-item/?recipe=7306", "7306"},
		{"/menu-item/", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractRecipeID(tt.href)
		if got != tt.expected {
			t.Errorf("extractRecipeID(%q) = %q, want %q", tt.href, got, tt.expected)
		}
	}
}

func TestParseRecipe(t *testing.T) {
	// Read the test fixture
	html, err := os.ReadFile("c:/Users/jackw/Desktop/Lutz Consulting Group, LLC/clients/feedme/temp_recipe.html")
	if err != nil {
		t.Skip("temp_recipe.html not found, skipping test")
	}

	nutrition, err := ParseRecipe(string(html), "813")
	if err != nil {
		t.Fatalf("ParseRecipe failed: %v", err)
	}

	t.Logf("Recipe: %s", nutrition.Name)
	t.Logf("Serving: %s", nutrition.ServingSize)

	if nutrition.Calories != nil {
		t.Logf("Calories: %.0f", *nutrition.Calories)
	}
	if nutrition.ProteinG != nil {
		t.Logf("Protein: %.2fg", *nutrition.ProteinG)
	}
	if nutrition.CarbsG != nil {
		t.Logf("Carbs: %.2fg", *nutrition.CarbsG)
	}
	if nutrition.FatG != nil {
		t.Logf("Fat: %.2fg", *nutrition.FatG)
	}

	t.Logf("Allergens: %v", nutrition.Allergens)
	t.Logf("Ingredients: %s", nutrition.IngredientsText)

	if nutrition.Name == "" {
		t.Error("Expected recipe name")
	}
}

// ==================== Hours Parsing Tests ====================

func TestMakeTime(t *testing.T) {
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		timeStr  string
		wantNil  bool
		wantHour int
		wantMin  int
	}{
		// Valid formats
		{"simple am", "7am", false, 7, 0},
		{"simple pm", "7pm", false, 19, 0},
		{"with colon am", "7:30am", false, 7, 30},
		{"with colon pm", "10:30pm", false, 22, 30},
		{"dotted am", "7:00 a.m.", false, 7, 0},
		{"dotted pm", "10:00 p.m.", false, 22, 0},
		{"uppercase", "7AM", false, 7, 0},
		{"uppercase pm", "10PM", false, 22, 0},
		{"noon", "12pm", false, 12, 0},
		{"midnight", "12am", false, 0, 0},
		{"11pm", "11pm", false, 23, 0},
		{"11:59pm", "11:59pm", false, 23, 59},

		// Invalid formats
		{"invalid hour 0", "0am", true, 0, 0},
		{"invalid hour 13", "13pm", true, 0, 0},
		{"empty string", "", true, 0, 0},
		{"no time", "closed", true, 0, 0},
		{"invalid minute", "7:60am", true, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeTime(date, tt.timeStr)
			if tt.wantNil {
				if result != nil {
					t.Errorf("makeTime(%q) = %v, want nil", tt.timeStr, result)
				}
				return
			}
			if result == nil {
				t.Errorf("makeTime(%q) = nil, want non-nil", tt.timeStr)
				return
			}
			if result.Hour() != tt.wantHour {
				t.Errorf("makeTime(%q).Hour() = %d, want %d", tt.timeStr, result.Hour(), tt.wantHour)
			}
			if result.Minute() != tt.wantMin {
				t.Errorf("makeTime(%q).Minute() = %d, want %d", tt.timeStr, result.Minute(), tt.wantMin)
			}
		})
	}
}

func TestIsClosed(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"closed", true},
		{"Closed", true},
		{"CLOSED", true},
		{"n/a", true},
		{"N/A", true},
		{"—", true},
		{"-", true},
		{"–", true},
		{"7am - 10am", false},
		{"Open", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := isClosed(tt.text); got != tt.want {
				t.Errorf("isClosed(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestIsKnownLocation(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Should match
		{"De Neve Dining", true},
		{"de neve", true},
		{"Bruin Plate", true},
		{"Epicuria at Covel", true},
		{"epicuria", true},
		// NOTE: Bruin Bowl intentionally removed from known locations
		// (limited hours make it unreliable for meal planning)
		{"Bruin Bowl", false},
		{"Bruin Cafe", true},
		{"Bruin Café", true},
		{"Cafe 1919", true},
		{"Café 1919", true},
		{"Feast at Rieber", true},
		{"Spice Kitchen", true},
		{"Rendezvous", true},
		{"The Drey", true},
		{"The Study at Hedrick", true},

		// Should not match
		{"", false},
		{"Unknown Location", false},
		{"Breakfast", false},
		{"Lunch", false},
		{"Dinner", false},
		{"Hours", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isKnownLocation(tt.name); got != tt.want {
				t.Errorf("isKnownLocation(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestNormalizeLocationName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"De Neve Dining", "De Neve Dining"},
		{"de neve dining hall", "De Neve Dining"},
		{"deneve", "De Neve Dining"},
		{"Bruin Plate", "Bruin Plate"},
		{"Epicuria at Covel", "Epicuria at Covel"},
		{"Epicuria at Ackerman", "Epicuria at Ackerman"},
		{"Bruin Bowl", "Bruin Bowl"},
		{"bruin cafe", "Bruin Cafe"},
		{"Bruin Café", "Bruin Cafe"},
		{"cafe 1919", "Cafe 1919"},
		{"Feast at Rieber", "Feast at Rieber"},
		{"Spice Kitchen at Rieber", "Feast at Rieber"},
		{"Rendezvous", "Rendezvous"},
		{"The Drey", "The Drey"},
		{"The Study at Hedrick", "The Study at Hedrick"},
		{"hedrick study", "The Study at Hedrick"},
		{"Unknown Location", "Unknown Location"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := NormalizeLocationName(tt.input); got != tt.want {
				t.Errorf("NormalizeLocationName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractTimeRange(t *testing.T) {
	tests := []struct {
		text      string
		wantCount int
	}{
		{"7am - 10am", 2},
		{"7:00 a.m. - 10:00 a.m.", 2},
		{"11:00am-2:00pm", 2},
		{"5:00 p.m. - 9:00 p.m.", 2},
		{"closed", 0},
		{"", 0},
		{"7am", 1},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := extractTimeRange(tt.text)
			if len(got) != tt.wantCount {
				t.Errorf("extractTimeRange(%q) returned %d matches, want %d", tt.text, len(got), tt.wantCount)
			}
		})
	}
}

func TestParseHours_TableFormat(t *testing.T) {
	// Mock HTML in UCLA table format
	html := `
	<html>
	<body>
	<table>
		<tbody>
			<tr>
				<td><a href="/bruin-plate/">Bruin Plate</a></td>
				<td>7:00 a.m. - 10:00 a.m.</td>
				<td>11:00 a.m. - 2:00 p.m.</td>
				<td>5:00 p.m. - 8:00 p.m.</td>
				<td>Closed</td>
			</tr>
			<tr>
				<td><a href="/de-neve/">De Neve Dining</a></td>
				<td>7:00 a.m. - 10:00 a.m.</td>
				<td>11:00 a.m. - 3:00 p.m.</td>
				<td>5:00 p.m. - 9:00 p.m.</td>
				<td>10:00 p.m. - 12:00 a.m.</td>
			</tr>
		</tbody>
	</table>
	</body>
	</html>
	`

	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	hours, err := ParseHours(html, date)
	if err != nil {
		t.Fatalf("ParseHours failed: %v", err)
	}

	if len(hours) != 2 {
		t.Errorf("ParseHours returned %d locations, want 2", len(hours))
	}

	// Check Bruin Plate
	var bruinPlate *struct {
		breakfast, lunch, dinner, lateNight bool
	}
	for _, h := range hours {
		if h.LocationName == "Bruin Plate" {
			bruinPlate = &struct{ breakfast, lunch, dinner, lateNight bool }{
				breakfast: h.BreakfastStart != nil,
				lunch:     h.LunchStart != nil,
				dinner:    h.DinnerStart != nil,
				lateNight: h.LateNightStart != nil,
			}
			break
		}
	}

	if bruinPlate == nil {
		t.Error("Bruin Plate not found in results")
	} else {
		if !bruinPlate.breakfast {
			t.Error("Bruin Plate breakfast not parsed")
		}
		if !bruinPlate.lunch {
			t.Error("Bruin Plate lunch not parsed")
		}
		if !bruinPlate.dinner {
			t.Error("Bruin Plate dinner not parsed")
		}
		if bruinPlate.lateNight {
			t.Error("Bruin Plate late night should be closed")
		}
	}

	// Check De Neve has late night
	for _, h := range hours {
		if h.LocationName == "De Neve Dining" {
			if h.LateNightStart == nil {
				t.Error("De Neve late night should be parsed")
			}
			break
		}
	}
}

func TestParseHours_EmptyHTML(t *testing.T) {
	html := "<html><body></body></html>"
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	hours, err := ParseHours(html, date)
	if err != nil {
		t.Fatalf("ParseHours failed: %v", err)
	}

	if len(hours) != 0 {
		t.Errorf("ParseHours with empty HTML returned %d locations, want 0", len(hours))
	}
}

// ==================== BYO Parsing Tests ====================

func TestIsBYOItem(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Build-Your-Own Burrito", true},
		{"Build Your Own Burrito", true},
		{"Craft Your Own Salad", true},
		{"Create Your Own Bowl", true},
		{"Make Your Own Taco", true},
		{"Grilled Chicken Breast", false},
		{"Steamed Rice", false},
		{"Regular Burrito", false},
		{"BYO Tacos", false}, // Only full phrases match
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBYOItem(tt.name); got != tt.want {
				t.Errorf("IsBYOItem(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestNormalizeCategory(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Tortilla", "base"},
		{"tortilla", "base"},
		{"Protein", "protein"},
		{"Protein Options", "protein"},
		{"Toppings", "topping"},
		{"Sauce", "sauce"},
		{"Dressings", "sauce"},
		{"Unknown Category", "component"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeCategory(tt.input); got != tt.want {
				t.Errorf("normalizeCategory(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCategorizeByIngredientName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		// Base items
		{"Flour Tortilla", "base"},
		{"Whole Wheat Wrap", "base"},
		{"Penne Pasta", "base"},
		{"Sourdough Bread", "base"},
		{"Soft Shell", "base"},

		// Protein items
		{"Grilled Chicken", "protein"},
		{"Carne Asada Steak", "protein"},
		{"Carnitas", "protein"},
		{"Scrambled Eggs", "protein"},
		{"Crispy Bacon", "protein"},
		{"Grilled Salmon", "protein"},
		{"Firm Tofu", "protein"},
		{"Turkey Sausage", "protein"},

		// Filling items
		{"Cilantro Lime Rice", "filling"},
		{"Black Beans", "filling"},
		{"Refried Beans", "filling"},
		{"Farro Grain", "filling"},

		// Topping items
		{"Shredded Lettuce", "topping"},
		{"Diced Tomatoes", "topping"},
		{"Shredded Cheddar Cheese", "topping"},
		{"Fresh Cilantro", "topping"},
		{"Sliced Jalapenos", "topping"},
		{"Sauteed Mushrooms", "topping"},
		{"Fresh Avocado", "topping"},

		// Sauce items
		{"Sour Cream", "sauce"},
		{"Fresh Salsa", "sauce"},
		{"Chipotle Mayo", "sauce"},
		{"Ranch Dressing", "sauce"},
		{"Guacamole", "sauce"},
		{"Balsamic Vinaigrette", "sauce"},

		// Extras (uncategorized items)
		{"Special Seasoning", "extras"},
		{"Lime Wedge", "extras"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := categorizeByIngredientName(tt.name); got != tt.want {
				t.Errorf("categorizeByIngredientName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestCategorizeComponentsByName(t *testing.T) {
	// Test that components with "component" category get recategorized
	components := []models.RecipeComponent{
		{ComponentRecipeID: "1", Category: "component"},
		{ComponentRecipeID: "2", Category: "component"},
		{ComponentRecipeID: "3", Category: "component"},
	}

	// Mock name resolver
	names := map[string]string{
		"1": "Flour Tortilla",
		"2": "Grilled Chicken",
		"3": "Sour Cream",
	}

	result := CategorizeComponentsByName(components, func(id string) string {
		return names[id]
	})

	expected := []string{"base", "protein", "sauce"}
	for i, c := range result {
		if c.Category != expected[i] {
			t.Errorf("Component %d category = %q, want %q", i, c.Category, expected[i])
		}
	}

	// Verify constraints were applied
	if result[0].MinSelections != 1 {
		t.Errorf("Base min_selections = %d, want 1", result[0].MinSelections)
	}
	if result[1].MinSelections != 1 {
		t.Errorf("Protein min_selections = %d, want 1", result[1].MinSelections)
	}
	if result[2].MinSelections != 0 {
		t.Errorf("Sauce min_selections = %d, want 0", result[2].MinSelections)
	}
}

func TestParseBYOFromRendezvous(t *testing.T) {
	html, err := os.ReadFile("c:/Users/jackw/Desktop/Lutz Consulting Group, LLC/clients/feedme/temp_rendezvous.html")
	if err != nil {
		t.Skip("temp_rendezvous.html not found, skipping test")
	}

	items, err := ParseHallMenu(string(html), "Rendezvous", "2025-01-13")
	if err != nil {
		t.Fatalf("ParseHallMenu failed: %v", err)
	}

	t.Logf("Total items: %d", len(items))

	// Find BYO items
	var byoItems []string
	for _, item := range items {
		if IsBYOItem(item.Name) {
			byoItems = append(byoItems, item.Name)
			t.Logf("BYO: %s (recipe=%s, section=%s)", item.Name, item.RecipeID, item.Section)
		}
	}

	t.Logf("Found %d BYO items", len(byoItems))
}
