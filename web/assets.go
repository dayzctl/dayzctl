// Package web exposes the embedded assets of the dayzctl web UI.
//
// The frontend lives in web/log-analyzer as plain ES modules with no build
// step, so it can be embedded as-is and served by `dayzctl web`.
package web

import "embed"

// Assets contains the single-page log analyzer.
//
//go:embed log-analyzer
var Assets embed.FS
