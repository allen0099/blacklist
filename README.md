# blacklist

A CLI tool written in Go that filters directory contents using
[gitignore](https://git-scm.com/docs/gitignore)-style rules.

## Features

- **Exclude** files/directories matching patterns (gitignore format).
- **Protect** files/directories from exclusion (higher priority than exclude).
- Supports multiple exclude/protect files per run.
- Optionally **copies** the filtered tree to an output directory.
- **Dry-run** mode to preview what would happen.
- Configurable log verbosity (quiet / normal / verbose).

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
| `--exclude <file>` | `-e` | Path to a gitignore-format file with exclusion patterns. Repeatable. |
| `--protect <file>` | `-p` | Path to a gitignore-format file with protection patterns. Repeatable. |
| `--output <dir>` | `-o` | Copy included files to this directory (preserving structure). Must not be the source directory or one of its parents; an output directory inside the source is skipped. |
| `--dry-run` | | Show what would be done without writing any files. |
| `--verbose` | `-v` | Enable debug output (env: `BLACKLIST_VERBOSE`). |
| `--quiet` | `-q` | Suppress all output except errors (env: `BLACKLIST_QUIET`). |
| `--version` | | Print the version. |
| `--help` | `-h` | Show help. |

`--verbose` and `--quiet` are mutually exclusive.

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

### Dry-run to preview a copy operation

```bash
blacklist -e .gitignore -o ./dist --dry-run ./src
```

### Use in GitLab CI

```yaml
deploy:
  image: ghcr.io/allen0099/blacklist:latest
  script:
    - blacklist -e ci/exclude.txt -o /tmp/deploy-root .
    - rsync -a /tmp/deploy-root/ user@server:/var/www/
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
