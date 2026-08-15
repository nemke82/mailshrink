// Package output provides formatted terminal output for MailShrink,
// including the branded banner, tables, and JSON output.
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/dustin/go-humanize"
	"github.com/fatih/color"

	"github.com/nemke82/mailshrink/internal/compressor"
	"github.com/nemke82/mailshrink/internal/estimator"
	"github.com/nemke82/mailshrink/internal/scanner"
)

// Colors for the branded output.
var (
	bold    = color.New(color.Bold)
	cyan    = color.New(color.FgCyan, color.Bold)
	green   = color.New(color.FgGreen, color.Bold)
	yellow  = color.New(color.FgYellow)
	red     = color.New(color.FgRed, color.Bold)
	dim     = color.New(color.Faint)
	header  = color.New(color.FgWhite, color.Bold)
	success = color.New(color.FgGreen)
)

// PrintBanner prints the MailShrink branded header.
func PrintBanner(version string) {
	fmt.Println()
	cyan.Printf("  MailShrink %s\n", version)
	dim.Println("  Dovecot Maildir disk space analyzer & compressor")
	fmt.Println()
}

// PrintDomainTable prints the domain summary table from a scan result.
func PrintDomainTable(result *scanner.ScanResult) {
	if len(result.DomainStats) == 0 {
		yellow.Println("  No mailboxes found.")
		return
	}

	// Sort domains alphabetically.
	domains := make([]string, 0, len(result.DomainStats))
	for domain := range result.DomainStats {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	header.Fprintf(w, "  DOMAIN\tMAILBOXES\tPHYSICAL\tUNCOMPRESSED\n")
	dim.Fprintf(w, "  %s\t%s\t%s\t%s\n",
		strings.Repeat("─", 20),
		strings.Repeat("─", 10),
		strings.Repeat("─", 12),
		strings.Repeat("─", 14))

	for _, domain := range domains {
		ds := result.DomainStats[domain]
		fmt.Fprintf(w, "  %s\t%d\t%s\t%s\n",
			domain,
			ds.MailboxCount,
			humanize.IBytes(uint64(ds.TotalSize)),
			humanize.IBytes(uint64(ds.UncompressedSize)),
		)
	}
	w.Flush()

	// Print totals.
	fmt.Println()
	fmt.Printf("  Total physical mail:        %s\n", bold.Sprintf("%s", humanize.IBytes(uint64(result.TotalSize))))
	fmt.Printf("  Uncompressed files:         %s\n", bold.Sprintf("%s", humanize.IBytes(uint64(getTotalUncompressedSize(result)))))
	fmt.Printf("  Already compressed:         %s\n", dim.Sprintf("%d files", result.TotalCompressed))
	fmt.Println()
}

// PrintDomainTableWithEstimate prints the domain table enhanced with
// sampling-based estimates.
func PrintDomainTableWithEstimate(result *scanner.ScanResult, estimates map[string]*estimator.EstimateResult) {
	if len(result.DomainStats) == 0 {
		yellow.Println("  No mailboxes found.")
		return
	}

	domains := make([]string, 0, len(result.DomainStats))
	for domain := range result.DomainStats {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	header.Fprintf(w, "  DOMAIN\tMAILBOXES\tPHYSICAL\tRECLAIMABLE\n")
	dim.Fprintf(w, "  %s\t%s\t%s\t%s\n",
		strings.Repeat("─", 20),
		strings.Repeat("─", 10),
		strings.Repeat("─", 12),
		strings.Repeat("─", 14))

	var totalReclaimable int64
	for _, domain := range domains {
		ds := result.DomainStats[domain]
		reclaimable := "—"
		if est, ok := estimates[domain]; ok && est.EstimatedSavings > 0 {
			reclaimable = humanize.IBytes(uint64(est.EstimatedSavings))
			totalReclaimable += est.EstimatedSavings
		}
		fmt.Fprintf(w, "  %s\t%d\t%s\t%s\n",
			domain,
			ds.MailboxCount,
			humanize.IBytes(uint64(ds.TotalSize)),
			reclaimable,
		)
	}
	w.Flush()

	fmt.Println()
	fmt.Printf("  Total physical mail:        %s\n", bold.Sprintf("%s", humanize.IBytes(uint64(result.TotalSize))))

	if totalReclaimable > 0 {
		green.Printf("  Estimated reclaimable:      %s\n", humanize.IBytes(uint64(totalReclaimable)))

		if result.TotalSize > 0 {
			pct := float64(totalReclaimable) / float64(result.TotalSize) * 100
			fmt.Printf("  Potential saving:           %s\n", green.Sprintf("%.1f%%", pct))
		}
	}

	fmt.Println()
	dim.Println("  No files were modified.")
	fmt.Printf("  Run %s for details.\n", cyan.Sprint("mailshrink plan"))
	fmt.Println()
}

// PrintAccountEstimate prints the detailed estimate for a specific account.
func PrintAccountEstimate(account string, totalSize int64, messageCount int, est *estimator.EstimateResult) {
	fmt.Printf("  Physical size:              %s\n", bold.Sprint(humanize.IBytes(uint64(totalSize))))
	fmt.Printf("  Messages:                   %s\n", bold.Sprint(humanize.Comma(int64(messageCount))))
	fmt.Println()

	if est.SampleCount > 0 {
		dim.Printf("  Sampling %d messages...\n", est.SampleCount)
		fmt.Println()
		fmt.Printf("  Sample original:            %s\n", humanize.IBytes(uint64(est.SampleOriginalSize)))
		fmt.Printf("  Sample compressed:          %s\n", humanize.IBytes(uint64(est.SampleCompressedSize)))
		fmt.Printf("  Measured reduction:         %s\n", green.Sprintf("%.2f%%", est.MeasuredRatio*100))
		fmt.Println()
		green.Printf("  Estimated reclaimable:      %s\n", humanize.IBytes(uint64(est.EstimatedSavings)))
	} else {
		dim.Println("  No uncompressed messages found to sample.")
	}
	fmt.Println()
}

// PrintPlanTable prints the detailed per-account, per-folder plan table.
func PrintPlanTable(stats []*scanner.AccountStat, estimates map[string]*estimator.EstimateResult) {
	if len(stats) == 0 {
		yellow.Println("  No compressible messages found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	header.Fprintf(w, "  ACCOUNT\tFOLDER\tPERIOD\tSIZE\tEST. SAVING\n")
	dim.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n",
		strings.Repeat("─", 24),
		strings.Repeat("─", 10),
		strings.Repeat("─", 10),
		strings.Repeat("─", 10),
		strings.Repeat("─", 12))

	for _, stat := range stats {
		saving := "—"
		key := stat.Account + "/" + stat.Folder
		if est, ok := estimates[key]; ok && est.EstimatedSavings > 0 {
			saving = humanize.IBytes(uint64(est.EstimatedSavings))
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n",
			stat.Account,
			stat.Folder,
			stat.Period,
			humanize.IBytes(uint64(stat.TotalSize)),
			saving,
		)
	}
	w.Flush()
	fmt.Println()
}

// PrintCompressResult prints the compression operation summary.
func PrintCompressResult(result *compressor.CompressResult, dryRun bool) {
	fmt.Println()
	if dryRun {
		yellow.Println("  DRY RUN — no files were modified")
		fmt.Println()
		fmt.Printf("  Files that would be compressed:  %s\n",
			bold.Sprint(humanize.Comma(result.FilesProcessed)))
		fmt.Printf("  Total size:                      %s\n",
			humanize.IBytes(uint64(result.SizeBefore)))
		fmt.Println()
		dim.Printf("  To apply, add %s\n", cyan.Sprint("--apply"))
	} else {
		success.Println("  ✓ Compression complete")
		fmt.Println()
		fmt.Printf("  Files compressed:    %s\n", green.Sprint(humanize.Comma(result.FilesCompressed)))
		fmt.Printf("  Files skipped:       %s\n", dim.Sprint(humanize.Comma(result.FilesSkipped)))
		fmt.Printf("  Size before:         %s\n", humanize.IBytes(uint64(result.SizeBefore)))
		fmt.Printf("  Size after:          %s\n", humanize.IBytes(uint64(result.SizeAfter)))
		green.Printf("  Space saved:         %s\n", humanize.IBytes(uint64(result.SpaceSaved)))

		if result.SizeBefore > 0 {
			pct := float64(result.SpaceSaved) / float64(result.SizeBefore) * 100
			fmt.Printf("  Reduction:           %s\n", green.Sprintf("%.1f%%", pct))
		}
	}

	if result.FilesErrored > 0 {
		fmt.Println()
		red.Printf("  Errors: %d files failed\n", result.FilesErrored)
		for _, e := range result.Errors {
			dim.Printf("    %s: %s\n", e.Path, e.Error)
		}
	}

	fmt.Println()
}

// PrintJSON outputs any value as formatted JSON.
func PrintJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// PrintScanningMessage prints a scanning progress message.
func PrintScanningMessage(path string) {
	dim.Printf("  Scanning %s...\n", path)
	fmt.Println()
}

// PrintProviderDetected prints which provider was auto-detected.
func PrintProviderDetected(name string) {
	dim.Printf("  Detected server type: %s\n", bold.Sprint(name))
	fmt.Println()
}

// getTotalUncompressedSize calculates the total uncompressed size from domain stats.
func getTotalUncompressedSize(result *scanner.ScanResult) int64 {
	var total int64
	for _, ds := range result.DomainStats {
		total += ds.UncompressedSize
	}
	return total
}
