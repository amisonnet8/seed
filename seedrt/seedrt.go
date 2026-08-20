// Package seedrt is the Go runtime library backing Seed builtins (open,
// read, write, close, ...) that generated AMIVM-IR calls via CALL.
//
// This package must stay outside internal/: the code that imports it is
// the Go source amivm generates from a user's .seed program, which lives
// in that user's own module, not this one. An internal/ package is only
// importable from within its own module tree, so seedrt has to be public.
package seedrt
