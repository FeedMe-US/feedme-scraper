package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jackwlutz/feedme/scraper/internal/fetch"
)

var cacheCmd = &cobra.Command{
	Use:   "clear-cache",
	Short: "Clear the response cache",
	Long:  `Remove all cached HTTP responses from the cache directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return clearCache()
	},
}

func init() {
	rootCmd.AddCommand(cacheCmd)

	cacheCmd.Flags().StringVar(&cacheDir, "cache-dir", ".cache", "Cache directory")
}

func clearCache() error {
	cache, err := fetch.NewCache(cacheDir, 24*time.Hour)
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}

	count, err := cache.Count()
	if err != nil {
		return fmt.Errorf("count cache: %w", err)
	}

	if count == 0 {
		logger.Info("Cache is already empty")
		return nil
	}

	logger.Info("Clearing cache", "entries", count)

	if err := cache.Clear(); err != nil {
		return fmt.Errorf("clear cache: %w", err)
	}

	logger.Info("Cache cleared")
	return nil
}
