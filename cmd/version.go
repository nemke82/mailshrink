package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("MailShrink %s\n", Version)
		fmt.Printf("  Build date:  %s\n", BuildDate)
		fmt.Printf("  Go version:  %s\n", GoVersion)
		fmt.Printf("  Platform:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
