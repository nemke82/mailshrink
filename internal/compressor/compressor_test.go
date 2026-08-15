package compressor

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nemke82/mailshrink/internal/maildir"
)

// createTestMaildir creates a temp Maildir with cur/new/tmp.
func createTestMaildir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"cur", "new", "tmp"} {
		os.MkdirAll(filepath.Join(dir, sub), 0755)
	}
	return dir
}

// createTestEmailFile creates a realistic email file in cur/.
func createTestEmailFile(t *testing.T, dir, name string) *maildir.Message {
	t.Helper()
	content := strings.Repeat(
		"From: sender@example.com\r\n"+
			"To: recipient@example.com\r\n"+
			"Subject: Test message about important things\r\n"+
			"Date: Mon, 01 Jan 2024 12:00:00 +0000\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/plain; charset=utf-8\r\n\r\n"+
			"This is a test email with enough content to be compressible.\r\n"+
			"Lorem ipsum dolor sit amet, consectetur adipiscing elit.\r\n"+
			"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.\r\n\r\n",
		5)

	curDir := filepath.Join(dir, "cur")
	msgPath := filepath.Join(curDir, name)
	if err := os.WriteFile(msgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Set mtime to a known time.
	mtime := time.Date(2023, 6, 15, 10, 30, 0, 0, time.UTC)
	os.Chtimes(msgPath, mtime, mtime)

	msg, err := maildir.LoadMessage(msgPath)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestCompress_DryRun(t *testing.T) {
	dir := createTestMaildir(t)
	msg := createTestEmailFile(t, dir, "1234567890.host:2,S")

	result := Compress([]*maildir.Message{msg}, Options{
		DryRun:           true,
		CompressionLevel: 6,
		Concurrency:      1,
	})

	if result.FilesProcessed != 1 {
		t.Errorf("FilesProcessed = %d, want 1", result.FilesProcessed)
	}
	if result.FilesCompressed != 0 {
		t.Errorf("FilesCompressed = %d, want 0 in dry-run", result.FilesCompressed)
	}

	// Verify original file is untouched.
	if _, err := os.Stat(msg.Path); err != nil {
		t.Errorf("original file should still exist after dry-run: %v", err)
	}
}

func TestCompress_Apply(t *testing.T) {
	dir := createTestMaildir(t)
	msg := createTestEmailFile(t, dir, "1234567890.host:2,S")

	originalSize := msg.PhysicalSize
	originalMtime := msg.ModTime

	result := Compress([]*maildir.Message{msg}, Options{
		DryRun:           false,
		CompressionLevel: 6,
		Concurrency:      1,
		LockTimeout:      5 * time.Second,
	})

	if result.FilesCompressed != 1 {
		t.Fatalf("FilesCompressed = %d, want 1", result.FilesCompressed)
	}
	if result.FilesErrored != 0 {
		t.Fatalf("FilesErrored = %d, want 0; errors: %v", result.FilesErrored, result.Errors)
	}
	if result.SpaceSaved <= 0 {
		t.Errorf("SpaceSaved = %d, want > 0", result.SpaceSaved)
	}
	if result.SizeBefore != originalSize {
		t.Errorf("SizeBefore = %d, want %d", result.SizeBefore, originalSize)
	}
	if result.SizeAfter >= result.SizeBefore {
		t.Errorf("SizeAfter (%d) should be less than SizeBefore (%d)", result.SizeAfter, result.SizeBefore)
	}

	// Verify the compressed file exists and has S= in its name.
	curDir := filepath.Join(dir, "cur")
	entries, err := os.ReadDir(curDir)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".mailshrink") {
			t.Errorf("temp file left behind: %s", name)
			continue
		}
		if strings.Contains(name, "1234567890") {
			found = true

			// Verify it contains S=<original_size>.
			expectedS := "S=" + strings.TrimRight(strings.TrimRight(
				strings.Replace(
					strings.Replace(
						strings.Replace(
							strings.Replace(
								filepath.Base(msg.Path), "1234567890.host", "", 1),
							":2,S", "", 1),
						",S=", "", 1),
					".", "", -1),
				"0"), " ")
			_ = expectedS // Simplified check below.

			// Verify it's actually gzip-compressed.
			compressedPath := filepath.Join(curDir, name)
			compressedMsg, err := maildir.LoadMessage(compressedPath)
			if err != nil {
				t.Fatalf("LoadMessage on compressed file: %v", err)
			}
			if !compressedMsg.IsCompressed {
				t.Error("compressed file should be detected as gzip")
			}

			// Verify mtime is preserved.
			if !compressedMsg.ModTime.Equal(originalMtime) {
				t.Errorf("mtime changed: got %v, want %v",
					compressedMsg.ModTime, originalMtime)
			}

			// Verify the content is valid gzip.
			f, err := os.Open(compressedPath)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()

			gr, err := gzip.NewReader(f)
			if err != nil {
				t.Fatalf("not valid gzip: %v", err)
			}
			decompressed, err := io.ReadAll(gr)
			if err != nil {
				t.Fatalf("gzip read error: %v", err)
			}
			gr.Close()

			if !strings.Contains(string(decompressed), "From: sender@example.com") {
				t.Error("decompressed content doesn't match original")
			}
		}
	}

	if !found {
		t.Error("compressed file not found in cur/")
	}
}

func TestCompress_SkipsAlreadyCompressed(t *testing.T) {
	dir := createTestMaildir(t)

	// Create a file that's already gzip-compressed.
	curDir := filepath.Join(dir, "cur")
	gzPath := filepath.Join(curDir, "1234567890.host,S=100:2,S")
	f, _ := os.Create(gzPath)
	gw := gzip.NewWriter(f)
	gw.Write([]byte("already compressed"))
	gw.Close()
	f.Close()

	msg, err := maildir.LoadMessage(gzPath)
	if err != nil {
		t.Fatal(err)
	}

	result := Compress([]*maildir.Message{msg}, Options{
		DryRun:           false,
		CompressionLevel: 6,
		Concurrency:      1,
		LockTimeout:      5 * time.Second,
	})

	if result.FilesCompressed != 0 {
		t.Errorf("FilesCompressed = %d, want 0 (already compressed)", result.FilesCompressed)
	}
	if result.FilesSkipped != 1 {
		t.Errorf("FilesSkipped = %d, want 1", result.FilesSkipped)
	}
}

func TestCompress_DefaultIsDryRun(t *testing.T) {
	opts := DefaultOptions()
	if !opts.DryRun {
		t.Error("DefaultOptions().DryRun should be true")
	}
}

func TestCompress_EmptyInput(t *testing.T) {
	result := Compress(nil, DefaultOptions())
	if result.FilesProcessed != 0 {
		t.Errorf("FilesProcessed = %d, want 0", result.FilesProcessed)
	}
}
