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
| `README.md` / `README_ja.md` | GitHub向けの導入ドキュメント(英語版/日本語版)。インストール方法(`go install`)・CLIコマンド一覧・簡単な例を掲載。amivmの README と対になる構成 |
| `amivm/` | 参照用にローカルへ置かれている amivm リポジトリのクローン(commit `22c701f`, https://github.com/amisonnet8/amivm )。**Seedのリポジトリの一部ではない。** amivmはSeedから見て「外部CLIツール」であり、`go install`で`PATH`に配置して呼び出す(下記参照)。このディレクトリは仕様を読むための参照物であり、Seed側のビルド成果物やimportパスがここに依存することがあってはならない。仕様書は`docs/`配下ではなくリポジトリ直下に`amivm_spec.md`(唯一の正確な仕様)・`amivm_instruction_spec.md`(設計判断の理由まで含む解説版)として置かれている(旧`docs/`配下からの移動済み) |
| `seed_implementation_notes.md` | AMIVM-IRを生成するフロントエンドを実装する際の実地の知見(踏んだ地雷・確立したパターン)をまとめたメモ。Seed自身の言語仕様や設計判断ではなく、「次にAMIVM上で別の言語を実装する人/AI」向けの申し送りという位置づけ。**この種の実装知見はamivm本体のリポジトリではなく、各言語(フロントエンド)側のリポジトリに置く運用**とし、Seedのものは本ファイルに集約する |
| 本ファイル(`CLAUDE.md`) | Seedプロジェクトの規約・AIによる開発支援のための注意点 |

## amivmのインストール・呼び出し方

`amivm`はGo製CLIで、`go install`でインストールして`PATH`経由で呼ぶ(コピーやパス直接参照はしない)。

```sh
# amivmリポジトリをclone後
make install   # go install ./cmd/amivm — $GOBIN(未設定なら$GOPATH/bin)に配置される
```

Seed側のビルドスクリプトは、`amivm`が既に`PATH`にある前提で呼び出せばよい。

**amivmリポジトリはPublic化されている**ため、GitHub Actions上でも認証情報無しに`go install github.com/amisonnet8/amivm/cmd/amivm@latest`でインストールできる(`.github/workflows/test.yml`参照)。これにより、これまで手元でしか検証できなかった「実際に`amivm`にかけて`go build`まで通す」確認(開発の進め方2.参照)をCIで毎回自動実行できる。

### CLIコマンド仕様

```
amivm <IRファイルパス> [-o|--output <出力ファイルパス>] [-v|--verbose] [-i|--import <名前>=<importパス>]... [-h|--help]
```

- `-o`/`--output`省略時の出力先は、IRファイルパスの拡張子を`.go`に置き換えたパス(拡張子が無ければ`.go`を付け足したパス)
- `-v`/`--verbose`を付けると元のIR・型チェックの過程・最終的な生成コード・完了メッセージを標準出力に表示する。付けない場合、成功時は何も出力しない
- `-i`/`--import <名前>=<importパス>`は繰り返し指定できる。指定した名前が生成コード内で`?<名前>.xxx`のように参照されていれば、`import <名前> "<importパス>"`という明示的なimportを生成コードに追加する。**Seedが独自のランタイムライブラリ(後述)を呼びたい場合はこれを使う。** 未使用の名前は自動的に取り除かれるため、同じマッピング一式を全IRファイルに使い回してよい
- `-h`/`--help`を付けると使い方を表示して終了する。他オプションの妥当性検証より先に判定されるため、`<IRファイルパス>`無しの`amivm -h`単体でも動作する(Seed側からは通常使わない)
- ファイル読み込み失敗・IRパースエラー・型チェック失敗などのエラーは`-v`/`--verbose`の有無に関わらず常に出力する。**エラー・使い方メッセージは全て英語。**Seed側で`amivm`の標準出力・標準エラーをそのままユーザーに転送している箇所(`cmd/seed/build.go`)は、この変更を意識した実装変更が不要(文字列の内容をパース・照合していないため)
- `go build`による実行ファイル生成は行わない(別工程。Seed側のビルドパイプラインで実行する)

## AMIVM-IRの書き方(唯一の正確な仕様)

以下はamivmの`amivm_spec.md`(commit `22c701f`時点。旧`docs/`配下から移動済み)からの転記。**Seedのコード生成部がAMIVM-IRを出力する際は、この命令セット・カテゴリ・Kind分類に厳密に従うこと。** amivm本体のバージョンを上げた際は、`amivm/amivm_spec.md`(または最新のamivmリポジトリ)と齟齬がないか確認すること。

### 制約・前提条件

- `FUNC`・`FUNCM`(4.23節。レシーバー付きメソッド定義)・`STTYPE`・`INTYPE`(4.24節。インターフェース型)はネスト不可(いずれもトップレベルのみ)。`IF`・`LOOP`・`CLOS`・`SEL`はいずれもネストできる(互いの本体の中に任意の組み合わせ・任意の深さで書ける。Seedの`internal/codegen`はif/elif/else・while・for-inをこの`IF`/`LOOP`に直接コンパイルしており、多用している)
- 配列は1次元固定長のみ。**多次元配列はAMIVM-IR自体では表現しない。Seed側で1次元に展開すること**(もっともSeedの言語仕様自体が1次元配列のみのため、この変換は不要になる見込み)
- チャネル・スライス・map・構造体・クロージャー・固定長配列は、対応する`TYPE`系命令(`SLTYPE`/`MPTYPE`/`STTYPE`/`FNTYPE`/`ARTYPE`)で型を定義してから使う。ただし固定長配列は`^[n]type1`という複合形でインラインに(`TYPE`系命令無しで)宣言することもでき、`ARTYPE`は名前を付けて使い回したい場合の選択肢に過ぎない(必須ではない)
- トークンの区切り文字は**タブ**。行頭のインデント用タブは無視。`//`で始まる行はコメントとして無視
- `FUNC`/`FUNCM`/`STTYPE`/`INTYPE`/`CALL`/`DEFER`/`SPAWN`はGoジェネリクス(型パラメータ・明示的型引数)に対応した(後述)。**Seedの言語仕様自体にジェネリクスは無いため、Seedのコード生成はこの拡張構文を一切使わない。** 型パラメータ・型引数部分は完全に省略可能(コロンの個数で構文が変わる)で、省略時は旧仕様と同一のIRになるため後方互換性がある(実機のamivmで全examplesが無修正でビルドできることを確認済み)
- 整数リテラルは10進に加えて16進(`0x1A`)・8進(`0o17`)・2進(`0b101`)、桁区切りの`_`(`1_000_000`)にも対応した(6節)。**Seedのコード生成は常に素の10進表記のみを出力し、この拡張形式は一切生成しない**(後方互換。既存の10進リテラルの構文はそのまま通る)

### 識別子のプレフィックス

全ての識別子は先頭の記号で種別が決まる。宣言側にも参照側と同じプレフィックスを要求する(`VAR`は`%`のみ、`GVAR`は`@`のみ)。

| 記号 | 意味 |
|---|---|
| `$` | 関数引数 |
| `&` | クロージャー引数(`&N`は自分がいる`CLOS`階層のN番目、`&L-N`で階層`L`を明示指定できる) |
| `%` | 関数内変数名 |
| `@` | 関数外変数名(グローバル変数) |
| `^` | 型名 |
| `>` | 構造体フィールド名 |
| `<` | メソッド名(`METHVAL`・`INTYPE`内の`METHOD`シグネチャで使う。未使用) |
| `!` | amivm定義関数名(`!xxx`→`<関数名>_amivm_function`、`!main`→`main`) |
| `?` | Go関数名(そのまま使う。標準ライブラリ・Seed独自ランタイム問わず) |
| `#` | ラベル名 |

### 命令一覧(抜粋・カテゴリごと)

| 分類 | 命令 |
|---|---|
| 変数宣言 | `VAR local type1` / `GVAR global type1` |
| 代入・ポインタ・配列 | `SET` `ASET` `AGET` `PSET` `PGET` `ADDR`(第3引数`point`省略可。`&v.field`/`&v[i]`も表現できる) |
| 算術 | `ADD` `SUB` `MUL` `DIV` `MOD` |
| ビット演算 | `BAND` `BOR` `BXOR` `BCLEAR` `BNOT` |
| シフト | `SHL` `SHR` |
| 論理演算 | `AND` `OR` `NOT` |
| 比較 | `EQ` `NEQ` `LT` `LTE` `GT` `GTE` |
| 文字列連結 | `CONCAT single1 slice1 slice2 ...` |
| ラベル・GOTO | `LABEL label` / `GOTO label` |
| 条件分岐 | `IF boolean1` / `ELIF boolean1` / `ELSE` / `ENDIF`(ブロック形。Goの`if`/`else if`/`else`に対応。単一行`IF boolean1 label`は廃止され後方互換は無い) |
| ループ | `LOOP` / `BREAK` / `CONTINUE` / `ENDLOOP`(Goの無限`for {}`。条件付きループは`LOOP`の中で`IF`+`BREAK`を組み合わせて表現する) |
| 型アサーション | `ASSERT multi1 (multi2) variable type1`(Goの`v.(T)`。未使用) |
| 関数定義 | `FUNC defname (typename1 constraint1 ... :) type1 ... : type3 ...` / `RET` / `ENDFUNC`(`(typename1 constraint1 ... :)`は型パラメータ宣言。省略可・未使用) |
| 関数呼び出し | `CALL multi1 ... : callname (type1 ... :) value1 ...` / `DEFER` / `SPAWN`(`(type1 ... :)`は明示的型引数。省略可・未使用) |
| チャネル | `CHTYPE` `CHMAKE` `CHSEND` `CHRECV` |
| select | `SEL` `CASESEND` `CASERECV` `DEFAULT` `ENDSEL`(`CASESEND`/`CASERECV`/`DEFAULT`はもう`label`を取らない。次のケースか`ENDSEL`までがブロック本体) |
| 固定長配列 | `ARTYPE typename1 type1 imm`(`type typename1 [imm]type1`。名前付きの固定長配列型宣言。`imm`はコンパイル時定数リテラルのみで識別子は不可。未使用) |
| スライス | `SLTYPE` `SLMAKE` `SLICE` |
| 構造体 | `STTYPE`(型パラメータ宣言可。省略可・未使用) `FIELD` `ENDSTTYPE` `FSET` `FGET` |
| map | `MPTYPE` `MPMAKE` `MSET` `MGET` `MPKEYS`(mapの全キーを`slices.Collect(maps.Keys(m))`で取得。未使用) |
| クロージャー・関数型 | `FNTYPE` `CLOS` `ENDCLOS`(`CLOS`のみ元々ネスト可。代入先は`%xxx`に限らず`$N`/`@xxx`/`&N`/`&L-N`も可。いずれも未使用) |
| メソッド値・関数値取得 | `METHVAL local variable method`(`local := variable.method`) / `FUNCVAL local callname`(`local := callname`)。いずれも`local`は`VAR`で事前宣言せず新規`:=`宣言。未使用 |
| メソッド定義 | `FUNCM defname receiver (typename1 ... :) type1 ... : type3 ...` / `RET` / `ENDFUNCM`(レシーバー付きメソッド。本体内でレシーバーは`$0`。未使用) |
| インターフェース型 | `INTYPE typename1 (typename2 constraint1 ...)` `METHOD method type1 ... : type3 ...` `ENDINTYPE`(未使用) |
| ジェネリクス型実体化 | `GETYPE typename1 typename2 type1 ...`(`type typename1 = typename2[type1, ...]`。未使用) |

各命令が生成するGoコードの詳細な対応表(全命令のGo生成形)は`amivm/amivm_spec.md`(4節)を参照。**キャスト・組み込み関数(`close`/`len`/`cap`等)は専用命令を持たず`CALL`に統合されている**(Goの型変換`T(v)`は構文上`ast.CallExpr`と同一のため)。型アサーション(`v.(T)`)だけは構文が異なる別ASTノード(`ast.TypeAssertExpr`)になるため`CALL`に含めず`ASSERT`という専用命令になっている。Seedの`int()`/`float()`/`string()`/`len()`等のビルトイン関数をIRへ落とす際は、この`CALL`統合方式を踏まえること。`callname`(`CALL`の呼び出し対象)・`value`(値全般)は`%xxx`保持の関数値・メソッド値に加えて`@xxx`(パッケージレベル変数)・`$N`/`&N`(パラメータ・クロージャー引数)・`!xxx`/`?xxx`(関数そのものを呼ばずに値として渡す)も許容するよう拡張されたが、Seedはまだ第一級関数を持たないためこの拡張も未使用。

`FUNC`/`FUNCM`/`STTYPE`/`INTYPE`の型パラメータ宣言、`CALL`/`DEFER`/`SPAWN`の明示的型引数は、いずれも`(...)`区切り(実際にはコロンの個数で判別)の**任意**セグメントであり、省略すればSeedがこれまで生成してきたIRと完全に同じ構文になる。**Seedはジェネリクスを持たない言語なので、これらの新規構文(`METHVAL`/`FUNCVAL`/`FUNCM`/`INTYPE`/`GETYPE`を含む)は現時点で一切使用せず、`internal/codegen`の変更も不要だった**(下記「確定した設計判断」参照)。

### オペランドカテゴリ・Kind

各命令の引数(`whole`/`integer`/`number`/`boolean`/`slice`/`ordered`/`value`/`variable`/`single1`/`single2`/`multi`/`field`/`type`/`label`等)がどの形式のトークンを許容するかは、`amivm/amivm_spec.md`の5節(オペランドカテゴリ)・6節(トークンの形状分類)に定義されている。Seedのコード生成部がここから逸脱したトークンを出力すると、amivmのパース段階で拒否される。実装前に必ず該当節を通読すること。

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

**メソッド呼び出し**(例: `file.Close()`)は、`FNTYPE`でレシーバー込みの関数型を定義→`FGET`でメソッドを値として取得→`CALL`、という手順になる(`FNTYPE`の型がGoの実際のメソッド値の型と厳密に一致しない場合の代替として、事前の型宣言を要らない`METHVAL`/`FUNCVAL`も追加された)。Seedの`File`型はどちらの方式も使わず、`seedrt.File`(`?pkg.Func`+`CALL`で呼べる普通の構造体)を`^*seedrt.File`という外部型ポインタとして直接参照する形で実装した(下記「確定した設計判断」参照)。

## 意味検証の責任分担(重要)

型の整合性・未定義識別子・関数シグネチャの不一致・メソッドの存在チェックなどは、**amivm側では検証せず`go/types`に全面的に委ねている。** amivmが保証するのは「構文的に妥当なGoコードを出力すること」だけ。

これはつまり、**Seedの意味検査(型チェック等)はSeedコンパイラ自身が担う必要がある** ということ。AMIVM-IR生成前の段階で、Seed言語仕様(`seed_spec.md`)に基づく型チェック・スコープ解決を完了させておくこと。IRを間違って生成した場合のエラーは、amivmの`go/types`型チェック失敗という形で(生成されたGoコードのエラーとして)返ってくるため、Seed側のエラーメッセージとしては非常にわかりにくい。**Seedコンパイラ自身が、ユーザーに分かりやすいエラーメッセージをamivm呼び出し前に出せるようにすること**が望ましい。

## Seed言語仕様とAMIVM-IRの対応関係で確定した設計判断

`seed_spec.md`の意味論をAMIVM-IRへどう落とし込むかは自明ではない箇所があった。Step1〜7の実装を通じて確定した内容を記す(各判断の詳細な理由は該当パッケージのdocコメント参照)。

- **`null`とベース値**: 各Seedスカラー変数を「値+`_isset`という付随bool」のペアとしてコード生成する(`internal/codegen`)。通常の読み取りは`_isset`を一切見ない(Goのゼロ値がSeedのベース値と全型で一致するため)。`isnull()`と`null`代入(値もベース値へリセットする)だけが`_isset`に触れる。配列は`_isset`を持たない(下記参照)
- **配列はGoスライスにコンパイル**(`SLTYPE`+`SLMAKE`。Goの固定長配列型`[N]T`、および同じ制約を持つ`ARTYPE`(4.16節。`imm`=コンパイル時定数リテラルしか受け付けない)は不使用): `Int[size]`のようにサイズが実行時変数になり得るため、コンパイル時定数を要求するGo配列型・`ARTYPE`のいずれでも表現できない。「固定長」の性質はSeedが一切resizeを生成しないことで保証する。配列は`_isset`を持たない(宣言時に`SLMAKE`で必ず具体的な値が入るため、「未代入」状態が存在しない)
- **配列の要素数不一致規則**(4節): 配列リテラルの代入・再代入・関数戻り値の代入のいずれでも、対象を`SLMAKE`で新しく確保し直してから、各要素を実行時境界チェック付きで`ASET`する(`LT`+`IF`+`ENDIF`で1要素ごとにガード)。パディングは「ガードでスキップされた添字がSLMAKE直後のゼロ値のまま」という形で実現される。関数戻り値のように要素数がコンパイル時に分からない場合は、`len()`呼び出し+ランタイムの`LOOP`でコピーする(`internal/codegen/array.go`)
- **配列の参照渡し**(7節): 配列パラメータはGoスライスの値渡しをそのまま使う(コピーしない)。Goのスライスヘッダは値渡しでも同じ裏付け配列を指すため、要素への`ASET`/`AGET`は呼び出し元にそのまま反映される。関数内でパラメータ自体を丸ごと再代入(`SLMAKE`)しても呼び出し元には反映されない(スコープ外の挙動として未対応。仕様の例では要求されていない)
- **`main`の特別扱い**: Seedの`main(String[] args) Int`はGoの`func main()`(引数・戻り値なし)と非互換なため、ユーザーの`main`は内部的に`!seed_main`という別関数として出力し、実際の`!main`は`os.Args`を渡して`!seed_main`を呼び、戻り値を`os.Exit()`する薄いラッパーとして生成する。`main`という関数名の直接呼び出し禁止と、`seed_main`という名前のユーザー定義関数の禁止は、いずれもSeed側の意味検査(`internal/sema`)で保証する
- **`+`演算子の型分岐**(5節): `Int+Int`→`ADD`、`Float+Float`→`ADD`、`String+String`→`CONCAT`と、Seedの`+`は演算対象の型によって生成するIR命令自体が変わる。異なる型同士の`+`はSeedの型チェック段階で弾く(amivm側は関与しない)。単項マイナスに対応するAMIVM-IR命令が無いため、`-x`は`SUB tmp 0 x`として生成する
- **`File`型と`seedrt`**: `File`は`^*seedrt.File`(`seedrt`パッケージで定義した、`*os.File`+永続的な`*bufio.Reader`を持つ構造体へのポインタ)にマッピングする。`*os.File`直接ではなく永続的なReaderを持たせているのは、`read()`を呼ぶたびに新しい`bufio.Reader`を作ると、先読みしてバッファに溜め込んだ未消費バイトが`*os.File`のカーソルはすでに進んだ状態のまま捨てられてしまう(データロス)ため。`File`は他のスカラー型と同じく`_isset`を持つ(配列とは異なり、「まだopenしていない」状態を通常のnull追跡でそのまま表現できるため)
- **`read()`のEOF表現**: `seedrt.Read`は`(string, bool)`を返す(`ok=false`がEOF)。AMIVM-IRの`CALL`は複数の結果オペランドを取れる(`CALL value isset : ?seedrt.Read file`)ため、この2値をSeed変数自身の値オペランドと`_isset`オペランドへ**直接**書き込める。これにより「`read()`の戻り値が動的にnullになりうる」という、リテラル`null`のような静的な仕組みでは表現できないケースが、既存のnull機構にそのまま乗る。ただしこの特別扱いが効くのは`x = read(f)`という直接代入の形のみで、`read()`を他の式にネストする使い方はsemaで拒否する(`isnull(x)`のように、結果を必ず変数へ受けてから判定するspecの用例に合わせた制約)
- **ビルトイン変換の実装先**: `int()`/`float()`は数値同士の変換に限りGoの素の型変換(`?int`/`?float64`。CALLとキャストの構文的同一性を利用)を使い、`String`↔数値・`Bool`→`Int`のように素の変換が無い組み合わせだけ`seedrt`(`ParseInt`/`ParseFloat`/`BoolToInt`)に実装する。`string()`は全パターンで`strconv`(`Itoa`/`FormatFloat`/`FormatBool`)を使い`seedrt`は不要(Goの`string(65)`はルーン変換になり`"A"`を返してしまうため、素の型変換は使えない)。`len()`の文字列版は`len(string)`(バイト数)ではなく`unicode/utf8.RuneCountInString`(文字数)を使う
- **`seedrt`の配布方法**: `seedrt`パッケージ自身の`.go`ファイルを`go:embed`で`seedrt`パッケージに埋め込み(`seedrt/embed.go`)、`seed build`実行時にスクラッチビルド用ディレクトリ配下の`seedrt/`へコピーしてから`amivm`の`-i seedrt=seedbuild/seedrt`で解決する。これにより生成コードの`import "seedrt"`は、`seed`バイナリがどこでビルド・実行されても常にローカルに解決できる(ネットワークアクセスや実モジュール依存が不要)
- **CLIコマンド構成**: `seed <build|run|emit-ir|emit-go|help> [-o file] [-v] <file.seed>`。`build`/`emit-ir`/`emit-go`はパイプラインの異なる段階(実行ファイル/AMIVM-IR/Goソース)で止めて出力するだけの違いで、共通ロジックは`cmd/seed/build.go`の`compileToIR`→`compileToGo`→`compileToBinary`という3段の関数に分けて実装している(各コマンドは必要な段までしか呼ばない)。`-v`は生成されたIR・amivm自身の`-v`トレース(型チェック過程+最終Goコード)を標準出力に流すだけで、amivmの`-v`を素通しする形に倣っている
- **`seed`自体の配布は`go install`のみ**(amivmと同じ方針): `go.mod`のモジュールパスは`github.com/amisonnet8/seed`(実リポジトリのパスと一致させる必要がある。`go install`はVCS経由でモジュールを解決するため、パスなしの`module seed`のようなプレースホルダー名では動かない)。Seedのビルドは最終的に必ず`go build`を経由するため、エンドユーザーは元々Goツールチェーンを持っている前提になり、バイナリ配布やDocker配布は現時点では採用していない(将来必要になれば追加を検討)
- **`IF`/`LOOP`ブロック化への追随**(commit `253a3fd`): amivmが単一行`IF boolean1 label`を廃止しブロック形の`IF`/`ELIF`/`ELSE`/`ENDIF`と`LOOP`/`BREAK`/`CONTINUE`/`ENDLOOP`に置き換えた(後方互換無し)のに伴い、`internal/codegen`のif/elif/else・while・for-inの生成をLABEL/GOTOの平坦なgoto連鎖から、これらのブロック命令へ全面的に書き換えた(`stmt.go`・`array.go`)。elif連鎖は`ELIF`トークンをそのまま使わず、`ELSE`の中に次の`IF`をネストする形で自前生成している(`ELIF boolean1`は条件オペランドがそのIR行の時点で既に値になっている必要があり、複数命令が要る条件式を「1つ前の節の`}`」と「`else if`」の間に挟む余地が無いため。ネストなら各節の条件計算をそれを守る`ELSE`の中に自然に置け、しかも短絡評価も自動で成立する)。`break`は常に素の`BREAK`(最も内側の`LOOP`を抜ける)。`continue`はwhileでは素の`CONTINUE`で足りる(条件再チェックが`LOOP`本体の先頭にあるため)が、for-inだけは例外で、インデックスの`++`が本体の**後**にあるため素の`CONTINUE`だと`++`を飛ばしてしまう。そこで`LABEL`/`GOTO`(今回の変更後も存置されている)をfor-inの`continue`専用に残し、`++`直前のラベルへ`GOTO`する。ただしラベルは`continue`が無いfor-in本体では誰からも参照されず`go/types`の「declared and not used」で失敗するため、本体の通常フォールスルー末尾にも常に`GOTO`を1本置いてラベルの被参照を保証している(実装時に実際にこの失敗を踏んで発見した。`internal/codegen/stmt.go`のgenForInStmt参照)。VARを関数先頭へホイストする既存の仕組み(旧goto連鎖時代に「gotoが宣言を飛び越える」問題を避けるためのもの)は、ブロック化で理論上は不要になったが、shadowing用の命名スキーム(`scope.go`)が依存しているため変更せず維持している
- **METHVAL/FUNCVAL/FUNCM/INTYPE/GETYPE追加・ジェネリクス対応への追随**(commit `020e6bc`): amivmが構造体メソッド定義(`FUNCM`/`ENDFUNCM`)・インターフェース型(`INTYPE`/`METHOD`/`ENDINTYPE`)・メソッド値/関数値の`:=`取得(`METHVAL`/`FUNCVAL`。旧`METHOD`は`METHVAL`に改名)・ジェネリクス型の実体化(`GETYPE`)を新規追加し、`FUNC`/`FUNCM`/`STTYPE`/`INTYPE`/`CALL`/`DEFER`/`SPAWN`にGoジェネリクス(型パラメータ・明示的型引数)対応を加えた。調査の結果、**`internal/codegen`の変更は一切不要と判断した**: Seedの言語仕様自体に構造体・インターフェース・ジェネリクス・メソッド定義が無く、これらの新命令をどれも生成する理由がないため。加えて、Seedが実際に生成している`FUNC`/`CALL`/`DEFER`/`SPAWN`/`STTYPE`の構文(型パラメータ・型引数を伴わない形)は、新仕様でもコロン1個の従来どおりの構文としてそのまま受理される(型パラメータ・型引数セグメントは省略可能な拡張として追加されたため)。実機の`amivm`(`go install`済み)で`examples/`配下の全`.seed`ファイルを`seed build`→実行まで無修正で確認済み。ドキュメント側は本ファイルの命令一覧・識別子プレフィックス表・節番号参照(`amivm/docs/amivm_spec.md`→`amivm/amivm_spec.md`、旧3.4/3.5/3.6節→新4/5/6節)を追随させた。今後Seedの言語仕様に構造体・インターフェース・ジェネリクスを追加する際は、`FUNCM`(メソッド定義)・`INTYPE`(インターフェース)・`GETYPE`(ジェネリクス型実体化)がその受け皿になる
- **ARTYPE・整数リテラル拡張への追随**(commit `22c701f`。`ARTYPE`追加は`ac108e9`): amivmが名前付き固定長配列型`ARTYPE typename1 type1 imm`(`type typename1 [imm]type1`。`imm`はコンパイル時定数リテラルのみで識別子不可)と、整数リテラルの16進(`0x1A`)・8進(`0o17`)・2進(`0b101`)・桁区切り`_`(`1_000_000`)対応を追加した。調査の結果、**どちらも`internal/codegen`の変更は不要と判断した**。`ARTYPE`はGoの`[N]T`型と全く同じ「サイズはコンパイル時定数」制約を持ち、Seedが最初から`SLTYPE`+`SLMAKE`(Goスライス)を選んだ理由(`Int[size]`のようにサイズが実行時変数になり得る。上記「配列はGoスライスにコンパイル」参照)がそのまま`ARTYPE`にも当てはまるため、採用しない。整数リテラル拡張は後方互換な追加(既存の素の10進表記はそのまま通る)で、Seedのコード生成は元々`%d`相当のGo標準フォーマットで10進のみを出力しており、この拡張を使う理由も無い。実機の`amivm`(`go install`済み)で`examples/`配下の全`.seed`ファイルを`seed build`→実行まで無修正で確認済み。ドキュメント側は本ファイルの命令一覧・節番号参照(`FUNCM`4.22→4.23節、`INTYPE`4.23→4.24節。`ARTYPE`挿入によるamivm側の全面繰り下げ)を追随させた

以降、新しい設計判断が生じた場合もこの節(または実装コード側のコメント)に確定内容を残し、仮説段階のまま放置しないこと。

## リポジトリ構成

Step1〜8で実装済みの実際の構成。

```
seed/
  seed_spec.md         Seed言語仕様(唯一の正確な仕様)
  CLAUDE.md            本ファイル
  seed_implementation_notes.md  AMIVM上への実装知見メモ(次にAMIVM上で別言語を作る人/AI向け)
  README.md / README_ja.md  導入ドキュメント
  LICENSE               MIT
  go.mod               module github.com/amisonnet8/seed
  Makefile             build/install/test/test-examples/fmt/vet/tidy/clean タスク
  .github/workflows/test.yml  CI: push/PR時にgofmt/go vet/go test/make test-examplesを実行
  cmd/seed/
    main.go             CLIエントリポイント(build/run/emit-ir/emit-go/help のディスパッチ)
    build.go             compileToIR → compileToGo → compileToBinary の3段パイプライン
  internal/lexer/       字句解析
  internal/parser/      構文解析 → AST
  internal/ast/         AST定義
  internal/sema/        意味検査(型チェック・スコープ解決。「amivm側は検証しない」ため必須)
  internal/codegen/     AST → AMIVM-IR生成(codegen.go/array.go/func.go/builtins.go/stmt.go/expr.go/scope.go)
  seedrt/                Seedランタイム(open/read/write/close等、CALLで呼ばれるGo実装)。
                          生成されたGoコード(ユーザー側の別モジュール)からimportされるため、
                          internal/配下に置けない(Goのinternal可視性ルールのため公開パッケージにする)。
                          自身の.goファイルをgo:embedで埋め込み(embed.go)、seedビルド時にスクラッチ
                          モジュールへコピーする(上記「確定した設計判断」参照)
  examples/              サンプルSeedプログラム(`.seed`。実装した構文ごとに追加)
```

## 開発の進め方

1. `seed_spec.md`を正として実装する。仕様に曖昧な点や矛盾を見つけたら、まず仕様側を疑い、確定させてからコードを直す
2. AMIVM-IRを生成する処理を書いたら、実際に`amivm`(`PATH`にインストール済みのもの)にかけて`go build`まで通し、動作確認する。IRの構文・カテゴリ違反はamivmのパース/型チェックで初めて顕在化するため、「ロジック上正しそうに見える」だけで済ませない
3. Seedの意味検査(型チェック等)は、amivmに渡す前にSeed側で完了させる。amivmの`go/types`エラーをユーザー向けエラーとしてそのまま出さない
4. 新しい構文・ビルトイン関数を実装したら、対応するサンプルSeedプログラムを`examples/`に追加し、生成されたIR・Goコード・実行結果まで確認する
5. amivm本体の仕様が更新された場合(`amivm/amivm_spec.md`または最新リポジトリを再確認)、本ファイルの「AMIVM-IRの書き方」節が古くなっていないか照合し、必要なら更新する
6. IRの生成方式を大きく書き換えた・「IR的には正しそうなのにgo buildで初めて発覚した」類のバグを踏んだなど、次にAMIVM上で別言語を実装する人/AIへ申し送るべき知見が生まれたら、`seed_implementation_notes.md`にも反映する
7. `.github/workflows/test.yml`によりpush/PR時に`gofmt`・`go vet`・`go test`・`make test-examples`(`examples/`配下を`amivm`→`go build`→実行まで通す統合テスト)がGitHub Actionsで自動実行される。ローカルでも`make test-examples`(`amivm`が`PATH`にある前提)を実行してからpushすること(CIで初めて失敗に気づくのは避ける)

## AIによる開発支援時の注意点

- 作業内容のsummary(要約)や、ユーザーへの質問は日本語で行う
