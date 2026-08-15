// Package cmd implements the MailShrink CLI commands using cobra.
package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Build-time variables set via -ldflags.
var (
	Version   = "2025.08-dev"
	BuildDate = "unknown"
	GoVersion = "unknown"
)

// Global flags.
var (
	flagPath        string
	flagVerbose     bool
	flagJSON        bool
	flagConcurrency int
	flagProvider    string
)

// rootCmd is the base command when called without subcommands.
var rootCmd = &cobra.Command{
	Use:   "mailshrink",
	Short: "Safely reclaim disk space on Dovecot Maildir servers",
	Long: `MailShrink analyzes and transparently compresses old email on Dovecot
Maildir servers, safely reclaiming disk space without deleting messages.

Supports cPanel, DirectAdmin, and any generic Maildir layout.

Get started:
  mailshrink analyze              Scan and estimate reclaimable space
  mailshrink plan <domain>        Show detailed per-account plan
  mailshrink compress --apply     Compress messages (dry-run by default)`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagPath, "path", "/home",
		"Base path to scan for mailboxes")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false,
		"Enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false,
		"Output results as JSON")
	rootCmd.PersistentFlags().IntVarP(&flagConcurrency, "concurrency", "j", 4,
		"Number of parallel workers")
	rootCmd.PersistentFlags().StringVar(&flagProvider, "provider", "",
		"Force discovery provider (cpanel, directadmin, generic)")
}

// parseDuration parses human-friendly duration strings like "2y", "6m", "90d".
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	s = strings.TrimSpace(strings.ToLower(s))

	// Try standard Go duration first.
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Parse custom formats: Ny, Nm, Nd.
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}

	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}

	switch unit {
	case 'y':
		return time.Duration(num) * 365 * 24 * time.Hour, nil
	case 'm':
		return time.Duration(num) * 30 * 24 * time.Hour, nil
	case 'd':
		return time.Duration(num) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration unit %q in %q (use y, m, or d)", string(unit), s)
	}
}

// parseDate parses a YYYY-MM-DD date string.
func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02", s)
}
