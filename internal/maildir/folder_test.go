package maildir

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// createTestMaildir creates a temporary Maildir with the standard structure.
func createTestMaildir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for _, sub := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// createTestMessage creates a test message file in the given directory.
func createTestMessage(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestIsMaildir(t *testing.T) {
	// Valid Maildir.
	dir := createTestMaildir(t)
	if !IsMaildir(dir) {
		t.Error("expected IsMaildir = true for valid Maildir")
	}

	// Missing directory.
	noDir := t.TempDir()
	if IsMaildir(noDir) {
		t.Error("expected IsMaildir = false for empty directory")
	}

	// Partial Maildir (missing tmp/).
	partial := t.TempDir()
	os.MkdirAll(filepath.Join(partial, "cur"), 0755)
	os.MkdirAll(filepath.Join(partial, "new"), 0755)
	if IsMaildir(partial) {
		t.Error("expected IsMaildir = false when tmp/ is missing")
	}
}

func TestScanFolder(t *testing.T) {
	dir := createTestMaildir(t)

	folder, err := ScanFolder(dir)
	if err != nil {
		t.Fatalf("ScanFolder() error = %v", err)
	}

	if folder.CurPath != filepath.Join(dir, "cur") {
		t.Errorf("CurPath = %q, expected %q", folder.CurPath, filepath.Join(dir, "cur"))
	}
	if folder.NewPath != filepath.Join(dir, "new") {
		t.Errorf("NewPath = %q, expected %q", folder.NewPath, filepath.Join(dir, "new"))
	}
}

func TestScanFolder_NotMaildir(t *testing.T) {
	dir := t.TempDir()
	_, err := ScanFolder(dir)
	if err == nil {
		t.Error("expected error for non-Maildir directory")
	}
}

func TestDiscoverFolders(t *testing.T) {
	root := t.TempDir()

	// Create root Maildir (INBOX).
	for _, sub := range []string{"cur", "new", "tmp"} {
		os.MkdirAll(filepath.Join(root, sub), 0755)
	}

	// Create .Sent sub-folder.
	for _, sub := range []string{"cur", "new", "tmp"} {
		os.MkdirAll(filepath.Join(root, ".Sent", sub), 0755)
	}

	// Create .Drafts sub-folder.
	for _, sub := range []string{"cur", "new", "tmp"} {
		os.MkdirAll(filepath.Join(root, ".Drafts", sub), 0755)
	}

	// Create a non-Maildir directory (should be ignored).
	os.MkdirAll(filepath.Join(root, ".notmaildir"), 0755)

	folders, err := DiscoverFolders(root)
	if err != nil {
		t.Fatalf("DiscoverFolders() error = %v", err)
	}

	if len(folders) != 3 {
		t.Fatalf("expected 3 folders (INBOX, Sent, Drafts), got %d", len(folders))
	}

	names := make(map[string]bool)
	for _, f := range folders {
		names[f.Name] = true
	}

	for _, expected := range []string{"INBOX", "Sent", "Drafts"} {
		if !names[expected] {
			t.Errorf("expected folder %q not found", expected)
		}
	}
}

func TestListMessages(t *testing.T) {
	dir := createTestMaildir(t)

	// Create some test messages in cur/.
	createTestMessage(t, filepath.Join(dir, "cur"),
		"1234567890.host:2,S",
		"From: test@example.com\r\nSubject: Test 1\r\n\r\nBody 1")

	createTestMessage(t, filepath.Join(dir, "cur"),
		"1234567891.host:2,",
		"From: test@example.com\r\nSubject: Test 2\r\n\r\nBody 2")

	// Create a message in new/.
	createTestMessage(t, filepath.Join(dir, "new"),
		"1234567892.host",
		"From: test@example.com\r\nSubject: Test 3\r\n\r\nBody 3")

	folder, err := ScanFolder(dir)
	if err != nil {
		t.Fatal(err)
	}

	messages, err := ListMessages(folder, MessageFilter{})
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
}

func TestListMessages_OlderThanFilter(t *testing.T) {
	dir := createTestMaildir(t)

	// Create an old message.
	oldPath := filepath.Join(dir, "cur", "1234567890.host:2,S")
	os.WriteFile(oldPath, []byte("old message"), 0644)
	oldTime := time.Now().Add(-365 * 24 * time.Hour) // 1 year ago
	os.Chtimes(oldPath, oldTime, oldTime)

	// Create a recent message.
	newPath := filepath.Join(dir, "cur", "1234567891.host:2,")
	os.WriteFile(newPath, []byte("new message"), 0644)
	// Keep current mtime.

	folder, err := ScanFolder(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Filter for messages older than 6 months.
	messages, err := ListMessages(folder, MessageFilter{
		OlderThan: 180 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 old message, got %d", len(messages))
	}
}

func TestListMessages_UncompressedOnlyFilter(t *testing.T) {
	dir := createTestMaildir(t)

	// Create a plain message.
	createTestMessage(t, filepath.Join(dir, "cur"),
		"1234567890.host:2,S", "plain text email")

	// Create a gzip-compressed message (magic bytes 0x1f, 0x8b).
	gzContent := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00}
	os.WriteFile(filepath.Join(dir, "cur", "1234567891.host:2,S"),
		gzContent, 0644)

	folder, err := ScanFolder(dir)
	if err != nil {
		t.Fatal(err)
	}

	messages, err := ListMessages(folder, MessageFilter{UncompressedOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 uncompressed message, got %d", len(messages))
	}
	if messages[0].IsCompressed {
		t.Error("expected the returned message to be uncompressed")
	}
}
