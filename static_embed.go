// Package genpic holds module-root assets embedded for cmd/mvplite and future binaries.
// Embed paths are relative to this file (module root), keeping ./web at repository root.
package genpic

import "embed"

//go:embed web/*
var WebStatic embed.FS
