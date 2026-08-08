// Package migrate embeds the goose schema migrations so `ach migrate` can apply
// them without a separate goose binary (the runtime image has no shell tools or
// schema files on disk).
package migrate

import "embed"

//go:embed schema/*.sql
var FS embed.FS
