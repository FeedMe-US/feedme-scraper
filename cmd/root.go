// Package cmd provides CLI commands for the scraper.
package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var (
	verbose bool
	noCache bool
	logger  *slog.Logger
)

// rootCmd represents the base command when called without subcommands.
var rootCmd = &cobra.Command{
	Use:   "feedme-scraper",
	Short: "UCLA dining menu scraper",
	Long: `FeedMe Scraper fetches menu data from UCLA dining halls.

It scrapes:
  - Daily menus for all residential and boutique locations
  - Nutrition facts for all recipes
  - Operating hours

Data is upserted to Supabase and runs daily via cron.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Set up logger based on verbosity
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: level,
		}))
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose/debug logging")
	rootCmd.PersistentFlags().BoolVar(&noCache, "no-cache", false, "Bypass cache and fetch fresh data")
}
