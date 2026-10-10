// Package frontend embeds the Vite SPA build so single-binary editions can
// serve the UI without a web/ directory next to the executable.
package frontend

import (
	"embed"
	"io/fs"
)

// dist holds the built SPA (index.html, assets/…). CI populates it before the
// Go compile: `wails build` runs `vite build` first, then builds the binary,
// so the embed always captures the fresh bundle. dist/README.txt keeps the
// pattern valid in jobs that never build the frontend.
//
//go:embed dist
var dist embed.FS

// Dist returns the embedded SPA rooted at index.html, or nil if the bundle
// was never built.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return sub
}
