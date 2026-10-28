# Changelog

## [0.2.1](https://github.com/allen0099/blacklist/compare/v0.2.0...v0.2.1) (2026-10-28)


### Bug Fixes

* **filter:** match patterns exactly like git ([7682de9](https://github.com/allen0099/blacklist/commit/7682de918f05284dde337c13a4498afba71e9d43))

## [0.2.0](https://github.com/allen0099/blacklist/compare/v0.1.0...v0.2.0) (2026-10-28)


### ⚠ BREAKING CHANGES

* **cli:** stdout lists paths relative to the target directory instead of absolute paths, and omits directories (tar would otherwise recurse into them and pull excluded files back in).

### Features

* **cli:** accept patterns on the command line and pattern files on stdin ([1e8a504](https://github.com/allen0099/blacklist/commit/1e8a50460618ceaf67240fc6c41a2f818a7a8726))
* **cli:** add --check to fail when paths match exclusion rules ([3dc126b](https://github.com/allen0099/blacklist/commit/3dc126b409c89838ac2c8f6900eeb66dd08afdba))
* **cli:** add --explain to show which rule decided each path ([8759466](https://github.com/allen0099/blacklist/commit/8759466b6bcbde804f8f5f988e2950847d7ecb42))
* **cli:** add --use-gitignore to honour the tree's .gitignore files ([a8c8f69](https://github.com/allen0099/blacklist/commit/a8c8f694fe9943963a68822562f25bffd720dae6))
* **cli:** add --version flag ([54598a4](https://github.com/allen0099/blacklist/commit/54598a471648656ecc6ce4bc5dcf18826e73273e))
* **cli:** print included files relative to the directory, add -0 ([8950968](https://github.com/allen0099/blacklist/commit/895096874bacfd98f2775aeb98e36a27c74eea27))
* **filter:** preserve permissions and mtimes, add --preserve-symlinks ([045dc2a](https://github.com/allen0099/blacklist/commit/045dc2a0c86e8352ad86bf0f8dd4def6191534a7))


### Bug Fixes

* **cli:** let explicit --verbose/--quiet override environment variables ([9c385e0](https://github.com/allen0099/blacklist/commit/9c385e099d9e69d69a32a852d09ba7fe3ba0a03b))
* **deps:** update Go to 1.27, alpine to 3.24 and spf13/pflag to v1.0.10 ([e0777e6](https://github.com/allen0099/blacklist/commit/e0777e6bbb7912ce830c3c8e2bd1809473f577e9))
* **filter:** prevent self-copy data loss and honor protect inside excluded dirs ([9005479](https://github.com/allen0099/blacklist/commit/9005479948b0df64b6c3bc2ce2580a39d780f865))
