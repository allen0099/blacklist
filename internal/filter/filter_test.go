package filter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/allen0099/blacklist/internal/filter"
)

// helpers ----------------------------------------------------------------

// makeTree creates a temporary directory containing the given file paths
// (relative to the returned root). Directories are created automatically.
func makeTree(t *testing.T, files []string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("makeTree mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte("content"), 0o644); err != nil {
			t.Fatalf("makeTree writefile: %v", err)
		}
	}
	return root
}

// writePatternFile writes lines to a temp file and returns its path.
func writePatternFile(t *testing.T, lines ...string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "patterns-*.txt")
	if err != nil {
		t.Fatalf("writePatternFile: %v", err)
	}
	defer f.Close()
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	return f.Name()
}

// resultMap converts a slice of Results to a map[rel]Result for easy lookup.
func resultMap(t *testing.T, root string, results []filter.Result) map[string]filter.Result {
	t.Helper()
	m := make(map[string]filter.Result, len(results))
	for _, r := range results {
		rel, err := filepath.Rel(root, r.Path)
		if err != nil {
			t.Fatalf("resultMap Rel: %v", err)
		}
		m[filepath.ToSlash(rel)] = r
	}
	return m
}

// tests ------------------------------------------------------------------

func TestRun_NoPatterns_AllIncluded(t *testing.T) {
	root := makeTree(t, []string{"a.go", "b.go", "sub/c.go"})

	results, err := filter.Run(filter.Options{Dir: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := resultMap(t, root, results)
	for _, key := range []string{"a.go", "b.go", "sub/c.go"} {
		r, ok := m[key]
		if !ok {
			t.Errorf("expected %q in results", key)
			continue
		}
		if !r.Included() {
			t.Errorf("%q should be included when no patterns given", key)
		}
	}
}

func TestRun_ExcludePattern(t *testing.T) {
	root := makeTree(t, []string{"main.go", "main_test.go", "README.md"})
	ef := writePatternFile(t, "*.md")

	results, err := filter.Run(filter.Options{
		Dir:          root,
		ExcludeFiles: []string{ef},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := resultMap(t, root, results)

	if !m["main.go"].Included() {
		t.Error("main.go should be included")
	}
	if !m["main_test.go"].Included() {
		t.Error("main_test.go should be included")
	}
	if m["README.md"].Included() {
		t.Error("README.md should be excluded by *.md pattern")
	}
}

func TestRun_ProtectOverridesExclude(t *testing.T) {
	root := makeTree(t, []string{"keep.txt", "drop.txt"})
	ef := writePatternFile(t, "*.txt")   // exclude all .txt files
	pf := writePatternFile(t, "keep.txt") // but protect keep.txt

	results, err := filter.Run(filter.Options{
		Dir:          root,
		ExcludeFiles: []string{ef},
		ProtectFiles: []string{pf},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := resultMap(t, root, results)

	if !m["keep.txt"].Included() {
		t.Error("keep.txt should be included because it is protected")
	}
	if !m["keep.txt"].Protected {
		t.Error("keep.txt should be marked as protected")
	}
	if m["drop.txt"].Included() {
		t.Error("drop.txt should be excluded")
	}
}

func TestRun_MultipleExcludeFiles(t *testing.T) {
	root := makeTree(t, []string{"a.go", "b.log", "c.tmp"})
	ef1 := writePatternFile(t, "*.log")
	ef2 := writePatternFile(t, "*.tmp")

	results, err := filter.Run(filter.Options{
		Dir:          root,
		ExcludeFiles: []string{ef1, ef2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := resultMap(t, root, results)
	if !m["a.go"].Included() {
		t.Error("a.go should be included")
	}
	if m["b.log"].Included() {
		t.Error("b.log should be excluded by ef1")
	}
	if m["c.tmp"].Included() {
		t.Error("c.tmp should be excluded by ef2")
	}
}

func TestRun_CommentLines(t *testing.T) {
	root := makeTree(t, []string{"a.go", "b.txt"})
	ef := writePatternFile(t, "# this is a comment", "*.txt")

	results, err := filter.Run(filter.Options{
		Dir:          root,
		ExcludeFiles: []string{ef},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := resultMap(t, root, results)
	if !m["a.go"].Included() {
		t.Error("a.go should be included")
	}
	if m["b.txt"].Included() {
		t.Error("b.txt should be excluded")
	}
}

func TestRun_DirectoryExclusion(t *testing.T) {
	root := makeTree(t, []string{"vendor/lib.go", "main.go"})
	ef := writePatternFile(t, "vendor/")

	results, err := filter.Run(filter.Options{
		Dir:          root,
		ExcludeFiles: []string{ef},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := resultMap(t, root, results)
	if !m["main.go"].Included() {
		t.Error("main.go should be included")
	}
	if m["vendor"].Included() {
		t.Error("vendor directory should be excluded")
	}
}

func TestRun_InvalidDirectory(t *testing.T) {
	_, err := filter.Run(filter.Options{Dir: "/nonexistent-dir-xyz"})
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestRun_PathIsFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "file-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = filter.Run(filter.Options{Dir: f.Name()})
	if err == nil {
		t.Error("expected error when Dir points to a file")
	}
}

func TestRun_InvalidExcludeFile(t *testing.T) {
	root := makeTree(t, []string{"a.go"})
	_, err := filter.Run(filter.Options{
		Dir:          root,
		ExcludeFiles: []string{"/nonexistent-patterns.txt"},
	})
	if err == nil {
		t.Error("expected error for nonexistent exclude file")
	}
}

func TestRun_InvalidProtectFile(t *testing.T) {
	root := makeTree(t, []string{"a.go"})
	_, err := filter.Run(filter.Options{
		Dir:          root,
		ProtectFiles: []string{"/nonexistent-patterns.txt"},
	})
	if err == nil {
		t.Error("expected error for nonexistent protect file")
	}
}

func TestRun_CopyToOutput(t *testing.T) {
	root := makeTree(t, []string{"keep.go", "drop.log"})
	ef := writePatternFile(t, "*.log")
	outDir := t.TempDir()

	_, err := filter.Run(filter.Options{
		Dir:          root,
		ExcludeFiles: []string{ef},
		OutputDir:    outDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// keep.go should have been copied
	dst := filepath.Join(outDir, "keep.go")
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		t.Errorf("expected %q to be copied to output dir", "keep.go")
	}

	// drop.log should NOT have been copied
	dropped := filepath.Join(outDir, "drop.log")
	if _, err := os.Stat(dropped); !os.IsNotExist(err) {
		t.Errorf("expected %q NOT to be copied to output dir", "drop.log")
	}
}

func TestRun_DryRunNoCopy(t *testing.T) {
	root := makeTree(t, []string{"keep.go", "drop.log"})
	ef := writePatternFile(t, "*.log")
	outDir := t.TempDir()

	_, err := filter.Run(filter.Options{
		Dir:          root,
		ExcludeFiles: []string{ef},
		OutputDir:    outDir,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Nothing should have been copied in dry-run mode.
	dst := filepath.Join(outDir, "keep.go")
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dry-run: expected %q NOT to be created", "keep.go")
	}
}

func TestRun_CopySubdirectory(t *testing.T) {
	root := makeTree(t, []string{"src/main.go", "src/util.go", "README.md"})
	outDir := t.TempDir()

	_, err := filter.Run(filter.Options{
		Dir:       root,
		OutputDir: outDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, rel := range []string{"src/main.go", "src/util.go", "README.md"} {
		dst := filepath.Join(outDir, filepath.FromSlash(rel))
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			t.Errorf("expected %q to be copied to output dir", rel)
		}
	}
}

func TestResult_Included(t *testing.T) {
	tests := []struct {
		name     string
		r        filter.Result
		expected bool
	}{
		{"not excluded not protected", filter.Result{Excluded: false, Protected: false}, true},
		{"excluded not protected", filter.Result{Excluded: true, Protected: false}, false},
		{"excluded and protected", filter.Result{Excluded: true, Protected: true}, true},
		{"not excluded but protected", filter.Result{Excluded: false, Protected: true}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Included(); got != tc.expected {
				t.Errorf("Included() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestRun_DryRunWithDirectory(t *testing.T) {
root := makeTree(t, []string{"sub/file.go", "main.go"})
outDir := t.TempDir()

_, err := filter.Run(filter.Options{
Dir:       root,
OutputDir: outDir,
DryRun:    true,
})
if err != nil {
t.Fatalf("unexpected error: %v", err)
}

// In dry-run mode, no files or directories should be created.
entries, _ := os.ReadDir(outDir)
if len(entries) != 0 {
t.Errorf("dry-run: expected empty output dir, got %d entries", len(entries))
}
}

func TestRun_PatternFileNoTrailingNewline(t *testing.T) {
root := makeTree(t, []string{"a.go", "b.log"})

// Write a pattern file without a trailing newline.
patFile := filepath.Join(t.TempDir(), "patterns.txt")
if err := os.WriteFile(patFile, []byte("*.log"), 0o644); err != nil {
t.Fatal(err)
}

results, err := filter.Run(filter.Options{
Dir:          root,
ExcludeFiles: []string{patFile},
})
if err != nil {
t.Fatalf("unexpected error: %v", err)
}

m := resultMap(t, root, results)
if !m["a.go"].Included() {
t.Error("a.go should be included")
}
if m["b.log"].Included() {
t.Error("b.log should be excluded by pattern without trailing newline")
}
}

func TestRun_MultipleProtectFiles(t *testing.T) {
root := makeTree(t, []string{"keep1.txt", "keep2.txt", "drop.txt"})
ef := writePatternFile(t, "*.txt")
pf1 := writePatternFile(t, "keep1.txt")
pf2 := writePatternFile(t, "keep2.txt")

results, err := filter.Run(filter.Options{
Dir:          root,
ExcludeFiles: []string{ef},
ProtectFiles: []string{pf1, pf2},
})
if err != nil {
t.Fatalf("unexpected error: %v", err)
}

m := resultMap(t, root, results)
if !m["keep1.txt"].Included() {
t.Error("keep1.txt should be included (protected by pf1)")
}
if !m["keep2.txt"].Included() {
t.Error("keep2.txt should be included (protected by pf2)")
}
if m["drop.txt"].Included() {
t.Error("drop.txt should be excluded")
}
}

func TestRun_CopyOutput_UnreadableSource(t *testing.T) {
if os.Getuid() == 0 {
t.Skip("running as root; file permission tests are not meaningful")
}
root := makeTree(t, []string{"secret.go"})
outDir := t.TempDir()

// Make the file unreadable.
secretPath := filepath.Join(root, "secret.go")
if err := os.Chmod(secretPath, 0o000); err != nil {
t.Fatal(err)
}
t.Cleanup(func() { os.Chmod(secretPath, 0o644) }) //nolint:errcheck

_, err := filter.Run(filter.Options{
Dir:       root,
OutputDir: outDir,
})
if err == nil {
t.Error("expected error when source file is unreadable")
}
}

func TestRun_CopyOutput_UnwritableDestination(t *testing.T) {
if os.Getuid() == 0 {
t.Skip("running as root; file permission tests are not meaningful")
}
root := makeTree(t, []string{"file.go"})
outDir := t.TempDir()

// Make the output directory read-only so we can't create files in it.
if err := os.Chmod(outDir, 0o555); err != nil {
t.Fatal(err)
}
t.Cleanup(func() { os.Chmod(outDir, 0o755) }) //nolint:errcheck

_, err := filter.Run(filter.Options{
Dir:       root,
OutputDir: outDir,
})
if err == nil {
t.Error("expected error when output directory is not writable")
}
}

func TestRun_WalkError(t *testing.T) {
if os.Getuid() == 0 {
t.Skip("running as root; permission tests are not meaningful")
}
root := makeTree(t, []string{"sub/hidden.go", "main.go"})
subDir := filepath.Join(root, "sub")

// Make the sub-directory inaccessible so WalkDir receives an error.
if err := os.Chmod(subDir, 0o000); err != nil {
t.Fatal(err)
}
t.Cleanup(func() { os.Chmod(subDir, 0o755) }) //nolint:errcheck

// The walker should continue past the error (it logs a warning).
results, err := filter.Run(filter.Options{Dir: root})
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
// At least main.go should still be present.
m := resultMap(t, root, results)
if _, ok := m["main.go"]; !ok {
t.Error("main.go should still appear despite walk error in sub/")
}
}
