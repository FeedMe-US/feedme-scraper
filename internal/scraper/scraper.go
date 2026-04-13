// Package scraper orchestrates the menu scraping process.
package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/FeedMe-US/feedme-scraper/internal/db"
	"github.com/FeedMe-US/feedme-scraper/internal/fetch"
	"github.com/FeedMe-US/feedme-scraper/internal/metrics"
	"github.com/FeedMe-US/feedme-scraper/internal/models"
	"github.com/FeedMe-US/feedme-scraper/internal/parse"
)

// Scraper orchestrates the menu scraping process.
type Scraper struct {
	client     *fetch.Client
	db         *db.DB
	logger     *slog.Logger
	maxWorkers int
	dryRun     bool
	Metrics    *metrics.Collector
}

// Options configures the scraper.
type Options struct {
	Client     *fetch.Client
	DB         *db.DB
	Logger     *slog.Logger
	MaxWorkers int
	DryRun     bool
}

// New creates a new Scraper.
func New(opts Options) *Scraper {
	if opts.MaxWorkers <= 0 {
		opts.MaxWorkers = 10
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Scraper{
		client:     opts.Client,
		db:         opts.DB,
		logger:     opts.Logger,
		maxWorkers: opts.MaxWorkers,
		dryRun:     opts.DryRun,
		Metrics:    metrics.New(),
	}
}

// Run executes the full scrape flow for a given date.
func (s *Scraper) Run(ctx context.Context, date time.Time) (*models.IngestReport, error) {
	report := &models.IngestReport{
		Date:      date.Format("2006-01-02"),
		StartedAt: time.Now(),
		Status:    "ok",
	}

	s.logger.Info("Starting scrape", "date", report.Date)

	// Step 1: Fetch all hall pages
	s.logger.Info("Fetching hall menus...")
	items, hallErrors := s.fetchAllHalls(ctx, date)
	report.MenuItems = len(items)
	report.Errors = append(report.Errors, hallErrors...)

	if len(items) == 0 {
		s.logger.Warn("No menu items found")
		report.Status = "warn"
	}

	// Step 2: Collect unique recipe IDs
	recipeIDs := parse.ExtractRecipeIDs(items)
	missingItems := parse.GetItemsWithoutRecipeID(items)
	s.logger.Info("Found unique recipes", "count", len(recipeIDs), "items_without_recipe_id", len(missingItems))

	// Log details of items missing recipe_id
	for _, item := range missingItems {
		s.logger.Debug("Missing recipe_id", "name", item.Name, "location", item.Location, "meal", item.Meal, "section", item.Section)
	}

	// Step 2b: Identify BYO items that need component extraction
	byoItems := s.identifyBYOItems(items)
	s.logger.Info("Found BYO items", "count", len(byoItems))

	// Step 3: Fetch all recipe pages
	s.logger.Info("Fetching recipe details...")
	nutrition, recipeErrors := s.fetchAllRecipes(ctx, recipeIDs)
	report.NutritionRecords = len(nutrition)
	report.Errors = append(report.Errors, recipeErrors...)

	// Step 3b: Extract components from BYO items and fetch their nutrition
	var allComponents []models.RecipeComponent
	if len(byoItems) > 0 {
		s.logger.Info("Extracting BYO components...")
		components, componentNutrition, componentErrors := s.extractBYOComponents(ctx, byoItems)
		allComponents = components
		report.Errors = append(report.Errors, componentErrors...)

		// Add component nutrition to main nutrition list (if not already present)
		componentRecipeIDs := make(map[string]bool)
		for _, n := range nutrition {
			componentRecipeIDs[n.RecipeID] = true
		}
		for _, n := range componentNutrition {
			if !componentRecipeIDs[n.RecipeID] {
				nutrition = append(nutrition, n)
				componentRecipeIDs[n.RecipeID] = true
			}
		}
		report.NutritionRecords = len(nutrition)

		s.logger.Info("Extracted BYO components",
			"total_components", len(components),
			"new_nutrition_records", len(componentNutrition),
		)
	}

	// Step 4: Fetch hours page (optional, non-blocking)
	s.logger.Info("Fetching hours...")
	hours, hoursErr := s.fetchHours(ctx, date)
	if hoursErr != nil {
		report.Errors = append(report.Errors, hoursErr.Error())
		s.logger.Warn("Failed to fetch hours", "error", hoursErr)
	} else {
		s.logger.Info("Parsed hours", "locations", len(hours))
	}

	// Step 5: Update fetch stats
	total, hits, misses, errs := s.client.Stats()
	report.FetchStats = models.FetchStats{
		TotalRequests: total,
		CacheHits:     hits,
		CacheMisses:   misses,
		Errors:        errs,
	}

	// Step 6: Upsert to database (unless dry-run)
	// Order matters: nutrition first (creates recipe_id), then allergens, then menu_items (FK reference)
	if !s.dryRun && s.db != nil {
		s.logger.Info("Upserting to database...")

		// Insert nutrition FIRST - creates recipe_id for FK references
		if len(nutrition) > 0 {
			if err := s.db.UpsertNutrition(ctx, nutrition); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("upsert nutrition: %v", err))
				s.logger.Error("Failed to upsert nutrition", "error", err)
			}

			// Upsert allergens (derived from nutrition records)
			if err := s.db.UpsertAllergens(ctx, nutrition); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("upsert allergens: %v", err))
				s.logger.Error("Failed to upsert allergens", "error", err)
			} else {
				report.AllergenRecords = len(nutrition)
			}
		}

		// Insert menu_items - references nutrition.recipe_id
		if len(items) > 0 {
			if err := s.db.UpsertMenuItems(ctx, items); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("upsert menu items: %v", err))
				s.logger.Error("Failed to upsert menu items", "error", err)
			}
		}

		// Insert BYO components LAST - references nutrition.recipe_id for both parent and component
		if len(allComponents) > 0 {
			if err := s.db.UpsertRecipeComponents(ctx, allComponents); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("upsert recipe components: %v", err))
				s.logger.Error("Failed to upsert recipe components", "error", err)
			} else {
				s.logger.Info("Upserted recipe components", "count", len(allComponents))
			}
		}

		// Upsert hours (date-specific operating hours)
		if len(hours) > 0 {
			if err := s.db.UpsertHours(ctx, hours); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("upsert hours: %v", err))
				s.logger.Error("Failed to upsert hours", "error", err)
			} else {
				s.logger.Info("Upserted hours", "locations", len(hours))
			}
		}

		// Log ingest run
		if err := s.db.LogIngestRun(ctx, *report); err != nil {
			s.logger.Error("Failed to log ingest run", "error", err)
		}
	} else {
		s.logger.Info("Dry run - skipping database writes")
	}

	// Step 7: Finalize report
	report.FinishedAt = time.Now()
	if len(report.Errors) > 0 {
		if report.Status == "ok" {
			report.Status = "warn"
		}
		if len(items) == 0 && len(nutrition) == 0 {
			report.Status = "failed"
		}
	}

	duration := report.FinishedAt.Sub(report.StartedAt)
	report.FetchStats.TotalDuration = duration

	s.logger.Info("Scrape complete",
		"status", report.Status,
		"items", report.MenuItems,
		"nutrition", report.NutritionRecords,
		"duration", duration,
		"errors", len(report.Errors),
	)

	return report, nil
}

