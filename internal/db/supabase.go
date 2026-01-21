// Package db provides a Supabase REST client for the scraper.
package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackwlutz/feedme/scraper/internal/models"
)

// DB is a Supabase REST client.
type DB struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// New creates a new Supabase DB client.
func New(url, apiKey string) *DB {
	return &DB{
		baseURL: url,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// request makes an HTTP request to Supabase.
func (db *DB) request(ctx context.Context, method, path string, body interface{}, headers map[string]string) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	url := db.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set standard headers
	req.Header.Set("apikey", db.apiKey)
	req.Header.Set("Authorization", "Bearer "+db.apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Set custom headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := db.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// menuItemRow is the database representation of a menu item.
// NOTE: No omitempty tags - PostgREST batch upserts require all objects to have identical keys.
type menuItemRow struct {
	Date       string   `json:"date"`
	Meal       string   `json:"meal"`
	LocationID int      `json:"location_id"`
	Section    string   `json:"section"`
	Name       string   `json:"name"`
	Tags       []string `json:"tags"`
	RecipeID   *string  `json:"recipe_id"` // nullable FK to nutrition
	DetailURL  *string  `json:"detail_url"`
}

// stringPtr returns a pointer to s, or nil if s is empty.
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// UpsertMenuItems upserts menu items to the database.
func (db *DB) UpsertMenuItems(ctx context.Context, items []models.MenuItem) error {
	if len(items) == 0 {
		return nil
	}

	// Deduplicate by unique constraint key (date, meal, location_id, name)
	// Note: idx_menu_items_upsert_key does NOT include recipe_id
	// PostgreSQL ON CONFLICT cannot handle duplicate keys within the same batch
	seen := make(map[string]bool)
	rows := make([]menuItemRow, 0, len(items))

	for _, item := range items {
		locationID := locationNameToID(item.Location)
		if locationID == 0 {
			continue // Skip unknown locations
		}

		// Build dedup key matching idx_menu_items_upsert_key (no recipe_id)
		dedupKey := fmt.Sprintf("%s|%s|%d|%s", item.Date, item.Meal, locationID, item.Name)

		if seen[dedupKey] {
			continue // Skip duplicate within batch
		}
		seen[dedupKey] = true

		// Ensure tags is never nil (PostgREST needs consistent keys)
		tags := item.Tags
		if tags == nil {
			tags = []string{}
		}

		rows = append(rows, menuItemRow{
			Date:       item.Date,
			Meal:       item.Meal,
			LocationID: locationID,
			Section:    item.Section,
			Name:       item.Name,
			Tags:       tags,
			RecipeID:   stringPtr(item.RecipeID), // nil if empty, satisfies nullable FK
			DetailURL:  stringPtr(item.DetailURL),
		})
	}

	if len(rows) == 0 {
		return nil
	}

	// Upsert with conflict resolution on idx_menu_items_upsert_key (date, meal, location_id, name)
	headers := map[string]string{
		"Prefer": "resolution=merge-duplicates",
	}

	_, err := db.request(ctx, http.MethodPost, "/rest/v1/menu_items?on_conflict=date,meal,location_id,name", rows, headers)
	if err != nil {
		return fmt.Errorf("upsert menu items: %w", err)
	}

	return nil
}

// nutritionRow is the database representation of nutrition data.
type nutritionRow struct {
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
	IngredientsText string   `json:"ingredients_text"`
}

// UpsertNutrition upserts nutrition records to the database.
func (db *DB) UpsertNutrition(ctx context.Context, nutrition []models.Nutrition) error {
	if len(nutrition) == 0 {
		return nil
	}

	// Convert to database rows
	rows := make([]nutritionRow, 0, len(nutrition))
	for _, n := range nutrition {
		rows = append(rows, nutritionRow{
			RecipeID:        n.RecipeID,
			Name:            n.Name,
			ServingSize:     n.ServingSize,
			Calories:        n.Calories,
			ProteinG:        n.ProteinG,
			CarbsG:          n.CarbsG,
			FatG:            n.FatG,
			FiberG:          n.FiberG,
			SodiumMG:        n.SodiumMG,
			SugarG:          n.SugarG,
			SaturatedFatG:   n.SaturatedFatG,
			TransFatG:       n.TransFatG,
			CholesterolMG:   n.CholesterolMG,
			AddedSugarsG:    n.AddedSugarsG,
			CalciumMG:       n.CalciumMG,
			IronMG:          n.IronMG,
			PotassiumMG:     n.PotassiumMG,
			VitaminDMCG:     n.VitaminDMCG,
			VitaminAMCG:     n.VitaminAMCG,
			VitaminB6MG:     n.VitaminB6MG,
			VitaminB12MCG:   n.VitaminB12MCG,
			VitaminCMG:      n.VitaminCMG,
			IngredientsText: n.IngredientsText,
		})
	}

	// Upsert with conflict resolution on recipe_id
	headers := map[string]string{
		"Prefer": "resolution=merge-duplicates",
	}

	_, err := db.request(ctx, http.MethodPost, "/rest/v1/nutrition", rows, headers)
	if err != nil {
		return fmt.Errorf("upsert nutrition: %w", err)
	}

	return nil
}

// allergenRow is the database representation of allergen data.
// Schema: recipe_id (PK), contains (JSONB), ingredients_text
type allergenRow struct {
	RecipeID        string          `json:"recipe_id"`
	Contains        map[string]bool `json:"contains"`
	IngredientsText string          `json:"ingredients_text"`
}

// UpsertAllergens upserts allergen records from nutrition data.
func (db *DB) UpsertAllergens(ctx context.Context, nutrition []models.Nutrition) error {
	// Build one row per recipe with allergens as JSONB map
	var rows []allergenRow
	for _, n := range nutrition {
		if len(n.Allergens) == 0 && n.IngredientsText == "" {
			continue
		}

		contains := make(map[string]bool)
		for _, allergen := range n.Allergens {
			contains[allergen] = true
		}

		rows = append(rows, allergenRow{
			RecipeID:        n.RecipeID,
			Contains:        contains,
			IngredientsText: n.IngredientsText,
		})
	}

	if len(rows) == 0 {
		return nil
	}

	// Upsert with conflict resolution on recipe_id
	headers := map[string]string{
		"Prefer": "resolution=merge-duplicates",
	}

	_, err := db.request(ctx, http.MethodPost, "/rest/v1/allergens", rows, headers)
	if err != nil {
		return fmt.Errorf("upsert allergens: %w", err)
	}

	return nil
}

// ingestRunDetails is the JSONB details for an ingest run.
type ingestRunDetails struct {
	Date             string   `json:"date"`
	MenuItems        int      `json:"menu_items"`
	NutritionRecords int      `json:"nutrition_records"`
	AllergenRecords  int      `json:"allergen_records"`
	Errors           []string `json:"errors,omitempty"`
	FetchStats       struct {
		TotalRequests int `json:"total_requests"`
		CacheHits     int `json:"cache_hits"`
		CacheMisses   int `json:"cache_misses"`
		Errors        int `json:"errors"`
	} `json:"fetch_stats"`
}

// ingestRunRow is the database representation of an ingest run.
// Schema: id, started_at, finished_at, status, details (JSONB)
type ingestRunRow struct {
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
	Status     string           `json:"status"`
	Details    ingestRunDetails `json:"details"`
}

// LogIngestRun logs an ingest run to the database.
func (db *DB) LogIngestRun(ctx context.Context, report models.IngestReport) error {
	row := ingestRunRow{
		StartedAt:  report.StartedAt,
		FinishedAt: report.FinishedAt,
		Status:     report.Status,
		Details: ingestRunDetails{
			Date:             report.Date,
			MenuItems:        report.MenuItems,
			NutritionRecords: report.NutritionRecords,
			AllergenRecords:  report.AllergenRecords,
			Errors:           report.Errors,
			FetchStats: struct {
				TotalRequests int `json:"total_requests"`
				CacheHits     int `json:"cache_hits"`
				CacheMisses   int `json:"cache_misses"`
				Errors        int `json:"errors"`
			}{
				TotalRequests: report.FetchStats.TotalRequests,
				CacheHits:     report.FetchStats.CacheHits,
				CacheMisses:   report.FetchStats.CacheMisses,
				Errors:        report.FetchStats.Errors,
			},
		},
	}

	_, err := db.request(ctx, http.MethodPost, "/rest/v1/ingest_runs", row, nil)
	if err != nil {
		return fmt.Errorf("log ingest run: %w", err)
	}

	return nil
}

// locationNameToID maps location names to database IDs.
func locationNameToID(name string) int {
	for _, loc := range models.AllLocations {
		if loc.Name == name {
			return loc.ID
		}
	}
	return 0
}

// GetExistingRecipeIDs returns the set of recipe IDs already in the database.
// This can be used to skip fetching recipes we already have nutrition for.
func (db *DB) GetExistingRecipeIDs(ctx context.Context) (map[string]bool, error) {
	resp, err := db.request(ctx, http.MethodGet, "/rest/v1/nutrition?select=recipe_id", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get existing recipes: %w", err)
	}

	var records []struct {
		RecipeID string `json:"recipe_id"`
	}
	if err := json.Unmarshal(resp, &records); err != nil {
		return nil, fmt.Errorf("unmarshal recipes: %w", err)
	}

	ids := make(map[string]bool)
	for _, r := range records {
		ids[r.RecipeID] = true
	}

	return ids, nil
}

// Ping checks if the database is reachable.
func (db *DB) Ping(ctx context.Context) error {
	_, err := db.request(ctx, http.MethodGet, "/rest/v1/locations?select=id&limit=1", nil, nil)
	if err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// recipeComponentRow is the database representation of a BYO component relationship.
type recipeComponentRow struct {
	ParentRecipeID      string   `json:"parent_recipe_id"`
	ComponentRecipeID   string   `json:"component_recipe_id"`
	Category            string   `json:"category"`
	CategoryDisplayName *string  `json:"category_display_name"`
	CategoryOrder       int      `json:"category_order"`
	MinSelections       int      `json:"min_selections"`
	MaxSelections       *int     `json:"max_selections"`
	IsDefault           bool     `json:"is_default"`
	DisplayOrder        int      `json:"display_order"`
	DefaultQuantity     *float64 `json:"default_quantity"`
}

// UpsertRecipeComponents upserts BYO component relationships to the database.
func (db *DB) UpsertRecipeComponents(ctx context.Context, components []models.RecipeComponent) error {
	if len(components) == 0 {
		return nil
	}

	// Deduplicate by unique constraint key (parent_recipe_id, component_recipe_id)
	// PostgreSQL ON CONFLICT cannot handle duplicate keys within the same batch
	seen := make(map[string]bool)
	rows := make([]recipeComponentRow, 0, len(components))

	for _, c := range components {
		// Build dedup key matching the unique constraint
		dedupKey := c.ParentRecipeID + "|" + c.ComponentRecipeID

		if seen[dedupKey] {
			continue // Skip duplicate within batch
		}
		seen[dedupKey] = true

		var categoryDisplayName *string
		if c.CategoryDisplayName != "" {
			categoryDisplayName = &c.CategoryDisplayName
		}

		var defaultQty *float64
		if c.DefaultQuantity != 0 {
			qty := c.DefaultQuantity
			defaultQty = &qty
		}

		rows = append(rows, recipeComponentRow{
			ParentRecipeID:      c.ParentRecipeID,
			ComponentRecipeID:   c.ComponentRecipeID,
			Category:            c.Category,
			CategoryDisplayName: categoryDisplayName,
			CategoryOrder:       c.CategoryOrder,
			MinSelections:       c.MinSelections,
			MaxSelections:       c.MaxSelections,
			IsDefault:           c.IsDefault,
			DisplayOrder:        c.DisplayOrder,
			DefaultQuantity:     defaultQty,
		})
	}

	// Upsert with conflict resolution on (parent_recipe_id, component_recipe_id)
	headers := map[string]string{
		"Prefer": "resolution=merge-duplicates",
	}

	_, err := db.request(ctx, http.MethodPost, "/rest/v1/recipe_components?on_conflict=parent_recipe_id,component_recipe_id", rows, headers)
	if err != nil {
		return fmt.Errorf("upsert recipe components: %w", err)
	}

	return nil
}

// hoursRow is the database representation of location hours.
type hoursRow struct {
	LocationID     int     `json:"location_id"`
	Date           string  `json:"date"`
	BreakfastStart *string `json:"breakfast_start"`
	BreakfastEnd   *string `json:"breakfast_end"`
	LunchStart     *string `json:"lunch_start"`
	LunchEnd       *string `json:"lunch_end"`
	DinnerStart    *string `json:"dinner_start"`
	DinnerEnd      *string `json:"dinner_end"`
	LateNightStart *string `json:"late_night_start"`
	LateNightEnd   *string `json:"late_night_end"`
}

// timeToHHMM converts a time.Time to HH:MM string format.
func timeToHHMM(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("15:04")
	return &s
}

// UpsertHours upserts location hours to the database.
func (db *DB) UpsertHours(ctx context.Context, hours []models.LocationHours) error {
	if len(hours) == 0 {
		return nil
	}

	rows := make([]hoursRow, 0, len(hours))
	for _, h := range hours {
		locationID := locationNameToID(h.LocationName)
		if locationID == 0 {
			continue
		}

		rows = append(rows, hoursRow{
			LocationID:     locationID,
			Date:           h.Date,
			BreakfastStart: timeToHHMM(h.BreakfastStart),
			BreakfastEnd:   timeToHHMM(h.BreakfastEnd),
			LunchStart:     timeToHHMM(h.LunchStart),
			LunchEnd:       timeToHHMM(h.LunchEnd),
			DinnerStart:    timeToHHMM(h.DinnerStart),
			DinnerEnd:      timeToHHMM(h.DinnerEnd),
			LateNightStart: timeToHHMM(h.LateNightStart),
			LateNightEnd:   timeToHHMM(h.LateNightEnd),
		})
	}

	if len(rows) == 0 {
		return nil
	}

	headers := map[string]string{
		"Prefer": "resolution=merge-duplicates",
	}

	_, err := db.request(ctx, http.MethodPost, "/rest/v1/hours?on_conflict=location_id,date", rows, headers)
	if err != nil {
		return fmt.Errorf("upsert hours: %w", err)
	}

	return nil
}
