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
//   - Both apply to everything inside a matching directory.
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

	"github.com/bmatcuk/doublestar/v4"
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
	gitignores := map[string]rules{}
	if opts.UseGitignore {
		if gitignores[""], err = loadGitignore(dir, ""); err != nil {
			return nil, err
		}
	}

	log.Debug("starting filter walk", "dir", dir,
		"excludeFiles", opts.ExcludeFiles,
		"protectFiles", opts.ProtectFiles)

	var results []Result
	// Descendants of an excluded or protected directory inherit its verdict.
	var excludedDir, protectedDir inherited

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
		relSlash := filepath.ToSlash(rel)

		excluded, excludedBy := excludeRules.match(relSlash, d.IsDir())
		if !excluded && opts.UseGitignore {
			excluded, excludedBy = matchGitignores(gitignores, relSlash, d.IsDir())
			if d.Name() == ".git" {
				excluded, excludedBy = true, ".git"
			}
		}
		if ok, by := excludedDir.get(relSlash); ok {
			excluded, excludedBy = true, by
		}
		protected, protectedBy := protectRules.match(relSlash, d.IsDir())
		if ok, by := protectedDir.get(relSlash); ok {
			protected, protectedBy = true, by
		}

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

		if !d.IsDir() {
			return nil
		}
		// An excluded, unprotected directory: skip its subtree unless protect
		// rules could still keep something inside it.
		if excluded && !protected && len(protectRules) == 0 {
			return fs.SkipDir
		}
		if excluded {
			excludedDir.set(relSlash, excludedBy)
		}
		if protected {
			protectedDir.set(relSlash, protectedBy)
		}
		if opts.UseGitignore {
			if gitignores[relSlash+"/"], err = loadGitignore(dir, relSlash+"/"); err != nil {
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

// inherited tracks the outermost directory whose verdict its descendants
// share.  The walk is depth-first, so one directory at a time is enough.
type inherited struct{ prefix, by string }

// get reports whether rel lies inside the tracked directory.
func (h *inherited) get(rel string) (bool, string) {
	if h.prefix != "" && !strings.HasPrefix(rel, h.prefix) {
		h.prefix = ""
	}
	return h.prefix != "", h.by
}

// set starts tracking dir unless an enclosing directory already is.
func (h *inherited) set(dir, by string) {
	if h.prefix == "" {
		h.prefix, h.by = dir+"/", by
	}
}

// pattern is one gitignore line compiled to a doublestar glob.
type pattern struct {
	glob    string
	negate  bool
	dirOnly bool
	origin  string // e.g. "exclude.txt:3: *.log"
}

// rules is an ordered pattern list; later patterns take precedence.
type rules []pattern

// parsePattern compiles one gitignore line, reporting false for blank lines
// and comments.  See https://git-scm.com/docs/gitignore#_pattern_format.
func parsePattern(line, origin string) (pattern, bool) {
	line = strings.TrimSuffix(line, "\r")
	if line == "" || line[0] == '#' {
		return pattern{}, false
	}
	// Trailing spaces are ignored unless escaped with a backslash.
	for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, `\ `) {
		line = line[:len(line)-1]
	}
	p := pattern{origin: origin + ": " + line}
	switch {
	case line[0] == '!':
		p.negate, line = true, line[1:]
	case strings.HasPrefix(line, `\!`), strings.HasPrefix(line, `\#`):
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		p.dirOnly, line = true, strings.TrimSuffix(line, "/")
	}
	if line == "" {
		return pattern{}, false
	}
	// Without a slash the pattern matches at any depth; with one it is
	// relative to the pattern file's directory.
	if !strings.Contains(line, "/") {
		line = "**/" + line
	}
	line = strings.TrimPrefix(line, "/")
	// "a/**" matches everything inside a, but not a itself.
	if strings.HasSuffix(line, "/**") {
		line += "/*"
	}
	// doublestar reads {a,b} as alternatives; gitignore does not.
	p.glob = strings.NewReplacer("{", `\{`, "}", `\}`).Replace(line)
	return p, true
}

// addLines appends the patterns in data, labelling each "name:N".
func (r rules) addLines(name, data string) rules {
	for i, line := range strings.Split(data, "\n") {
		if p, ok := parsePattern(line, fmt.Sprintf("%s:%d", name, i+1)); ok {
			r = append(r, p)
		}
	}
	return r
}

// match reports whether the last pattern matching rel is a non-negated one,
// and which pattern that is.  Matching a directory is left to the caller,
// which applies the verdict to everything inside it.
func (r rules) match(rel string, isDir bool) (bool, string) {
	decided, matched, by := r.decide(rel, isDir)
	return decided && matched, by
}

// decide is match that also reports whether any pattern matched at all, so
// that a negation can override an earlier decision.
func (r rules) decide(rel string, isDir bool) (decided, matched bool, by string) {
	for i := len(r) - 1; i >= 0; i-- {
		p := r[i]
		if p.dirOnly && !isDir {
			continue
		}
		if ok, _ := doublestar.Match(p.glob, rel); ok {
			return true, !p.negate, p.origin
		}
	}
	return false, false, ""
}

// loadRules compiles the patterns from files (where "-" reads stdin) followed
// by patterns.
func loadRules(files, patterns []string, stdin io.Reader) (rules, error) {
	var r rules
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
		r = r.addLines(f, string(data))
	}
	for _, p := range patterns {
		if pat, ok := parsePattern(p, "pattern"); ok {
			r = append(r, pat)
		}
	}
	return r, nil
}

// loadGitignore loads the .gitignore of directory relDir ("" or "a/b/") under
// root, returning nil if it has none.
func loadGitignore(root, relDir string) (rules, error) {
	name := relDir + ".gitignore"
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", name, err)
	}
	return rules(nil).addLines(name, string(data)), nil
}

// matchGitignores applies the .gitignore of each ancestor directory of rel,
// relative to that directory.  As in git, a deeper .gitignore takes
// precedence, so its "!pattern" can re-include what a parent one ignores.
func matchGitignores(gitignores map[string]rules, rel string, isDir bool) (bool, string) {
	dirs := []string{""}
	for i := range len(rel) {
		if rel[i] == '/' {
			dirs = append(dirs, rel[:i+1])
		}
	}
	for k := len(dirs) - 1; k >= 0; k-- {
		if decided, matched, by := gitignores[dirs[k]].decide(rel[len(dirs[k]):], isDir); decided {
			return matched, by
		}
	}
	return false, ""
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
