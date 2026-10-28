package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/allen0099/blacklist/internal/filter"
	"github.com/spf13/cobra"
)

// Environment variables that set the default for --verbose and --quiet.
// An explicit flag on the command line always takes precedence.
const (
	EnvVerbose = "BLACKLIST_VERBOSE"
	EnvQuiet   = "BLACKLIST_QUIET"
)

// isTruthy returns true when the value of an environment variable should be
// interpreted as boolean true (accepts "1", "true", "yes", case-insensitive).
func isTruthy(val string) bool {
	switch strings.ToLower(val) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// NewRootCmd creates and returns a fresh root command.  This enables tests to
// create isolated instances without shared flag state.
func NewRootCmd(version string) *cobra.Command {
	var (
		excludeFiles    []string
		protectFiles    []string
		excludePatterns []string
		protectPatterns []string
		outputDir       string
		verbose         bool
		quiet           bool
		dryRun          bool
		null            bool
		explain         bool
		check           bool
	)

	c := &cobra.Command{
		Use:   "blacklist [directory]",
		Short: "Filter directory contents using gitignore-style rules",
		Long: `blacklist walks a directory and applies gitignore-style patterns to
determine which files should be excluded or protected.

Exclusion rules (-e/--exclude) specify files whose contents follow the gitignore
format.  Files matched by an exclude pattern are omitted from the output.

Protection rules (-p/--protect) follow the same format.  A file that matches a
protect pattern is always included, even if it also matches an exclude pattern.

Priority: protect > exclude.

Included files (not directories) are printed to stdout relative to the
directory, ready for tar -T, rsync --files-from or zip -@.

With --check the excluded paths are printed instead and the command exits 1
if there are any, e.g. to fail CI when forbidden files are present.

When --output is given, included files are copied to that directory, preserving
the relative path structure.  Combined with --dry-run, the tool prints what it
would copy without making any changes.`,
		Version:      version,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
	}

	c.Flags().StringArrayVarP(&excludeFiles, "exclude", "e", nil,
		"path to gitignore-format file containing exclusion patterns, - for stdin (repeatable)")
	c.Flags().StringArrayVarP(&protectFiles, "protect", "p", nil,
		"path to gitignore-format file containing protection patterns, - for stdin (repeatable)")
	c.Flags().StringArrayVarP(&excludePatterns, "exclude-pattern", "E", nil,
		"exclusion pattern given directly (repeatable)")
	c.Flags().StringArrayVarP(&protectPatterns, "protect-pattern", "P", nil,
		"protection pattern given directly (repeatable)")
	c.Flags().StringVarP(&outputDir, "output", "o", "",
		"output directory: copy included files here (preserving tree structure)")
	c.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"enable verbose (debug) output (env: "+EnvVerbose+")")
	c.Flags().BoolVarP(&quiet, "quiet", "q", false,
		"suppress non-error log output (env: "+EnvQuiet+")")
	c.Flags().BoolVar(&dryRun, "dry-run", false,
		"print what would be done without making any changes")
	c.Flags().BoolVar(&check, "check", false,
		"list excluded paths instead and exit 1 if there are any (CI guard)")
	c.Flags().BoolVar(&explain, "explain", false,
		"list every path with its verdict and the rule that decided it")
	c.Flags().BoolVarP(&null, "null", "0", false,
		"separate output paths with NUL instead of newline (for xargs -0, tar --null -T -)")

	// verbose and quiet are mutually exclusive.
	c.MarkFlagsMutuallyExclusive("verbose", "quiet")
	c.MarkFlagsMutuallyExclusive("check", "output")

	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runCmd(cmd, args, filter.Options{
			ExcludeFiles:    excludeFiles,
			ProtectFiles:    protectFiles,
			ExcludePatterns: excludePatterns,
			ProtectPatterns: protectPatterns,
			OutputDir:       outputDir,
			DryRun:          dryRun,
			Stdin:           cmd.InOrStdin(),
		}, verbose, quiet, null, explain, check)
	}

	return c
}

// Execute runs the root command.  Call this from main.
func Execute(version string) {
	if err := NewRootCmd(version).Execute(); err != nil {
		os.Exit(1)
	}
}

func runCmd(
	cmd *cobra.Command,
	args []string,
	opts filter.Options,
	verbose, quiet, null, explain, check bool,
) error {
	// Apply environment-variable defaults when the flags were not explicitly
	// set on the command line.  An explicit flag always wins.
	if !cmd.Flags().Changed("verbose") && !cmd.Flags().Changed("quiet") {
		verbose = isTruthy(os.Getenv(EnvVerbose))
		quiet = isTruthy(os.Getenv(EnvQuiet))
	}

	level := slog.LevelInfo
	switch {
	case verbose:
		level = slog.LevelDebug
	case quiet:
		level = slog.LevelError
	}
	log := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level}))

	// Resolve working directory.
	opts.Dir = "."
	if len(args) == 1 {
		opts.Dir = args[0]
	}
	opts.Logger = log

	log.Info("starting blacklist", "dir", opts.Dir,
		"excludeFiles", opts.ExcludeFiles,
		"protectFiles", opts.ProtectFiles,
		"outputDir", opts.OutputDir,
		"dryRun", opts.DryRun)

	results, err := filter.Run(opts)
	if err != nil {
		return err
	}

	// Print paths relative to dir: included files (not directories, so tar -T
	// does not recurse into them), or with --check the excluded paths.
	sep := "\n"
	if null {
		sep = "\x00"
	}
	out := cmd.OutOrStdout()
	var violations int
	for _, r := range results {
		if !r.Included() {
			violations++
		}
		listed := r.Included() && !r.IsDir
		if check {
			listed = !r.Included()
		}
		switch {
		case explain:
			fmt.Fprint(out, explainLine(r)+sep)
		case listed && r.IsDir:
			fmt.Fprint(out, r.Rel+"/"+sep)
		case listed:
			fmt.Fprint(out, r.Rel+sep)
		}
	}

	// Summary log.
	var included, excluded, protected int
	for _, r := range results {
		switch {
		case r.Protected:
			protected++
		case r.Excluded:
			excluded++
		default:
			included++
		}
	}
	log.Info("done",
		"total", len(results),
		"included", included+protected,
		"excluded", excluded,
		"protected", protected)

	if check && violations > 0 {
		return fmt.Errorf("check failed: %d path(s) match exclusion rules", violations)
	}
	return nil
}

// explainLine formats r as "verdict<TAB>path<TAB>reasons".
func explainLine(r filter.Result) string {
	verdict, path := "include", r.Rel
	if !r.Included() {
		verdict = "exclude"
	}
	if r.IsDir {
		path += "/"
	}
	var reasons []string
	if r.Excluded {
		reasons = append(reasons, "excluded by "+r.ExcludedBy)
	}
	if r.Protected {
		reasons = append(reasons, "protected by "+r.ProtectedBy)
	}
	return strings.TrimSuffix(verdict+"\t"+path+"\t"+strings.Join(reasons, "; "), "\t")
}
