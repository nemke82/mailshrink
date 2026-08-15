package estimator

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nemke82/mailshrink/internal/maildir"
)

func createTestMessages(t *testing.T, count int) []*maildir.Message {
	t.Helper()
	dir := t.TempDir()
	curDir := filepath.Join(dir, "cur")
	os.MkdirAll(curDir, 0755)

	var messages []*maildir.Message
	for i := 0; i < count; i++ {
		// Create realistic email content (text compresses well).
		content := strings.Repeat("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test message\r\nDate: Mon, 01 Jan 2024 12:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: text/plain\r\n\r\nThis is a test email message with some content that should be compressible.\r\nLorem ipsum dolor sit amet, consectetur adipiscing elit.\r\n\r\n", 10)

		name := filepath.Join(curDir, strings.Replace(
			"1234567890.M{{i}}.host:2,S", "{{i}}", string(rune('A'+i)), 1))
		if err := os.WriteFile(name, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		msg, err := maildir.LoadMessage(name)
		if err != nil {
			t.Fatal(err)
		}
		messages = append(messages, msg)
	}

	return messages
}

func TestEstimate_BasicRatio(t *testing.T) {
	messages := createTestMessages(t, 5)

	opts := DefaultOptions()
	opts.SampleSize = 5

	result, err := Estimate(messages, opts)
	if err != nil {
		t.Fatalf("Estimate() error = %v", err)
	}

	if result.SampleCount != 5 {
		t.Errorf("SampleCount = %d, want 5", result.SampleCount)
	}

	// Text emails should compress well — at least 40% ratio.
	if result.MeasuredRatio < 0.4 {
		t.Errorf("MeasuredRatio = %.4f, expected > 0.4 for text emails", result.MeasuredRatio)
	}

	if result.EstimatedSavings <= 0 {
		t.Error("expected positive EstimatedSavings")
	}

	if result.SampleCompressedSize >= result.SampleOriginalSize {
		t.Error("expected compressed size < original size for text content")
	}
}

func TestEstimate_EmptyInput(t *testing.T) {
	result, err := Estimate(nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Estimate() error = %v", err)
	}
	if result.SampleCount != 0 {
		t.Errorf("SampleCount = %d, want 0", result.SampleCount)
	}
}

func TestEstimate_AlreadyCompressedSkipped(t *testing.T) {
	dir := t.TempDir()
	curDir := filepath.Join(dir, "cur")
	os.MkdirAll(curDir, 0755)

	// Create a gzip file.
	gzPath := filepath.Join(curDir, "1234567890.host:2,S")
	f, _ := os.Create(gzPath)
	gw := gzip.NewWriter(f)
	gw.Write([]byte("compressed content"))
	gw.Close()
	f.Close()

	msg, _ := maildir.LoadMessage(gzPath)
	messages := []*maildir.Message{msg}

	result, err := Estimate(messages, DefaultOptions())
	if err != nil {
		t.Fatalf("Estimate() error = %v", err)
	}

	if result.SampleCount != 0 {
		t.Errorf("SampleCount = %d, want 0 (all compressed)", result.SampleCount)
	}
}

func TestEstimate_SampleSizeLargerThanMessages(t *testing.T) {
	messages := createTestMessages(t, 3)

	opts := DefaultOptions()
	opts.SampleSize = 100 // Larger than available.

	result, err := Estimate(messages, opts)
	if err != nil {
		t.Fatalf("Estimate() error = %v", err)
	}

	if result.SampleCount != 3 {
		t.Errorf("SampleCount = %d, want 3 (all available)", result.SampleCount)
	}
}
