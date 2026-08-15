// Package scanner provides high-level mailbox discovery across different
// server control panel layouts (cPanel, DirectAdmin, Plesk, Generic Maildir).
package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nemke82/mailshrink/internal/maildir"
)

// Provider defines the interface for discovering mailboxes on a server.
// Each control panel (cPanel, DirectAdmin, Plesk) implements this
// interface with its own path conventions.
type Provider interface {
	// Name returns the human-readable name of the provider.
	Name() string

	// Detect checks whether the given base path matches this provider's
	// expected layout. Returns true if the provider can handle this path.
	Detect(basePath string) bool

	// Discover scans the base path and returns all discovered mailboxes.
	Discover(basePath string) ([]*Mailbox, error)
}

// Mailbox represents a single email account's mailbox.
type Mailbox struct {
	// Account is the full email address (user@domain).
	Account string

	// Domain is the domain part of the email address.
	Domain string

	// LocalPart is the local part of the email address (before @).
	LocalPart string

	// BasePath is the absolute path to the mailbox root directory.
	BasePath string

	// Folders contains all Maildir folders within this mailbox.
	Folders []*maildir.Folder
}

// --- cPanel Provider ---
// Path: /home/<user>/mail/<domain>/<emailuser>/
// Structure: cur/, new/, tmp/, .Sent/, .Drafts/, etc.

type cPanelProvider struct{}

func (p *cPanelProvider) Name() string { return "cPanel" }

func (p *cPanelProvider) Detect(basePath string) bool {
	// cPanel: look for /home/*/mail/ directories.
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mailPath := filepath.Join(basePath, entry.Name(), "mail")
		if info, err := os.Stat(mailPath); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func (p *cPanelProvider) Discover(basePath string) ([]*Mailbox, error) {
	var mailboxes []*Mailbox

	// Enumerate /home/*/ directories.
	homeEntries, err := os.ReadDir(basePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", basePath, err)
	}

	for _, homeEntry := range homeEntries {
		if !homeEntry.IsDir() {
			continue
		}
		mailRoot := filepath.Join(basePath, homeEntry.Name(), "mail")
		if _, err := os.Stat(mailRoot); os.IsNotExist(err) {
			continue
		}

		// Enumerate domains under mail/.
		domainEntries, err := os.ReadDir(mailRoot)
		if err != nil {
			continue
		}

		for _, domainEntry := range domainEntries {
			if !domainEntry.IsDir() {
				continue
			}
			domain := domainEntry.Name()
			domainPath := filepath.Join(mailRoot, domain)

			// Enumerate email users under domain.
			userEntries, err := os.ReadDir(domainPath)
			if err != nil {
				continue
			}

			for _, userEntry := range userEntries {
				if !userEntry.IsDir() {
					continue
				}
				localPart := userEntry.Name()
				userPath := filepath.Join(domainPath, localPart)

				if !maildir.IsMaildir(userPath) {
					continue
				}

				folders, err := maildir.DiscoverFolders(userPath)
				if err != nil {
					continue
				}

				mailboxes = append(mailboxes, &Mailbox{
					Account:   localPart + "@" + domain,
					Domain:    domain,
					LocalPart: localPart,
					BasePath:  userPath,
					Folders:   folders,
				})
			}
		}
	}

	return mailboxes, nil
}

// --- DirectAdmin Provider ---
// Path: /home/<user>/imap/<domain>/<emailuser>/Maildir/

type directAdminProvider struct{}

func (p *directAdminProvider) Name() string { return "DirectAdmin" }

func (p *directAdminProvider) Detect(basePath string) bool {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		imapPath := filepath.Join(basePath, entry.Name(), "imap")
		if info, err := os.Stat(imapPath); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func (p *directAdminProvider) Discover(basePath string) ([]*Mailbox, error) {
	var mailboxes []*Mailbox

	homeEntries, err := os.ReadDir(basePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", basePath, err)
	}

	for _, homeEntry := range homeEntries {
		if !homeEntry.IsDir() {
			continue
		}
		imapRoot := filepath.Join(basePath, homeEntry.Name(), "imap")
		if _, err := os.Stat(imapRoot); os.IsNotExist(err) {
			continue
		}

		domainEntries, err := os.ReadDir(imapRoot)
		if err != nil {
			continue
		}

		for _, domainEntry := range domainEntries {
			if !domainEntry.IsDir() {
				continue
			}
			domain := domainEntry.Name()
			domainPath := filepath.Join(imapRoot, domain)

			userEntries, err := os.ReadDir(domainPath)
			if err != nil {
				continue
			}

			for _, userEntry := range userEntries {
				if !userEntry.IsDir() {
					continue
				}
				localPart := userEntry.Name()
				maildirPath := filepath.Join(domainPath, localPart, "Maildir")

				if !maildir.IsMaildir(maildirPath) {
					continue
				}

				folders, err := maildir.DiscoverFolders(maildirPath)
				if err != nil {
					continue
				}

				mailboxes = append(mailboxes, &Mailbox{
					Account:   localPart + "@" + domain,
					Domain:    domain,
					LocalPart: localPart,
					BasePath:  maildirPath,
					Folders:   folders,
				})
			}
		}
	}

	return mailboxes, nil
}

// --- Generic Maildir Provider ---
// Recursively discovers any directory containing cur/new/tmp.
// Falls back to this if no specific panel is detected.

type genericProvider struct{}

func (p *genericProvider) Name() string { return "Generic Maildir" }

func (p *genericProvider) Detect(basePath string) bool {
	// Generic always matches as the fallback.
	return true
}

func (p *genericProvider) Discover(basePath string) ([]*Mailbox, error) {
	var mailboxes []*Mailbox
	seen := make(map[string]bool)

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths.
		}
		if !info.IsDir() {
			return nil
		}
		// Skip hidden directories other than Maildir sub-folders.
		name := info.Name()
		if strings.HasPrefix(name, ".") && name != "." {
			// Could be a Maildir sub-folder — let DiscoverFolders handle it.
			return filepath.SkipDir
		}

		if !maildir.IsMaildir(path) {
			return nil
		}

		// Avoid duplicates from nested Maildirs.
		if seen[path] {
			return filepath.SkipDir
		}
		seen[path] = true

		// Try to derive account and domain from path.
		account, domain, localPart := inferIdentity(path, basePath)

		folders, err := maildir.DiscoverFolders(path)
		if err != nil {
			return nil
		}

		mailboxes = append(mailboxes, &Mailbox{
			Account:   account,
			Domain:    domain,
			LocalPart: localPart,
			BasePath:  path,
			Folders:   folders,
		})

		// Don't descend into the Maildir itself (we've already discovered its folders).
		return filepath.SkipDir
	})

	return mailboxes, err
}

