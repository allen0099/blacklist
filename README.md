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
| `--output <dir>` | `-o` | Copy included files to this directory (preserving structure). |
| `--dry-run` | | Show what would be done without writing any files. |
| `--verbose` | `-v` | Enable debug output. |
| `--quiet` | `-q` | Suppress all output except errors. |
| `--help` | `-h` | Show help. |

`--verbose` and `--quiet` are mutually exclusive.

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

- Go 1.22 or later

### Run tests

```bash
go test ./... -race
```

### Check test coverage

```bash
go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out
```

### Build

```bash
go build -o blacklist .
```

### Build Docker image

```bash
docker build -t blacklist .
```

## GitHub Actions

| Workflow | Trigger | Description |
|---|---|---|
| [CI](.github/workflows/ci.yml) | push / pull request | Vet, test (multi-Go-version), build binaries |
| [Release](.github/workflows/release.yml) | `v*.*.*` tag | Build & publish binaries + Docker image |

## License

MIT
