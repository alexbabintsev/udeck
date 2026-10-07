// Package assets embeds static resources into the binary so a released or
// `go install`ed build does not depend on the source checkout.
package assets

import "embed"

// Lucide holds the Lucide SVG icon set under lucide/icons/.
//
//go:embed lucide/icons/*.svg
var Lucide embed.FS
