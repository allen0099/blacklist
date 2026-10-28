// Package filter provides directory-walking logic that applies gitignore-style
// exclude and protect rules to each file found.
//
// Rules:
//   - A file is "excluded" when it matches at least one pattern from the
//     exclude set.
//   - A file is "protected" when it matches at least one pattern from the
//     protect set.
//   - Priority: protect > exclude.  When a file would be excluded but also
//     matches a protect rule it is kept (included).
package filter

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	ignore "github.com/sabhiram/go-gitignore"
)

// Result describes what happened to a single file during a filter run.
type Result struct {
	Path      string // absolute path
	Rel       string // slash-separated path relative to Options.Dir
	IsDir     bool
	Excluded  bool
	Protected bool
	// ExcludedBy and ProtectedBy name the matching rule, e.g.
	// "exclude.txt:3: *.log".
	ExcludedBy  string
	ProtectedBy string
}

// Included reports whether the file should be kept (not excluded, or protected
// despite being excluded).
func (r Result) Included() bool {
	if r.Protected {
		return true
	}
	return !r.Excluded
}

// Options carries the configuration for a filter run.
type Options struct {
	// Dir is the root directory to walk.
	Dir string
	// ExcludeFiles contains paths to gitignore-format files whose patterns
	// determine which files to exclude.  "-" reads Stdin.
	ExcludeFiles []string
	// ProtectFiles contains paths to gitignore-format files whose patterns
	// determine which files to protect from exclusion.  "-" reads Stdin.
	ProtectFiles []string
	// ExcludePatterns and ProtectPatterns are individual patterns given
	// directly, applied after the ones from files.
	ExcludePatterns []string
	ProtectPatterns []string
	// Stdin is read for a pattern file named "-".
	Stdin io.Reader
	// UseGitignore also excludes paths ignored by .gitignore files in the
	// tree (each applying to its own directory) and the .git directory.
	UseGitignore bool
	// OutputDir, when non-empty, causes included files to be copied there
	// preserving the relative directory structure.
	OutputDir string
	// PreserveSymlinks recreates symlinks in OutputDir instead of skipping
	// them.  Links are copied as-is and never followed.
	PreserveSymlinks bool
	// DryRun prevents any file-system writes while still printing what would
	// happen.
	DryRun bool
	// Logger is used for diagnostic output.  If nil, the default slog logger
	// is used.
	Logger *slog.Logger
}

