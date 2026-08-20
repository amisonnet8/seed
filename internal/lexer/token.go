package lexer

// Kind identifies the lexical category of a Token.
type Kind int

const (
	EOF Kind = iota
	Newline
	Ident
	Int
	Float
	String

	// structural keywords (seed_spec.md §10)
	KwFunc
	KwReturn
	KwIf
	KwElif
	KwElse
	KwWhile
	KwFor
	KwIn
	KwBreak
	KwContinue
	KwNull
	KwTrue
	KwFalse
	KwTypeInt
	KwTypeFloat
	KwTypeString
	KwTypeBool
	KwTypeFile

	// punctuation
	LParen
	RParen
	LBrace
	RBrace
	LBracket
	RBracket
	Comma

	// operators (seed_spec.md §5)
	Plus
	Minus
	Star
	Slash
	Percent
	PlusAssign
	MinusAssign
	StarAssign
	SlashAssign
	PercentAssign
	Inc
	Dec
	Assign
	Eq
	Neq
	Lt
	Lte
	Gt
	Gte
	AndAnd
	OrOr
	Not
)

// keywords holds the structural keywords that affect grammar. Builtin
// function names (print, isnull, open, ...) are reserved words per
// seed_spec.md §10 but are not syntax keywords: they lex as plain Ident
// tokens and are resolved as ordinary calls.
var keywords = map[string]Kind{
	"func":     KwFunc,
	"return":   KwReturn,
	"if":       KwIf,
	"elif":     KwElif,
	"else":     KwElse,
	"while":    KwWhile,
	"for":      KwFor,
	"in":       KwIn,
	"break":    KwBreak,
	"continue": KwContinue,
	"null":     KwNull,
	"true":     KwTrue,
	"false":    KwFalse,
	"Int":      KwTypeInt,
	"Float":    KwTypeFloat,
	"String":   KwTypeString,
	"Bool":     KwTypeBool,
	"File":     KwTypeFile,
}

// Token is a single lexical token together with its source line (1-based).
type Token struct {
	Kind    Kind
	Literal string
	Line    int
}
