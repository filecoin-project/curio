//go:build tools

// Package tools tracks dev/CI tool dependencies.
// This file is not compiled into the binary but ensures tools are tracked in go.mod.
// Install all tools with: go install ./tools/...
// Or individually: go install golang.org/x/tools/cmd/goimports
package tools

import (
	_ "github.com/hannahhoward/cbor-gen-for"
	_ "github.com/rhysd/actionlint/cmd/actionlint"
	_ "github.com/swaggo/swag/cmd/swag"
	_ "golang.org/x/tools/cmd/goimports"
)
