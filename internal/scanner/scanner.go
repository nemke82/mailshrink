package scanner

import (
	"fmt"
	"strings"

	"github.com/nemke82/mailshrink/internal/maildir"
)

// ScanOptions controls the scanning behavior.
type ScanOptions struct {
	// BasePath is the root directory to scan (e.g., "/home").
	BasePath string

	// Provider forces a specific discovery provider by name.
	// Empty string means auto-detect.
	Provider string

	// Domain filters results to a specific domain.
	Domain string

	// Account filters results to a specific account (user@domain).
	Account string

	// Verbose enables detailed progress output.
	Verbose bool

	// Filter controls which messages are included in the results.
	Filter maildir.MessageFilter

	// FolderName filters results to a specific folder name (e.g., "Sent", "INBOX").
	FolderName string
}

// ScanResult contains the aggregated results of a mailbox scan.
type ScanResult struct {
	// Provider is the name of the discovery provider used.
	Provider string

	// Mailboxes is the list of all discovered mailboxes.
	Mailboxes []*Mailbox

	// DomainStats contains per-domain aggregated statistics.
	DomainStats map[string]*DomainStat

	// TotalSize is the total physical size of all scanned messages.
	TotalSize int64

	// TotalFiles is the total number of message files scanned.
	TotalFiles int64

	// TotalCompressed is the number of already-compressed files.
	TotalCompressed int64

	// TotalUncompressed is the number of uncompressed files.
	TotalUncompressed int64
}

// DomainStat contains aggregated statistics for a single domain.
type DomainStat struct {
	Domain            string
	MailboxCount      int
	TotalSize         int64
	TotalFiles        int64
	CompressedFiles   int64
	UncompressedFiles int64
	UncompressedSize  int64
}

// AccountStat contains per-account, per-folder statistics.
type AccountStat struct {
	Account   string
	Domain    string
	Folder    string
	Period    string // e.g., "2021-2024"
	TotalSize int64
	FileCount int64
	Messages  []*maildir.Message
}

// Scan discovers all mailboxes under the given base path and collects
// statistics about their contents.
func Scan(opts ScanOptions) (*ScanResult, error) {
	// Select provider.
	var provider Provider
	if opts.Provider != "" {
		provider = GetProvider(opts.Provider)
		if provider == nil {
			return nil, fmt.Errorf("unknown provider %q (available: %s)",
				opts.Provider, strings.Join(AllProviderNames(), ", "))
		}
	} else {
		// Auto-detect.
		for _, p := range allProviders {
			if p.Detect(opts.BasePath) {
				provider = p
				break
			}
		}
	}

	if provider == nil {
		return nil, fmt.Errorf("no suitable provider found for %s", opts.BasePath)
	}

	// Discover mailboxes.
	mailboxes, err := provider.Discover(opts.BasePath)
	if err != nil {
		return nil, fmt.Errorf("discovery failed (%s): %w", provider.Name(), err)
	}

	// Apply domain/account filters.
	mailboxes = filterMailboxes(mailboxes, opts)

	// Build result with statistics.
	result := &ScanResult{
		Provider:    provider.Name(),
		Mailboxes:   mailboxes,
		DomainStats: make(map[string]*DomainStat),
	}

	for _, mb := range mailboxes {
		ds, ok := result.DomainStats[mb.Domain]
		if !ok {
			ds = &DomainStat{Domain: mb.Domain}
			result.DomainStats[mb.Domain] = ds
		}
		ds.MailboxCount++

		for _, folder := range mb.Folders {
			if opts.FolderName != "" && !strings.EqualFold(folder.Name, opts.FolderName) {
				continue
			}

			messages, err := maildir.ListMessages(folder, opts.Filter)
			if err != nil {
				continue
			}

			for _, msg := range messages {
				result.TotalFiles++
				result.TotalSize += msg.PhysicalSize
				ds.TotalFiles++
				ds.TotalSize += msg.PhysicalSize

				if msg.IsCompressed {
					result.TotalCompressed++
					ds.CompressedFiles++
				} else {
					result.TotalUncompressed++
					ds.UncompressedFiles++
					ds.UncompressedSize += msg.PhysicalSize
				}
			}
		}
	}

	return result, nil
}

// GetAccountStats returns detailed per-account, per-folder statistics
// for a specific domain or all domains.
func GetAccountStats(opts ScanOptions) ([]*AccountStat, error) {
	var provider Provider
	if opts.Provider != "" {
		provider = GetProvider(opts.Provider)
	} else {
		for _, p := range allProviders {
			if p.Detect(opts.BasePath) {
				provider = p
				break
			}
		}
	}
	if provider == nil {
		return nil, fmt.Errorf("no suitable provider found for %s", opts.BasePath)
	}

	mailboxes, err := provider.Discover(opts.BasePath)
	if err != nil {
		return nil, err
	}

	mailboxes = filterMailboxes(mailboxes, opts)

	var stats []*AccountStat

	for _, mb := range mailboxes {
		for _, folder := range mb.Folders {
			if opts.FolderName != "" && !strings.EqualFold(folder.Name, opts.FolderName) {
				continue
			}

			filter := opts.Filter
			filter.UncompressedOnly = true

			messages, err := maildir.ListMessages(folder, filter)
			if err != nil || len(messages) == 0 {
				continue
			}

			// Calculate period from message dates.
			var totalSize int64
			var minYear, maxYear int
			for _, msg := range messages {
				totalSize += msg.PhysicalSize
				year := msg.ModTime.Year()
				if minYear == 0 || year < minYear {
					minYear = year
				}
				if year > maxYear {
					maxYear = year
				}
			}

			period := fmt.Sprintf("%d", minYear)
			if maxYear > minYear {
				period = fmt.Sprintf("%d-%d", minYear, maxYear)
			}

			stats = append(stats, &AccountStat{
				Account:   mb.Account,
				Domain:    mb.Domain,
				Folder:    folder.Name,
				Period:    period,
				TotalSize: totalSize,
				FileCount: int64(len(messages)),
				Messages:  messages,
			})
		}
	}

	return stats, nil
}

// filterMailboxes applies domain and account filters.
func filterMailboxes(mailboxes []*Mailbox, opts ScanOptions) []*Mailbox {
	if opts.Domain == "" && opts.Account == "" {
		return mailboxes
	}

	var filtered []*Mailbox
	for _, mb := range mailboxes {
		if opts.Domain != "" && !strings.EqualFold(mb.Domain, opts.Domain) {
			continue
		}
		if opts.Account != "" && !strings.EqualFold(mb.Account, opts.Account) {
			continue
		}
		filtered = append(filtered, mb)
	}
	return filtered
}
