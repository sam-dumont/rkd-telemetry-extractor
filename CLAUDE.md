# Claude Code Guidelines

## Project overview

Race-Keeper RKD Telemetry Extractor — a standalone Python tool that parses proprietary `.rkd` binary telemetry files from Race-Keeper "Instant Video" systems (Trivinci Systems LLC) and exports to CSV (Telemetry Overlay) and GPX formats.

- Single-file parser: `rkd_parser.py`
- Zero external dependencies (Python 3.9+ stdlib only)
- Tests in `tests/` using pytest with 100% code coverage enforced

## Commands

- `make install` — Install test dependencies (pytest, pytest-cov)
- `make test` — Run full test suite with coverage (fails under 100%)
- `make lint` — Check syntax with py_compile
- `make clean` — Remove caches and coverage artifacts

## Pull request rules

1. **Branch naming**: Use descriptive branch names prefixed with the change type (e.g., `feat/obd2-support`, `fix/gps-timestamp-overflow`)
2. **One concern per PR**: Each PR should address a single feature, bug fix, or refactoring
3. **Tests required**: Every PR must include tests for new/changed code. Coverage must remain at 100%
4. **Run `make test` before submitting**: All tests must pass locally
5. **Run `make lint` before submitting**: Code must compile without syntax errors
6. **No new dependencies**: This project uses stdlib only. Do not add pip dependencies to the parser. Test dependencies (pytest, pytest-cov) are the only allowed external packages
7. **Commit messages**: Use imperative mood, be concise (e.g., "Add OBD-2 record parsing", "Fix altitude unit conversion")
8. **PR description**: Include a summary of what changed and why, plus a test plan

## Code style

- Use type hints for all function signatures
- Use dataclasses for structured data
- Keep inline comments for non-obvious binary format details
- Follow existing patterns in `rkd_parser.py` for new record type parsers

## Architecture

- `rkd_parser.py` — All parsing, export, and CLI logic in a single file
- `tests/test_rkd_parser.py` — All tests
- `samples/` — Sample `.rkd` file and expected outputs for manual verification

## Known limitations

- OBD-2 record types are not yet supported (no test data available). PRs with sample `.rkd` files containing OBD-2 data are welcome.
