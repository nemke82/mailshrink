package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nemke82/mailshrink/internal/estimator"
	"github.com/nemke82/mailshrink/internal/maildir"
	"github.com/nemke82/mailshrink/internal/output"
	"github.com/nemke82/mailshrink/internal/scanner"
)

var (
	planOlderThan string
	planFolder    string
)

var planCmd = &cobra.Command{
	Use:   "plan [domain]",
	Short: "Show detailed per-account compression plan",
	Long: `Plan shows a detailed breakdown of compressible messages for each
account and folder, with estimated savings. This helps you decide
which mailboxes to target before running compress.

Examples:
  mailshrink plan                    Plan for all domains
  mailshrink plan example.com        Plan for a specific domain
  mailshrink plan --folder Sent      Only show Sent folders`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlan,
}

func init() {
	planCmd.Flags().StringVar(&planOlderThan, "older-than", "",
		"Only consider messages older than this (e.g., 2y, 6m, 90d)")
	planCmd.Flags().StringVar(&planFolder, "folder", "",
		"Restrict plan to a specific folder (e.g., Sent, INBOX)")

	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	output.PrintBanner(Version)

	var filterDomain string
	if len(args) == 1 {
		filterDomain = args[0]
	}

	olderThan, err := parseDuration(planOlderThan)
	if err != nil {
		return fmt.Errorf("invalid --older-than: %w", err)
	}

	scanOpts := scanner.ScanOptions{
		BasePath:   flagPath,
		Provider:   flagProvider,
		Domain:     filterDomain,
		FolderName: planFolder,
		Filter: maildir.MessageFilter{
			OlderThan:        olderThan,
			UncompressedOnly: true,
		},
	}

	output.PrintScanningMessage(flagPath)

	stats, err := scanner.GetAccountStats(scanOpts)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	// Estimate savings for each account/folder combination.
	estimates := make(map[string]*estimator.EstimateResult)
	for _, stat := range stats {
		key := stat.Account + "/" + stat.Folder
		estOpts := estimator.DefaultOptions()
		estOpts.SampleSize = 50 // Smaller sample per folder for speed.
		est, err := estimator.Estimate(stat.Messages, estOpts)
		if err == nil {
			estimates[key] = est
		}
	}

	if flagJSON {
		type planEntry struct {
			Account  string `json:"account"`
			Folder   string `json:"folder"`
			Period   string `json:"period"`
			Size     int64  `json:"size"`
			Files    int64  `json:"files"`
			Estimate *estimator.EstimateResult `json:"estimate,omitempty"`
		}

		var entries []planEntry
		for _, stat := range stats {
			key := stat.Account + "/" + stat.Folder
			entries = append(entries, planEntry{
				Account:  stat.Account,
				Folder:   stat.Folder,
				Period:   stat.Period,
				Size:     stat.TotalSize,
				Files:    stat.FileCount,
				Estimate: estimates[key],
			})
		}

		return output.PrintJSON(entries)
	}

	if filterDomain != "" {
		fmt.Printf("  Plan for %s\n", filterDomain)
	} else {
		fmt.Printf("  Plan for all domains")
		// Show which domains were found.
		domains := make(map[string]bool)
		for _, stat := range stats {
			domains[stat.Domain] = true
		}
		if len(domains) > 0 {
			names := make([]string, 0, len(domains))
			for d := range domains {
				names = append(names, d)
			}
			fmt.Printf(" (%s)", strings.Join(names, ", "))
		}
		fmt.Println()
	}
	fmt.Println()

	output.PrintPlanTable(stats, estimates)
	return nil
}
