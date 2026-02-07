# Claude Code Guidelines

## Project overview

Race-Keeper RKD Telemetry Extractor — a standalone tool (Python + Go) that parses proprietary `.rkd` binary telemetry files from Race-Keeper "Instant Video" systems (Trivinci Systems LLC) and exports to CSV (Telemetry Overlay) and GPX formats.

Both implementations produce **byte-for-byte identical** output. Zero external dependencies (stdlib only in both languages).

## Repository structure

```
├── python/              # Python implementation (3.9+)
│   ├── rkd_parser.py    # Parser, exporters, and CLI
│   ├── tests/           # pytest tests (100% branch coverage)
│   ├── pyproject.toml   # pytest/coverage config
│   └── Makefile         # install, test, lint, clean
├── go/                  # Go implementation (1.21+)
│   ├── main.go          # CLI entry point
│   ├── rkd/             # Parser, exporters, session info, sample creator
│   │   ├── parser.go
│   │   ├── export.go
│   │   ├── session_info.go
│   │   ├── sample.go
│   │   └── parser_test.go
│   ├── main_test.go
│   ├── go.mod
│   └── Makefile         # build, test, lint, clean
├── samples/             # Shared sample .rkd file and reference outputs
├── Makefile             # Root: delegates to python/ and go/
├── README.md
├── CLAUDE.md            # This file
├── RKD_FORMAT_SPEC.md   # Binary format specification
├── RESEARCH_NOTES.md    # Reverse-engineering notes
├── SESSION_LOG.md       # Development session log
└── LICENSE
```

## Commands

### Python
- `cd python && make install` — Install test dependencies (pytest, pytest-cov)
- `cd python && make test` — Run full test suite with coverage (fails under 100%)
- `cd python && make lint` — Check syntax with py_compile

### Go
- `cd go && make build` — Build the binary
- `cd go && make test` — Run tests with race detector and coverage
- `cd go && make lint` — Run `go vet`

### Root (both)
- `make test` — Run both Python and Go test suites
- `make lint` — Lint both implementations
- `make clean` — Remove build/test artifacts

## Pull request rules

1. **Branch naming**: Use descriptive branch names prefixed with the change type (e.g., `feat/obd2-support`, `fix/gps-timestamp-overflow`)
2. **One concern per PR**: Each PR should address a single feature, bug fix, or refactoring
3. **Tests required**: Every PR must include tests for new/changed code. Python coverage must remain at 100%
4. **Run `make test` before submitting**: All tests must pass locally (both Python and Go)
5. **Run `make lint` before submitting**: Code must compile without errors
6. **No new dependencies**: Both implementations use stdlib only. Test dependencies (pytest, pytest-cov) are the only allowed external packages
7. **Commit messages**: Use imperative mood, be concise (e.g., "Add OBD-2 record parsing", "Fix altitude unit conversion")
8. **PR description**: Include a summary of what changed and why, plus a test plan
9. **Output parity**: Changes to parsing or export logic must produce identical output in both implementations

## Code style

### Python
- Use type hints for all function signatures
- Use dataclasses for structured data
- Keep inline comments for non-obvious binary format details
- Follow existing patterns in `rkd_parser.py` for new record type parsers

### Go
- Follow standard Go conventions (gofmt, go vet)
- Use `encoding/binary` for all binary parsing
- Keep the `rkd` package self-contained (no external dependencies)
- Tests use crafted binary payloads + sample file integration tests

## Known limitations

- OBD-2 record types are not yet supported (no test data available). PRs with sample `.rkd` files containing OBD-2 data are welcome.
