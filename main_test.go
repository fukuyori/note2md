package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeFileStemPortableSafety(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "portable invalid punctuation becomes fullwidth",
			input: `a<b>c:d"e/f\g|h?i*j`,
			want:  `a＜b＞c：d＂e／f＼g｜h？i＊j`,
		},
		{
			name:  "trailing dot and space become portable variants",
			input: "title. ",
			want:  "title．",
		},
		{
			name:  "windows reserved names are avoided",
			input: "CON",
			want:  "CON-file",
		},
		{
			name:  "whitespace collapses",
			input: "a\t \n  b",
			want:  "a b",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sanitizeFileStem(tt.input)
			if got != tt.want {
				t.Fatalf("sanitizeFileStem(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTrimFileStemToLengthPreservesRuneBoundaries(t *testing.T) {
	t.Parallel()

	input := "あいうえお"
	got := trimFileStemToLength(input, 3)
	want := "あいう"
	if got != want {
		t.Fatalf("trimFileStemToLength(%q, 3) = %q, want %q", input, got, want)
	}
}

func TestDefaultOutputPathTrimsLongTitle(t *testing.T) {
	tempDir := t.TempDir()

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(previousWD); chdirErr != nil {
			t.Fatalf("restore working directory: %v", chdirErr)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	title := strings.Repeat("あ", maxFileStemRunes+20)
	got := defaultOutputPath("https://note.com/fukuy/n/example", title)
	want := strings.Repeat("あ", maxFileStemRunes) + ".md"
	if got != want {
		t.Fatalf("defaultOutputPath() = %q, want %q", got, want)
	}
}

func TestMakeUniqueFilePathAddsSuffixOnCollision(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	firstPath := filepath.Join(tempDir, "image.png")
	if err := os.WriteFile(firstPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write first file: %v", err)
	}

	got := makeUniqueFilePath(tempDir, "image.png")
	want := filepath.Join(tempDir, "image-2.png")
	if got != want {
		t.Fatalf("makeUniqueFilePath returned %q, want %q", got, want)
	}
}

func TestParseArgsVersion(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"--version", "-v"} {
		_, err := parseArgs([]string{arg})
		if err != errShowVersion {
			t.Fatalf("parseArgs(%s) error = %v, want %v", arg, err, errShowVersion)
		}
	}
}

func TestPrintUsageIncludesHelpAndVersion(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printUsage(&buf)
	got := buf.String()

	for _, want := range []string{"-h, --help", "-v, --version", "Usage: note2md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("printUsage() missing %q in %q", want, got)
		}
	}
}

func TestPrintVersion(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printVersion(&buf)

	got := buf.String()
	want := "note2md " + version + "\n"
	if got != want {
		t.Fatalf("printVersion() = %q, want %q", got, want)
	}
}

func TestQiitaMarkdownURLAppendsDotMD(t *testing.T) {
	t.Parallel()

	articleURL, err := url.Parse("https://qiita.com/spumoni/items/c2159cff7436b1f9a17c?foo=bar")
	if err != nil {
		t.Fatalf("parse article URL: %v", err)
	}

	got := qiitaMarkdownURL(articleURL)
	want := "https://qiita.com/spumoni/items/c2159cff7436b1f9a17c.md?foo=bar"
	if got.String() != want {
		t.Fatalf("qiitaMarkdownURL() = %q, want %q", got.String(), want)
	}
}

func TestConvertQiitaURLDownloadsMarkdownImages(t *testing.T) {
	t.Parallel()

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/items/example":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head><title>Ignored | Qiita</title></head><body>Last updated at 2026-03-30 Posted at 2026-03-30</body></html>`))
		case "/items/example.md":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("---\ntitle: Sample Title\ntags: Go, Qiita\nauthor: spumoni\nslide: false\n---\n本文です。\n\n![recording_0000.gif](" + serverURL + "/images/recording_0000.gif)\n"))
		case "/images/recording_0000.gif":
			w.Header().Set("Content-Type", "image/gif")
			_, _ = w.Write([]byte("GIF89a"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	articleURL, err := url.Parse(server.URL + "/items/example")
	if err != nil {
		t.Fatalf("parse article URL: %v", err)
	}

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "article.md")
	client := &http.Client{Timeout: 5 * time.Second}

	got, err := convertQiitaURL(articleURL, client, outputPath, "images", false)
	if err != nil {
		t.Fatalf("convertQiitaURL() error = %v", err)
	}

	if got.Title != "Sample Title" {
		t.Fatalf("convertQiitaURL() title = %q, want %q", got.Title, "Sample Title")
	}

	for _, want := range []string{
		"Source: " + server.URL + "/items/example",
		"# Sample Title",
		"Last updated at 2026-03-30",
		"Posted at 2026-03-30",
		"---\n本文です。",
		"![recording_0000.gif](images/recording_0000.gif)",
	} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("convertQiitaURL() markdown missing %q in %q", want, got.Markdown)
		}
	}

	if !strings.Contains(got.Markdown, "![recording_0000.gif](images/recording_0000.gif)") {
		t.Fatalf("convertQiitaURL() markdown missing rewritten image path: %q", got.Markdown)
	}

	for _, unwanted := range []string{"author: spumoni", "tags: Go, Qiita", "slide: false"} {
		if strings.Contains(got.Markdown, unwanted) {
			t.Fatalf("convertQiitaURL() markdown should not contain %q in %q", unwanted, got.Markdown)
		}
	}

	imagePath := filepath.Join(outputDir, "images", "recording_0000.gif")
	imageBody, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatalf("read downloaded image: %v", err)
	}
	if string(imageBody) != "GIF89a" {
		t.Fatalf("downloaded image body = %q, want %q", string(imageBody), "GIF89a")
	}
}

