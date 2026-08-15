package maildir

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseFilename_Standard(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantUID  string
		wantFlags string
		wantSize int64
		wantVSize int64
	}{
		{
			name:      "standard with flags",
			filename:  "1234567890.M123P456.host:2,S",
			wantUID:   "1234567890.M123P456.host",
			wantFlags: "S",
		},
		{
			name:      "with S= size field",
			filename:  "1234567890.M123P456.host,S=12345:2,S",
			wantUID:   "1234567890.M123P456.host,S=12345",
			wantFlags: "S",
			wantSize:  12345,
		},
		{
			name:      "with S= and W= fields",
			filename:  "1234567890.M123P456.host,S=12345,W=12400:2,SR",
			wantUID:   "1234567890.M123P456.host,S=12345,W=12400",
			wantFlags: "SR",
			wantSize:  12345,
			wantVSize: 12400,
		},
		{
			name:      "no info section (new mail)",
			filename:  "1234567890.M123P456.host",
			wantUID:   "1234567890.M123P456.host",
			wantFlags: "",
		},
		{
			name:      "multiple flags",
			filename:  "1234567890.host:2,FRS",
			wantUID:   "1234567890.host",
			wantFlags: "FRS",
		},
		{
			name:      "large S= value",
			filename:  "msg.host,S=1073741824:2,S",
			wantUID:   "msg.host,S=1073741824",
			wantFlags: "S",
			wantSize:  1073741824, // 1 GiB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseFilename("/tmp/cur/" + tt.filename)
			if err != nil {
				t.Fatalf("ParseFilename() error = %v", err)
			}

			if msg.UniqueID != tt.wantUID {
				t.Errorf("UniqueID = %q, want %q", msg.UniqueID, tt.wantUID)
			}
			if msg.Flags != tt.wantFlags {
				t.Errorf("Flags = %q, want %q", msg.Flags, tt.wantFlags)
			}
			if msg.Size != tt.wantSize {
				t.Errorf("Size = %d, want %d", msg.Size, tt.wantSize)
			}
			if msg.VSize != tt.wantVSize {
				t.Errorf("VSize = %d, want %d", msg.VSize, tt.wantVSize)
			}
			if msg.Dir != "cur" {
				t.Errorf("Dir = %q, want %q", msg.Dir, "cur")
			}
		})
	}
}

func TestBuildFilename_AddSize(t *testing.T) {
	tests := []struct {
		name         string
		basename     string
		originalSize int64
		want         string
	}{
		{
			name:         "add S= to filename without it",
			basename:     "1234567890.host:2,S",
			originalSize: 54321,
			want:         "1234567890.host,S=54321:2,S",
		},
		{
			name:         "replace existing S=",
			basename:     "1234567890.host,S=12345:2,S",
			originalSize: 54321,
			want:         "1234567890.host,S=54321:2,S",
		},
		{
			name:         "no info section",
			basename:     "1234567890.host",
			originalSize: 54321,
			want:         "1234567890.host,S=54321",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{Basename: tt.basename}
			got := BuildFilename(msg, tt.originalSize)
			if got != tt.want {
				t.Errorf("BuildFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildFilename_Roundtrip(t *testing.T) {
	// Parse a filename, build it with a new size, then parse again.
	original := "1234567890.M123P456.host,S=12345,W=12400:2,SR"
	msg, err := ParseFilename("/test/cur/" + original)
	if err != nil {
		t.Fatalf("ParseFilename() error = %v", err)
	}

	newName := BuildFilename(msg, 99999)
	msg2, err := ParseFilename("/test/cur/" + newName)
	if err != nil {
		t.Fatalf("ParseFilename(rebuilt) error = %v", err)
	}

	if msg2.Size != 99999 {
		t.Errorf("Size after roundtrip = %d, want 99999", msg2.Size)
	}
	if msg2.Flags != "SR" {
		t.Errorf("Flags after roundtrip = %q, want %q", msg2.Flags, "SR")
	}
}

func TestDetectCompression_GzipFile(t *testing.T) {
	// Create a temporary gzip file.
	dir := t.TempDir()
	gzPath := filepath.Join(dir, "test.gz")

	f, err := os.Create(gzPath)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	gw.Write([]byte("Hello, World!"))
	gw.Close()
	f.Close()

	msg := &Message{Path: gzPath}
	if err := DetectCompression(msg); err != nil {
		t.Fatalf("DetectCompression() error = %v", err)
	}
	if !msg.IsCompressed {
		t.Error("expected IsCompressed = true for gzip file")
	}
}

func TestDetectCompression_PlainFile(t *testing.T) {
	dir := t.TempDir()
	plainPath := filepath.Join(dir, "test.txt")

	if err := os.WriteFile(plainPath, []byte("Hello, World!"), 0644); err != nil {
		t.Fatal(err)
	}

	msg := &Message{Path: plainPath}
	if err := DetectCompression(msg); err != nil {
		t.Fatalf("DetectCompression() error = %v", err)
	}
	if msg.IsCompressed {
		t.Error("expected IsCompressed = false for plain file")
	}
}

func TestDetectCompression_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty")

	if err := os.WriteFile(emptyPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	msg := &Message{Path: emptyPath}
	if err := DetectCompression(msg); err != nil {
		t.Fatalf("DetectCompression() error = %v", err)
	}
	if msg.IsCompressed {
		t.Error("expected IsCompressed = false for empty file")
	}
}

func TestLoadMessage(t *testing.T) {
	// Create a temp Maildir-like structure with a message file.
	dir := t.TempDir()
	curDir := filepath.Join(dir, "cur")
	os.MkdirAll(curDir, 0755)

	content := []byte("From: test@example.com\r\nSubject: Test\r\n\r\nBody")
	msgPath := filepath.Join(curDir, "1234567890.M123.host,S=42:2,S")
	if err := os.WriteFile(msgPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	msg, err := LoadMessage(msgPath)
	if err != nil {
		t.Fatalf("LoadMessage() error = %v", err)
	}

	if msg.Size != 42 {
		t.Errorf("Size = %d, want 42", msg.Size)
	}
	if msg.PhysicalSize != int64(len(content)) {
		t.Errorf("PhysicalSize = %d, want %d", msg.PhysicalSize, len(content))
	}
	if msg.IsCompressed {
		t.Error("expected IsCompressed = false")
	}
	if msg.Dir != "cur" {
		t.Errorf("Dir = %q, want %q", msg.Dir, "cur")
	}
	if msg.Flags != "S" {
		t.Errorf("Flags = %q, want %q", msg.Flags, "S")
	}
}

func TestIsEligible(t *testing.T) {
	eligible := &Message{PhysicalSize: 1024, IsCompressed: false}
	if !IsEligible(eligible) {
		t.Error("expected eligible for uncompressed file with size > 0")
	}

	compressed := &Message{PhysicalSize: 1024, IsCompressed: true}
	if IsEligible(compressed) {
		t.Error("expected not eligible for compressed file")
	}

	empty := &Message{PhysicalSize: 0, IsCompressed: false}
	if IsEligible(empty) {
		t.Error("expected not eligible for empty file")
	}
}

func TestGetMessageDate(t *testing.T) {
	now := time.Now()
	msg := &Message{ModTime: now}
	if got := GetMessageDate(msg); !got.Equal(now) {
		t.Errorf("GetMessageDate() = %v, want %v", got, now)
	}
}
