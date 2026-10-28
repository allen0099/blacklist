package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allen0099/blacklist/cmd"
)

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

// run executes a fresh root command and returns what it wrote to stdout and
// stderr.
func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := cmd.NewRootCmd()
	root.SetArgs(args)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

func TestHelp(t *testing.T) {
	if out, _, _ := run(t, "--help"); !strings.Contains(out, "blacklist") {
		t.Errorf("help output should mention blacklist, got: %s", out)
	}
}

func TestFilterAndCopy(t *testing.T) {
	src := t.TempDir()
	writeFile(t, src, "keep.log", "k")
	writeFile(t, src, "drop.log", "d")
	writeFile(t, src, "main.go", "package main")
	ef := writeFile(t, t.TempDir(), "excl", "*.log\n")
	pf := writeFile(t, t.TempDir(), "prot", "keep.log\n")

	for _, dryRun := range []bool{false, true} {
		dst := t.TempDir()
		args := []string{"-e", ef, "-p", pf, "-o", dst, src}
		if dryRun {
			args = append(args, "--dry-run")
		}
		out, _, err := run(t, args...)
		if err != nil {
			t.Fatalf("dry-run=%v: unexpected error: %v", dryRun, err)
		}
		for name, want := range map[string]bool{"keep.log": true, "main.go": true, "drop.log": false} {
			if strings.Contains(out, name) != want {
				t.Errorf("dry-run=%v: %s listed = %v, want %v\n%s", dryRun, name, !want, want, out)
			}
			_, statErr := os.Stat(filepath.Join(dst, name))
			if copied := statErr == nil; copied != (want && !dryRun) {
				t.Errorf("dry-run=%v: %s copied = %v", dryRun, name, copied)
			}
		}
	}
}

func TestVerbosity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "")

	tests := []struct {
		name      string
		env       map[string]string
		args      []string
		wantInfo  bool
		wantDebug bool
	}{
		{"default", nil, nil, true, false},
		{"--verbose", nil, []string{"--verbose"}, true, true},
		{"--quiet", nil, []string{"--quiet"}, false, false},
		{"env verbose", map[string]string{cmd.EnvVerbose: "YES"}, nil, true, true},
		{"env quiet", map[string]string{cmd.EnvQuiet: "true"}, nil, false, false},
		{"env not truthy", map[string]string{cmd.EnvVerbose: "no"}, nil, true, false},
		{"--quiet beats env verbose", map[string]string{cmd.EnvVerbose: "1"}, []string{"--quiet"}, false, false},
		{"--verbose beats env quiet", map[string]string{cmd.EnvQuiet: "1"}, []string{"--verbose"}, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(cmd.EnvVerbose, "")
			t.Setenv(cmd.EnvQuiet, "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, stderr, err := run(t, append(tc.args, dir)...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.Contains(stderr, "level=INFO"); got != tc.wantInfo {
				t.Errorf("info logged = %v, want %v\n%s", got, tc.wantInfo, stderr)
			}
			if got := strings.Contains(stderr, "level=DEBUG"); got != tc.wantDebug {
				t.Errorf("debug logged = %v, want %v\n%s", got, tc.wantDebug, stderr)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"verbose and quiet": {"--verbose", "--quiet", t.TempDir()},
		"missing directory": {"/nonexistent-dir-xyz-abc"},
		"too many args":     {"dir1", "dir2"},
	} {
		if _, _, err := run(t, args...); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
