# blacklist

A CLI tool written in Go that filters directory contents using
[gitignore](https://git-scm.com/docs/gitignore)-style rules.

## Features

- **Exclude** files/directories matching patterns (gitignore format).
- **Protect** files/directories from exclusion (higher priority than exclude).
- Patterns from files, stdin or the command line; optionally the tree's own
  `.gitignore` files.
- Prints included files as relative paths, ready for `tar`, `rsync`, `zip` or
  `xargs -0`.
- Optionally **copies** the filtered tree to an output directory, preserving
  permissions and modification times.
- **Check** mode to fail CI when forbidden files are present.
- **Explain** mode showing which rule decided each path.
- **Dry-run** mode to preview what would happen.

## Installation

### Pre-built binaries

Download the latest binary for your platform from the
[Releases](../../releases) page.

### Build from source

```bash
go install github.com/allen0099/blacklist@latest
```

Or clone and build manually:

```bash
git clone https://github.com/allen0099/blacklist.git
cd blacklist
go build -o blacklist .
```

### Docker

```bash
docker pull ghcr.io/allen0099/blacklist:latest
```

## Usage

```
blacklist [flags] [directory]
```

If `directory` is omitted the current working directory is used.

### Flags

| Flag | Short | Description |
|---|---|---|
| `--exclude <file>` | `-e` | Path to a gitignore-format file with exclusion patterns, `-` for stdin. Repeatable. |
| `--protect <file>` | `-p` | Path to a gitignore-format file with protection patterns, `-` for stdin. Repeatable. |
| `--exclude-pattern <p>` | `-E` | Exclusion pattern given directly. Repeatable. |
| `--protect-pattern <p>` | `-P` | Protection pattern given directly. Repeatable. |
| `--use-gitignore` | | Also exclude what the tree's `.gitignore` files ignore, and `.git`. |
| `--output <dir>` | `-o` | Copy included files to this directory (preserving structure, permissions and modification times). Must not be the source directory or one of its parents; an output directory inside the source is skipped. |
| `--preserve-symlinks` | | With `--output`, recreate symlinks instead of skipping them. Links are never followed. |
| `--dry-run` | | Show what would be done without writing any files. |
| `--null` | `-0` | Separate printed paths with NUL instead of newline. |
| `--check` | | Print excluded paths instead and exit 1 if there are any. |
| `--explain` | | Print every path with its verdict and the rule that decided it. |
| `--verbose` | `-v` | Enable debug output (env: `BLACKLIST_VERBOSE`). |
| `--quiet` | `-q` | Suppress all output except errors (env: `BLACKLIST_QUIET`). |
| `--version` | | Print the version. |
| `--help` | `-h` | Show help. |

`--verbose` and `--quiet` are mutually exclusive, as are `--check` and `--output`.

### Output

stdout lists the **included files** (not directories) relative to the
directory, one per line (NUL-separated with `-0`). Directories are left out so
that `tar -T` does not recurse into them and pull excluded files back in. Logs
go to stderr.

> **Changed in 0.2.0:** earlier versions printed absolute paths and also listed
> directories.

### Environment variables

Verbosity can be controlled without passing flags by setting environment
variables.  An explicit flag always takes precedence over an environment variable.

| Variable | Equivalent flag | Accepted values |
|---|---|---|
| `BLACKLIST_VERBOSE` | `--verbose` | `1`, `true`, `yes` (case-insensitive) |
| `BLACKLIST_QUIET` | `--quiet` | `1`, `true`, `yes` (case-insensitive) |

### Priority

> **protect > exclude**

When a file matches both an exclusion rule and a protection rule, the
protection rule wins and the file is **included**.

### Pattern file format

Pattern files follow the
[gitignore specification](https://git-scm.com/docs/gitignore):

```gitignore
# Comments start with '#'

# Exclude all log files
*.log

# Exclude the vendor directory and everything inside it
vendor/

# Exclude all .tmp files inside any build/ directory
build/**/*.tmp
```

With `--use-gitignore`, each `.gitignore` applies to its own directory, as in
git. One difference: a `!pattern` in a deeper `.gitignore` cannot re-include a
path that a parent `.gitignore` ignores.

## Examples

### List files not matched by any exclusion rule

```bash
blacklist -e .gitignore ./src
```

### Multiple exclude files

```bash
blacklist -e exclude-logs.txt -e exclude-build.txt ./project
```

### Exclude everything except protected files

```bash
blacklist -e everything.txt -p keep-these.txt ./project
```

### Copy filtered tree to an output directory

```bash
blacklist -e .gitignore -o ./dist ./src
```

### Archive the filtered tree

```bash
blacklist --use-gitignore -e deploy-exclude.txt -0 . | tar --null -T - -czf deploy.tgz
```

### Patterns without a file

```bash
blacklist -E '*.log' -E 'node_modules/' -P 'logs/keep.log' .
printf '*.tmp\n' | blacklist -e - .
```

### Fail CI when forbidden files are present

```bash
blacklist --check -E .env -E '*.pem' -E 'node_modules/' .
```

### Find out why a file was excluded

```bash
$ blacklist --explain -e exclude.txt -P keep.go .
include	main.go
exclude	vendor/	excluded by exclude.txt:2: vendor/
include	vendor/keep.go	excluded by exclude.txt:2: vendor/; protected by pattern: keep.go
```

### Dry-run to preview a copy operation

```bash
blacklist -e .gitignore -o ./dist --dry-run ./src
```

### Use in GitLab CI

The image's entrypoint is `blacklist`, so reset it for GitLab to run the
script. Pin a minor version: 0.x releases may change the output format.

```yaml
package:
  image:
    name: ghcr.io/allen0099/blacklist:0.2
    entrypoint: [""]
  script:
    - blacklist --check -E .env -E '*.pem' .
    - blacklist --use-gitignore -e ci/exclude.txt -o deploy-root .
  artifacts:
    paths: [deploy-root/]
```

## Development

### Prerequisites

- Go 1.27 or later
- `make` (GNU Make or compatible)

### Makefile targets

```
make build          # compile the binary
make test           # run tests with race detector
make test-coverage  # run tests and print per-function coverage
make fmt            # format all Go files in-place (gofmt)
make fmt-check      # fail if any files are not formatted (used in CI)
make vet            # run go vet
make vuln           # run govulncheck to scan for known vulnerabilities
make clean          # remove build artifacts
make help           # list all targets
```

### Run tests

```bash
make test
```

### Check test coverage

```bash
make test-coverage
```

### Build

```bash
make build
# or:
go build -o blacklist .
```

### Releases

Releases are managed by [release-please](https://github.com/googleapis/release-please).
Use [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`,
`feat!:` …); release-please keeps a release PR open with the next version and
changelog, and merging it tags the release, uploads the binaries and pushes the
Docker image.

### Verifying downloads

Release binaries and Docker images carry build provenance attestations:

```bash
gh attestation verify blacklist-v0.2.0-linux-amd64 -R allen0099/blacklist
gh attestation verify oci://ghcr.io/allen0099/blacklist:0.2 -R allen0099/blacklist
```
