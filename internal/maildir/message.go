// Package maildir provides types and utilities for working with Maildir
// format mailboxes, including filename parsing, message metadata extraction,
// and compression detection.
package maildir

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Message represents a single email file in a Maildir folder.
type Message struct {
	// Path is the full absolute path to the message file.
	Path string

	// Dir is the Maildir subdirectory: "cur", "new", or "tmp".
	Dir string

	// Basename is the filename without path.
	Basename string

	// UniqueID is the part of the filename before the ':' separator.
	// This is the stable identifier across flag changes.
	UniqueID string

	// InfoSection is the part after ":2," containing flags.
	InfoSection string

	// Flags contains the standard Maildir flags (D, F, R, S, T).
	Flags string

	// Size is the virtual message size from the ,S= field in the filename.
	// This represents the original uncompressed size when compression is used.
	// Zero if not present in the filename.
	Size int64

	// VSize is the virtual size from the ,W= field (RFC822.SIZE with CRLF).
	// Zero if not present in the filename.
	VSize int64

	// PhysicalSize is the actual file size on disk (from stat).
	PhysicalSize int64

	// ModTime is the file's modification time, which corresponds to
	// the IMAP INTERNALDATE for the message.
	ModTime time.Time

	// IsCompressed indicates whether the file content is gzip-compressed,
	// detected by reading the file's magic bytes.
	IsCompressed bool

	// OwnerUID is the file owner's user ID.
	OwnerUID int

	// OwnerGID is the file owner's group ID.
	OwnerGID int
}

// gzipMagic is the first two bytes of a gzip-compressed file.
var gzipMagic = []byte{0x1f, 0x8b}

// sizeFieldRegexp matches ,S=<digits> in a Maildir filename.
var sizeFieldRegexp = regexp.MustCompile(`,S=(\d+)`)

// vsizeFieldRegexp matches ,W=<digits> in a Maildir filename.
var vsizeFieldRegexp = regexp.MustCompile(`,W=(\d+)`)

// ParseFilename parses a Maildir filename and extracts all metadata fields.
// The path should be the full path to the message file.
func ParseFilename(path string) (*Message, error) {
	basename := filepath.Base(path)
	dir := filepath.Base(filepath.Dir(path))

	msg := &Message{
		Path:     path,
		Dir:      dir,
		Basename: basename,
	}

	// Split on ':' — format is <unique>:2,<flags>[,<extensions>]
	colonIdx := strings.Index(basename, ":")
	if colonIdx == -1 {
		// No info section — valid for new/ messages
		msg.UniqueID = basename
		return msg, nil
	}

	msg.UniqueID = basename[:colonIdx]
	msg.InfoSection = basename[colonIdx+1:]

	// Parse info section — expected format: "2,<flags>"
	if strings.HasPrefix(msg.InfoSection, "2,") {
		flagsPart := msg.InfoSection[2:]
		// Standard flags are single uppercase letters before any comma
		commaIdx := strings.Index(flagsPart, ",")
		if commaIdx == -1 {
			msg.Flags = flagsPart
		} else {
			msg.Flags = flagsPart[:commaIdx]
		}
	}

	// Extract ,S=<size> from the full basename
	if match := sizeFieldRegexp.FindStringSubmatch(basename); len(match) == 2 {
		if size, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			msg.Size = size
		}
	}

	// Extract ,W=<vsize> from the full basename
	if match := vsizeFieldRegexp.FindStringSubmatch(basename); len(match) == 2 {
		if vsize, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			msg.VSize = vsize
		}
	}

	return msg, nil
}

// StatMessage populates the file metadata fields (PhysicalSize, ModTime,
// OwnerUID, OwnerGID) by stat'ing the file on disk.
func StatMessage(msg *Message) error {
	info, err := os.Lstat(msg.Path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", msg.Path, err)
	}

	msg.PhysicalSize = info.Size()
	msg.ModTime = info.ModTime()

	// Extract UID/GID from the underlying stat structure.
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		msg.OwnerUID = int(stat.Uid)
		msg.OwnerGID = int(stat.Gid)
	}

	return nil
}

// DetectCompression reads the first two bytes of the file to check for
// the gzip magic number (0x1f, 0x8b). Sets msg.IsCompressed accordingly.
func DetectCompression(msg *Message) error {
	f, err := os.Open(msg.Path)
	if err != nil {
		return fmt.Errorf("open %s: %w", msg.Path, err)
	}
	defer f.Close()

	header := make([]byte, 2)
	n, err := io.ReadFull(f, header)
	if err != nil || n < 2 {
		// File too small to be compressed; not compressed.
		msg.IsCompressed = false
		return nil
	}

	msg.IsCompressed = header[0] == gzipMagic[0] && header[1] == gzipMagic[1]
	return nil
}

// LoadMessage parses the filename, stats the file, and detects compression
// in a single call. This is the primary way to load a complete Message.
func LoadMessage(path string) (*Message, error) {
	msg, err := ParseFilename(path)
	if err != nil {
		return nil, err
	}

	if err := StatMessage(msg); err != nil {
		return nil, err
	}

	if err := DetectCompression(msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// BuildFilename constructs a Maildir filename with the updated S= field.
// This is used after compression to set S= to the original uncompressed size.
func BuildFilename(msg *Message, originalSize int64) string {
	basename := msg.Basename

	// If there's already an S= field, replace it with the new size.
	if sizeFieldRegexp.MatchString(basename) {
		basename = sizeFieldRegexp.ReplaceAllString(basename,
			fmt.Sprintf(",S=%d", originalSize))
		return basename
	}

	// Otherwise, add S= before the :2, info section.
	colonIdx := strings.Index(basename, ":")
	if colonIdx == -1 {
		// No info section — add one.
		return fmt.Sprintf("%s,S=%d", basename, originalSize)
	}

	return fmt.Sprintf("%s,S=%d:%s",
		basename[:colonIdx], originalSize, basename[colonIdx+1:])
}

// GetMessageDate returns the message date, which is the file modification
// time. This corresponds to the IMAP INTERNALDATE.
func GetMessageDate(msg *Message) time.Time {
	return msg.ModTime
}

// IsEligible returns true if the message is eligible for compression:
// it must not already be compressed, and it must be a regular file.
func IsEligible(msg *Message) bool {
	return !msg.IsCompressed && msg.PhysicalSize > 0
}
