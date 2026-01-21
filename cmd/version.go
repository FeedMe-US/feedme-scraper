package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version info - can be set at build time with ldflags
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("feedme-scraper %s\n", Version)
		fmt.Printf("  Build time: %s\n", BuildTime)
		fmt.Printf("  Git commit: %s\n", GitCommit)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