// inferIdentity tries to derive an email address from the filesystem path.
// This is a best-effort heuristic for the generic provider.
func inferIdentity(maildirPath, basePath string) (account, domain, localPart string) {
	rel, err := filepath.Rel(basePath, maildirPath)
	if err != nil {
		return maildirPath, "unknown", filepath.Base(maildirPath)
	}

	parts := strings.Split(rel, string(filepath.Separator))

	// Try to identify patterns like:
	//   <user>/mail/<domain>/<localpart>  (cPanel-like)
	//   <user>/imap/<domain>/<localpart>/Maildir (DirectAdmin-like)
	//   <domain>/<localpart>/Maildir (Plesk-like)
	switch {
	case len(parts) >= 4:
		// Could be user/mail/domain/localpart or user/imap/domain/localpart/Maildir
		domain = parts[len(parts)-2]
		localPart = parts[len(parts)-1]
		if localPart == "Maildir" && len(parts) >= 3 {
			localPart = parts[len(parts)-2]
			domain = parts[len(parts)-3]
		}
	case len(parts) >= 2:
		domain = parts[0]
		localPart = parts[1]
	case len(parts) == 1:
		localPart = parts[0]
		domain = "local"
	default:
		localPart = filepath.Base(maildirPath)
		domain = "unknown"
	}

	if strings.Contains(localPart, "@") {
		account = localPart
		idx := strings.Index(localPart, "@")
		domain = localPart[idx+1:]
		localPart = localPart[:idx]
	} else {
		account = localPart + "@" + domain
	}

	return account, domain, localPart
}

// allProviders returns the ordered list of discovery providers.
// Order matters: first match wins during auto-detection.
var allProviders = []Provider{
	&cPanelProvider{},
	&directAdminProvider{},
	&genericProvider{},
}

// GetProvider returns a provider by name, or nil if not found.
func GetProvider(name string) Provider {
	for _, p := range allProviders {
		if strings.EqualFold(p.Name(), name) {
			return p
		}
	}
	return nil
}

// AllProviderNames returns the names of all available providers.
func AllProviderNames() []string {
	names := make([]string, len(allProviders))
	for i, p := range allProviders {
		names[i] = p.Name()
	}
	return names
}
