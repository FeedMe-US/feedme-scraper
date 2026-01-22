package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/FeedMe-US/feedme-scraper/internal/db"
	"github.com/FeedMe-US/feedme-scraper/internal/fetch"
	"github.com/FeedMe-US/feedme-scraper/internal/models"
	"github.com/FeedMe-US/feedme-scraper/internal/scraper"
)

var (
	dateStr       string
	location      string
	dryRun        bool
	cacheDir      string
	rateLimit     float64
	maxWorkers    int
	days          int    // Number of days to scrape (1 = today only, 7 = week ahead)
	metricsOutput string // File path to write Prometheus metrics
	pushMetrics   bool   // Push metrics to Grafana Cloud
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the scraper",
	Long: `Run the scraper to fetch menu data from UCLA dining halls.

By default, fetches data for today's date. Use --date to specify a different date.
Use --dry-run to parse data without writing to the database.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runScraper()
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVar(&dateStr, "date", "", "Target date (YYYY-MM-DD), defaults to today")
	runCmd.Flags().IntVar(&days, "days", 1, "Number of days to scrape (1 = today only, 7 = week ahead)")
	runCmd.Flags().StringVar(&location, "location", "", "Single location to scrape (for testing)")
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Parse only, skip database writes")
	runCmd.Flags().StringVar(&cacheDir, "cache-dir", ".cache", "Cache directory")
	runCmd.Flags().Float64Var(&rateLimit, "rate-limit", 1.0, "Requests per second")
	runCmd.Flags().IntVar(&maxWorkers, "workers", 10, "Max concurrent workers")
	runCmd.Flags().StringVar(&metricsOutput, "metrics-output", "", "File path to write Prometheus metrics (optional)")
	runCmd.Flags().BoolVar(&pushMetrics, "push-metrics", false, "Push metrics to Grafana Cloud (requires GRAFANA_METRICS_* env vars)")
}

func runScraper() error {
	ctx := context.Background()

	// Parse date
	var date time.Time
	if dateStr != "" {
		var err error
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return fmt.Errorf("invalid date format: %w", err)
		}
	} else {
		date = time.Now()
	}

	// Set up cache
	var cache *fetch.Cache
	if !noCache {
		var err error
		cache, err = fetch.NewCache(cacheDir, 24*time.Hour)
		if err != nil {
			return fmt.Errorf("create cache: %w", err)
		}
	}

	// Set up HTTP client
	clientOpts := []fetch.ClientOption{
		fetch.WithRateLimit(rateLimit),
		fetch.WithTimeout(30 * time.Second),
	}
	if cache != nil {
		clientOpts = append(clientOpts, fetch.WithCache(cache))
	}
	client := fetch.New(clientOpts...)

	// Set up database client
	var database *db.DB
	if !dryRun {
		supabaseURL := os.Getenv("SUPABASE_URL")
		supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

		if supabaseURL == "" || supabaseKey == "" {
			logger.Warn("SUPABASE_URL or SUPABASE_SERVICE_KEY not set, running in dry-run mode")
			dryRun = true
		} else {
			database = db.New(supabaseURL, supabaseKey)

			// Test connection
			if err := database.Ping(ctx); err != nil {
				logger.Warn("Could not connect to database, running in dry-run mode", "error", err)
				dryRun = true
				database = nil
			}
		}
	}

	// Create scraper
	s := scraper.New(scraper.Options{
		Client:     client,
		DB:         database,
		Logger:     logger,
		MaxWorkers: maxWorkers,
		DryRun:     dryRun,
	})

	// Run scraper
	var report *models.IngestReport
	var err error

	if location != "" {
		// Single location mode (for testing)
		items, err := s.FetchSingleLocation(ctx, location, date)
		if err != nil {
			return fmt.Errorf("fetch location: %w", err)
		}

		logger.Info("Fetched single location",
			"location", location,
			"items", len(items),
		)

		// Print items as JSON
		output, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	// Full scrape - loop through all requested days
	var allReports []*models.IngestReport
	var totalItems, totalNutrition int
	var anyFailed bool

	for i := 0; i < days; i++ {
		currentDate := date.AddDate(0, 0, i)
		logger.Info("Scraping day", "date", currentDate.Format("2006-01-02"), "day", i+1, "of", days)

		report, err = s.Run(ctx, currentDate)
		if err != nil {
			logger.Error("Scrape failed for date", "date", currentDate.Format("2006-01-02"), "error", err)
			anyFailed = true
			continue
		}

		allReports = append(allReports, report)
		totalItems += report.MenuItems
		totalNutrition += report.NutritionRecords

		if report.Status == "failed" {
			anyFailed = true
		}
	}

	// Print summary
	fmt.Println()
	fmt.Println("=== Scrape Summary ===")
	if days > 1 {
		fmt.Printf("Days:       %d (from %s to %s)\n", days,
			date.Format("2006-01-02"),
			date.AddDate(0, 0, days-1).Format("2006-01-02"))
	} else {
		fmt.Printf("Date:       %s\n", date.Format("2006-01-02"))
	}

	// Aggregate stats from all reports
	var totalRequests, totalHits, totalMisses int
	var totalDuration time.Duration
	var allErrors []string

	for _, r := range allReports {
		totalRequests += r.FetchStats.TotalRequests
		totalHits += r.FetchStats.CacheHits
		totalMisses += r.FetchStats.CacheMisses
		totalDuration += r.FetchStats.TotalDuration
		allErrors = append(allErrors, r.Errors...)
	}

	status := "ok"
	if anyFailed {
		status = "failed"
	} else if len(allErrors) > 0 {
		status = "warn"
	}

	fmt.Printf("Status:     %s\n", status)
	fmt.Printf("Duration:   %s\n", totalDuration)
	fmt.Printf("Menu Items: %d\n", totalItems)
	fmt.Printf("Nutrition:  %d\n", totalNutrition)
	fmt.Printf("Requests:   %d (cache hits: %d, misses: %d)\n",
		totalRequests, totalHits, totalMisses)

	if len(allErrors) > 0 {
		fmt.Printf("\nErrors (%d):\n", len(allErrors))
		for _, e := range allErrors {
			fmt.Printf("  - %s\n", e)
		}
	}

	// Finalize metrics
	// Calculate items missing nutrition (items without recipe_id can't have nutrition)
	itemsMissingNutrition := 0
	if totalItems > totalNutrition {
		itemsMissingNutrition = totalItems - totalNutrition
	}

	s.Metrics.SetTotals(totalItems, totalNutrition, itemsMissingNutrition, len(allErrors), days)
	s.Metrics.Finish(!anyFailed)

	// Output metrics to file if requested
	if metricsOutput != "" {
		metricsData := s.Metrics.ToPrometheus()
		if err := os.WriteFile(metricsOutput, []byte(metricsData), 0644); err != nil {
			logger.Error("Failed to write metrics file", "error", err)
		} else {
			logger.Info("Metrics written", "file", metricsOutput)
		}
	}

	// Push metrics to Grafana Cloud if requested
	if pushMetrics {
		grafanaURL := os.Getenv("GRAFANA_METRICS_URL")
		grafanaUser := os.Getenv("GRAFANA_METRICS_USER")
		grafanaKey := os.Getenv("GRAFANA_METRICS_API_KEY")

		if grafanaURL == "" || grafanaUser == "" || grafanaKey == "" {
			logger.Warn("GRAFANA_METRICS_* env vars not set, skipping metrics push")
		} else {
			if err := s.Metrics.Push(grafanaURL, grafanaUser, grafanaKey); err != nil {
				logger.Error("Failed to push metrics to Grafana", "error", err)
			} else {
				logger.Info("Metrics pushed to Grafana Cloud")
			}
		}
	}

	if anyFailed {
		return fmt.Errorf("scrape failed for one or more days")
	}

	return nil
}
