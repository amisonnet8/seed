// Package seedrt is the Go runtime library backing the Seed builtins
// that don't map onto a single AMIVM-IR instruction or a bare stdlib
// call: open/read/write/close (seed_spec.md §8) and the couple of
// value conversions (§9) Go has no direct expression for.
//
// This package must stay outside internal/: the code that imports it is
// the Go source amivm generates from a user's .seed program, which lives
// in that user's own module, not this one. An internal/ package is only
// importable from within its own module tree, so seedrt has to be public.
// cmd/seed embeds this package's own source (see embed.go) and copies it
// into every build's scratch module, so a compiled Seed program never
// needs network access or a real dependency on this module to resolve
// `import "seedrt"`.
//
// Seed has no exception or Result type, so every failure here (a file
// that doesn't exist, a String that isn't a valid number, ...) is
// unrecoverable at the language level: report it and exit, rather than
// return an error value Seed code has no way to inspect.
package seedrt

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// File pairs the open *os.File with a persistent buffered reader.
// Read must reuse the same *bufio.Reader across calls: a fresh one each
// call would read ahead into its own buffer and then discard whatever
// wasn't consumed when it went out of scope, silently losing bytes the
// underlying *os.File's cursor had already moved past.
type File struct {
	f *os.File
	r *bufio.Reader
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "seed: "+format+"\n", args...)
	os.Exit(1)
}

// Open implements seed_spec.md §8/§9's open(path, mode): mode is "r" or
// "w", matching os.Open/os.Create.
func Open(path, mode string) *File {
	var f *os.File
	var err error
	switch mode {
	case "r":
		f, err = os.Open(path)
	case "w":
		f, err = os.Create(path)
	default:
		fatalf("open: invalid mode %q (want \"r\" or \"w\")", mode)
	}
	if err != nil {
		fatalf("open: %v", err)
	}
	return &File{f: f, r: bufio.NewReader(f)}
}

// Read implements read(file): it returns one line with any trailing
// newline trimmed, and ok=false at EOF — the (value, ok) pair maps
// directly onto a Seed variable's (value, isset) pair, so codegen
// captures both results of the CALL straight into it (see
// internal/codegen/stmt.go's genReadAssign), giving read()'s "returns
// null at EOF" (seed_spec.md §8) for free.
func Read(file *File) (string, bool) {
	line, err := file.r.ReadString('\n')
	if err != nil {
		if len(line) == 0 {
			return "", false
		}
		// A final line with no trailing newline before EOF still counts.
		return strings.TrimRight(line, "\r\n"), true
	}
	return strings.TrimRight(line, "\r\n"), true
}

// Write implements write(file, line): line is written followed by a
// newline, matching Read's framing.
func Write(file *File, line string) {
	if _, err := file.f.WriteString(line + "\n"); err != nil {
		fatalf("write: %v", err)
	}
}

// Close implements close(file).
func Close(file *File) {
	if err := file.f.Close(); err != nil {
		fatalf("close: %v", err)
	}
}

// ParseInt implements int(String) (seed_spec.md §9).
func ParseInt(s string) int {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fatalf("int(): cannot parse %q as Int", s)
	}
	return int(v)
}

// ParseFloat implements float(String) (seed_spec.md §9).
func ParseFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		fatalf("float(): cannot parse %q as Float", s)
	}
	return v
}

// BoolToInt implements int(Bool) (seed_spec.md §9: true->1, false->0).
// Go has no direct bool->numeric conversion, unlike Int<->Float or any
// of the *->String conversions (strconv.Itoa/FormatFloat/FormatBool),
// which compile straight through without needing a seedrt helper.
func BoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
