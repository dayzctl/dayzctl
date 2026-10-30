//go:build nomaps

package web

import "embed"

// Assets holds the UI only: a `nomaps` build ships the map data separately and
// the server reads it from the fixed locations next to the binary, in
// /usr/local/share/dayzctl/maps or in /usr/share/dayzctl/maps.
//
//go:embed log-analyzer/index.html log-analyzer/styles.css log-analyzer/README.md log-analyzer/js
var Assets embed.FS
