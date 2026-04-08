package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allen0099/blacklist/cmd"
)

// executeRoot runs Execute-equivalent logic via the exported test helper.
// We test the cobra command directly by reaching into its internals through the
// public Execute function, but that calls os.Exit on error. Instead we expose a
// testable variant.

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecute_Help(t *testing.T) {
	// Ensure the command can produce help text without panicking.
	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--help"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// Help exits with code 0; Execute() doesn't return an error in that case.
	_ = root.Execute()
	if !strings.Contains(out.String(), "blacklist") {
		t.Errorf("help output should contain 'blacklist', got: %s", out.String())
	}
}

func TestExecute_Default(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "README.md", "# hi")

	root := cmd.NewRootCmd()
	root.SetArgs([]string{dir})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "main.go") {
		t.Errorf("expected main.go in output, got: %s", output)
	}
}

func TestExecute_ExcludeFlag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "main.log", "log data")

	ef := writeFile(t, t.TempDir(), "excl.txt", "*.log\n")

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"-e", ef, dir})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "main.go") {
		t.Errorf("expected main.go in output, got: %s", output)
	}
	if strings.Contains(output, "main.log") {
		t.Errorf("did not expect main.log in output, got: %s", output)
	}
}

func TestExecute_ProtectFlag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "keep.go", "package main")
	writeFile(t, dir, "drop.go", "package main")

	ef := writeFile(t, t.TempDir(), "excl.txt", "*.go\n")
	pf := writeFile(t, t.TempDir(), "prot.txt", "keep.go\n")

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"-e", ef, "-p", pf, dir})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "keep.go") {
		t.Errorf("expected keep.go in output (protected), got: %s", output)
	}
	if strings.Contains(output, "drop.go") {
		t.Errorf("did not expect drop.go in output (excluded), got: %s", output)
	}
}

func TestExecute_OutputFlag(t *testing.T) {
	src := t.TempDir()
	writeFile(t, src, "main.go", "package main")
	writeFile(t, src, "notes.tmp", "tmp")

	ef := writeFile(t, t.TempDir(), "excl.txt", "*.tmp\n")
	out := t.TempDir()

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"-e", ef, "-o", out, src})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dst := filepath.Join(out, "main.go")
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		t.Errorf("expected main.go to be copied to output dir")
	}
	dropped := filepath.Join(out, "notes.tmp")
	if _, err := os.Stat(dropped); !os.IsNotExist(err) {
		t.Errorf("did not expect notes.tmp to be copied to output dir")
	}
}

func TestExecute_DryRun(t *testing.T) {
	src := t.TempDir()
	writeFile(t, src, "main.go", "package main")

	ef := writeFile(t, t.TempDir(), "excl.txt", "*.log\n")
	outDir := t.TempDir()

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"-e", ef, "-o", outDir, "--dry-run", src})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// In dry-run mode, nothing should be copied.
	dst := filepath.Join(outDir, "main.go")
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dry-run: expected main.go NOT to be copied")
	}
}

func TestExecute_VerboseFlag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "")

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--verbose", dir})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecute_QuietFlag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "")

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--quiet", dir})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecute_VerboseQuietMutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--verbose", "--quiet", dir})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	err := root.Execute()
	if err == nil {
		t.Error("expected error when both --verbose and --quiet are set")
	}
}

func TestExecute_InvalidDirectory(t *testing.T) {
	root := cmd.NewRootCmd()
	root.SetArgs([]string{"/nonexistent-dir-xyz-abc"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	err := root.Execute()
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestExecute_TooManyArgs(t *testing.T) {
	root := cmd.NewRootCmd()
	root.SetArgs([]string{"dir1", "dir2"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	err := root.Execute()
	if err == nil {
		t.Error("expected error when more than one positional arg is given")
	}
}
