# Seed プロジェクト規約

## プロジェクト概要

Seedは新規に設計する独自プログラミング言語。**Go言語で実装する**(Seedコンパイラ自身の実装言語がGo)。ソースファイルの拡張子は**`.seed`**。

コンパイルパイプラインは以下の通り。

```
Seedソース (.seed)
  ↓ (Seedが担当。本リポジトリのスコープ)
AMIVM-IR (.ir)
  ↓ amivm (外部CLIツール。別リポジトリ)
Goコード (.go)
  ↓ go build (Seedのビルドパイプラインが担当)
実行ファイル
```

**Seedの責務は「Seedソース → AMIVM-IR」の生成まで。** AMIVM-IRからGoコードへの変換は`amivm`が担い、Goコードから実行ファイルにする`go build`はさらに別工程(Seed側のビルドパイプラインが担当)。この3工程の境界を越えて責務を持たせない(例: SeedがGoコードを直接生成する、AMIVM-IRの意味検証をSeed側でも二重に行う、といった実装はしない)。

## ドキュメント構成

| ファイル | 役割 |
|---|---|
| `seed_spec.md` | **Seed言語仕様。唯一の正確な仕様。** 字句規則・型・演算子・制御構文・関数・ビルトイン関数などを定義。実装と齟齬が出たら、まず`seed_spec.md`の記述を疑い、仕様として確定してからコードを直すこと |
| `amivm-handoff-for-xxlang.md` | amivm開発側から受け取った一時的な引き継ぎ資料。**本ファイルは一時ファイルであり、必要な情報は本CLAUDE.mdに転記済み。** ローカルから削除してよい(下記「AMIVM-IRの書き方」節が転記内容の本体) |
| `amivm/` | 参照用にローカルへ置かれている amivm リポジトリのクローン(commit `ae07a2d`, https://github.com/amisonnet8/amivm )。**Seedのリポジトリの一部ではない。** amivmはSeedから見て「外部CLIツール」であり、`go install`で`PATH`に配置して呼び出す(下記参照)。このディレクトリは仕様を読むための参照物であり、Seed側のビルド成果物やimportパスがここに依存することがあってはならない |
| 本ファイル(`CLAUDE.md`) | Seedプロジェクトの規約・AIによる開発支援のための注意点 |

## amivmのインストール・呼び出し方

`amivm`はGo製CLIで、`go install`でインストールして`PATH`経由で呼ぶ(コピーやパス直接参照はしない)。

```sh
# amivmリポジトリをclone後
make install   # go install ./cmd/amivm — $GOBIN(未設定なら$GOPATH/bin)に配置される
```

Seed側のビルドスクリプトは、`amivm`が既に`PATH`にある前提で呼び出せばよい。

### CLIコマンド仕様

```
amivm <IRファイルパス> [-o|--output <出力ファイルパス>] [-v|--verbose] [-i|--import <名前>=<importパス>]...
```

- `-o`/`--output`省略時の出力先は、IRファイルパスの拡張子を`.go`に置き換えたパス(拡張子が無ければ`.go`を付け足したパス)
- `-v`/`--verbose`を付けると元のIR・型チェックの過程・最終的な生成コード・完了メッセージを標準出力に表示する。付けない場合、成功時は何も出力しない
- `-i`/`--import <名前>=<importパス>`は繰り返し指定できる。指定した名前が生成コード内で`?<名前>.xxx`のように参照されていれば、`import <名前> "<importパス>"`という明示的なimportを生成コードに追加する。**Seedが独自のランタイムライブラリ(後述)を呼びたい場合はこれを使う。** 未使用の名前は自動的に取り除かれるため、同じマッピング一式を全IRファイルに使い回してよい
- ファイル読み込み失敗・IRパースエラー・型チェック失敗などのエラーは`-v`/`--verbose`の有無に関わらず常に出力する
- `go build`による実行ファイル生成は行わない(別工程。Seed側のビルドパイプラインで実行する)

## AMIVM-IRの書き方(唯一の正確な仕様)

以下はamivmの`docs/amivm_spec.md`(commit `ae07a2d`時点)からの転記。**Seedのコード生成部がAMIVM-IRを出力する際は、この命令セット・カテゴリ・Kind分類に厳密に従うこと。** amivm本体のバージョンを上げた際は、`amivm/docs/amivm_spec.md`(または最新のamivmリポジトリ)と齟齬がないか確認すること。

### 制約・前提条件

- `FUNC`はトップレベルのみに置ける(関数のネスト不可)。`STTYPE`・`CLOS`・`SEL`もネスト不可
- 配列は1次元固定長のみ。**多次元配列はAMIVM-IR自体では表現しない。Seed側で1次元に展開すること**(もっともSeedの言語仕様自体が1次元配列のみのため、この変換は不要になる見込み)
- チャネル・スライス・map・構造体・クロージャーは、対応する`TYPE`系命令(`SLTYPE`/`MPTYPE`/`STTYPE`/`FNTYPE`)で型を定義してから使う
- トークンの区切り文字は**タブ**。行頭のインデント用タブは無視。`//`で始まる行はコメントとして無視

### 識別子のプレフィックス

全ての識別子は先頭の記号で種別が決まる。宣言側にも参照側と同じプレフィックスを要求する(`VAR`は`%`のみ、`GVAR`は`@`のみ)。

| 記号 | 意味 |
|---|---|
| `$` | 関数引数 |
| `&` | クロージャー引数 |
| `%` | 関数内変数名 |
| `@` | 関数外変数名(グローバル変数) |
| `^` | 型名 |
| `>` | 構造体フィールド名 |
| `!` | amivm定義関数名(`!xxx`→`<関数名>_amivm_function`、`!main`→`main`) |
| `?` | Go関数名(そのまま使う。標準ライブラリ・Seed独自ランタイム問わず) |
| `#` | ラベル名 |

### 命令一覧(抜粋・カテゴリごと)

| 分類 | 命令 |
|---|---|
| 変数宣言 | `VAR local type1` / `GVAR global type1` |
| 代入・ポインタ・配列 | `SET` `ASET` `AGET` `PSET` `PGET` `ADDR` |
| 算術 | `ADD` `SUB` `MUL` `DIV` `MOD` |
| ビット演算 | `BAND` `BOR` `BXOR` `BCLEAR` `BNOT` |
| シフト | `SHL` `SHR` |
| 論理演算 | `AND` `OR` `NOT` |
| 比較 | `EQ` `NEQ` `LT` `LTE` `GT` `GTE` |
| 文字列連結 | `CONCAT single slice1 slice2 ...` |
| ラベル・分岐 | `LABEL label` / `GOTO label` / `IF boolean1 label` |
| 関数定義 | `FUNC defname type1 ... : type3 ...` / `RET` / `ENDFUNC` |
| 関数呼び出し | `CALL multi1 ... : callname value1 ...` / `DEFER` / `SPAWN` |
| チャネル | `CHTYPE` `CHMAKE` `CHSEND` `CHRECV` |
| select | `SEL` `CASESEND` `CASERECV` `DEFAULT` `ENDSEL` |
| スライス | `SLTYPE` `SLMAKE` `SLICE` |
| 構造体 | `STTYPE` `FIELD` `ENDSTTYPE` `FSET` `FGET` |
| map | `MPTYPE` `MPMAKE` `MSET` `MGET` |
| クロージャー・関数型 | `FNTYPE` `CLOS` `ENDCLOS` |

各命令が生成するGoコードの詳細な対応表(全命令のGo生成形)は`amivm/docs/amivm_spec.md`(3.4節)を参照。**キャスト・組み込み関数(`close`/`len`/`cap`等)は専用命令を持たず`CALL`に統合されている**(Goの型変換`T(v)`は構文上`ast.CallExpr`と同一のため)。Seedの`int()`/`float()`/`string()`/`len()`等のビルトイン関数をIRへ落とす際は、この`CALL`統合方式を踏まえること。

### オペランドカテゴリ・Kind

各命令の引数(`whole`/`integer`/`number`/`boolean`/`slice`/`ordered`/`value`/`variable`/`single`/`multi`/`field`/`type`/`label`等)がどの形式のトークンを許容するかは、`amivm/docs/amivm_spec.md`の3.5節(オペランドカテゴリ)・3.6節(Kindの形状分類)に定義されている。Seedのコード生成部がここから逸脱したトークンを出力すると、amivmのパース段階で拒否される。実装前に必ず該当節を通読すること。

## 独自のGoランタイムを呼ぶ

`amivm`は`?pkg.Func`+`CALL`の仕組みで任意のGo関数を呼べる。Seedのビルトイン関数(`print`/`open`/`read`/`write`/`close`等)のうち、単純な演算命令に対応しないものは、Seed自身が用意するGoランタイムパッケージ(例: `seedrt`)の関数として実装し、生成したIRから`CALL`で呼び出す設計になる見込み。

1. ランタイム関数は普通のGoパッケージとして用意する(公開不要。プライベートモジュール/monorepoでの`replace`でも可)
2. `amivm`の出力先ディレクトリはGoモジュールである必要がある(型チェックが`golang.org/x/tools/go/packages`に依存するため)
3. amivmがまだ見たことのないパッケージを参照する場合は`-i 名前=importパス`を渡す(goimportsによる自動推測は、標準ライブラリと既参照パッケージ以外では信頼できないため)

例(`xxrt`は`seedrt`などSeed独自パッケージに置き換わる想定):

```go
package xxrt

func Helper(a, b int) int { return a*10 + b }
```

```
FUNC	!main	:
	VAR	%r	^int
	CALL	%r	:	?xxrt.Helper	1	2
	CALL	:	?fmt.Println	%r
	RET
ENDFUNC
```

```sh
amivm hello.ir -o hello.go -i xxrt=yourmodule/xxrt
```

**メソッド呼び出し**(例: `file.Close()`)は、`FNTYPE`でレシーバー込みの関数型を定義→`FGET`でメソッドを値として取得→`CALL`、という手順になる。ファイル操作(`open`/`read`/`write`/`close`)をSeedランタイム経由で`os.File`等をラップして実装するか、Seed独自の`File`型を`STTYPE`で定義してラップするかは、実装時に設計すること。

## 意味検証の責任分担(重要)

型の整合性・未定義識別子・関数シグネチャの不一致・メソッドの存在チェックなどは、**amivm側では検証せず`go/types`に全面的に委ねている。** amivmが保証するのは「構文的に妥当なGoコードを出力すること」だけ。

これはつまり、**Seedの意味検査(型チェック等)はSeedコンパイラ自身が担う必要がある** ということ。AMIVM-IR生成前の段階で、Seed言語仕様(`seed_spec.md`)に基づく型チェック・スコープ解決を完了させておくこと。IRを間違って生成した場合のエラーは、amivmの`go/types`型チェック失敗という形で(生成されたGoコードのエラーとして)返ってくるため、Seed側のエラーメッセージとしては非常にわかりにくい。**Seedコンパイラ自身が、ユーザーに分かりやすいエラーメッセージをamivm呼び出し前に出せるようにすること**が望ましい。

## Seed言語仕様とAMIVM-IRの対応関係で確定した設計判断

`seed_spec.md`の意味論をAMIVM-IRへどう落とし込むかは自明ではない箇所があった。Step1〜6の実装を通じて確定した内容を記す(各判断の詳細な理由は該当パッケージのdocコメント参照)。

- **`null`とベース値**: 各Seedスカラー変数を「値+`_isset`という付随bool」のペアとしてコード生成する(`internal/codegen`)。通常の読み取りは`_isset`を一切見ない(Goのゼロ値がSeedのベース値と全型で一致するため)。`isnull()`と`null`代入(値もベース値へリセットする)だけが`_isset`に触れる。配列は`_isset`を持たない(下記参照)
- **配列はGoスライスにコンパイル**(`SLTYPE`+`SLMAKE`。Goの固定長配列型`[N]T`は不使用): `Int[size]`のようにサイズが実行時変数になり得るため、コンパイル時定数を要求するGo配列型では表現できない。「固定長」の性質はSeedが一切resizeを生成しないことで保証する。配列は`_isset`を持たない(宣言時に`SLMAKE`で必ず具体的な値が入るため、「未代入」状態が存在しない)
- **配列の要素数不一致規則**(4節): 配列リテラルの代入・再代入・関数戻り値の代入のいずれでも、対象を`SLMAKE`で新しく確保し直してから、各要素を実行時境界チェック付きで`ASET`する(`LT`+`IF`+`GOTO`で1要素ごとにガード)。パディングは「ガードでスキップされた添字がSLMAKE直後のゼロ値のまま」という形で実現される。関数戻り値のように要素数がコンパイル時に分からない場合は、`len()`呼び出し+ランタイムループでコピーする(`internal/codegen/array.go`)
- **配列の参照渡し**(7節): 配列パラメータはGoスライスの値渡しをそのまま使う(コピーしない)。Goのスライスヘッダは値渡しでも同じ裏付け配列を指すため、要素への`ASET`/`AGET`は呼び出し元にそのまま反映される。関数内でパラメータ自体を丸ごと再代入(`SLMAKE`)しても呼び出し元には反映されない(スコープ外の挙動として未対応。仕様の例では要求されていない)
- **`main`の特別扱い**: Seedの`main(String[] args) Int`はGoの`func main()`(引数・戻り値なし)と非互換なため、ユーザーの`main`は内部的に`!seed_main`という別関数として出力し、実際の`!main`は`os.Args`を渡して`!seed_main`を呼び、戻り値を`os.Exit()`する薄いラッパーとして生成する。`main`という関数名の直接呼び出し禁止と、`seed_main`という名前のユーザー定義関数の禁止は、いずれもSeed側の意味検査(`internal/sema`)で保証する
- **`+`演算子の型分岐**(5節): `Int+Int`→`ADD`、`Float+Float`→`ADD`、`String+String`→`CONCAT`と、Seedの`+`は演算対象の型によって生成するIR命令自体が変わる。異なる型同士の`+`はSeedの型チェック段階で弾く(amivm側は関与しない)。単項マイナスに対応するAMIVM-IR命令が無いため、`-x`は`SUB tmp 0 x`として生成する
- **`read()`のEOF表現**: ファイル終端で`null`を返す仕様。Goの`bufio.Scanner`等は`(string, bool)`や`io.EOF`エラーで表現するため、Seedランタイム側の`read`実装で「EOFならSeedの`null`相当を返す」変換が必要(File I/OはStep7未着手)

以降、新しい設計判断が生じた場合もこの節(または実装コード側のコメント)に確定内容を残し、仮説段階のまま放置しないこと。

## リポジトリ構成(提案)

現時点でSeedはコード未着手(Go moduleも未初期化)。実装を始める際の構成の目安。決定事項ではないため、実装が進む中で実態に合わせて更新すること。

```
seed/
  seed_spec.md         Seed言語仕様(唯一の正確な仕様)
  CLAUDE.md            本ファイル
  go.mod
  cmd/seed/            CLIエントリポイント(seed build 等)
  internal/lexer/       字句解析
  internal/parser/      構文解析 → AST
  internal/ast/         AST定義
  internal/sema/        意味検査(型チェック・スコープ解決。「amivm側は検証しない」ため必須)
  internal/codegen/     AST → AMIVM-IR生成
  seedrt/                Seedランタイム(open/read/write/close等、CALLで呼ばれるGo実装)。
                          生成されたGoコード(ユーザー側の別モジュール)からimportされるため、
                          internal/配下に置けない(Goのinternal可視性ルールのため公開パッケージにする)
  testdata/ or examples/  サンプルSeedプログラム(`.seed`。実装した構文ごとに追加)
```

## 開発の進め方

1. `seed_spec.md`を正として実装する。仕様に曖昧な点や矛盾を見つけたら、まず仕様側を疑い、確定させてからコードを直す
2. AMIVM-IRを生成する処理を書いたら、実際に`amivm`(`PATH`にインストール済みのもの)にかけて`go build`まで通し、動作確認する。IRの構文・カテゴリ違反はamivmのパース/型チェックで初めて顕在化するため、「ロジック上正しそうに見える」だけで済ませない
3. Seedの意味検査(型チェック等)は、amivmに渡す前にSeed側で完了させる。amivmの`go/types`エラーをユーザー向けエラーとしてそのまま出さない
4. 新しい構文・ビルトイン関数を実装したら、対応するサンプルSeedプログラムを`testdata/`等に追加し、生成されたIR・Goコード・実行結果まで確認する
5. amivm本体の仕様が更新された場合(`amivm/docs/amivm_spec.md`または最新リポジトリを再確認)、本ファイルの「AMIVM-IRの書き方」節が古くなっていないか照合し、必要なら更新する
