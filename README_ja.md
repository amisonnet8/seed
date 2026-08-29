# Seed

[![test](https://github.com/amisonnet8/seed/actions/workflows/test.yml/badge.svg)](https://github.com/amisonnet8/seed/actions/workflows/test.yml)

AMIVM-IRを経由してGoソースコードへコンパイルする、Go実装のプログラミング言語です。

> [English README is here](README.md)

## ステータス

Seedのフロントエンド(字句解析・構文解析・意味検査・AMIVM-IRコード生成)は、[`seed_spec.md`](seed_spec.md)に記載された言語仕様を一通り実装済みです: 変数・スコープ、演算子、制御構文、配列、ユーザー定義関数、ファイルI/O。

## パイプライン

```
Seedソース (.seed)
  ↓ (Seed — 本リポジトリ)
AMIVM-IR (.ir)
  ↓ amivm (外部ツール。github.com/amisonnet8/amivm)
Goソースコード (.go)
  ↓ go build
実行ファイル
```

Seed自身の責務はAMIVM-IRを出力するところまでです。それをGoソースへ変換するのは[amivm](https://github.com/amisonnet8/amivm)の仕事、実行ファイルにする単純な`go build`はさらに別工程で、どちらも`seed`が呼び出す外部ツールであり、本リポジトリ自体が実装しているものではありません。

## 動作要件

- Go([`go.mod`](go.mod)記載のバージョン)
- `PATH`の通った場所にインストールされた[`amivm`](https://github.com/amisonnet8/amivm)

## インストール

```sh
go install github.com/amisonnet8/amivm/cmd/amivm@latest
go install github.com/amisonnet8/seed/cmd/seed@latest
```

どちらも`$GOBIN`(未設定なら`$GOPATH/bin`)に配置されるので、そのディレクトリが`PATH`に通っていることを確認してください。Seedのビルドは最終的に必ず素の`go build`で終わるため、Goさえインストールされていれば`seed`が実行時に必要とするものは全て揃います(それ以外に取得すべきものはありません)。

## 使い方

```
seed <コマンド> [フラグ] <file.seed>
```

| コマンド | 出力 |
|---|---|
| `build` | 実行ファイル |
| `run` | コンパイルして即座に実行(stdin/stdout/stderrをそのまま引き継ぐ) |
| `emit-ir` | AMIVM-IR |
| `emit-go` | Goソースコード(amivm経由) |
| `help` | このコマンド一覧 |

`build`・`emit-ir`・`emit-go`は以下のフラグを受け付けます。

| フラグ | 説明 |
|---|---|
| `-o <file>` | 出力ファイルパス(省略時は入力パスから導出。例: `foo.seed` → `foo`/`foo.ir`/`foo.go`) |
| `-v` | 各パイプライン段階の出力を実行しながら表示(生成されたIR、amivm自身の`-v`トレース、最終的なGoソース) |

## 例

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

変数/null・演算子・制御構文・配列・関数・ファイルI/O・ビルトイン関数・文字列エスケープシーケンスを一通り網羅した実行可能なサンプルを[`examples/`](examples/)に置いています。

## 言語仕様

**唯一の正確な仕様は[`seed_spec.md`](seed_spec.md)です。** 本READMEを含む他のドキュメントと矛盾する場合は`seed_spec.md`を優先してください。

## リポジトリ構成

```
cmd/seed/           CLIエントリポイント(本READMEの`seed`コマンド群)
internal/lexer/      字句解析
internal/parser/     構文解析 → AST
internal/ast/        AST定義
internal/sema/       意味検査(型チェック・スコープ解決。amivm側は全てgo/typesに委ねているため
                      Seed自身がここを担う必要がある。詳細はCLAUDE.md参照)
internal/codegen/    AST → AMIVM-IR
seedrt/              Seedランタイム(open/read/write/close、いくつかの型変換)。
                      seedビルドのたびに埋め込まれる。詳細はCLAUDE.md参照
examples/            実行可能な.seedサンプル(言語機能ごとにグループ化)
seed_spec.md         Seed言語仕様(唯一の正確な仕様)
CLAUDE.md            AIによる開発支援のためのプロジェクト規約
seed_implementation_notes.md  AMIVM-IRを生成するフロントエンド実装の知見メモ。
                      次にamivm上で別言語を実装する人/AI向け
Makefile              ビルド・テスト・クリーンアップ用タスク(`make help`で一覧表示)
.github/workflows/test.yml  CI: push/PR時にgofmt/go vet/go test/make test-examplesを実行
LICENSE              MIT
```

## ライセンス

[MIT](LICENSE)
