package lexer_test

import (
	"strings"
	"testing"

	"github.com/amisonnet8/seed/internal/lexer"
)

func lexOneString(t *testing.T, src string) string {
	t.Helper()
	toks, err := lexer.Tokenize(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toks) == 0 || toks[0].Kind != lexer.String {
		t.Fatalf("expected a String token, got %+v", toks)
	}
	return toks[0].Literal
}

func TestStringLiteralEscapes(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`"\a\b\f\n\r\t\v"`, "\a\b\f\n\r\t\v"},
		{`"\\"`, `\`},
		{`"\""`, `"`},
		{`"\x41\x42"`, "AB"},
		{`"\101\102"`, "AB"}, // octal for 'A', 'B'
		{`"あ"`, "あ"},
		{`"\U0001F600"`, "😀"},
		{`"no escapes here"`, "no escapes here"},
	}
	for _, c := range cases {
		got := lexOneString(t, c.src)
		if got != c.want {
			t.Errorf("Tokenize(%q): got %q, want %q", c.src, got, c.want)
		}
	}
}

func TestStringLiteralEscapeErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"unknown escape", `"\z"`},
		{"unterminated escape at EOF", `"\`},
		{"unterminated escape at newline", "\"\\\n\""},
		{"short hex digits", `"\x4"`},
		{"short unicode digits", `"\u304"`},
		{"octal out of range", `"\777"`},
		{"surrogate code point", `"\uD800"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := lexer.Tokenize(c.src); err == nil {
				t.Errorf("Tokenize(%q): expected an error, got none", c.src)
			}
		})
	}
}

func TestStringLiteralStillRejectsRawNewline(t *testing.T) {
	_, err := lexer.Tokenize("\"a\nb\"")
	if err == nil || !strings.Contains(err.Error(), "unterminated string literal") {
		t.Errorf("expected unterminated string literal error, got %v", err)
	}
}
