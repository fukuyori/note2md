# Embed Notes

- `https://note.com/fukuy/n/n0605e9bf341d` に `embedded-service="external-article"` の `figure` がある。
- 現在の実装は `figure` 自体を特別扱いしていない。最初の `<img>` だけを別処理し、それ以外のタグは通常の HTML -> Markdown 変換に流している。
- 該当の Amazon 埋め込みは `<img>` ではなく `style="background-image: url(...)"` を使っているため、画像保存や Markdown 画像化はされない。
- `<a ...>(...)</a>` は共通ルールで `$2 ($1)` に変換しているため、カード内の 2 つのリンクがテキスト + URL、URL 単体として残る。
- すぐに専用対応は入れず、他の埋め込みパターンの出力状況を調べてから方針を決める。
- 調査したい点:
  - `external-article` 以外にどんな `embedded-service` があるか
  - それぞれで現在の Markdown 出力がどう崩れるか
  - `figure` 単位で専用変換するべきか、`a` / `img` / `background-image` の汎用処理で吸収できるか
