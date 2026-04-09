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

		// Use forward slashes for consistent cross-platform matching.
		// Append a trailing slash for directories so that patterns like
		// "vendor/" are correctly recognised as directory-only rules.
		relSlash := filepath.ToSlash(rel)
		matchPath := relSlash
		if d.IsDir() {
			matchPath = relSlash + "/"
		}

		excluded := excludeMatcher != nil && excludeMatcher.MatchesPath(matchPath)
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

		// If the directory itself is excluded and not protected, skip its
		// subtree.
		if d.IsDir() && excluded && !protected {
			return fs.SkipDir
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	// Copy included files to output directory if requested.
	if opts.OutputDir != "" {
		if err := copyResults(results, dir, opts.OutputDir, opts.DryRun, log); err != nil {
			return results, err
		}
	}

	return results, nil
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
		// Keep each file's contents as a separate block for splitLines.
		// The trailing empty block is harmless and does not emit a blank line.
		lines = append(lines, string(data), "")
	}

	return ignore.CompileIgnoreLines(splitLines(lines)...), nil
}

// splitLines converts a slice of multi-line strings into individual lines,
// normalising CRLF to LF so that patterns are not broken on Windows or when
// files were edited with CRLF line endings.
func splitLines(blocks []string) []string {
	var out []string
	for _, block := range blocks {
		start := 0
		for i := 0; i < len(block); i++ {
			if block[i] == '\n' {
				line := block[start:i]
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
				out = append(out, line)
				start = i + 1
			}
		}
		if start < len(block) {
			line := block[start:]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
		}
	}
	return out
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
			if err := os.MkdirAll(dst, info.Mode()); err != nil {
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
func copyFile(src, dst string, mode fs.FileMode) error {
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
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying %q to %q: %w", src, dst, err)
	}
	return nil
}
