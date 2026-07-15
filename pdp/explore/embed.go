package explore

import "embed"

// StaticFS holds the Dataset Explorer HTML/JS/CSS assets.
//
//go:embed static/*
var StaticFS embed.FS