// Run executes the filter walk and returns results for every file encountered.
func Run(opts Options) ([]Result, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	// Resolve the target directory.
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolving directory: %w", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("accessing directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", dir)
	}

	// Resolve the output directory.  Writing into the source tree itself (or
	// into a parent of it) would overwrite the files being read.
	var outDir string
	if opts.OutputDir != "" {
		if outDir, err = filepath.Abs(opts.OutputDir); err != nil {
			return nil, fmt.Errorf("resolving output directory: %w", err)
		}
		if within(outDir, dir) {
			return nil, fmt.Errorf("output directory %q must not contain the source directory %q", outDir, dir)
		}
	}

	// Compile patterns.
	stdinUses := 0
	for _, f := range slices.Concat(opts.ExcludeFiles, opts.ProtectFiles) {
		if f == "-" {
			stdinUses++
		}
	}
	if stdinUses > 1 {
		return nil, fmt.Errorf("stdin (-) can only be used for one pattern file")
	}

	excludeRules, err := loadRules(opts.ExcludeFiles, opts.ExcludePatterns, opts.Stdin)
	if err != nil {
		return nil, fmt.Errorf("compiling exclude patterns: %w", err)
	}

	protectRules, err := loadRules(opts.ProtectFiles, opts.ProtectPatterns, opts.Stdin)
	if err != nil {
		return nil, fmt.Errorf("compiling protect patterns: %w", err)
	}

	// gitignores holds the .gitignore rules per directory ("" is the root,
	// "a/b/" a subdirectory); nil entries mean the directory has none.
	gitignores := map[string]*rules{}
	if opts.UseGitignore {
		if gitignores[""], err = loadGitignore(dir, ""); err != nil {
			return nil, err
		}
	}

	log.Debug("starting filter walk", "dir", dir,
		"excludeFiles", opts.ExcludeFiles,
		"protectFiles", opts.ProtectFiles)

	var results []Result
	// excludedDir is the excluded (unprotected) directory whose subtree is
	// being walked, so that its descendants inherit the exclusion.
	var excludedDir, excludedDirBy string

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Warn("walk error", "path", path, "error", walkErr)
			return nil // continue walking
		}

		// Compute the path relative to the root for pattern matching.
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Skip the root itself.
		if rel == "." {
			return nil
		}

		// Never walk into the output directory when it lives inside dir.
		if d.IsDir() && path == outDir {
			return fs.SkipDir
		}

		// Use forward slashes for consistent cross-platform matching.
		// Append a trailing slash for directories so that patterns like
		// "vendor/" are correctly recognised as directory-only rules.
		relSlash := filepath.ToSlash(rel)
		matchPath := relSlash
		if d.IsDir() {
			matchPath = relSlash + "/"
		}

		if excludedDir != "" && !strings.HasPrefix(relSlash, excludedDir) {
			excludedDir = ""
		}
		excluded, excludedBy := excludeRules.match(matchPath)
		if !excluded && opts.UseGitignore {
			excluded, excludedBy = matchGitignores(gitignores, matchPath)
			if d.Name() == ".git" {
				excluded, excludedBy = true, ".git"
			}
		}
		if excludedDir != "" {
			excluded, excludedBy = true, excludedDirBy
		}
		protected, protectedBy := protectRules.match(matchPath)

		r := Result{
			Path:        path,
			Rel:         relSlash,
			IsDir:       d.IsDir(),
			Excluded:    excluded,
			Protected:   protected,
			ExcludedBy:  excludedBy,
			ProtectedBy: protectedBy,
		}

		if r.Included() {
			log.Debug("included", "rel", relSlash)
		} else {
			log.Debug("excluded", "rel", relSlash)
		}

		results = append(results, r)

		// An excluded, unprotected directory: skip its subtree unless protect
		// rules could still keep something inside it.
		if d.IsDir() && excluded && !protected {
			if protectRules == nil {
				return fs.SkipDir
			}
			if excludedDir == "" {
				excludedDir, excludedDirBy = matchPath, excludedBy
			}
		}

		if d.IsDir() && opts.UseGitignore {
			if gitignores[matchPath], err = loadGitignore(dir, matchPath); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	// Copy included files to output directory if requested.
	if outDir != "" {
		if err := copyResults(results, outDir, opts, log); err != nil {
			return results, err
		}
	}

	return results, nil
}

// within reports whether child is parent itself or lies inside it.
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// rules is a compiled pattern set that remembers where each line came from.
type rules struct {
	gi      *ignore.GitIgnore
	origins []string // origins[i] names line i+1, e.g. "exclude.txt:3"
}

// addLines appends the lines of data to lines, labelling each "name:N".
// CR is stripped so CRLF-edited files match like LF ones.
func addLines(lines, origins []string, name, data string) ([]string, []string) {
	for i, line := range strings.Split(data, "\n") {
		lines = append(lines, strings.TrimSuffix(line, "\r"))
		origins = append(origins, fmt.Sprintf("%s:%d", name, i+1))
	}
	return lines, origins
}

// loadRules compiles the patterns from files (where "-" reads stdin) followed
// by patterns.  It returns nil when there are none.
func loadRules(files, patterns []string, stdin io.Reader) (*rules, error) {
	var lines, origins []string
	for _, f := range files {
		var data []byte
		var err error
		if f == "-" {
			if stdin == nil {
				return nil, fmt.Errorf("pattern file - given but no stdin available")
			}
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(f)
		}
		if err != nil {
			return nil, fmt.Errorf("reading pattern file %q: %w", f, err)
		}
		lines, origins = addLines(lines, origins, f, string(data))
	}
	for _, p := range patterns {
		lines = append(lines, p)
		origins = append(origins, "pattern")
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return &rules{ignore.CompileIgnoreLines(lines...), origins}, nil
}

// loadGitignore loads the .gitignore of directory relDir ("" or "a/b/") under
// root, returning nil if it has none.
func loadGitignore(root, relDir string) (*rules, error) {
	name := relDir + ".gitignore"
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", name, err)
	}
	lines, origins := addLines(nil, nil, name, string(data))
	return &rules{ignore.CompileIgnoreLines(lines...), origins}, nil
}

