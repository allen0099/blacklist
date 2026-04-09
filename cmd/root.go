package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/allen0099/blacklist/internal/filter"
	"github.com/allen0099/blacklist/internal/logger"
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
func NewRootCmd() *cobra.Command {
	var (
		excludeFiles []string
		protectFiles []string
		outputDir    string
		verbose      bool
		quiet        bool
		dryRun       bool
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

When --output is given, included files are copied to that directory, preserving
the relative path structure.  Combined with --dry-run, the tool prints what it
would copy without making any changes.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
	}

	c.Flags().StringArrayVarP(&excludeFiles, "exclude", "e", nil,
		"path to gitignore-format file containing exclusion patterns (repeatable)")
	c.Flags().StringArrayVarP(&protectFiles, "protect", "p", nil,
		"path to gitignore-format file containing protection patterns (repeatable)")
	c.Flags().StringVarP(&outputDir, "output", "o", "",
		"output directory: copy included files here (preserving tree structure)")
	c.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"enable verbose (debug) output (env: "+EnvVerbose+")")
	c.Flags().BoolVarP(&quiet, "quiet", "q", false,
		"suppress non-error log output (env: "+EnvQuiet+")")
	c.Flags().BoolVar(&dryRun, "dry-run", false,
		"print what would be done without making any changes")

	// verbose and quiet are mutually exclusive.
	c.MarkFlagsMutuallyExclusive("verbose", "quiet")

	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runCmd(cmd, args, excludeFiles, protectFiles, outputDir, verbose, quiet, dryRun)
	}

	return c
}

// Execute runs the root command.  Call this from main.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func runCmd(
	cmd *cobra.Command,
	args []string,
	excludeFiles, protectFiles []string,
	outputDir string,
	verbose, quiet, dryRun bool,
) error {
	// Apply environment-variable defaults when the flags were not explicitly
	// set on the command line.  An explicit flag always wins.
	if !cmd.Flags().Changed("verbose") && isTruthy(os.Getenv(EnvVerbose)) {
		verbose = true
	}
	if !cmd.Flags().Changed("quiet") && isTruthy(os.Getenv(EnvQuiet)) {
		quiet = true
	}

	// Initialise logger.
	switch {
	case verbose:
		logger.Init(logger.LevelVerbose)
	case quiet:
		logger.Init(logger.LevelQuiet)
	default:
		logger.Init(logger.LevelNormal)
	}

	log := logger.Get()

	// Resolve working directory.
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}

	log.Info("starting blacklist", "dir", dir,
		"excludeFiles", excludeFiles,
		"protectFiles", protectFiles,
		"outputDir", outputDir,
		"dryRun", dryRun)

	results, err := filter.Run(filter.Options{
		Dir:          dir,
		ExcludeFiles: excludeFiles,
		ProtectFiles: protectFiles,
		OutputDir:    outputDir,
		DryRun:       dryRun,
		Logger:       log,
	})
	if err != nil {
		return err
	}

	// Print the list of included files to stdout (one per line).
	for _, r := range results {
		if r.Included() {
			fmt.Fprintln(cmd.OutOrStdout(), r.Path)
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

	return nil
}
