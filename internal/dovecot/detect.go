// Package dovecot provides detection and verification of Dovecot
// mail server configuration, including zlib plugin status.
package dovecot

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Info contains detected Dovecot configuration details.
type Info struct {
	// Installed is true if Dovecot is found on the system.
	Installed bool

	// Version is the detected Dovecot version string (e.g., "2.3.21").
	Version string

	// ZlibEnabled is true if the zlib plugin is loaded in mail_plugins.
	ZlibEnabled bool

	// ZlibSaveFormat is the compression format for new mail (e.g., "gz", "zstd").
	// Empty if zlib_save is not configured.
	ZlibSaveFormat string

	// ZlibSaveLevel is the compression level (0-9). -1 if not configured.
	ZlibSaveLevel int

	// PluginModulePath is the path to the zlib plugin .so file, if found.
	PluginModulePath string

	// DoveconfPath is the path to the doveconf binary.
	DoveconfPath string

	// ConfigSnippet contains the relevant config lines for diagnostics.
	ConfigSnippet string
}

// Common paths where Dovecot plugin modules live.
var pluginSearchPaths = []string{
	"/usr/lib/dovecot/modules",
	"/usr/lib64/dovecot/modules",
	"/usr/local/lib/dovecot/modules",
	"/usr/libexec/dovecot",
}

// Common plugin filenames for the zlib module.
var zlibPluginNames = []string{
	"lib20_zlib_plugin.so",
	"lib20_mail_compress_plugin.so", // Dovecot 2.4+ renamed it
	"lib10_zlib_plugin.so",
}

var versionRegexp = regexp.MustCompile(`(\d+\.\d+\.\d+)`)
var zlibLevelRegexp = regexp.MustCompile(`zlib_save_level\s*=\s*(\d+)`)

// Detect checks the current system for Dovecot installation and
// zlib plugin configuration. This is safe to call — it only reads
// configuration, never modifies anything.
func Detect() *Info {
	info := &Info{
		ZlibSaveLevel: -1,
	}

	// Step 1: Find doveconf binary.
	doveconfPath, err := exec.LookPath("doveconf")
	if err != nil {
		// Try common paths.
		for _, p := range []string{"/usr/bin/doveconf", "/usr/sbin/doveconf", "/usr/local/bin/doveconf"} {
			if _, err := os.Stat(p); err == nil {
				doveconfPath = p
				break
			}
		}
	}

	if doveconfPath == "" {
		// No doveconf — check if dovecot binary exists at least.
		if dovecotPath, err := exec.LookPath("dovecot"); err == nil {
			info.Installed = true
			info.Version = getDovecotVersion(dovecotPath)
		}
		// Can't check config without doveconf.
		info.PluginModulePath = findPluginModule()
		if info.PluginModulePath != "" {
			info.Installed = true
		}
		return info
	}

	info.Installed = true
	info.DoveconfPath = doveconfPath

	// Step 2: Get Dovecot version.
	info.Version = getDoveconfVersion(doveconfPath)

	// Step 3: Check mail_plugins for zlib.
	confOutput := runDoveconf(doveconfPath, "-n")
	info.ConfigSnippet = confOutput

	info.ZlibEnabled = checkZlibInConfig(confOutput)

	// Step 4: Check zlib_save and zlib_save_level.
	info.ZlibSaveFormat = extractConfigValue(confOutput, "zlib_save")
	if levelStr := extractConfigValue(confOutput, "zlib_save_level"); levelStr != "" {
		if level, err := strconv.Atoi(levelStr); err == nil {
			info.ZlibSaveLevel = level
		}
	}

	// Step 5: Find the plugin .so file on disk.
	info.PluginModulePath = findPluginModule()

	return info
}

// IsReady returns true if Dovecot is installed and zlib is properly
// configured for reading compressed mail.
func (info *Info) IsReady() bool {
	return info.Installed && info.ZlibEnabled
}

// FixInstructions returns human-readable instructions for enabling
// the zlib plugin if it's not currently configured.
func (info *Info) FixInstructions() string {
	var b strings.Builder

	if !info.Installed {
		b.WriteString("  Dovecot is not installed on this system.\n\n")
		b.WriteString("  Install it first:\n")
		b.WriteString("    Ubuntu/Debian:  apt install dovecot-core\n")
		b.WriteString("    AlmaLinux/RHEL: dnf install dovecot\n")
		return b.String()
	}

	if info.ZlibEnabled {
		return ""
	}

	b.WriteString("  Dovecot's zlib plugin is not enabled. Compressed messages\n")
	b.WriteString("  will NOT be readable by IMAP clients until you enable it.\n\n")

	b.WriteString("  To fix, add to /etc/dovecot/conf.d/10-mail.conf:\n")
	b.WriteString("    mail_plugins = $mail_plugins zlib\n\n")

	b.WriteString("  And add to /etc/dovecot/conf.d/90-plugin.conf:\n")
	b.WriteString("    plugin {\n")
	b.WriteString("      zlib_save = gz\n")
	b.WriteString("      zlib_save_level = 6\n")
	b.WriteString("    }\n\n")

	b.WriteString("  Then restart Dovecot:\n")
	b.WriteString("    systemctl restart dovecot\n\n")

	b.WriteString("  After fixing, run 'mailshrink check' to verify.\n")

	return b.String()
}

// --- internal helpers ---

func runDoveconf(path string, args ...string) string {
	cmd := exec.Command(path, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}

func getDovecotVersion(dovecotPath string) string {
	cmd := exec.Command(dovecotPath, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return "unknown"
	}
	if m := versionRegexp.FindString(out.String()); m != "" {
		return m
	}
	return strings.TrimSpace(out.String())
}

func getDoveconfVersion(doveconfPath string) string {
	output := runDoveconf(doveconfPath, "-n")
	// doveconf -n header includes: # <version> <hash>
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			if m := versionRegexp.FindString(line); m != "" {
				return m
			}
		}
	}
	// Fallback: try dovecot --version.
	if dovecotPath, err := exec.LookPath("dovecot"); err == nil {
		return getDovecotVersion(dovecotPath)
	}
	return "unknown"
}

// checkZlibInConfig checks if zlib appears in any mail_plugins directive.
func checkZlibInConfig(config string) bool {
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Match: mail_plugins = ... zlib ...
		// Also match: mail_plugins = $mail_plugins zlib
		if strings.Contains(line, "mail_plugins") && strings.Contains(line, "=") {
			value := line[strings.Index(line, "=")+1:]
			// Check for zlib or mail_compress (Dovecot 2.4+ name).
			if containsPlugin(value, "zlib") || containsPlugin(value, "mail_compress") {
				return true
			}
		}
	}
	return false
}

// containsPlugin checks if a plugin name appears as a whole word in the value.
func containsPlugin(value, plugin string) bool {
	for _, word := range strings.Fields(value) {
		if word == plugin {
			return true
		}
	}
	return false
}

func extractConfigValue(config, key string) string {
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, key) && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func findPluginModule() string {
	for _, dir := range pluginSearchPaths {
		for _, name := range zlibPluginNames {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}

// CheckDoveconfAvailable returns an error message if doveconf is not
// available on the system. Used for early warnings.
func CheckDoveconfAvailable() error {
	if _, err := exec.LookPath("doveconf"); err == nil {
		return nil
	}
	for _, p := range []string{"/usr/bin/doveconf", "/usr/sbin/doveconf", "/usr/local/bin/doveconf"} {
		if _, err := os.Stat(p); err == nil {
			return nil
		}
	}
	return fmt.Errorf("doveconf not found — cannot verify Dovecot configuration")
}
