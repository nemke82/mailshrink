// Package compressor provides the core compression engine for MailShrink.
// It handles gzip compression of Maildir messages with atomic operations,
// mtime preservation, ownership restoration, and Maildir locking.
package compressor

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nemke82/mailshrink/internal/maildir"
)

// CompressResult contains the outcome of a compression operation.
type CompressResult struct {
	// FilesProcessed is the total number of files examined.
	FilesProcessed int64

	// FilesCompressed is the number of files successfully compressed.
	FilesCompressed int64

	// FilesSkipped is the number of files skipped (already compressed, too small, etc.).
	FilesSkipped int64

	// FilesErrored is the number of files that failed to compress.
	FilesErrored int64

	// SizeBefore is the total physical size before compression.
	SizeBefore int64

	// SizeAfter is the total physical size after compression.
	SizeAfter int64

	// SpaceSaved is the total bytes saved (SizeBefore - SizeAfter).
	SpaceSaved int64

	// Errors contains details about individual file errors.
	Errors []FileError

	// mu protects concurrent updates.
	mu sync.Mutex
}

// FileError records a compression failure for a specific file.
type FileError struct {
	Path  string
	Error string
}

// Options controls compression behavior.
type Options struct {
	// DryRun if true, no files are modified — only reports what would happen.
	DryRun bool

	// CompressionLevel is the gzip compression level (1-9). Default: 6.
	CompressionLevel int

	// Concurrency is the number of parallel compression workers. Default: 4.
	Concurrency int

	// Before compresses only messages with ModTime before this date.
	Before time.Time

	// OlderThan compresses only messages older than this duration.
	OlderThan time.Duration

	// LockTimeout is the maximum time to wait for a Maildir lock.
	LockTimeout time.Duration

	// ProgressFn is called periodically with progress updates.
	// Arguments: (current, total int64).
	ProgressFn func(current, total int64)
}

// DefaultOptions returns safe default compression options.
func DefaultOptions() Options {
	return Options{
		DryRun:           true, // Safe by default!
		CompressionLevel: 6,
		Concurrency:      4,
		LockTimeout:      maildir.DefaultLockTimeout,
	}
}

// Compress compresses the given messages using gzip. This is the core
// compression engine. In dry-run mode, it reports what would happen
// without modifying any files.
func Compress(messages []*maildir.Message, opts Options) *CompressResult {
	if opts.CompressionLevel <= 0 || opts.CompressionLevel > 9 {
		opts.CompressionLevel = 6
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = maildir.DefaultLockTimeout
	}

	result := &CompressResult{}

	// Filter eligible messages.
	var eligible []*maildir.Message
	for _, msg := range messages {
		if !maildir.IsEligible(msg) {
			atomic.AddInt64(&result.FilesSkipped, 1)
			continue
		}
		eligible = append(eligible, msg)
	}

	if len(eligible) == 0 {
		return result
	}

	total := int64(len(eligible))

	if opts.DryRun {
		// Dry-run: just count and report.
		for _, msg := range eligible {
			atomic.AddInt64(&result.FilesProcessed, 1)
			atomic.AddInt64(&result.SizeBefore, msg.PhysicalSize)
			if opts.ProgressFn != nil {
				opts.ProgressFn(atomic.LoadInt64(&result.FilesProcessed), total)
			}
		}
		return result
	}

	// Real compression with worker pool.
	jobs := make(chan *maildir.Message, opts.Concurrency*2)
	var wg sync.WaitGroup

	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for msg := range jobs {
				compressOne(msg, opts, result)
				if opts.ProgressFn != nil {
					opts.ProgressFn(
						atomic.LoadInt64(&result.FilesProcessed),
						total,
					)
				}
			}
		}()
	}

	for _, msg := range eligible {
		jobs <- msg
	}
	close(jobs)
	wg.Wait()

	result.SpaceSaved = result.SizeBefore - result.SizeAfter

	return result
}

