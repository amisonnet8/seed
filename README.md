# Seed

A small programming language, implemented in Go, that compiles to Go source via AMIVM-IR.

> [日本語版 README はこちら](README_ja.md)

## Status

Seed's front end (lexer, parser, semantic checker, and AMIVM-IR code generator) implements the full language described in [`seed_spec.md`](seed_spec.md): variables and scope, operators, control flow, arrays, user-defined functions, and file I/O.

## Pipeline

```
Seed source (.seed)
  ↓ (Seed — this repository)
AMIVM-IR (.ir)
  ↓ amivm (external tool, github.com/amisonnet8/amivm)
Go source (.go)
  ↓ go build
executable
```

Seed's own responsibility stops at emitting AMIVM-IR. Turning that into Go source is [amivm](https://github.com/amisonnet8/amivm)'s job, and turning that into an executable is a plain `go build` — both are separate tools `seed` shells out to, not something this repository implements itself.

## Requirements

- Go, matching the version in [`go.mod`](go.mod).
- [`amivm`](https://github.com/amisonnet8/amivm) on your `PATH`.

## Install

```sh
go install github.com/amisonnet8/amivm/cmd/amivm@latest
go install github.com/amisonnet8/seed/cmd/seed@latest
```

Both land in `$GOBIN` (or `$GOPATH/bin` if unset) — make sure that directory is on your `PATH`. Since every Seed build ends in a plain `go build`, having Go installed already covers every dependency `seed` needs at runtime; there's nothing else to fetch.

## Usage

```
seed <command> [flags] <file.seed>
```

| Command | Output |
|---|---|
| `build` | a native executable |
| `run` | compiles and immediately runs, streaming its stdin/stdout/stderr |
| `emit-ir` | the AMIVM-IR |
| `emit-go` | the Go source (via amivm) |
| `help` | this command list |

`build`, `emit-ir`, and `emit-go` accept:

| Flag | Description |
|---|---|
| `-o <file>` | output file path (default: derived from the input path, e.g. `foo.seed` → `foo`/`foo.ir`/`foo.go`) |
| `-v` | show each pipeline stage's output as it runs (the generated IR, amivm's own `-v` trace, the final Go source) |

## Example

```seed
func Int main(String[] args) {
    print("Hello, Seed!")
    return 0
}
```

```sh
$ seed run hello.seed
Hello, Seed!
```

More runnable examples covering variables/null, operators, control flow, arrays, functions, and file I/O live in [`examples/`](examples/).

## Language

**The only authoritative specification is [`seed_spec.md`](seed_spec.md).** If any other document (including this README) disagrees with it, `seed_spec.md` wins.

## Repository layout

```
cmd/seed/          CLI entry point (this README's `seed` commands)
internal/lexer/     tokenizing
internal/parser/    parsing → AST
internal/ast/       AST definitions
internal/sema/      semantic analysis (type checking, scope resolution — amivm delegates
                     all of this to go/types, so Seed has to catch it itself; see CLAUDE.md)
internal/codegen/   AST → AMIVM-IR
seedrt/             Seed's Go runtime library (open/read/write/close, and a few
                     conversions), embedded into every seed build — see CLAUDE.md
examples/           runnable .seed sample programs, one group per language feature
seed_spec.md        the Seed language specification (the only authoritative one)
CLAUDE.md           project conventions for AI-assisted development
seed_implementation_notes.md  lessons learned writing an AMIVM-IR-generating frontend,
                     for whoever implements the next language on top of amivm
LICENSE             MIT
```

## License

[MIT](LICENSE)
