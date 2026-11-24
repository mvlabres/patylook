// Package web embeds the HTML templates and static assets into the binary,
// so deployment is a single file plus configuration.
package web

import "embed"

//go:embed templates static
var Files embed.FS
