// Package models defines the data structures used by the scraper.
package models

import "time"

// Location represents a UCLA dining location.
type Location struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Type          string `json:"type"` // "residential" or "boutique"
	IsResidential bool   `json:"is_residential"`
	URL           string `json:"url"`
}

// MenuItem represents a single menu item at a specific location/date/meal.
type MenuItem struct {
	Date      string   `json:"date"`       // YYYY-MM-DD
	Meal      string   `json:"meal"`       // breakfast, lunch, dinner, late_night
	Location  string   `json:"location"`   // Location name
	Section   string   `json:"section"`    // Station within hall
	Name      string   `json:"name"`       // Display name
	RecipeID  string   `json:"recipe_id"`  // UCLA recipe ID
	DetailURL string   `json:"detail_url"` // Full recipe URL
	Tags      []string `json:"tags"`       // vegan, vegetarian, halal, etc.
}

// Nutrition holds nutrition facts for a recipe.
type Nutrition struct {
	RecipeID        string   `json:"recipe_id"`
	Name            string   `json:"name"`
	ServingSize     string   `json:"serving_size_text"`
	Calories        *float64 `json:"calories"`
	ProteinG        *float64 `json:"protein_g"`
	CarbsG          *float64 `json:"carbs_g"`
	FatG            *float64 `json:"fat_g"`
	FiberG          *float64 `json:"fiber_g"`
	SodiumMG        *float64 `json:"sodium_mg"`
	SugarG          *float64 `json:"sugar_g"`
	SaturatedFatG   *float64 `json:"saturated_fat_g"`
	TransFatG       *float64 `json:"trans_fat_g"`
	CholesterolMG   *float64 `json:"cholesterol_mg"`
	AddedSugarsG    *float64 `json:"added_sugars_g"`
	CalciumMG       *float64 `json:"calcium_mg"`
	IronMG          *float64 `json:"iron_mg"`
	PotassiumMG     *float64 `json:"potassium_mg"`
	VitaminDMCG     *float64 `json:"vitamin_d_mcg"`
	VitaminAMCG     *float64 `json:"vitamin_a_mcg"`
	VitaminB6MG     *float64 `json:"vitamin_b6_mg"`
	VitaminB12MCG   *float64 `json:"vitamin_b12_mcg"`
	VitaminCMG      *float64 `json:"vitamin_c_mg"`
	Allergens       []string `json:"allergens"`
	IngredientsText string   `json:"ingredients_text"`
}

// Allergen represents allergen data for a recipe.
type Allergen struct {
	RecipeID        string          `json:"recipe_id"`
	Contains        map[string]bool `json:"contains"`
	IngredientsText string          `json:"ingredients_text"`
}

// RecipeComponent represents a component of a BYO (Build-Your-Own) item.
type RecipeComponent struct {
	ParentRecipeID      string  `json:"parent_recipe_id"`      // Parent BYO item recipe ID
	ComponentRecipeID   string  `json:"component_recipe_id"`   // Component's recipe ID
	Category            string  `json:"category"`              // base, protein, filling, topping, sauce
	CategoryDisplayName string  `json:"category_display_name"` // "Choose Your Base"
	CategoryOrder       int     `json:"category_order"`        // Display order of categories
	MinSelections       int     `json:"min_selections"`        // 0 = optional
	MaxSelections       *int    `json:"max_selections"`        // nil = unlimited
	IsDefault           bool    `json:"is_default"`            // Pre-selected in default build
	DisplayOrder        int     `json:"display_order"`         // Order within category
	DefaultQuantity     float64 `json:"default_quantity"`      // Multiplier for nutrition
}

// BYOItem represents a Build-Your-Own menu item with its components.
type BYOItem struct {
	MenuItem   MenuItem          `json:"menu_item"`
	Components []RecipeComponent `json:"components"`
}

