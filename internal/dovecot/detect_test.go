package dovecot

import (
	"testing"
)

func TestCheckZlibInConfig_Enabled(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{
			name:   "zlib in mail_plugins",
			config: "mail_plugins = $mail_plugins zlib\n",
			want:   true,
		},
		{
			name:   "zlib standalone",
			config: "mail_plugins = zlib quota\n",
			want:   true,
		},
		{
			name:   "mail_compress (Dovecot 2.4+)",
			config: "mail_plugins = $mail_plugins mail_compress\n",
			want:   true,
		},
		{
			name:   "protocol section with zlib",
			config: "protocol imap {\n  mail_plugins = $mail_plugins zlib imap_zlib\n}\n",
			want:   true,
		},
		{
			name:   "no zlib",
			config: "mail_plugins = quota acl\n",
			want:   false,
		},
		{
			name:   "empty config",
			config: "",
			want:   false,
		},
		{
			name:   "commented out zlib",
			config: "# mail_plugins = $mail_plugins zlib\n",
			want:   false,
		},
		{
			name:   "zlib in value but not as plugin name",
			config: "some_setting = /path/to/zlib.so\n",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkZlibInConfig(tt.config)
			if got != tt.want {
				t.Errorf("checkZlibInConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractConfigValue(t *testing.T) {
	config := `# Dovecot config
mail_plugins = $mail_plugins zlib
plugin {
  zlib_save = gz
  zlib_save_level = 6
}
`
	tests := []struct {
		key  string
		want string
	}{
		{"zlib_save", "gz"},
		{"zlib_save_level", "6"},
		{"nonexistent", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := extractConfigValue(config, tt.key)
			if got != tt.want {
				t.Errorf("extractConfigValue(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestContainsPlugin(t *testing.T) {
	tests := []struct {
		value  string
		plugin string
		want   bool
	}{
		{"$mail_plugins zlib quota", "zlib", true},
		{"$mail_plugins quota", "zlib", false},
		{"zlib", "zlib", true},
		{"zlib_extra", "zlib", false}, // Whole-word match only.
		{"", "zlib", false},
	}

	for _, tt := range tests {
		t.Run(tt.value+"_"+tt.plugin, func(t *testing.T) {
			got := containsPlugin(tt.value, tt.plugin)
			if got != tt.want {
				t.Errorf("containsPlugin(%q, %q) = %v, want %v", tt.value, tt.plugin, got, tt.want)
			}
		})
	}
}

func TestInfo_IsReady(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want bool
	}{
		{"installed and zlib", Info{Installed: true, ZlibEnabled: true}, true},
		{"installed no zlib", Info{Installed: true, ZlibEnabled: false}, false},
		{"not installed", Info{Installed: false, ZlibEnabled: false}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.IsReady(); got != tt.want {
				t.Errorf("IsReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInfo_FixInstructions(t *testing.T) {
	// Not installed — should suggest installing.
	notInstalled := Info{Installed: false}
	fix := notInstalled.FixInstructions()
	if fix == "" {
		t.Error("expected fix instructions for not-installed Dovecot")
	}
	if !containsStr(fix, "apt install") || !containsStr(fix, "dnf install") {
		t.Error("expected distro-specific install commands")
	}

	// Installed but no zlib — should suggest config changes.
	noZlib := Info{Installed: true, ZlibEnabled: false}
	fix = noZlib.FixInstructions()
	if fix == "" {
		t.Error("expected fix instructions for missing zlib")
	}
	if !containsStr(fix, "mail_plugins") || !containsStr(fix, "zlib_save") {
		t.Error("expected Dovecot config instructions")
	}
	if !containsStr(fix, "systemctl restart dovecot") {
		t.Error("expected restart command")
	}

	// Ready — no fix needed.
	ready := Info{Installed: true, ZlibEnabled: true}
	fix = ready.FixInstructions()
	if fix != "" {
		t.Errorf("expected empty fix for ready system, got %q", fix)
	}
}

func containsStr(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && contains(s, substr)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