// matchGitignores checks matchPath against the .gitignore of each of its
// ancestor directories, each relative to its own directory.
//
// ponytail: any match excludes, so a deeper "!pattern" cannot re-include a
// path ignored higher up (go-gitignore does not report negation-only
// matches); git itself would re-include it.
func matchGitignores(gitignores map[string]*rules, matchPath string) (bool, string) {
	for i := 0; ; {
		if ok, by := gitignores[matchPath[:i]].match(matchPath[i:]); ok {
			return true, by
		}
		j := strings.IndexByte(matchPath[i:], '/')
		if j < 0 || i+j+1 == len(matchPath) {
			return false, ""
		}
		i += j + 1
	}
}

// match reports whether path matches, and if so which rule matched it.
func (r *rules) match(path string) (bool, string) {
	if r == nil {
		return false, ""
	}
	ok, p := r.gi.MatchesPathHow(path)
	if !ok {
		return false, ""
	}
	return true, r.origins[p.LineNo-1] + ": " + p.Line
}

// copyResults copies each included file to dstRoot, preserving the relative
// sub-tree, permissions and modification times.
func copyResults(results []Result, dstRoot string, opts Options, log *slog.Logger) error {
	type dirTime struct {
		path  string
		mtime time.Time
	}
	var dirs []dirTime

	for _, r := range results {
		if !r.Included() {
			continue
		}

		dst := filepath.Join(dstRoot, filepath.FromSlash(r.Rel))

		info, err := os.Lstat(r.Path)
		if err != nil {
			return fmt.Errorf("stat %q: %w", r.Path, err)
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			// Never follow symlinks: that could copy data from outside the
			// source tree.  Recreate them as links only when asked to.
			if !opts.PreserveSymlinks {
				log.Debug("skipping symlink", "path", r.Path)
				continue
			}
			if opts.DryRun {
				log.Info("dry-run: would link", "src", r.Path, "dst", dst)
				continue
			}
			if err := copySymlink(r.Path, dst); err != nil {
				return err
			}

		case info.IsDir():
			if opts.DryRun {
				log.Info("dry-run: would create directory", "dst", dst)
				continue
			}
			// Keep the owner's write bit so the directory can be filled.
			if err := os.MkdirAll(dst, info.Mode().Perm()|0o700); err != nil {
				return fmt.Errorf("creating directory %q: %w", dst, err)
			}
			dirs = append(dirs, dirTime{dst, info.ModTime()})

		default:
			if opts.DryRun {
				log.Info("dry-run: would copy", "src", r.Path, "dst", dst)
				continue
			}
			if err := copyFile(r.Path, dst, info); err != nil {
				return err
			}
		}
		log.Debug("copied", "src", r.Path, "dst", dst)
	}

	// Directory times last: creating entries inside a directory updates it.
	for _, d := range dirs {
		if err := os.Chtimes(d.path, time.Time{}, d.mtime); err != nil {
			return fmt.Errorf("setting times on %q: %w", d.path, err)
		}
	}
	return nil
}

// replace prepares dst to be written: it creates the parent directories and
// removes an existing entry, so a stale symlink in the output is never
// written through.
func replace(dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating parent directories for %q: %w", dst, err)
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("replacing %q: %w", dst, err)
	}
	return nil
}

// copySymlink recreates the symlink src at dst with the same target.
func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return fmt.Errorf("reading link %q: %w", src, err)
	}
	if err := replace(dst); err != nil {
		return err
	}
	if err := os.Symlink(target, dst); err != nil {
		return fmt.Errorf("creating link %q: %w", dst, err)
	}
	return nil
}

// copyFile copies src to dst with the permissions and modification time
// from info.
func copyFile(src, dst string, info fs.FileInfo) (err error) {
	if err := replace(dst); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating destination %q: %w", dst, err)
	}
	defer func() {
		// A failed Close can mean the data never reached the disk.
		if cerr := out.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing destination %q: %w", dst, cerr)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying %q to %q: %w", src, dst, err)
	}
	// Chmod after creation so the umask does not alter the copied mode.
	if err := out.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("setting mode on %q: %w", dst, err)
	}
	if err := os.Chtimes(dst, time.Time{}, info.ModTime()); err != nil {
		return fmt.Errorf("setting times on %q: %w", dst, err)
	}
	return nil
}