// fetchAllHalls fetches all dining hall pages concurrently.
func (s *Scraper) fetchAllHalls(ctx context.Context, date time.Time) ([]models.MenuItem, []string) {
	var allItems []models.MenuItem
	var allErrors []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Semaphore for max concurrent workers
	sem := make(chan struct{}, s.maxWorkers)

	dateStr := date.Format("2006-01-02")

	for _, loc := range models.AllLocations {
		loc := loc // Capture loop variable
		wg.Add(1)

		go func() {
			defer wg.Done()

			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			hallStart := time.Now()

			// Append date parameter to URL to fetch specific day's menu
			url := loc.URL + "?date=" + dateStr
			s.logger.Debug("Fetching", "location", loc.Name, "date", dateStr)

			html, fromCache, err := s.client.Get(ctx, url)
			if err != nil {
				mu.Lock()
				allErrors = append(allErrors, fmt.Sprintf("%s: %v", loc.Name, err))
				mu.Unlock()
				s.logger.Warn("Failed to fetch", "location", loc.Name, "error", err)
				s.Metrics.RecordHall(loc.Name, 0, time.Since(hallStart), err)
				return
			}

			cacheStatus := "fresh"
			if fromCache {
				cacheStatus = "cached"
			}

			items, err := parse.ParseHallMenu(html, loc.Name, dateStr)
			if err != nil {
				mu.Lock()
				allErrors = append(allErrors, fmt.Sprintf("%s parse: %v", loc.Name, err))
				mu.Unlock()
				s.logger.Warn("Failed to parse", "location", loc.Name, "error", err)
				s.Metrics.RecordHall(loc.Name, 0, time.Since(hallStart), err)
				return
			}

			mu.Lock()
			allItems = append(allItems, items...)
			mu.Unlock()

			// Record successful hall scrape
			s.Metrics.RecordHall(loc.Name, len(items), time.Since(hallStart), nil)

			s.logger.Info("Fetched hall",
				"location", loc.Name,
				"items", len(items),
				"cache", cacheStatus,
			)
		}()
	}

	wg.Wait()
	return allItems, allErrors
}

