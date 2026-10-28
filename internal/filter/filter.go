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
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// Result describes what happened to a single file during a filter run.
type Result struct {
	Path      string
	Excluded  bool
	Protected bool
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
	// determine which files to exclude.
	ExcludeFiles []string
	// ProtectFiles contains paths to gitignore-format files whose patterns
	// determine which files to protect from exclusion.
	ProtectFiles []string
	// OutputDir, when non-empty, causes included files to be copied there
	// preserving the relative directory structure.
	OutputDir string
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
	excludeMatcher, err := compilePatterns(opts.ExcludeFiles)
	if err != nil {
		return nil, fmt.Errorf("compiling exclude patterns: %w", err)
	}

	protectMatcher, err := compilePatterns(opts.ProtectFiles)
	if err != nil {
		return nil, fmt.Errorf("compiling protect patterns: %w", err)
	}

	log.Debug("starting filter walk", "dir", dir,
		"excludeFiles", opts.ExcludeFiles,
		"protectFiles", opts.ProtectFiles)

	var results []Result
	// excludedDir is the excluded (unprotected) directory whose subtree is
	// being walked, so that its descendants inherit the exclusion.
	var excludedDir string

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
		excluded := excludedDir != "" || (excludeMatcher != nil && excludeMatcher.MatchesPath(matchPath))
		protected := protectMatcher != nil && protectMatcher.MatchesPath(matchPath)

		r := Result{
			Path:      path,
			Excluded:  excluded,
			Protected: protected,
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
			if protectMatcher == nil {
				return fs.SkipDir
			}
			if excludedDir == "" {
				excludedDir = matchPath
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	// Copy included files to output directory if requested.
	if outDir != "" {
		if err := copyResults(results, dir, outDir, opts.DryRun, log); err != nil {
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

// compilePatterns loads each file and returns a single combined Matcher.
// Returns nil (no error) when files is empty.
func compilePatterns(files []string) (*ignore.GitIgnore, error) {
	if len(files) == 0 {
		return nil, nil
	}

	var lines []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading pattern file %q: %w", f, err)
		}
		// Strip CR so CRLF-edited files match like LF ones.
		for _, line := range strings.Split(string(data), "\n") {
			lines = append(lines, strings.TrimSuffix(line, "\r"))
		}
	}

	return ignore.CompileIgnoreLines(lines...), nil
}

// copyResults copies each included file from srcRoot to dstRoot, preserving
// the relative sub-tree.
func copyResults(results []Result, srcRoot, dstRoot string, dryRun bool, log *slog.Logger) error {
	for _, r := range results {
		if !r.Included() {
			continue
		}

		rel, err := filepath.Rel(srcRoot, r.Path)
		if err != nil {
			return err
		}

		dst := filepath.Join(dstRoot, rel)

		info, err := os.Lstat(r.Path)
		if err != nil {
			return fmt.Errorf("stat %q: %w", r.Path, err)
		}

		// Skip symlinks: following them could copy data from outside srcRoot.
		if info.Mode()&os.ModeSymlink != 0 {
			log.Debug("skipping symlink", "path", r.Path)
			continue
		}

		if info.IsDir() {
			if dryRun {
				log.Info("dry-run: would create directory", "dst", dst)
				continue
			}
			// Keep the owner's write bit so the directory can be filled.
			if err := os.MkdirAll(dst, info.Mode().Perm()|0o700); err != nil {
				return fmt.Errorf("creating directory %q: %w", dst, err)
			}
			log.Debug("created directory", "dst", dst)
			continue
		}

		if dryRun {
			log.Info("dry-run: would copy", "src", r.Path, "dst", dst)
			continue
		}

		if err := copyFile(r.Path, dst, info.Mode()); err != nil {
			return err
		}
		log.Debug("copied", "src", r.Path, "dst", dst)
	}
	return nil
}

// copyFile copies src to dst, creating parent directories as needed.
func copyFile(src, dst string, mode fs.FileMode) (err error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating parent directories for %q: %w", dst, err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
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
	return nil
}
