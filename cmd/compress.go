package cmd

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/nemke82/mailshrink/internal/compressor"
	"github.com/nemke82/mailshrink/internal/dovecot"
	"github.com/nemke82/mailshrink/internal/maildir"
	"github.com/nemke82/mailshrink/internal/output"
	"github.com/nemke82/mailshrink/internal/scanner"
)

var (
	compressDomain      string
	compressAccount     string
	compressFolder      string
	compressBefore      string
	compressOlderThn    string
	compressApply       bool
	compressLevel       int
	compressBatchSz     int
	compressSkipDoveChk bool
)

var compressCmd = &cobra.Command{
	Use:   "compress",
	Short: "Compress mailbox messages (dry-run by default)",
	Long: `Compress transparently gzip-compresses email messages in Dovecot
Maildir format. Compressed messages remain fully accessible through
IMAP — Dovecot's zlib plugin handles decompression on the fly.

IMPORTANT: This command is DRY-RUN by default. No files will be
modified unless you explicitly pass --apply.

Safety guarantees:
  • Dovecot zlib check — verifies plugin is enabled before compressing
  • Atomic operations — original file is untouched until compression succeeds
  • mtime preservation — IMAP INTERNALDATE is not affected
  • Ownership preservation — file UID/GID are maintained
  • Maildir locking — prevents concurrent access during compression
  • Already-compressed files are automatically skipped

Examples:
  mailshrink compress --domain example.com --folder Sent
  mailshrink compress --domain example.com --before 2025-01-01
  mailshrink compress --domain example.com --folder Sent --apply
  mailshrink compress --account user@example.com --older-than 2y --apply`,
	RunE: runCompress,
}

func init() {
	compressCmd.Flags().StringVar(&compressDomain, "domain", "",
		"Target domain")
	compressCmd.Flags().StringVar(&compressAccount, "account", "",
		"Target account (user@domain)")
	compressCmd.Flags().StringVar(&compressFolder, "folder", "",
		"Target folder (e.g., Sent, INBOX)")
	compressCmd.Flags().StringVar(&compressBefore, "before", "",
		"Compress messages before this date (YYYY-MM-DD)")
	compressCmd.Flags().StringVar(&compressOlderThn, "older-than", "",
		"Compress messages older than this (e.g., 2y, 6m, 90d)")
	compressCmd.Flags().BoolVar(&compressApply, "apply", false,
		"Actually modify files (without this flag, only a dry-run is performed)")
	compressCmd.Flags().IntVar(&compressLevel, "compression-level", 6,
		"Gzip compression level (1-9)")
	compressCmd.Flags().IntVar(&compressBatchSz, "batch-size", 1000,
		"Process files in batches for progress reporting")
	compressCmd.Flags().BoolVar(&compressSkipDoveChk, "skip-dovecot-check", false,
		"Skip Dovecot zlib verification (for non-standard setups)")

	rootCmd.AddCommand(compressCmd)
}

func runCompress(cmd *cobra.Command, args []string) error {
	output.PrintBanner(Version)

	// ── Dovecot zlib safety gate ────────────────────────────────
	// When --apply is set, verify Dovecot has zlib enabled.
	// This prevents compressing mail that Dovecot can't read.
	if compressApply && !compressSkipDoveChk {
		info := dovecot.Detect()

		if !info.Installed {
			red := color.New(color.FgRed, color.Bold)
			red.Println("  ✗ Dovecot is not installed on this system.")
			fmt.Println()
			fmt.Print(info.FixInstructions())
			fmt.Println()
			return fmt.Errorf("aborting: Dovecot not found")
		}

		if !info.ZlibEnabled {
			red := color.New(color.FgRed, color.Bold)
			yellow := color.New(color.FgYellow, color.Bold)
			dim := color.New(color.Faint)

			red.Println("  ✗ Dovecot's zlib plugin is NOT enabled.")
			fmt.Println()
			yellow.Println("  Compressing mail without zlib will make messages unreadable!")
			fmt.Println()
			fmt.Print(info.FixInstructions())
			dim.Println("  To skip this check (advanced users only):")
			dim.Println("    mailshrink compress --apply --skip-dovecot-check")
			fmt.Println()
			return fmt.Errorf("aborting: Dovecot zlib plugin not enabled")
		}

		if flagVerbose {
			green := color.New(color.FgGreen)
			green.Println("  ✓ Dovecot zlib check passed")
			fmt.Println()
		}
	}

	// ── Parse filters ───────────────────────────────────────────
	olderThan, err := parseDuration(compressOlderThn)
	if err != nil {
		return fmt.Errorf("invalid --older-than: %w", err)
	}

	before, err := parseDate(compressBefore)
	if err != nil {
		return fmt.Errorf("invalid --before: %w", err)
	}

	// Extract domain from account if provided.
	domain := compressDomain
	if compressAccount != "" && domain == "" {
		parts := strings.SplitN(compressAccount, "@", 2)
		if len(parts) == 2 {
			domain = parts[1]
		}
	}

	scanOpts := scanner.ScanOptions{
		BasePath:   flagPath,
		Provider:   flagProvider,
		Domain:     domain,
		Account:    compressAccount,
		FolderName: compressFolder,
		Filter: maildir.MessageFilter{
			Before:           before,
			OlderThan:        olderThan,
			UncompressedOnly: true,
		},
	}

	output.PrintScanningMessage(flagPath)

	// Discover matching messages.
	stats, err := scanner.GetAccountStats(scanOpts)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	// Collect all messages.
	var allMessages []*maildir.Message
	for _, stat := range stats {
		allMessages = append(allMessages, stat.Messages...)
	}

	if len(allMessages) == 0 {
		fmt.Println("  No compressible messages found matching the criteria.")
		fmt.Println()
		return nil
	}

	fmt.Printf("  Found %d compressible messages\n", len(allMessages))
	fmt.Println()

	// Run compression.
	opts := compressor.Options{
		DryRun:           !compressApply,
		CompressionLevel: compressLevel,
		Concurrency:      flagConcurrency,
	}

	result := compressor.Compress(allMessages, opts)

	if flagJSON {
		return output.PrintJSON(result)
	}

	output.PrintCompressResult(result, !compressApply)
	return nil
}
