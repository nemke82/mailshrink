package maildir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Folder represents a Maildir folder (INBOX, Sent, Drafts, etc.).
type Folder struct {
	// Name is the display name of the folder (e.g., "INBOX", "Sent", "Drafts").
	Name string

	// Path is the absolute path to the folder root.
	Path string

	// CurPath is the absolute path to the cur/ subdirectory.
	CurPath string

	// NewPath is the absolute path to the new/ subdirectory.
	NewPath string
}

// MessageFilter controls which messages are returned by ListMessages.
type MessageFilter struct {
	// Before returns only messages with ModTime before this time.
	// Zero value means no upper bound.
	Before time.Time

	// OlderThan returns only messages older than this duration from now.
	// Zero value means no age filter.
	OlderThan time.Duration

	// CompressedOnly if true, returns only compressed messages.
	CompressedOnly bool

	// UncompressedOnly if true, returns only uncompressed messages.
	UncompressedOnly bool

	// MinSize filters messages smaller than this size (bytes).
	MinSize int64
}

// IsMaildir returns true if the given path contains the standard Maildir
// subdirectories (cur, new, tmp).
func IsMaildir(path string) bool {
	for _, sub := range []string{"cur", "new", "tmp"} {
		info, err := os.Stat(filepath.Join(path, sub))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

// ScanFolder checks whether the given path is a valid Maildir and returns
// a Folder struct. The folder name is derived from the directory name,
// translating Maildir conventions (e.g., ".Sent" → "Sent").
func ScanFolder(path string) (*Folder, error) {
	if !IsMaildir(path) {
		return nil, fmt.Errorf("%s is not a valid Maildir (missing cur/new/tmp)", path)
	}

	name := filepath.Base(path)
	// Maildir sub-folders are prefixed with a dot (e.g., ".Sent", ".Drafts").
	name = strings.TrimPrefix(name, ".")
	// If the folder is the root mailbox directory, name it INBOX.
	if name == "Maildir" || name == "mail" || name == "" {
		name = "INBOX"
	}

	return &Folder{
		Name:    name,
		Path:    path,
		CurPath: filepath.Join(path, "cur"),
		NewPath: filepath.Join(path, "new"),
	}, nil
}

// DiscoverFolders finds all Maildir folders within a mailbox root path.
// This includes the root folder (INBOX) and any sub-folders (e.g., .Sent, .Drafts).
func DiscoverFolders(mailboxPath string) ([]*Folder, error) {
	var folders []*Folder

	// Check if the root itself is a Maildir (INBOX).
	if IsMaildir(mailboxPath) {
		folder, err := ScanFolder(mailboxPath)
		if err == nil {
			folder.Name = "INBOX"
			folders = append(folders, folder)
		}
	}

	// Scan for sub-folders (directories starting with '.').
	entries, err := os.ReadDir(mailboxPath)
	if err != nil {
		return folders, fmt.Errorf("read dir %s: %w", mailboxPath, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, ".") {
			continue
		}
		// Skip special directories.
		if name == "." || name == ".." || name == ".strstrstrstrstrstr" {
			continue
		}

		subPath := filepath.Join(mailboxPath, name)
		if IsMaildir(subPath) {
			folder, err := ScanFolder(subPath)
			if err == nil {
				folders = append(folders, folder)
			}
		}
	}

	return folders, nil
}

// ListMessages returns all messages in a folder's cur/ and new/ directories,
// applying the given filter. Messages in tmp/ are always excluded.
func ListMessages(folder *Folder, filter MessageFilter) ([]*Message, error) {
	var messages []*Message

	// Process both cur/ and new/ directories.
	for _, dir := range []string{folder.CurPath, folder.NewPath} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			// Skip hidden files and Dovecot index files.
			name := entry.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "dovecot") {
				continue
			}

			path := filepath.Join(dir, name)
			msg, err := LoadMessage(path)
			if err != nil {
				// Skip files we can't parse — don't fail the whole scan.
				continue
			}

			if matchesFilter(msg, filter) {
				messages = append(messages, msg)
			}
		}
	}

	return messages, nil
}

// matchesFilter returns true if the message passes all filter criteria.
func matchesFilter(msg *Message, f MessageFilter) bool {
	if !f.Before.IsZero() && !msg.ModTime.Before(f.Before) {
		return false
	}

	if f.OlderThan > 0 {
		cutoff := time.Now().Add(-f.OlderThan)
		if !msg.ModTime.Before(cutoff) {
			return false
		}
	}

	if f.CompressedOnly && !msg.IsCompressed {
		return false
	}

	if f.UncompressedOnly && msg.IsCompressed {
		return false
	}

	if f.MinSize > 0 && msg.PhysicalSize < f.MinSize {
		return false
	}

	return true
}
