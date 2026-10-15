# Contributing to ByLonis MCP Server

## Development workflow

1. Create a feature branch from `bylonis-main`.
2. Make your changes with tests where applicable.
3. Run the local checks below before opening a PR.
4. Open a pull request against `bylonis-main` with a clear summary and validation steps.

## Required sync for MCP changes

If your PR adds, removes, or renames MCP tools/resources/config behavior, update in the same PR:

- `README.md` (tool list and parameter references)
- Relevant files in `docs/` when user-facing behavior changes

## Suggested validation

CI (`.github/workflows/ci.yaml`) runs the same checks:

```bash
gofmt -l .
go vet ./...
go test ./...
```

## Pull request checklist

- [ ] Code/tests updated as needed
- [ ] README/docs updated for user-facing changes
- [ ] Validation commands and results included in PR description
