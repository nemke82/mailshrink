package cmd

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/nemke82/mailshrink/internal/dovecot"
	"github.com/nemke82/mailshrink/internal/output"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Verify Dovecot is installed and zlib plugin is enabled",
	Long: `Check verifies that Dovecot is properly configured to read
compressed messages. This should be run before compressing any mail.

It checks:
  • Dovecot is installed and accessible
  • The zlib plugin is loaded in mail_plugins
  • Compression format and level are configured
  • The plugin .so module exists on disk

If any issues are found, it provides exact fix instructions.`,
	RunE: runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, args []string) error {
	output.PrintBanner(Version)

	bold := color.New(color.Bold)
	green := color.New(color.FgGreen, color.Bold)
	red := color.New(color.FgRed, color.Bold)
	dim := color.New(color.Faint)
	yellow := color.New(color.FgYellow, color.Bold)

	fmt.Println("  Dovecot Status")
	dim.Println("  ──────────────────────────────────")

	info := dovecot.Detect()

	if flagJSON {
		return output.PrintJSON(info)
	}

	// Dovecot installed?
	if info.Installed {
		fmt.Printf("  Dovecot installed:     %s", green.Sprint("✓ yes"))
		if info.Version != "" && info.Version != "unknown" {
			fmt.Printf(" (%s)", info.Version)
		}
		fmt.Println()
	} else {
		fmt.Printf("  Dovecot installed:     %s\n", red.Sprint("✗ not found"))
		fmt.Println()
		red.Println("  ⚠ Dovecot is not installed on this system.")
		fmt.Println()
		fmt.Println(info.FixInstructions())
		return fmt.Errorf("dovecot not installed")
	}

	// Zlib plugin?
	if info.ZlibEnabled {
		fmt.Printf("  Zlib plugin loaded:    %s\n", green.Sprint("✓ yes"))
	} else {
		fmt.Printf("  Zlib plugin loaded:    %s\n", red.Sprint("✗ NOT enabled"))
	}

	// Compression format.
	if info.ZlibSaveFormat != "" {
		level := ""
		if info.ZlibSaveLevel >= 0 {
			level = fmt.Sprintf(" (level %d)", info.ZlibSaveLevel)
		}
		fmt.Printf("  Compression format:    %s%s\n", bold.Sprint(info.ZlibSaveFormat), level)
	} else {
		fmt.Printf("  Compression format:    %s\n", dim.Sprint("not configured"))
	}

	// Plugin module on disk.
	if info.PluginModulePath != "" {
		fmt.Printf("  Plugin module:         %s\n", dim.Sprint(info.PluginModulePath))
	}

	// Doveconf path.
	if info.DoveconfPath != "" && flagVerbose {
		fmt.Printf("  Doveconf path:         %s\n", dim.Sprint(info.DoveconfPath))
	}

	fmt.Println()

	// Final verdict.
	if info.IsReady() {
		green.Println("  ✓ Server is ready for MailShrink compression.")
		fmt.Println()
		return nil
	}

	// Not ready — show fix instructions.
	yellow.Println("  ⚠ Server is NOT ready for compression.")
	fmt.Println()
	fmt.Print(info.FixInstructions())
	fmt.Println()

	return fmt.Errorf("dovecot zlib plugin is not enabled")
}
