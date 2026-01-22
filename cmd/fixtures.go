package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/FeedMe-US/feedme-scraper/internal/fetch"
	"github.com/FeedMe-US/feedme-scraper/internal/models"
)

var outputDir string

var fixturesCmd = &cobra.Command{
	Use:   "collect-fixtures",
	Short: "Collect HTML fixtures for testing",
	Long: `Fetch and save HTML pages from UCLA dining for use in tests.

This saves:
  - Each dining hall's menu page
  - Sample recipe detail pages
  - Hours page
  - Menus overview page`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return collectFixtures()
	},
}

func init() {
	rootCmd.AddCommand(fixturesCmd)

	fixturesCmd.Flags().StringVarP(&outputDir, "output", "o", "testdata", "Output directory for fixtures")
}

func collectFixtures() error {
	ctx := context.Background()

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Create client without cache (we want fresh data)
	client := fetch.New(
		fetch.WithRateLimit(1.0),
		fetch.WithTimeout(30*time.Second),
	)

	logger.Info("Collecting fixtures", "output", outputDir)

	// Fetch all hall pages
	for _, loc := range models.AllLocations {
		logger.Info("Fetching", "location", loc.Name)

		html, _, err := client.Get(ctx, loc.URL)
		if err != nil {
			logger.Warn("Failed to fetch", "location", loc.Name, "error", err)
			continue
		}

		filename := filepath.Join(outputDir, loc.Slug+".html")
		if err := os.WriteFile(filename, []byte(html), 0644); err != nil {
			logger.Warn("Failed to write", "file", filename, "error", err)
			continue
		}

		logger.Info("Saved", "file", filename)
	}

	// Fetch menus overview page
	logger.Info("Fetching menus overview")
	if html, _, err := client.Get(ctx, models.MenusOverviewURL); err == nil {
		filename := filepath.Join(outputDir, "menus-overview.html")
		if err := os.WriteFile(filename, []byte(html), 0644); err != nil {
			logger.Warn("Failed to write", "file", filename, "error", err)
		} else {
			logger.Info("Saved", "file", filename)
		}
	} else {
		logger.Warn("Failed to fetch menus overview", "error", err)
	}

	// Fetch hours page
	logger.Info("Fetching hours")
	if html, _, err := client.Get(ctx, models.HoursURL); err == nil {
		filename := filepath.Join(outputDir, "hours.html")
		if err := os.WriteFile(filename, []byte(html), 0644); err != nil {
			logger.Warn("Failed to write", "file", filename, "error", err)
		} else {
			logger.Info("Saved", "file", filename)
		}
	} else {
		logger.Warn("Failed to fetch hours", "error", err)
	}

	// Fetch a few sample recipe pages
	sampleRecipeIDs := []string{"813", "1922", "7306", "1976", "2692"}
	for _, id := range sampleRecipeIDs {
		logger.Info("Fetching recipe", "id", id)

		url := fmt.Sprintf(models.RecipeURLFormat, id)
		html, _, err := client.Get(ctx, url)
		if err != nil {
			logger.Warn("Failed to fetch recipe", "id", id, "error", err)
			continue
		}

		filename := filepath.Join(outputDir, fmt.Sprintf("recipe-%s.html", id))
		if err := os.WriteFile(filename, []byte(html), 0644); err != nil {
			logger.Warn("Failed to write", "file", filename, "error", err)
			continue
		}

		logger.Info("Saved", "file", filename)
	}

	logger.Info("Fixtures collection complete")
	return nil
}