// fetchAllRecipes fetches all recipe detail pages concurrently.
func (s *Scraper) fetchAllRecipes(ctx context.Context, recipeIDs []string) ([]models.Nutrition, []string) {
	var allNutrition []models.Nutrition
	var allErrors []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Semaphore for max concurrent workers
	sem := make(chan struct{}, s.maxWorkers)

	for _, id := range recipeIDs {
		id := id // Capture loop variable
		wg.Add(1)

		go func() {
			defer wg.Done()

			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			url := fmt.Sprintf(models.RecipeURLFormat, id)

			html, _, err := s.client.Get(ctx, url)
			if err != nil {
				mu.Lock()
				allErrors = append(allErrors, fmt.Sprintf("recipe %s: %v", id, err))
				mu.Unlock()
				return
			}

			nutrition, err := parse.ParseRecipeWithHTTP(html, id, s.client.HTTPClient())
			if err != nil {
				mu.Lock()
				allErrors = append(allErrors, fmt.Sprintf("recipe %s parse: %v", id, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			allNutrition = append(allNutrition, *nutrition)
			mu.Unlock()
		}()
	}

	wg.Wait()
	return allNutrition, allErrors
}

// fetchHours fetches and parses the hours page.
func (s *Scraper) fetchHours(ctx context.Context, date time.Time) ([]models.LocationHours, error) {
	html, _, err := s.client.Get(ctx, models.HoursURL)
	if err != nil {
		return nil, fmt.Errorf("fetch hours: %w", err)
	}

	hours, err := parse.ParseHours(html, date)
	if err != nil {
		return nil, fmt.Errorf("parse hours: %w", err)
	}

	return hours, nil
}

// identifyBYOItems filters menu items to find Build-Your-Own items that need component extraction.
func (s *Scraper) identifyBYOItems(items []models.MenuItem) []models.MenuItem {
	var byoItems []models.MenuItem
	seen := make(map[string]bool) // Dedupe by recipe_id

	for _, item := range items {
		if item.RecipeID == "" {
			continue
		}
		if seen[item.RecipeID] {
			continue
		}
		if parse.IsBYOItem(item.Name) {
			byoItems = append(byoItems, item)
			seen[item.RecipeID] = true
			s.logger.Debug("Found BYO item", "name", item.Name, "recipe_id", item.RecipeID, "location", item.Location)
		}
	}

	return byoItems
}

// extractBYOComponents fetches BYO detail pages and extracts component information.
// Returns: components, additional nutrition records for components, errors
func (s *Scraper) extractBYOComponents(ctx context.Context, byoItems []models.MenuItem) ([]models.RecipeComponent, []models.Nutrition, []string) {
	var allComponents []models.RecipeComponent
	var allNutrition []models.Nutrition
	var allErrors []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Track component recipe IDs that need nutrition fetched
	componentIDsToFetch := make(map[string]bool)

	// Semaphore for max concurrent workers
	sem := make(chan struct{}, s.maxWorkers)

	// Step 1: Fetch and parse BYO detail pages for components
	for _, item := range byoItems {
		item := item
		wg.Add(1)

		go func() {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			url := fmt.Sprintf(models.RecipeURLFormat, item.RecipeID)

			html, _, err := s.client.Get(ctx, url)
			if err != nil {
				mu.Lock()
				allErrors = append(allErrors, fmt.Sprintf("BYO %s: fetch: %v", item.RecipeID, err))
				mu.Unlock()
				return
			}

			components, err := parse.ParseBYODetailPage(html, item.RecipeID)
			if err != nil {
				mu.Lock()
				allErrors = append(allErrors, fmt.Sprintf("BYO %s: parse components: %v", item.RecipeID, err))
				mu.Unlock()
				return
			}

			// Infer defaults if none were detected
			if len(components) > 0 {
				components = parse.InferDefaultComponents(components)
			}

			mu.Lock()
			allComponents = append(allComponents, components...)
			for _, c := range components {
				componentIDsToFetch[c.ComponentRecipeID] = true
			}
			mu.Unlock()

			s.logger.Debug("Extracted BYO components",
				"parent_recipe_id", item.RecipeID,
				"parent_name", item.Name,
				"component_count", len(components),
			)
		}()
	}

	wg.Wait()

	// Step 2: Fetch nutrition for all component recipe IDs
	if len(componentIDsToFetch) > 0 {
		var componentIDs []string
		for id := range componentIDsToFetch {
			componentIDs = append(componentIDs, id)
		}

		s.logger.Info("Fetching component nutrition", "count", len(componentIDs))
		nutrition, errors := s.fetchAllRecipes(ctx, componentIDs)
		allNutrition = nutrition
		allErrors = append(allErrors, errors...)
	}

	return allComponents, allNutrition, allErrors
}

// FetchSingleLocation fetches and parses a single location (for testing).
func (s *Scraper) FetchSingleLocation(ctx context.Context, locationName string, date time.Time) ([]models.MenuItem, error) {
	var loc *models.Location
	for _, l := range models.AllLocations {
		if l.Name == locationName {
			loc = &l
			break
		}
	}
	if loc == nil {
		return nil, fmt.Errorf("unknown location: %s", locationName)
	}

	html, _, err := s.client.Get(ctx, loc.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	items, err := parse.ParseHallMenu(html, loc.Name, date.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	return items, nil
}
