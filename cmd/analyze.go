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
	analyzeSampleSize int
	analyzeOlderThan  string
	analyzeFolder     string
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze [path|domain|account]",
	Short: "Scan mailboxes and estimate reclaimable disk space",
	Long: `Analyze scans the specified path (or the default /home) for Dovecot
Maildir mailboxes, samples messages to measure the actual compression
ratio, and reports how much space could be reclaimed.

Examples:
  mailshrink analyze                     Scan all mailboxes under /home
  mailshrink analyze --path /var/mail    Scan a custom path
  mailshrink analyze user@example.com    Analyze a specific account
  mailshrink analyze --older-than 2y     Only consider old messages`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAnalyze,
}

func init() {
	analyzeCmd.Flags().IntVar(&analyzeSampleSize, "sample-size", 100,
		"Number of messages to sample for compression ratio estimation")
	analyzeCmd.Flags().StringVar(&analyzeOlderThan, "older-than", "",
		"Only consider messages older than this (e.g., 2y, 6m, 90d)")
	analyzeCmd.Flags().StringVar(&analyzeFolder, "folder", "",
		"Restrict analysis to a specific folder (e.g., Sent, INBOX)")

	rootCmd.AddCommand(analyzeCmd)
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	output.PrintBanner(Version)

	basePath := flagPath
	var filterDomain, filterAccount string

	// If an argument is provided, determine if it's a path, domain, or account.
	if len(args) == 1 {
		arg := args[0]
		if strings.Contains(arg, "@") {
			filterAccount = arg
			// Extract domain for filtering.
			parts := strings.SplitN(arg, "@", 2)
			if len(parts) == 2 {
				filterDomain = parts[1]
			}
		} else if strings.Contains(arg, "/") {
			basePath = arg
		} else {
			filterDomain = arg
		}
	}

	// Parse age filter.
	olderThan, err := parseDuration(analyzeOlderThan)
	if err != nil {
		return fmt.Errorf("invalid --older-than: %w", err)
	}

	scanOpts := scanner.ScanOptions{
		BasePath:   basePath,
		Provider:   flagProvider,
		Domain:     filterDomain,
		Account:    filterAccount,
		FolderName: analyzeFolder,
		Filter: maildir.MessageFilter{
			OlderThan: olderThan,
		},
	}

	output.PrintScanningMessage(basePath)

	// Run the scan.
	result, err := scanner.Scan(scanOpts)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	output.PrintProviderDetected(result.Provider)

	// If a specific account was requested, show detailed per-account estimate.
	if filterAccount != "" {
		return runAccountAnalysis(result, scanOpts)
	}

	// For domain/path-level analysis, sample each domain and show the table.
	estimates := make(map[string]*estimator.EstimateResult)

	for domain, ds := range result.DomainStats {
		if ds.UncompressedFiles == 0 {
			continue
		}

		// Collect all uncompressed messages for this domain.
		var domainMessages []*maildir.Message
		for _, mb := range result.Mailboxes {
			if mb.Domain != domain {
				continue
			}
			for _, folder := range mb.Folders {
				if analyzeFolder != "" && !strings.EqualFold(folder.Name, analyzeFolder) {
					continue
				}
				msgs, err := maildir.ListMessages(folder, maildir.MessageFilter{
					OlderThan:        olderThan,
					UncompressedOnly: true,
				})
				if err != nil {
					continue
				}
				domainMessages = append(domainMessages, msgs...)
			}
		}

		if len(domainMessages) > 0 {
			estOpts := estimator.DefaultOptions()
			estOpts.SampleSize = analyzeSampleSize
			est, err := estimator.Estimate(domainMessages, estOpts)
			if err == nil {
				estimates[domain] = est
			}
		}
	}

	if flagJSON {
		return output.PrintJSON(map[string]interface{}{
			"provider":    result.Provider,
			"domains":     result.DomainStats,
			"estimates":   estimates,
			"total_size":  result.TotalSize,
			"total_files": result.TotalFiles,
		})
	}

	output.PrintDomainTableWithEstimate(result, estimates)
	return nil
}

// runAccountAnalysis handles the case where a specific account is analyzed.
func runAccountAnalysis(result *scanner.ScanResult, opts scanner.ScanOptions) error {
	// Find all uncompressed messages for this account.
	var allMessages []*maildir.Message
	var totalSize int64

	for _, mb := range result.Mailboxes {
		for _, folder := range mb.Folders {
			if opts.FolderName != "" && !strings.EqualFold(folder.Name, opts.FolderName) {
				continue
			}
			msgs, err := maildir.ListMessages(folder, opts.Filter)
			if err != nil {
				continue
			}
			for _, msg := range msgs {
				allMessages = append(allMessages, msg)
				totalSize += msg.PhysicalSize
			}
		}
	}

	// Run estimation.
	estOpts := estimator.DefaultOptions()
	estOpts.SampleSize = analyzeSampleSize
	est, err := estimator.Estimate(allMessages, estOpts)
	if err != nil {
		return fmt.Errorf("estimation failed: %w", err)
	}

	if flagJSON {
		return output.PrintJSON(map[string]interface{}{
			"account":       opts.Account,
			"total_size":    totalSize,
			"message_count": len(allMessages),
			"estimate":      est,
		})
	}

	output.PrintAccountEstimate(opts.Account, totalSize, len(allMessages), est)
	return nil
}
