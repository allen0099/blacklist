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
	root := cmd.NewRootCmd("v1.2.3")
	root.SetArgs(args)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

func TestHelpAndVersion(t *testing.T) {
	if out, _, _ := run(t, "--help"); !strings.Contains(out, "blacklist") {
		t.Errorf("help output should mention blacklist, got: %s", out)
	}
	if out, _, _ := run(t, "--version"); !strings.Contains(out, "v1.2.3") {
		t.Errorf("version output should contain v1.2.3, got: %s", out)
	}
}

func TestFilterAndCopy(t *testing.T) {
	src := t.TempDir()
	writeFile(t, src, "keep.log", "k")
	writeFile(t, src, "drop.log", "d")
	writeFile(t, src, "main.go", "package main")
	ef := writeFile(t, t.TempDir(), "excl", "*.log\n")

	for _, dryRun := range []bool{false, true} {
		dst := t.TempDir()
		args := []string{"-e", ef, "-P", "keep.log", "-o", dst, src}
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

func TestListing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "b.go", "")
	writeFile(t, dir, "sub/a.go", "")

	out, _, err := run(t, "-q", dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "b.go\nsub/a.go\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if out, _, _ = run(t, "-q", "-0", dir); out != "b.go\x00sub/a.go\x00" {
		t.Errorf("-0: got %q", out)
	}
}

func TestExplain(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "")
	writeFile(t, dir, "vendor/keep.go", "")
	writeFile(t, dir, "vendor/x.go", "")
	ef := writeFile(t, t.TempDir(), "excl", "# deps\nvendor/\n")

	out, _, err := run(t, "-q", "--explain", "-e", ef, "-P", "keep.go", dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "include\ta.go\n" +
		"exclude\tvendor/\texcluded by " + ef + ":2: vendor/\n" +
		"include\tvendor/keep.go\texcluded by " + ef + ":2: vendor/; protected by pattern: keep.go\n" +
		"exclude\tvendor/x.go\texcluded by " + ef + ":2: vendor/\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "")
	writeFile(t, dir, ".env", "")
	writeFile(t, dir, "secrets/key.pem", "")

	out, stderr, err := run(t, "-q", "--check", "-E", ".env", "-E", "secrets/", dir)
	if err == nil || !strings.Contains(stderr, "2 path(s)") {
		t.Errorf("expected check failure for 2 paths, got err=%v stderr=%s", err, stderr)
	}
	if want := ".env\nsecrets/\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}

	if out, _, err := run(t, "-q", "--check", "-E", "*.log", dir); err != nil || out != "" {
		t.Errorf("clean tree: err=%v out=%q", err, out)
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
		"check and output":  {"--check", "-o", t.TempDir(), t.TempDir()},
	} {
		if _, _, err := run(t, args...); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
