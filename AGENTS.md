# Agent Instructions

## Go import fixes

Instead of manually adding, removing, or reordering imports in Go files, run goimports then the project's formatter:

```bash
goimports -w path/to/file.go
go run ./scripts/fiximports
```

Use when:
- Adding or removing imports
- Fixing "imported and not used" or "undefined" import errors
- Reordering imports to match project style
