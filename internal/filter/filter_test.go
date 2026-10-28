package filter_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/allen0099/blacklist/internal/filter"
)

// makeTree creates a temporary directory containing the given file paths
// (relative to the returned root). Directories are created automatically.
func makeTree(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// patternFiles writes each content string to its own temp file.
func patternFiles(t *testing.T, contents ...string) []string {
	t.Helper()
	var paths []string
	for i, c := range contents {
		p := filepath.Join(t.TempDir(), "patterns"+string(rune('a'+i)))
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// included returns the sorted relative paths of the included results.
func included(results []filter.Result) []string {
	var out []string
	for _, r := range results {
		if r.Included() {
			out = append(out, r.Rel)
		}
	}
	slices.Sort(out)
	return out
}

func TestRun_Patterns(t *testing.T) {
	tests := []struct {
		name             string
		files            []string
		exclude, protect []string // pattern file contents
		want             []string
	}{
		{
			name:  "no patterns",
			files: []string{"a.go", "sub/c.go"},
			want:  []string{"a.go", "sub", "sub/c.go"},
		},
		{
			name:    "glob",
			files:   []string{"main.go", "README.md"},
			exclude: []string{"*.md\n"},
			want:    []string{"main.go"},
		},
		{
			name:    "comments, CRLF, no trailing newline",
			files:   []string{"a.go", "b.txt", "c.log"},
			exclude: []string{"# comment\r\n*.txt\r\n*.log"},
			want:    []string{"a.go"},
		},
		{
			name:    "multiple exclude files",
			files:   []string{"a.go", "b.log", "c.tmp"},
			exclude: []string{"*.log\n", "*.tmp\n"},
			want:    []string{"a.go"},
		},
		{
			name:    "directory",
			files:   []string{"vendor/lib.go", "main.go"},
			exclude: []string{"vendor/\n"},
			want:    []string{"main.go"},
		},
		{
			name:    "protect beats exclude",
			files:   []string{"keep1.txt", "keep2.txt", "drop.txt"},
			exclude: []string{"*.txt\n"},
			protect: []string{"keep1.txt\n", "keep2.txt\n"},
			want:    []string{"keep1.txt", "keep2.txt"},
		},
		{
			name:    "protect inside excluded directory",
			files:   []string{"vendor/keep.go", "vendor/sub/drop.go", "main.go", "zz.go"},
			exclude: []string{"vendor/\n"},
			protect: []string{"keep.go\n"},
			want:    []string{"main.go", "vendor/keep.go", "zz.go"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := makeTree(t, tc.files...)
			results, err := filter.Run(filter.Options{
				Dir:          root,
				ExcludeFiles: patternFiles(t, tc.exclude...),
				ProtectFiles: patternFiles(t, tc.protect...),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := included(results); !slices.Equal(got, tc.want) {
				t.Errorf("included = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRun_Errors(t *testing.T) {
	root := makeTree(t, "a.go")
	tests := map[string]filter.Options{
		"missing directory":      {Dir: "/nonexistent-dir-xyz"},
		"directory is a file":    {Dir: filepath.Join(root, "a.go")},
		"missing exclude file":   {Dir: root, ExcludeFiles: []string{"/nonexistent-patterns"}},
		"missing protect file":   {Dir: root, ProtectFiles: []string{"/nonexistent-patterns"}},
		"output is source":       {Dir: root, OutputDir: root},
		"output is above source": {Dir: root, OutputDir: filepath.Dir(root)},
	}
	for name, opts := range tests {
		if _, err := filter.Run(opts); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	// The source must survive the rejected self-copy untouched.
	if data, _ := os.ReadFile(filepath.Join(root, "a.go")); string(data) != "content" {
		t.Errorf("source file was modified: %q", data)
	}
}

func TestRun_Copy(t *testing.T) {
	root := makeTree(t, "src/main.go", "README.md", "drop.log")
	ef := patternFiles(t, "*.log\n")

	t.Run("copies included files", func(t *testing.T) {
		out := t.TempDir()
		if _, err := filter.Run(filter.Options{Dir: root, ExcludeFiles: ef, OutputDir: out}); err != nil {
			t.Fatal(err)
		}
		for _, rel := range []string{"src/main.go", "README.md"} {
			if data, err := os.ReadFile(filepath.Join(out, rel)); err != nil || string(data) != "content" {
				t.Errorf("%s: got %q, %v", rel, data, err)
			}
		}
		if _, err := os.Stat(filepath.Join(out, "drop.log")); !os.IsNotExist(err) {
			t.Error("drop.log should not be copied")
		}
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		out := t.TempDir()
		if _, err := filter.Run(filter.Options{Dir: root, OutputDir: out, DryRun: true}); err != nil {
			t.Fatal(err)
		}
		if entries, _ := os.ReadDir(out); len(entries) != 0 {
			t.Errorf("expected empty output dir, got %d entries", len(entries))
		}
	})

	t.Run("symlinks are not followed", func(t *testing.T) {
		src := makeTree(t, "a.go")
		if err := os.Symlink(filepath.Join(root, "README.md"), filepath.Join(src, "link")); err != nil {
			t.Skip("symlinks unsupported:", err)
		}
		out := t.TempDir()
		if _, err := filter.Run(filter.Options{Dir: src, OutputDir: out}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(out, "link")); !os.IsNotExist(err) {
			t.Error("symlink should not be copied")
		}
	})

	t.Run("output inside source is not walked", func(t *testing.T) {
		src := makeTree(t, "a.go")
		out := filepath.Join(src, "dist")
		for range 2 {
			if _, err := filter.Run(filter.Options{Dir: src, OutputDir: out}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := os.Stat(filepath.Join(out, "dist")); !os.IsNotExist(err) {
			t.Error("output directory was copied into itself")
		}
	})
}

func TestRun_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission semantics need a non-root Unix user")
	}
	// chmod sets mode on path and restores 0o755 after the test.
	chmod := func(t *testing.T, path string, mode os.FileMode) {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, 0o755) }) //nolint:errcheck
	}

	t.Run("read-only source directory is copied", func(t *testing.T) {
		root := makeTree(t, "ro/f.go")
		chmod(t, filepath.Join(root, "ro"), 0o555)
		out := t.TempDir()
		if _, err := filter.Run(filter.Options{Dir: root, OutputDir: out}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(out, "ro", "f.go")); err != nil {
			t.Error(err)
		}
	})

	t.Run("unreadable source file", func(t *testing.T) {
		root := makeTree(t, "secret.go")
		chmod(t, filepath.Join(root, "secret.go"), 0o000)
		if _, err := filter.Run(filter.Options{Dir: root, OutputDir: t.TempDir()}); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("unwritable output", func(t *testing.T) {
		out := t.TempDir()
		chmod(t, out, 0o555)
		if _, err := filter.Run(filter.Options{Dir: makeTree(t, "f.go"), OutputDir: out}); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("walk error is skipped", func(t *testing.T) {
		root := makeTree(t, "sub/hidden.go", "main.go")
		chmod(t, filepath.Join(root, "sub"), 0o000)
		results, err := filter.Run(filter.Options{Dir: root})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := included(results); !slices.Contains(got, "main.go") {
			t.Errorf("main.go missing from %v", got)
		}
	})
}