// LocationHours represents operating hours for a location.
type LocationHours struct {
	LocationName    string     `json:"location_name"`
	Date            string     `json:"date"` // YYYY-MM-DD
	BreakfastStart  *time.Time `json:"breakfast_start"`
	BreakfastEnd    *time.Time `json:"breakfast_end"`
	LunchStart      *time.Time `json:"lunch_start"`
	LunchEnd        *time.Time `json:"lunch_end"`
	DinnerStart     *time.Time `json:"dinner_start"`
	DinnerEnd       *time.Time `json:"dinner_end"`
	LateNightStart  *time.Time `json:"late_night_start"`
	LateNightEnd    *time.Time `json:"late_night_end"`
}

// FetchStats holds statistics about the fetch phase.
type FetchStats struct {
	TotalRequests  int           `json:"total_requests"`
	CacheHits      int           `json:"cache_hits"`
	CacheMisses    int           `json:"cache_misses"`
	Errors         int           `json:"errors"`
	TotalDuration  time.Duration `json:"total_duration"`
}

// IngestReport summarizes a scrape run.
type IngestReport struct {
	Date             string      `json:"date"`
	StartedAt        time.Time   `json:"started_at"`
	FinishedAt       time.Time   `json:"finished_at"`
	Status           string      `json:"status"` // ok, warn, failed
	MenuItems        int         `json:"menu_items"`
	NutritionRecords int         `json:"nutrition_records"`
	AllergenRecords  int         `json:"allergen_records"`
	Errors           []string    `json:"errors"`
	FetchStats       FetchStats  `json:"fetch_stats"`
}

// AllLocations defines all UCLA dining locations to scrape.
// IDs must match the database locations table.
var AllLocations = []Location{
	// Residential Halls (rotating daily menus, unlimited swipe)
	{ID: 28, Name: "De Neve Dining", Slug: "de-neve-dining", Type: "residential", IsResidential: true, URL: "https://dining.ucla.edu/de-neve-dining/"},
	{ID: 29, Name: "Bruin Plate", Slug: "bruin-plate", Type: "residential", IsResidential: true, URL: "https://dining.ucla.edu/bruin-plate/"},
	{ID: 31, Name: "Epicuria at Covel", Slug: "epicuria-at-covel", Type: "residential", IsResidential: true, URL: "https://dining.ucla.edu/epicuria-at-covel/"},

	// Boutique Locations (static menus, single item per swipe)
	{ID: 35, Name: "Bruin Bowl", Slug: "bruin-bowl", Type: "boutique", IsResidential: false, URL: "https://dining.ucla.edu/bruin-bowl/"},
	{ID: 34, Name: "Bruin Cafe", Slug: "bruin-cafe", Type: "boutique", IsResidential: false, URL: "https://dining.ucla.edu/bruin-cafe/"},
	{ID: 36, Name: "Cafe 1919", Slug: "cafe-1919", Type: "boutique", IsResidential: false, URL: "https://dining.ucla.edu/cafe-1919/"},
	{ID: 41, Name: "Epicuria at Ackerman", Slug: "epicuria-at-ackerman", Type: "boutique", IsResidential: false, URL: "https://dining.ucla.edu/epicuria-at-ackerman/"},
	{ID: 30, Name: "Feast at Rieber", Slug: "spice-kitchen", Type: "boutique", IsResidential: false, URL: "https://dining.ucla.edu/spice-kitchen/"},
	{ID: 39, Name: "Rendezvous", Slug: "rendezvous", Type: "boutique", IsResidential: false, URL: "https://dining.ucla.edu/rendezvous/"},
	{ID: 38, Name: "The Drey", Slug: "the-drey", Type: "boutique", IsResidential: false, URL: "https://dining.ucla.edu/the-drey/"},
	{ID: 37, Name: "The Study at Hedrick", Slug: "the-study-at-hedrick", Type: "boutique", IsResidential: false, URL: "https://dining.ucla.edu/the-study-at-hedrick/"},
}

// MealOrder defines the canonical order of meals.
var MealOrder = []string{"breakfast", "lunch", "dinner", "late_night"}

// URLs for special pages.
const (
	MenusOverviewURL = "https://dining.ucla.edu/menus-at-a-glance/"
	HoursURL         = "https://dining.ucla.edu/hours/"
	RecipeURLFormat  = "https://dining.ucla.edu/menu-item/?recipe=%s"
)
