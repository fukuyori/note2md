# note2md

`note.com` と `Qiita` の記事 URL を Markdown に変換する小さな CLI ツールです。

Version: `0.9.3`

主な機能:

- `note.com` の記事本文を Markdown 化
- `Qiita` の記事本文を `.md` エンドポイントからそのまま取得
- 画像を取得して Markdown に埋め込み
- `--no-images` で画像取得を無効化し、元画像 URL をそのまま使用
- URL を1行ずつ書いたファイルを読み込んで連続処理
- 出力ファイル名は記事タイトルから自動生成

## Build

```powershell
go build ./...
```

この環境では既定の Go キャッシュが壊れている場合があるため、その場合はローカルキャッシュを指定します。

```powershell
$env:GOCACHE=(Join-Path (Get-Location) '.gocache')
go build ./...
```

## Usage

単体 URL を Markdown に変換:

```powershell
.\note2md.exe https://note.com/example/n/abcdef123456
```

```powershell
.\note2md.exe https://qiita.com/spumoni/items/c2159cff7436b1f9a17c
```

出力ファイルを明示:

```powershell
.\note2md.exe -o article.md https://note.com/example/n/abcdef123456
```

Markdown を標準出力へ出す:

```powershell
.\note2md.exe -o - https://note.com/example/n/abcdef123456
```

画像を保存せず、画像 URL をそのまま使う:

```powershell
.\note2md.exe --no-images https://note.com/example/n/abcdef123456
```

```powershell
.\note2md.exe --no-images https://qiita.com/spumoni/items/c2159cff7436b1f9a17c
```

URL リストを連続処理:

```powershell
.\note2md.exe --input-file urls.txt
```

## Options

- `-h`, `--help`
  使い方を表示します。
- `-v`, `--version`
  バージョンを表示します。
- `-o`, `--output`
  出力先ファイルを指定します。`-` を指定すると標準出力に出します。
- `-f`, `--input-file`
  URL を1行ずつ書いたファイルを読み込みます。空行と `#` で始まる行は無視します。
- `--images-dir`
  保存した画像の出力先ディレクトリを指定します。既定値は `images` です。
- `--no-images`
  画像を保存しません。Markdown では元記事中の画像 URL をそのまま使います。
- `--timeout`
  タイムアウト秒数を指定します。既定値は `30` 秒です。

## Output Rules

- `-o` を指定しない場合、記事タイトルから Markdown ファイル名を作ります。
- ファイル名は、Windows / macOS / Linux のいずれでも扱いやすい安全側の規則で生成します。
- ファイル名に使えない文字は全角に変換します。
- 長すぎるタイトルは安全な長さに切り詰めます。
- 同名ファイルがすでにある場合は `-2`, `-3` のような suffix を付けます。
- 標準出力時は自動で画像非取得になります。

## Current Behavior

- 本文は最初の `<div data-note-id=...>` ブロックを優先して抽出します。
- 最初の `<img>` だけを画像として特別処理します。
- `Qiita` は記事 URL の末尾に `.md` を付けて Markdown を取得します。
- `Qiita` の Markdown 内にある画像リンクはすべてローカル保存対象になります。
- `hr` は Markdown の `---` に変換します。
- 先頭の著者名と日時の並びは、note の記事表示に合わせて整形します。

## Known Limitations

- 埋め込みカードや `figure` は専用対応していません。
- `note.com` 側の画像特別処理は最初の1枚だけです。
- 記事の HTML 構造が大きく変わると抽出結果が崩れる可能性があります。

## Author

- GitHub: `fukuyori`
- Email: `self@spumoni.org`
- Repository: `https://github.com/fukuyori/note2md.git`
