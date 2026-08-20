package seedrt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amisonnet8/seed/seedrt"
)

func TestWriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")

	w := seedrt.Open(path, "w")
	seedrt.Write(w, "line one")
	seedrt.Write(w, "line two")
	seedrt.Close(w)

	r := seedrt.Open(path, "r")
	line1, ok1 := seedrt.Read(r)
	if !ok1 || line1 != "line one" {
		t.Errorf("first Read = %q, %v; want %q, true", line1, ok1, "line one")
	}
	line2, ok2 := seedrt.Read(r)
	if !ok2 || line2 != "line two" {
		t.Errorf("second Read = %q, %v; want %q, true", line2, ok2, "line two")
	}
	line3, ok3 := seedrt.Read(r)
	if ok3 {
		t.Errorf("Read at EOF = %q, true; want ok=false", line3)
	}
	seedrt.Close(r)
}

// TestReadPersistsBufferAcrossCalls guards against the exact bug
// seedrt.File's doc comment warns about: a fresh bufio.Reader on every
// call reads ahead into its own buffer and silently drops whatever
// wasn't consumed when it's discarded, since the underlying *os.File's
// cursor has already moved past those bytes. This writes enough lines
// that a small implicit read-ahead would jump past line 2 if File
// didn't keep one reader alive across calls.
func TestReadPersistsBufferAcrossCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "many.txt")
	w := seedrt.Open(path, "w")
	for i := 0; i < 50; i++ {
		seedrt.Write(w, "line")
	}
	seedrt.Close(w)

	r := seedrt.Open(path, "r")
	count := 0
	for {
		_, ok := seedrt.Read(r)
		if !ok {
			break
		}
		count++
	}
	seedrt.Close(r)

	if count != 50 {
		t.Errorf("read %d lines, want 50 (buffered-ahead data must not be lost between calls)", count)
	}
}

func TestReadLastLineWithoutTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no_newline.txt")
	if err := os.WriteFile(path, []byte("first\nsecond"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := seedrt.Open(path, "r")
	first, ok1 := seedrt.Read(r)
	if !ok1 || first != "first" {
		t.Errorf("first Read = %q, %v; want %q, true", first, ok1, "first")
	}
	second, ok2 := seedrt.Read(r)
	if !ok2 || second != "second" {
		t.Errorf("second Read (no trailing newline) = %q, %v; want %q, true", second, ok2, "second")
	}
	_, ok3 := seedrt.Read(r)
	if ok3 {
		t.Error("Read after the unterminated last line should report EOF")
	}
	seedrt.Close(r)
}

func TestParseInt(t *testing.T) {
	if got := seedrt.ParseInt("42"); got != 42 {
		t.Errorf("ParseInt(42) = %d, want 42", got)
	}
}

func TestParseFloat(t *testing.T) {
	if got := seedrt.ParseFloat("3.5"); got != 3.5 {
		t.Errorf("ParseFloat(3.5) = %v, want 3.5", got)
	}
}

func TestBoolToInt(t *testing.T) {
	if got := seedrt.BoolToInt(true); got != 1 {
		t.Errorf("BoolToInt(true) = %d, want 1", got)
	}
	if got := seedrt.BoolToInt(false); got != 0 {
		t.Errorf("BoolToInt(false) = %d, want 0", got)
	}
}