func TestConvertQiitaURLKeepsOriginalImagesWithNoImages(t *testing.T) {
	t.Parallel()

	var serverURL string
	imageRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/items/example":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body>Last updated at 2026-03-30 Posted at 2026-03-30</body></html>`))
		case "/items/example.md":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("---\ntitle: Sample Title\n---\n![recording_0000.gif](" + serverURL + "/images/recording_0000.gif)\n"))
		case "/images/recording_0000.gif":
			imageRequests++
			w.Header().Set("Content-Type", "image/gif")
			_, _ = w.Write([]byte("GIF89a"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	articleURL, err := url.Parse(server.URL + "/items/example")
	if err != nil {
		t.Fatalf("parse article URL: %v", err)
	}

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "article.md")
	client := &http.Client{Timeout: 5 * time.Second}

	got, err := convertQiitaURL(articleURL, client, outputPath, "images", true)
	if err != nil {
		t.Fatalf("convertQiitaURL() error = %v", err)
	}

	wantImage := "![recording_0000.gif](" + server.URL + "/images/recording_0000.gif)"
	if !strings.Contains(got.Markdown, wantImage) {
		t.Fatalf("convertQiitaURL() markdown missing original image URL: %q", got.Markdown)
	}

	if imageRequests != 0 {
		t.Fatalf("image requests = %d, want 0", imageRequests)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "images")); !os.IsNotExist(err) {
		t.Fatalf("images directory should not exist when no-images is enabled, stat err = %v", err)
	}
}

