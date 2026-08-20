package seedrt

import "embed"

// Source embeds this package's own .go files so cmd/seed can copy them
// into each build's disposable scratch module (see cmd/seed/build.go).
// That's what lets a compiled Seed program resolve `import "seedrt"`
// without ever needing network access or a real module dependency on
// this repository: the "dependency" is just a handful of files written
// alongside the generated code.
//
//go:embed *.go
var Source embed.FS