// compressOne handles the compression of a single message file.
// This is the critical path — safety is paramount:
//  1. Acquire Maildir lock
//  2. Read original file and stat
//  3. Compress to temp file in same directory
//  4. Compute new filename with ,S=<original_size>
//  5. Atomic rename temp → new filename
//  6. Restore mtime and ownership
//  7. Release lock
func compressOne(msg *maildir.Message, opts Options, result *CompressResult) {
	atomic.AddInt64(&result.FilesProcessed, 1)
	atomic.AddInt64(&result.SizeBefore, msg.PhysicalSize)

	dir := filepath.Dir(msg.Path)
	maildirRoot := filepath.Dir(dir) // Go up from cur/ or new/ to the Maildir root.

	// Step 1: Acquire lock on the Maildir.
	lock, err := maildir.AcquireLock(maildirRoot, opts.LockTimeout)
	if err != nil {
		recordError(result, msg.Path, fmt.Sprintf("lock failed: %v", err))
		return
	}
	defer lock.Release()

	// Step 2: Open and read the original file.
	srcFile, err := os.Open(msg.Path)
	if err != nil {
		recordError(result, msg.Path, fmt.Sprintf("open failed: %v", err))
		return
	}

	srcInfo, err := srcFile.Stat()
	if err != nil {
		srcFile.Close()
		recordError(result, msg.Path, fmt.Sprintf("stat failed: %v", err))
		return
	}

	originalSize := srcInfo.Size()
	originalMtime := srcInfo.ModTime()
	originalMode := srcInfo.Mode()

	// Step 3: Create temp file in the same directory for atomic rename.
	tmpFile, err := os.CreateTemp(dir, ".mailshrink.tmp.*")
	if err != nil {
		srcFile.Close()
		recordError(result, msg.Path, fmt.Sprintf("create temp failed: %v", err))
		return
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup on any failure path.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	// Step 3b: Compress source into temp file.
	gw, err := gzip.NewWriterLevel(tmpFile, opts.CompressionLevel)
	if err != nil {
		srcFile.Close()
		tmpFile.Close()
		recordError(result, msg.Path, fmt.Sprintf("gzip init failed: %v", err))
		return
	}

	_, copyErr := io.Copy(gw, srcFile)
	srcFile.Close()

	if copyErr != nil {
		gw.Close()
		tmpFile.Close()
		recordError(result, msg.Path, fmt.Sprintf("compress failed: %v", copyErr))
		return
	}

	if err := gw.Close(); err != nil {
		tmpFile.Close()
		recordError(result, msg.Path, fmt.Sprintf("gzip finalize failed: %v", err))
		return
	}

	if err := tmpFile.Close(); err != nil {
		recordError(result, msg.Path, fmt.Sprintf("close temp failed: %v", err))
		return
	}

	// Get compressed size.
	compressedInfo, err := os.Stat(tmpPath)
	if err != nil {
		recordError(result, msg.Path, fmt.Sprintf("stat compressed failed: %v", err))
		return
	}
	compressedSize := compressedInfo.Size()

	// Don't compress if it's larger than or equal to the original.
	if compressedSize >= originalSize {
		os.Remove(tmpPath)
		atomic.AddInt64(&result.FilesSkipped, 1)
		atomic.AddInt64(&result.SizeAfter, originalSize)
		return
	}

	// Step 4: Compute new filename with S=<original_uncompressed_size>.
	newBasename := maildir.BuildFilename(msg, originalSize)
	newPath := filepath.Join(dir, newBasename)

	// Step 5: Set permissions on temp file before rename.
	if err := os.Chmod(tmpPath, originalMode); err != nil {
		recordError(result, msg.Path, fmt.Sprintf("chmod failed: %v", err))
		return
	}

	// Step 5b: Set ownership on temp file.
	if err := os.Chown(tmpPath, msg.OwnerUID, msg.OwnerGID); err != nil {
		// Non-fatal — we might not be running as root.
		// Log but continue.
	}

	// Step 5c: Atomic rename — if the path changed (S= added/updated),
	// first rename temp to new path, then remove old file.
	if newPath == msg.Path {
		// Filename didn't change — direct atomic replacement.
		if err := os.Rename(tmpPath, msg.Path); err != nil {
			recordError(result, msg.Path, fmt.Sprintf("rename failed: %v", err))
			return
		}
	} else {
		// Filename changed (S= field added/updated).
		// Rename temp to new name, then remove old file.
		if err := os.Rename(tmpPath, newPath); err != nil {
			recordError(result, msg.Path, fmt.Sprintf("rename to %s failed: %v", newBasename, err))
			return
		}
		// Remove old file (only if different from new path).
		if msg.Path != newPath {
			os.Remove(msg.Path)
		}
	}

	// Step 6: Restore original mtime (= IMAP INTERNALDATE).
	os.Chtimes(newPath, originalMtime, originalMtime)

	// Success!
	success = true
	atomic.AddInt64(&result.FilesCompressed, 1)
	atomic.AddInt64(&result.SizeAfter, compressedSize)
}

// recordError adds an error to the result in a thread-safe manner.
func recordError(result *CompressResult, path string, errMsg string) {
	atomic.AddInt64(&result.FilesErrored, 1)
	result.mu.Lock()
	result.Errors = append(result.Errors, FileError{Path: path, Error: errMsg})
	result.mu.Unlock()
}
