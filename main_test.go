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
