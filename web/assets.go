//go:build !nomaps

// Package web exposes the embedded assets of the dayzctl web UI.
//
// The frontend lives in web/log-analyzer as plain ES modules with no build
// step, so it can be embedded as-is and served by `dayzctl web`. Map data under
// log-analyzer/maps is embedded too unless the build uses the `nomaps` tag
// (packages ship the maps as files and read them from disk instead).
package web

import "embed"

// Assets contains the single-page log analyzer.
//
//go:embed log-analyzer
var Assets embed.FS
