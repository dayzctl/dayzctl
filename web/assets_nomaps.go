//go:build nomaps

package web

import "embed"

// Assets holds the UI only: a `nomaps` build ships the map data separately and
// the server reads it from disk (see paths.maps in the configuration).
//
//go:embed log-analyzer/index.html log-analyzer/styles.css log-analyzer/README.md log-analyzer/js
var Assets embed.FS