func TestNormalizeQiitaMarkdownRemovesHeadingSelfLinks(t *testing.T) {
	t.Parallel()

	input := "## (#%E3%82%AF%E3%83%A9%E3%82%A4%E3%82%A2%E3%83%B3%E3%83%88)クライアント・サーバー型アーキテクチャ\n"
	got := normalizeQiitaMarkdown(input, "")
	want := "## クライアント・サーバー型アーキテクチャ"
	if got != want {
		t.Fatalf("normalizeQiitaMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownRewritesRubyMarkup(t *testing.T) {
	t.Parallel()

	input := "<p><ruby>御代<rt>おほむとき</rt></ruby></p>"
	got := htmlToMarkdown(input)
	want := "｜御代《おほむとき》"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownRewritesRubyMarkupWithHalfWidthLeaderWhenPresent(t *testing.T) {
	t.Parallel()

	input := "<p>|既存《きそん》の表記です。</p><p><ruby>御代<rt>おほむとき</rt></ruby></p>"
	got := htmlToMarkdown(input)
	want := "|既存《きそん》の表記です。\n\n|御代《おほむとき》"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsStrongAndStrike(t *testing.T) {
	t.Parallel()

	input := "<p>重要な<strong>箇所</strong>と<s>古い表現</s>です。</p>"
	got := htmlToMarkdown(input)
	want := "重要な**箇所**と~~古い表現~~です。"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsEmphasisAndInlineCode(t *testing.T) {
	t.Parallel()

	input := "<p><em>強調</em>と<code>fmt.Println(&quot;note&quot;)</code>です。</p>"
	got := htmlToMarkdown(input)
	want := "*強調*と`fmt.Println(\"note\")`です。"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsInlineLinksToMarkdownLinks(t *testing.T) {
	t.Parallel()

	input := `<p><a href="https://example.com/page">参考ページ</a>を参照してください。</p>`
	got := htmlToMarkdown(input)
	want := "[参考ページ](https://example.com/page)を参照してください。"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownKeepsEmptyAnchorsCompatibleForAuthorRewrite(t *testing.T) {
	t.Parallel()

	input := `<p><a href="/akihamitsuki"></a></p>`
	got := htmlToMarkdown(input)
	want := "(/akihamitsuki)"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsBlockquote(t *testing.T) {
	t.Parallel()

	input := "<blockquote><p>引用文です。</p><p>2行目です。</p></blockquote>"
	got := htmlToMarkdown(input)
	want := "> 引用文です。\n>\n> 2行目です。"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsOrderedList(t *testing.T) {
	t.Parallel()

	input := "<ol><li>最初</li><li>次</li><li>最後</li></ol>"
	got := htmlToMarkdown(input)
	want := "1. 最初\n2. 次\n3. 最後"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsNestedUnorderedList(t *testing.T) {
	t.Parallel()

	input := "<ul><li><p>イヌ科</p><ul><li><p>柴犬</p></li><li><p>秋田犬</p></li></ul></li><li><p>ネコ科</p><ul><li><p>三毛猫</p></li></ul></li></ul>"
	got := htmlToMarkdown(input)
	want := "- イヌ科\n  - 柴犬\n  - 秋田犬\n- ネコ科\n  - 三毛猫"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsNestedOrderedList(t *testing.T) {
	t.Parallel()

	input := "<ol><li><p>手順</p><ol><li><p>準備</p></li><li><p>実行</p></li></ol></li><li><p>完了</p></li></ol>"
	got := htmlToMarkdown(input)
	want := "1. 手順\n  1. 準備\n  2. 実行\n2. 完了"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsCodeBlock(t *testing.T) {
	t.Parallel()

	input := "<pre><code>const name = &quot;note&quot;;\nconsole.log(name);</code></pre>"
	got := htmlToMarkdown(input)
	want := "```\nconst name = \"note\";\nconsole.log(name);\n```"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownUsesLongerFenceWhenCodeContainsTripleBackticks(t *testing.T) {
	t.Parallel()

	input := "<pre><code>```\nconst name = &quot;note&quot;;\n```</code></pre>"
	got := htmlToMarkdown(input)
	want := "````\n```\nconst name = \"note\";\n```\n````"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsExternalArticleFigure(t *testing.T) {
	t.Parallel()

	input := `<figure embedded-service="external-article" data-src="https://example.com/article"><div class="external-article-widget"><a href="https://example.com/article"><strong class="external-article-widget-title">記事タイトル</strong><em class="external-article-widget-description">概要文です。</em><em class="external-article-widget-url">example.com</em></a></div></figure>`
	got := htmlToMarkdown(input)
	want := "[記事タイトル](https://example.com/article)\n\n概要文です。\n\nexample.com"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsFigureWithQuoteAndCaption(t *testing.T) {
	t.Parallel()

	input := `<figure><blockquote><p>「## ＋ 半角スペース」で大見出し<br>「### ＋ 半角スペース」で小見出し</p></blockquote><figcaption><a href="https://www.help-note.com/example">Markdownショートカット - noteヘルプセンター</a></figcaption></figure>`
	got := htmlToMarkdown(input)
	want := "> 「## ＋ 半角スペース」で大見出し\n> 「### ＋ 半角スペース」で小見出し\n\n[Markdownショートカット - noteヘルプセンター](https://www.help-note.com/example)"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsEmbeddedNoteFigureToStandaloneURL(t *testing.T) {
	t.Parallel()

	input := `<p>前文です。</p><figure embedded-service="note" data-src="https://note.com/example/n/embedded"><div><iframe data-src="https://note.com/embed/notes/embedded"></iframe><a href="https://note.com/example/n/embedded" target="_blank" rel="noopener"></a></div></figure><p>後文です。</p>`
	got := htmlToMarkdown(input)
	want := "前文です。\n\nhttps://note.com/example/n/embedded\n\n後文です。"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestHTMLToMarkdownConvertsListLinksToMarkdownLinks(t *testing.T) {
	t.Parallel()

	input := `<ul><li><p><a href="https://www.help-note.com/example">Markdownショートカット - noteヘルプセンター</a></p></li></ul>`
	got := htmlToMarkdown(input)
	want := "- [Markdownショートカット - noteヘルプセンター](https://www.help-note.com/example)"
	if got != want {
		t.Fatalf("htmlToMarkdown() = %q, want %q", got, want)
	}
}

func TestEnrichEmbeddedNoteFiguresAddsFetchedCaption(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/embedded":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head><meta property="og:title" content="埋め込み先タイトル｜著者名"></head><body></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	input := `<figure embedded-service="note" data-src="` + server.URL + `/embedded"><div><iframe data-src="` + server.URL + `/embed"></iframe></div></figure>`

	got, err := enrichEmbeddedNoteFigures(input, client)
	if err != nil {
		t.Fatalf("enrichEmbeddedNoteFigures() error = %v", err)
	}

	want := `<figcaption><a href="` + server.URL + `/embedded" target="_blank" rel="noopener nofollow">埋め込み先タイトル</a></figcaption>`
	if !strings.Contains(got, want) {
		t.Fatalf("enrichEmbeddedNoteFigures() = %q, want substring %q", got, want)
	}
}

func TestNormalizeNoteMarkdownKeepsWrappedParagraphs(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"いづれの｜御代《おほむとき》にか、",
		"｜女御《にょうご》・｜更衣《かうい》あまたさぶらひ給ひける中に、",
		"いとやむごとなき｜際《きは》にはあらぬが、",
		"すぐれて時めき給ふありけり。",
		"",
		"もとより、",
		"われこそはと思ひ上がり給へる御方がた、",
		"｜めざましき《めざわり》ものに見て、",
		"｜貶《おとし》め、",
		"｜嫉《そね》み給ふ。",
	}, "\n")

	got := normalizeNoteMarkdown(input)
	want := strings.Join([]string{
		"いづれの｜御代《おほむとき》にか、",
		"｜女御《にょうご》・｜更衣《かうい》あまたさぶらひ給ひける中に、",
		"いとやむごとなき｜際《きは》にはあらぬが、",
		"すぐれて時めき給ふありけり。",
		"",
		"もとより、",
		"われこそはと思ひ上がり給へる御方がた、",
		"｜めざましき《めざわり》ものに見て、",
		"｜貶《おとし》め、",
		"｜嫉《そね》み給ふ。",
	}, "\n")
	if got != want {
		t.Fatalf("normalizeNoteMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizeNoteMarkdownKeepsWrappedParagraphsWithHalfWidthRubyLeader(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"いづれの|御代《おほむとき》にか、",
		"|女御《にょうご》・|更衣《かうい》あまたさぶらひ給ひける中に、",
		"いとやむごとなき|際《きは》にはあらぬが、",
		"すぐれて時めき給ふありけり。",
	}, "\n")

	got := normalizeNoteMarkdown(input)
	want := input
	if got != want {
		t.Fatalf("normalizeNoteMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizeNoteMarkdownKeepsTwoLineBulletItem(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"- 原文の内容・構成・意味を損なわないことを最優先とし、",
		"省略や恣意的な改変は行っていない。",
	}, "\n")

	got := normalizeNoteMarkdown(input)
	want := input
	if got != want {
		t.Fatalf("normalizeNoteMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizeNoteMarkdownMergesAdjacentListBlocks(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"- ネコ科",
		"  - 三毛猫",
		"",
		"- ヒト科",
	}, "\n")

	got := normalizeNoteMarkdown(input)
	want := strings.Join([]string{
		"- ネコ科",
		"  - 三毛猫",
		"- ヒト科",
	}, "\n")
	if got != want {
		t.Fatalf("normalizeNoteMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizeNoteMarkdownRewritesInlineAuthorMetadataBlock(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"# noteで使えるマークダウン記法まとめ",
		"",
		"**",
		"190",
		"[](/akihamitsuki) [宮野秋彦](/akihamitsuki) 2023年6月18日 11:31     noteエディタとマークダウンの関係について調べたので、そのまとめです。",
		"",
		"別エディタでnoteの下書きをしたり、ChatGPTで出力した要素をnoteに貼り付ける場合などには、どのような対応関係なのかを知っておくと、ちょっとだけ便利です。",
	}, "\n")

	got := normalizeNoteMarkdown(input)
	want := strings.Join([]string{
		"# noteで使えるマークダウン記法まとめ",
		"",
		"宮野秋彦",
		"2023年6月18日 11:31",
		"",
		"---",
		"",
		"noteエディタとマークダウンの関係について調べたので、そのまとめです。",
		"",
		"別エディタでnoteの下書きをしたり、ChatGPTで出力した要素をnoteに貼り付ける場合などには、どのような対応関係なのかを知っておくと、ちょっとだけ便利です。",
	}, "\n")
	if got != want {
		t.Fatalf("normalizeNoteMarkdown() = %q, want %q", got, want)
	}
}

func TestNoteFormattingMatchesExpectedExcerpt(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"<p>いづれの<ruby>御代<rt>おほむとき</rt></ruby>にか、<br><ruby>女御<rt>にょうご</rt></ruby>・<ruby>更衣<rt>かうい</rt></ruby>あまたさぶらひ給ひける中に、<br>いとやむごとなき<ruby>際<rt>きは</rt></ruby>にはあらぬが、<br>すぐれて時めき給ふありけり。</p>",
		"<p>もとより、<br>われこそはと思ひ上がり給へる御方がた、<br><ruby>めざましき<rt>めざわり</rt></ruby>ものに見て、<br><ruby>貶<rt>おとし</rt></ruby>め、<br><ruby>嫉<rt>そね</rt></ruby>み給ふ。</p>",
		"<p>同じほどの更衣たちも、<br>なほ心やすからず。<br>まして、<br>それより<ruby>下臈<rt>げらふ</rt></ruby>の更衣たちは、<br>いとど耐へがたく思ひけり。</p>",
		"<p>朝夕の宮仕へにつけても、<br>人の心をのみ動かし、<br>恨みを負ふ積もりにやありけむ、<br>いと<ruby>篤<rt>あつ</rt></ruby>しくなりゆき、<br>もの心ぼそげに、<br>里がちなるを、<br>いよいよあかず、<br>あはれなるものに思ほして、<br>人のそしりをもえ<ruby>憚<rt>はばか</rt></ruby>らせ給はず、<br>世のためしにもなりぬべき御もてなしなり。</p>",
		"<p><ruby>上達部<rt>かむだちめ</rt></ruby>、<ruby>上人<rt>うへびと</rt></ruby>なども、<br>あひなく目をそばめつつ、<br>いとまばゆき人の御おぼえなり。</p>",
		"<p><ruby>唐土<rt>もろこし</rt></ruby>にも、<br>かかる事の起こりにこそ、<br>世も乱れ、<br>あしかりけれと、<br>やうやう<ruby>天<rt>あめ</rt></ruby>の下にも、<br><ruby>あぢきなう<rt>どうしようもなく</rt></ruby>人のもてなやみ草となりて、<br>楊貴妃の<ruby>例<rt>ためし</rt></ruby>も引き出でつべくなりゆくに、<br>いとはしたなき事多かれど、<br>かたじけなき御心ばへの、<br>たぐひなきを頼みにて、<br>まじらひ給ふ。</p>",
		"<p>父の大納言は亡くなりて、<br>母北の方なむ、<br>いにしへの人のよしあるにて、<br>親うち具し、<br>さしあたりて、<br>世のおぼえはなやかなる御方がたにも、<br>いたう劣らず、<br>なにごとの儀式をも、<br>もてなし給ひけれど、<br>とりたてて、<br>はかばかしき<ruby>後見<rt>うしろみ</rt></ruby>しなければ、<br>事ある時は、<br>なほ<ruby>拠<rt>より</rt></ruby>り所なく、<br>心ぼそげなり。</p>",
	}, "")

	got := normalizeNoteMarkdown(htmlToMarkdown(input))
	want := strings.Join([]string{
		"いづれの｜御代《おほむとき》にか、",
		"｜女御《にょうご》・｜更衣《かうい》あまたさぶらひ給ひける中に、",
		"いとやむごとなき｜際《きは》にはあらぬが、",
		"すぐれて時めき給ふありけり。",
		"",
		"もとより、",
		"われこそはと思ひ上がり給へる御方がた、",
		"｜めざましき《めざわり》ものに見て、",
		"｜貶《おとし》め、",
		"｜嫉《そね》み給ふ。",
		"",
		"同じほどの更衣たちも、",
		"なほ心やすからず。",
		"まして、",
		"それより｜下臈《げらふ》の更衣たちは、",
		"いとど耐へがたく思ひけり。",
		"",
		"朝夕の宮仕へにつけても、",
		"人の心をのみ動かし、",
		"恨みを負ふ積もりにやありけむ、",
		"いと｜篤《あつ》しくなりゆき、",
		"もの心ぼそげに、",
		"里がちなるを、",
		"いよいよあかず、",
		"あはれなるものに思ほして、",
		"人のそしりをもえ｜憚《はばか》らせ給はず、",
		"世のためしにもなりぬべき御もてなしなり。",
		"",
		"｜上達部《かむだちめ》、｜上人《うへびと》なども、",
		"あひなく目をそばめつつ、",
		"いとまばゆき人の御おぼえなり。",
		"",
		"｜唐土《もろこし》にも、",
		"かかる事の起こりにこそ、",
		"世も乱れ、",
		"あしかりけれと、",
		"やうやう｜天《あめ》の下にも、",
		"｜あぢきなう《どうしようもなく》人のもてなやみ草となりて、",
		"楊貴妃の｜例《ためし》も引き出でつべくなりゆくに、",
		"いとはしたなき事多かれど、",
		"かたじけなき御心ばへの、",
		"たぐひなきを頼みにて、",
		"まじらひ給ふ。",
		"",
		"父の大納言は亡くなりて、",
		"母北の方なむ、",
		"いにしへの人のよしあるにて、",
		"親うち具し、",
		"さしあたりて、",
		"世のおぼえはなやかなる御方がたにも、",
		"いたう劣らず、",
		"なにごとの儀式をも、",
		"もてなし給ひけれど、",
		"とりたてて、",
		"はかばかしき｜後見《うしろみ》しなければ、",
		"事ある時は、",
		"なほ｜拠《より》り所なく、",
		"心ぼそげなり。",
	}, "\n")
	if got != want {
		t.Fatalf("normalizeNoteMarkdown(htmlToMarkdown(input)) = %q, want %q", got, want)
	}
}

func TestReplaceMarkdownImagesSkipsCodeFences(t *testing.T) {
	t.Parallel()

	imageRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		imageRequests++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer server.Close()

	pageURL, err := url.Parse("https://qiita.com/spumoni/items/example")
	if err != nil {
		t.Fatalf("parse page URL: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	markdown := "```markdown\n![example](" + server.URL + "/image.png)\n```\n"

	got, err := replaceMarkdownImages(markdown, pageURL, client, filepath.Join(t.TempDir(), "article.md"), "images", false)
	if err != nil {
		t.Fatalf("replaceMarkdownImages() error = %v", err)
	}

	if got != markdown {
		t.Fatalf("replaceMarkdownImages() = %q, want %q", got, markdown)
	}

	if imageRequests != 0 {
		t.Fatalf("image requests = %d, want 0", imageRequests)
	}
}

func TestReplaceFirstImageWithMarkdownSkipsDataImageAndUsesNextImage(t *testing.T) {
	t.Parallel()

	imageRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		imageRequests++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer server.Close()

	pageURL, err := url.Parse("https://note.com/fukuy/n/example")
	if err != nil {
		t.Fatalf("parse page URL: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	outputPath := filepath.Join(t.TempDir(), "article.md")
	fragment := `<p><img src="data:image/svg+xml,%3Csvg%3E%3C/svg%3E" alt="placeholder"><img src="` + server.URL + `/hero.png" alt="hero"></p>`

	got, err := replaceFirstImageWithMarkdown(fragment, pageURL, client, outputPath, "images", false)
	if err != nil {
		t.Fatalf("replaceFirstImageWithMarkdown() error = %v", err)
	}

	if imageRequests != 1 {
		t.Fatalf("image requests = %d, want 1", imageRequests)
	}

	if !strings.Contains(got, `src="data:image/svg+xml,%3Csvg%3E%3C/svg%3E"`) {
		t.Fatalf("replaceFirstImageWithMarkdown() should keep placeholder img in fragment, got %q", got)
	}

	if !strings.Contains(got, "![hero](images/hero.png)") {
		t.Fatalf("replaceFirstImageWithMarkdown() missing rewritten image markdown in %q", got)
	}
}

func TestReplaceMarkdownImagesKeepsDataImagesWithoutDownloading(t *testing.T) {
	t.Parallel()

	pageURL, err := url.Parse("https://qiita.com/spumoni/items/example")
	if err != nil {
		t.Fatalf("parse page URL: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	markdown := "![placeholder](data:image/svg+xml,%3Csvg%3E%3C/svg%3E)\n"

	got, err := replaceMarkdownImages(markdown, pageURL, client, filepath.Join(t.TempDir(), "article.md"), "images", false)
	if err != nil {
		t.Fatalf("replaceMarkdownImages() error = %v", err)
	}

	if got != markdown {
		t.Fatalf("replaceMarkdownImages() = %q, want %q", got, markdown)
	}
}
