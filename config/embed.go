// Package configfs embeds the default configuration tree shipped with the
// application so the desktop installer can materialize it into the per-user
// directory on first run.
package configfs

import "embed"

//go:embed *.yaml *.example prompt_templates
var FS embed.FS
