package main

import (
	"bufio"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type cliOptions struct {
	URL       string
	InputFile string
	Output    string
	ImagesDir string
	NoImages  bool
	Timeout   time.Duration
}

type convertedNote struct {
	Title    string
	Markdown string
}

type qiitaFrontMatter struct {
	Title string
}

type qiitaArticleMeta struct {
	Title       string
	UpdatedDate string
	PostedDate  string
}

const maxFileStemRunes = 80
const version = "0.9.3"
const emptyArticleBodyMessage = "_No article body could be extracted from the page._"

var invalidFileNameRuneMap = map[rune]rune{
	'<':  '＜',
	'>':  '＞',
	':':  '：',
	'"':  '＂',
	'/':  '／',
	'\\': '＼',
	'|':  '｜',
	'?':  '？',
	'*':  '＊',
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseArgs(args)
	if err != nil {
		if errors.Is(err, errShowUsage) {
			printUsage(os.Stdout)
			return nil
		}
		if errors.Is(err, errShowVersion) {
			printVersion(os.Stdout)
			return nil
		}
		printUsage(os.Stderr)
		return err
	}

	if opts.InputFile != "" {
		return runBatch(opts)
	}

	return runSingle(opts)
}

var errShowUsage = errors.New("show usage")
var errShowVersion = errors.New("show version")

func parseArgs(args []string) (cliOptions, error) {
	opts := cliOptions{
		ImagesDir: "images",
		Timeout:   30 * time.Second,
	}

	for index := 0; index < len(args); index++ {
		arg := args[index]

		switch arg {
		case "-h", "--help":
			return cliOptions{}, errShowUsage
		case "-v", "--version":
			return cliOptions{}, errShowVersion
		case "-o", "--output":
			index++
			if index >= len(args) {
				return cliOptions{}, fmt.Errorf("%s requires a value", arg)
			}
			opts.Output = args[index]
		case "-f", "--input-file":
			index++
			if index >= len(args) {
				return cliOptions{}, fmt.Errorf("%s requires a value", arg)
			}
			opts.InputFile = args[index]
		case "--no-images":
			opts.NoImages = true
		case "--images-dir":
			index++
			if index >= len(args) {
				return cliOptions{}, fmt.Errorf("%s requires a value", arg)
			}
			opts.ImagesDir = args[index]
		case "--timeout":
			index++
			if index >= len(args) {
				return cliOptions{}, fmt.Errorf("%s requires a value", arg)
			}
			seconds, err := strconv.Atoi(args[index])
			if err != nil || seconds <= 0 {
				return cliOptions{}, fmt.Errorf("invalid --timeout value: %q", args[index])
			}
			opts.Timeout = time.Duration(seconds) * time.Second
		default:
			if strings.HasPrefix(arg, "-") {
				return cliOptions{}, fmt.Errorf("unknown option: %s", arg)
			}
			if opts.URL != "" {
				return cliOptions{}, fmt.Errorf("unexpected extra argument: %s", arg)
			}
			opts.URL = arg
		}
	}

	if opts.URL != "" && opts.InputFile != "" {
		return cliOptions{}, fmt.Errorf("use either an article URL or --input-file, not both")
	}

	if opts.InputFile != "" && opts.Output != "" {
		return cliOptions{}, fmt.Errorf("--output cannot be used with --input-file")
	}

	if opts.URL == "" && opts.InputFile == "" {
		return cliOptions{}, fmt.Errorf("article URL is required")
	}

	return opts, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: note2md [--timeout 30] [--images-dir images] [--no-images] [-o output.md] <article-url>")
	fmt.Fprintln(w, "   or: note2md [--timeout 30] [--images-dir images] [--no-images] --input-file urls.txt")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -h, --help        Show this help message")
	fmt.Fprintln(w, "  -v, --version     Show version")
	fmt.Fprintln(w, "  -o, --output      Write Markdown to a file, or '-' for stdout")
	fmt.Fprintln(w, "  -f, --input-file  Read article URLs from a file")
	fmt.Fprintln(w, "      --images-dir  Directory for downloaded images (default: images)")
	fmt.Fprintln(w, "      --no-images   Keep original image URLs instead of downloading")
	fmt.Fprintln(w, "      --timeout     Timeout in seconds (default: 30)")
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "note2md %s\n", version)
}

func runSingle(opts cliOptions) error {
	if opts.Output == "-" {
		opts.NoImages = true
		note, err := convertArticleURL(opts.URL, opts.Timeout, "", opts.ImagesDir, opts.NoImages)
		if err != nil {
			return err
		}
		_, err = os.Stdout.WriteString(note.Markdown)
		return err
	}

	if opts.Output == "" {
		note, err := convertArticleURL(opts.URL, opts.Timeout, "", opts.ImagesDir, opts.NoImages)
		if err != nil {
			return err
		}
		opts.Output = defaultOutputPath(opts.URL, note.Title)
		if err := os.WriteFile(opts.Output, []byte(note.Markdown), 0o644); err != nil {
			return err
		}
		fmt.Println(filepath.Clean(opts.Output))
		return nil
	}

	note, err := convertArticleURL(opts.URL, opts.Timeout, opts.Output, opts.ImagesDir, opts.NoImages)
	if err != nil {
		return err
	}

	if err := os.WriteFile(opts.Output, []byte(note.Markdown), 0o644); err != nil {
		return err
	}

	fmt.Println(filepath.Clean(opts.Output))
	return nil
}

func runBatch(opts cliOptions) error {
	urls, err := readURLFile(opts.InputFile)
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return fmt.Errorf("no article URLs found in %s", filepath.Clean(opts.InputFile))
	}

	for _, articleURL := range urls {
		itemOpts := opts
		itemOpts.URL = articleURL
		itemOpts.Output = ""
		if err := runSingle(itemOpts); err != nil {
			return fmt.Errorf("%s: %w", articleURL, err)
		}
	}

	return nil
}

func readURLFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return urls, nil
}

func convertArticleURL(articleURL string, timeout time.Duration, outputPath string, imagesDir string, noImages bool) (convertedNote, error) {
	parsedURL, err := url.Parse(articleURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return convertedNote{}, fmt.Errorf("invalid article URL: %q", articleURL)
	}

	client := &http.Client{Timeout: timeout}
	if isQiitaURL(parsedURL) {
		return convertQiitaURL(parsedURL, client, outputPath, imagesDir, noImages)
	}

	return convertHTMLArticleURL(parsedURL, client, outputPath, imagesDir, noImages)
}

func convertHTMLArticleURL(parsedURL *url.URL, client *http.Client, outputPath string, imagesDir string, noImages bool) (convertedNote, error) {
	document, err := fetchText(client, parsedURL)
	if err != nil {
		return convertedNote{}, err
	}

	title := normalizeDocumentTitle(extractTitle(document))
	content := extractNoteContent(document)
	if content == "" {
		content = extractArticleContent(document)
	}

	content, err = replaceFirstImageWithMarkdown(content, parsedURL, client, outputPath, imagesDir, noImages)
	if err != nil {
		return convertedNote{}, err
	}

	markdownBody := htmlToMarkdown(content)
	markdownBody = trimDuplicateHeading(markdownBody, title)
	markdownBody = normalizeNoteMarkdown(markdownBody)

	return buildConvertedArticle(parsedURL, title, markdownBody), nil
}

func convertQiitaURL(articleURL *url.URL, client *http.Client, outputPath string, imagesDir string, noImages bool) (convertedNote, error) {
	markdownURL := qiitaMarkdownURL(articleURL)
	markdownBody, err := fetchText(client, markdownURL)
	if err != nil {
		return convertedNote{}, err
	}

	frontMatter, body := parseQiitaFrontMatter(markdownBody)
	markdownBody, err = replaceMarkdownImages(markdownBody, articleURL, client, outputPath, imagesDir, noImages)
	if err != nil {
		return convertedNote{}, err
	}
	frontMatter, body = parseQiitaFrontMatter(markdownBody)

	meta, _ := fetchQiitaArticleMeta(articleURL, client)
	title := firstNonEmpty(frontMatter.Title, meta.Title, extractMarkdownTitle(body))
	markdownBody = normalizeQiitaMarkdown(body, title)
	return buildQiitaArticle(articleURL, title, meta, markdownBody), nil
}

func buildConvertedArticle(articleURL *url.URL, title string, markdownBody string) convertedNote {
	if title == "" {
		title = deriveSlug(articleURL)
	}
	if strings.TrimSpace(markdownBody) == "" {
		markdownBody = emptyArticleBodyMessage
	}

	var builder strings.Builder
	builder.WriteString("Source: ")
	builder.WriteString(articleURL.String())
	builder.WriteString("\n\n")
	builder.WriteString(markdownBody)
	if !strings.HasSuffix(markdownBody, "\n") {
		builder.WriteString("\n")
	}

	return convertedNote{
		Title:    title,
		Markdown: builder.String(),
	}
}

func fetchText(client *http.Client, targetURL *url.URL) (string, error) {
	response, err := client.Get(targetURL.String())
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", targetURL.Redacted(), err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("fetch %s: unexpected status %s", targetURL.Redacted(), response.Status)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", targetURL.Redacted(), err)
	}

	return string(body), nil
}

func fetchQiitaArticleMeta(articleURL *url.URL, client *http.Client) (qiitaArticleMeta, error) {
	document, err := fetchText(client, articleURL)
	if err != nil {
		return qiitaArticleMeta{}, err
	}

	return qiitaArticleMeta{
		Title:       normalizeDocumentTitle(extractTitle(document)),
		UpdatedDate: extractQiitaDate(document, "Last updated at"),
		PostedDate:  extractQiitaDate(document, "Posted at"),
	}, nil
}

func buildQiitaArticle(articleURL *url.URL, title string, meta qiitaArticleMeta, markdownBody string) convertedNote {
	title = strings.TrimSpace(title)
	if title == "" {
		title = deriveSlug(articleURL)
	}
	if strings.TrimSpace(markdownBody) == "" {
		markdownBody = emptyArticleBodyMessage
	}

	var builder strings.Builder
	builder.WriteString("Source: ")
	builder.WriteString(articleURL.String())
	builder.WriteString("\n\n")
	if title != "" {
		builder.WriteString("# ")
		builder.WriteString(title)
		builder.WriteString("\n\n")
	}
	if meta.UpdatedDate != "" {
		builder.WriteString("Last updated at ")
		builder.WriteString(meta.UpdatedDate)
		builder.WriteString("\n")
	}
	if meta.PostedDate != "" {
		builder.WriteString("Posted at ")
		builder.WriteString(meta.PostedDate)
		builder.WriteString("\n")
	}
	if meta.UpdatedDate != "" || meta.PostedDate != "" {
		builder.WriteString("\n")
	}
	builder.WriteString("---\n")
	if markdownBody != "" {
		builder.WriteString(markdownBody)
		if !strings.HasSuffix(markdownBody, "\n") {
			builder.WriteString("\n")
		}
	}

	return convertedNote{
		Title:    title,
		Markdown: builder.String(),
	}
}

func defaultOutputPath(noteURL string, title string) string {
	stem := ""
	if title != "" {
		stem = sanitizeFileStem(title)
	}

	if stem == "" {
		parsedURL, err := url.Parse(noteURL)
		if err != nil {
			stem = "article"
		} else {
			stem = deriveSlug(parsedURL)
		}
	}

	if stem == "" {
		stem = "article"
	}

	stem = trimFileStemToLength(stem, maxFileStemRunes)
	return makeUniqueOutputPath(stem, ".md")
}

func deriveSlug(parsedURL *url.URL) string {
	parts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	for index := len(parts) - 1; index >= 0; index-- {
		candidate := sanitizeFileStem(parts[index])
		if candidate != "" {
			return candidate
		}
	}
	return sanitizeFileStem(parsedURL.Hostname())
}

func sanitizeFileStem(value string) string {
	value = strings.TrimSpace(value)

	var builder strings.Builder
	for _, r := range value {
		switch {
		case r < 32:
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			builder.WriteRune(invalidFileNameRuneMap[r])
		case unicode.IsSpace(r):
			builder.WriteRune(' ')
		default:
			builder.WriteRune(r)
		}
	}

	sanitized := strings.TrimSpace(builder.String())
	sanitized = regexp.MustCompile(` {2,}`).ReplaceAllString(sanitized, " ")
	sanitized = replaceTrailingInvalidFilenameRunes(sanitized)
	if sanitized == "" {
		return ""
	}

	if isReservedWindowsName(sanitized) {
		sanitized += "-file"
	}

	return sanitized
}

func replaceTrailingInvalidFilenameRunes(value string) string {
	if value == "" {
		return value
	}

	runes := []rune(value)
	for index := len(runes) - 1; index >= 0; index-- {
		switch runes[index] {
		case '.':
			runes[index] = '．'
		case ' ':
			runes[index] = '　'
		default:
			return string(runes)
		}
	}

	return string(runes)
}

func makeUniqueOutputPath(stem string, extension string) string {
	if stem == "" {
		stem = "article"
	}

	candidate := stem + extension
	if !pathExists(candidate) {
		return candidate
	}

	for index := 2; ; index++ {
		suffix := fmt.Sprintf("-%d", index)
		candidate = stem + suffix + extension
		if !pathExists(candidate) {
			return candidate
		}
	}
}

func makeUniqueFilePath(directory string, fileName string) string {
	if fileName == "" {
		fileName = "file"
	}

	candidate := filepath.Join(directory, fileName)
	if !pathExists(candidate) {
		return candidate
	}

	extension := filepath.Ext(fileName)
	stem := strings.TrimSuffix(fileName, extension)
	if stem == "" {
		stem = "file"
	}

	for index := 2; ; index++ {
		suffix := fmt.Sprintf("-%d", index)
		candidate = filepath.Join(directory, stem+suffix+extension)
		if !pathExists(candidate) {
			return candidate
		}
	}
}

func trimFileStemToLength(stem string, limit int) string {
	if limit <= 0 {
		return "article"
	}

	runes := []rune(stem)
	if len(runes) <= limit {
		return stem
	}

	trimmed := strings.TrimSpace(string(runes[:limit]))
	trimmed = strings.Trim(trimmed, ". ")
	if trimmed == "" {
		return "article"
	}

	if isReservedWindowsName(trimmed) {
		return trimFileStemToLength(trimmed+"-file", limit)
	}

	return trimmed
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isReservedWindowsName(name string) bool {
	base := strings.TrimSpace(name)
	if base == "" {
		return false
	}

	base = strings.Trim(base, ". ")
	base = strings.ToUpper(base)
	switch base {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

func extractTitle(document string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?is)<meta[^>]+property=["']og:title["'][^>]+content=["'](.*?)["']`),
		regexp.MustCompile(`(?is)<meta[^>]+content=["'](.*?)["'][^>]+property=["']og:title["']`),
		regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`),
		regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(document)
		if len(matches) < 2 {
			continue
		}
		title := cleanInlineHTML(matches[1])
		if title != "" {
			return title
		}
	}

	return ""
}

func extractMarkdownTitle(markdownBody string) string {
	for _, line := range strings.Split(markdownBody, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		break
	}

	return ""
}

func parseQiitaFrontMatter(markdownBody string) (qiitaFrontMatter, string) {
	const delimiter = "---"

	normalized := strings.ReplaceAll(markdownBody, "\r\n", "\n")
	if !strings.HasPrefix(normalized, delimiter+"\n") {
		return qiitaFrontMatter{}, markdownBody
	}

	rest := normalized[len(delimiter)+1:]
	end := strings.Index(rest, "\n"+delimiter+"\n")
	if end < 0 {
		return qiitaFrontMatter{}, markdownBody
	}

	rawFrontMatter := rest[:end]
	body := rest[end+len("\n"+delimiter+"\n"):]
	return qiitaFrontMatter{
		Title: parseFrontMatterScalar(rawFrontMatter, "title"),
	}, body
}

func parseFrontMatterScalar(frontMatter string, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(frontMatter, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}

		value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		value = strings.Trim(value, `"'`)
		return value
	}

	return ""
}

func extractQiitaDate(document string, label string) string {
	pattern := regexp.MustCompile(regexp.QuoteMeta(label) + `\s*([0-9]{4}-[0-9]{2}-[0-9]{2})`)
	matches := pattern.FindStringSubmatch(document)
	if len(matches) < 2 {
		return ""
	}

	return matches[1]
}

func isQiitaURL(parsedURL *url.URL) bool {
	host := strings.ToLower(parsedURL.Hostname())
	return host == "qiita.com" || host == "www.qiita.com"
}

func qiitaMarkdownURL(articleURL *url.URL) *url.URL {
	markdownURL := *articleURL
	if !strings.HasSuffix(strings.ToLower(markdownURL.Path), ".md") {
		markdownURL.Path += ".md"
	}
	if markdownURL.RawPath != "" && !strings.HasSuffix(strings.ToLower(markdownURL.RawPath), ".md") {
		markdownURL.RawPath += ".md"
	}

	return &markdownURL
}

func extractArticleContent(document string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?is)<article[^>]*>(.*?)</article>`),
		regexp.MustCompile(`(?is)<main[^>]*>(.*?)</main>`),
		regexp.MustCompile(`(?is)<body[^>]*>(.*?)</body>`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(document)
		if len(matches) < 2 {
			continue
		}
		content := strings.TrimSpace(matches[1])
		if content != "" {
			return content
		}
	}

	return document
}

func extractNoteContent(document string) string {
	startPattern := regexp.MustCompile(`(?is)<div\b[^>]*\bdata-note-id(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+))?[^>]*>`)
	tokenPattern := regexp.MustCompile(`(?is)</?div\b[^>]*>`)

	start := startPattern.FindStringIndex(document)
	if start == nil {
		return ""
	}

	depth := 1
	offset := start[1]
	matches := tokenPattern.FindAllStringIndex(document[offset:], -1)
	for _, match := range matches {
		tokenStart := offset + match[0]
		tokenEnd := offset + match[1]
		token := document[tokenStart:tokenEnd]
		lowerToken := strings.ToLower(token)

		if strings.HasPrefix(lowerToken, "</div") {
			depth--
			if depth == 0 {
				return document[start[0]:tokenEnd]
			}
			continue
		}

		if !strings.HasSuffix(strings.TrimSpace(token), "/>") {
			depth++
		}
	}

	return document[start[0]:]
}

func replaceFirstImageWithMarkdown(fragment string, pageURL *url.URL, client *http.Client, outputPath string, imagesDir string, noImages bool) (string, error) {
	imagePattern := regexp.MustCompile(`(?is)<img\b[^>]*>`)
	location := imagePattern.FindStringIndex(fragment)
	if location == nil {
		return fragment, nil
	}

	tag := fragment[location[0]:location[1]]
	sourceURL := extractImageSource(tag)
	if sourceURL == "" {
		return fragment, nil
	}

	markdownPath, err := resolveImageMarkdownPath(sourceURL, pageURL, client, outputPath, imagesDir, noImages)
	if err != nil {
		return "", err
	}

	alt := extractImageAlt(tag)
	if alt == "" {
		alt = "image"
	}
	imageMarkdown := "\n\n![" + alt + "](" + markdownPath + ")\n\n"

	return fragment[:location[0]] + imageMarkdown + fragment[location[1]:], nil
}

func resolveImageMarkdownPath(rawURL string, pageURL *url.URL, client *http.Client, outputPath string, imagesDir string, noImages bool) (string, error) {
	resolvedURL, err := pageURL.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("resolve image URL %q: %w", rawURL, err)
	}

	if noImages {
		return resolvedURL.String(), nil
	}

	return downloadImage(resolvedURL, client, outputPath, imagesDir)
}

func replaceMarkdownImages(markdownBody string, pageURL *url.URL, client *http.Client, outputPath string, imagesDir string, noImages bool) (string, error) {
	var builder strings.Builder
	inFence := false
	fenceRune := rune(0)
	fenceCount := 0

	for _, segment := range strings.SplitAfter(markdownBody, "\n") {
		currentFenceRune, currentFenceCount := markdownFenceMarker(segment)
		if inFence {
			builder.WriteString(segment)
			if currentFenceRune == fenceRune && currentFenceCount >= fenceCount {
				inFence = false
				fenceRune = 0
				fenceCount = 0
			}
			continue
		}

		if currentFenceRune != 0 {
			inFence = true
			fenceRune = currentFenceRune
			fenceCount = currentFenceCount
			builder.WriteString(segment)
			continue
		}

		replaced, err := replaceMarkdownImagesInText(segment, pageURL, client, outputPath, imagesDir, noImages)
		if err != nil {
			return "", err
		}
		builder.WriteString(replaced)
	}

	return builder.String(), nil
}

func splitMarkdownImageTarget(target string) (string, string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", ""
	}

	if strings.HasPrefix(target, "<") {
		end := strings.Index(target, ">")
		if end <= 0 {
			return "", ""
		}
		return strings.TrimSpace(target[1:end]), target[end+1:]
	}

	for index, r := range target {
		if unicode.IsSpace(r) {
			return strings.TrimSpace(target[:index]), target[index:]
		}
	}

	return target, ""
}

func replaceMarkdownImagesInText(text string, pageURL *url.URL, client *http.Client, outputPath string, imagesDir string, noImages bool) (string, error) {
	imagePattern := regexp.MustCompile(`!\[([^\]]*)\]\(([^)\r\n]+)\)`)
	matches := imagePattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}

	var builder strings.Builder
	lastIndex := 0
	for _, match := range matches {
		builder.WriteString(text[lastIndex:match[0]])

		alt := text[match[2]:match[3]]
		target := text[match[4]:match[5]]
		sourceURL, suffix := splitMarkdownImageTarget(target)
		if sourceURL == "" {
			builder.WriteString(text[match[0]:match[1]])
			lastIndex = match[1]
			continue
		}

		markdownPath, err := resolveImageMarkdownPath(sourceURL, pageURL, client, outputPath, imagesDir, noImages)
		if err != nil {
			return "", err
		}

		builder.WriteString("![")
		builder.WriteString(alt)
		builder.WriteString("](")
		builder.WriteString(markdownPath)
		builder.WriteString(suffix)
		builder.WriteString(")")
		lastIndex = match[1]
	}

	builder.WriteString(text[lastIndex:])
	return builder.String(), nil
}

func markdownFenceMarker(line string) (rune, int) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return 0, 0
	}

	firstRune, _ := utf8.DecodeRuneInString(trimmed)
	if firstRune != '`' && firstRune != '~' {
		return 0, 0
	}

	count := 0
	for _, r := range trimmed {
		if r != firstRune {
			break
		}
		count++
	}
	if count < 3 {
		return 0, 0
	}

	return firstRune, count
}

func extractImageSource(tag string) string {
	for _, key := range []string{"src", "data-src", "srcset", "data-srcset"} {
		value := extractTagAttribute(tag, key)
		if value == "" {
			continue
		}
		if strings.Contains(key, "srcset") {
			return firstSrcsetCandidate(value)
		}
		return value
	}

	return ""
}

func extractImageAlt(tag string) string {
	return cleanInlineHTML(extractTagAttribute(tag, "alt"))
}

func extractTagAttribute(tag string, key string) string {
	pattern := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(key) + `\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
	matches := pattern.FindStringSubmatch(tag)
	if len(matches) == 0 {
		return ""
	}

	for _, group := range matches[2:] {
		if group != "" {
			return html.UnescapeString(strings.TrimSpace(group))
		}
	}

	return ""
}

func firstSrcsetCandidate(srcset string) string {
	candidates := strings.Split(srcset, ",")
	if len(candidates) == 0 {
		return ""
	}

	fields := strings.Fields(strings.TrimSpace(candidates[0]))
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

func downloadImage(resolvedURL *url.URL, client *http.Client, outputPath string, imagesDir string) (string, error) {
	if imagesDir == "" {
		return "", fmt.Errorf("images directory is required to download article images")
	}

	response, err := client.Get(resolvedURL.String())
	if err != nil {
		return "", fmt.Errorf("fetch image %s: %w", resolvedURL.Redacted(), err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("fetch image %s: unexpected status %s", resolvedURL.Redacted(), response.Status)
	}

	baseDir, imageDir := resolveImageDirectories(outputPath, imagesDir)
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return "", fmt.Errorf("create images directory %s: %w", filepath.Clean(imageDir), err)
	}

	fileName := deriveImageFileName(resolvedURL, response.Header.Get("Content-Type"))
	targetPath := makeUniqueFilePath(imageDir, fileName)

	file, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("create image file %s: %w", filepath.Clean(targetPath), err)
	}

	if _, err := io.Copy(file, response.Body); err != nil {
		file.Close()
		return "", fmt.Errorf("save image %s: %w", filepath.Clean(targetPath), err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close image file %s: %w", filepath.Clean(targetPath), err)
	}

	relativePath, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return filepath.ToSlash(targetPath), nil
	}

	return filepath.ToSlash(relativePath), nil
}

func resolveImageDirectories(outputPath string, imagesDir string) (string, string) {
	baseDir := "."
	if outputPath != "" {
		baseDir = filepath.Dir(outputPath)
		if baseDir == "" {
			baseDir = "."
		}
	}

	if filepath.IsAbs(imagesDir) {
		return baseDir, imagesDir
	}

	return baseDir, filepath.Join(baseDir, imagesDir)
}

func deriveImageFileName(imageURL *url.URL, contentType string) string {
	stem := sanitizeFileStem(strings.TrimSuffix(path.Base(imageURL.Path), path.Ext(imageURL.Path)))
	if stem == "" {
		stem = "image"
	}

	extension := strings.ToLower(path.Ext(imageURL.Path))
	if extension == "" || !isSupportedImageExtension(extension) {
		extension = detectImageExtension(contentType)
	}
	if extension == "" {
		extension = ".jpg"
	}

	return stem + extension
}

func detectImageExtension(contentType string) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType == "" {
		return ""
	}

	extensions, err := mime.ExtensionsByType(contentType)
	if err != nil || len(extensions) == 0 {
		return ""
	}

	for _, extension := range extensions {
		if isSupportedImageExtension(extension) {
			return extension
		}
	}

	return ""
}

func isSupportedImageExtension(extension string) bool {
	switch extension {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg":
		return true
	default:
		return false
	}
}

func htmlToMarkdown(fragment string) string {
	replacements := []struct {
		pattern *regexp.Regexp
		value   string
	}{
		{regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`), ""},
		{regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`), ""},
		{regexp.MustCompile(`(?is)<hr\b[^>]*>`), "\n\n---\n\n"},
		{regexp.MustCompile(`(?is)<br\s*/?>`), "\n"},
		{regexp.MustCompile(`(?is)</p>`), "\n\n"},
		{regexp.MustCompile(`(?is)<p[^>]*>`), ""},
		{regexp.MustCompile(`(?is)</h[1-6]>`), "\n\n"},
		{regexp.MustCompile(`(?is)<h1[^>]*>`), "# "},
		{regexp.MustCompile(`(?is)<h2[^>]*>`), "## "},
		{regexp.MustCompile(`(?is)<h3[^>]*>`), "### "},
		{regexp.MustCompile(`(?is)<h4[^>]*>`), "#### "},
		{regexp.MustCompile(`(?is)<h5[^>]*>`), "##### "},
		{regexp.MustCompile(`(?is)<h6[^>]*>`), "###### "},
		{regexp.MustCompile(`(?is)</li>`), "\n"},
		{regexp.MustCompile(`(?is)<li[^>]*>`), "- "},
		{regexp.MustCompile(`(?is)</?(ul|ol)[^>]*>`), "\n"},
		{regexp.MustCompile(`(?is)<a[^>]+href=["'](.*?)["'][^>]*>(.*?)</a>`), "$2 ($1)"},
	}

	content := fragment
	for _, replacement := range replacements {
		content = replacement.pattern.ReplaceAllString(content, replacement.value)
	}

	content = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(content, "")
	content = html.UnescapeString(content)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")

	lines := strings.Split(content, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimSpace(line)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func normalizeMarkdownContent(markdownBody string) string {
	markdownBody = strings.ReplaceAll(markdownBody, "\r\n", "\n")
	return strings.TrimSpace(markdownBody)
}

func normalizeQiitaMarkdown(markdownBody string, title string) string {
	markdownBody = normalizeMarkdownContent(markdownBody)
	markdownBody = trimDuplicateHeading(markdownBody, title)

	lines := strings.Split(markdownBody, "\n")
	for index, line := range lines {
		lines[index] = normalizeQiitaHeadingLine(line)
	}

	result := strings.Join(lines, "\n")
	result = regexp.MustCompile(`\n{3,}`).ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}

func normalizeQiitaHeadingLine(line string) string {
	pattern := regexp.MustCompile(`^(#{1,6}) \(\#[^)]+\)(.+)$`)
	matches := pattern.FindStringSubmatch(line)
	if len(matches) == 3 {
		return matches[1] + " " + strings.TrimSpace(matches[2])
	}

	return line
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func cleanInlineHTML(value string) string {
	value = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	return strings.TrimSpace(value)
}

func normalizeDocumentTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}

	for _, separator := range []string{"｜", "|"} {
		if before, _, found := strings.Cut(title, separator); found {
			title = strings.TrimSpace(before)
			break
		}
	}

	return title
}

func trimDuplicateHeading(markdownBody string, title string) string {
	title = strings.TrimSpace(title)
	if title == "" || markdownBody == "" {
		return markdownBody
	}

	lines := strings.Split(markdownBody, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}

	if len(lines) == 0 {
		return markdownBody
	}

	firstLine := strings.TrimSpace(lines[0])
	if firstLine != "# "+title {
		return markdownBody
	}

	lines = lines[1:]
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}

	return strings.Join(lines, "\n")
}

func normalizeNoteMarkdown(markdownBody string) string {
	lines := strings.Split(markdownBody, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " \t")
	}

	lines = normalizeBrokenHeadings(lines)
	lines = rewriteLeadingAuthorBlock(lines)
	lines = removeUnwantedLines(lines)

	result := strings.Join(lines, "\n")
	result = regexp.MustCompile(`\n{3,}`).ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}

func normalizeBrokenHeadings(lines []string) []string {
	normalized := make([]string, 0, len(lines))
	for index := 0; index < len(lines); index++ {
		current := strings.TrimSpace(lines[index])
		if matched, _ := regexp.MatchString(`^#{1,6}$`, current); matched {
			nextIndex := index + 1
			for nextIndex < len(lines) && strings.TrimSpace(lines[nextIndex]) == "" {
				nextIndex++
			}
			if nextIndex < len(lines) {
				normalized = append(normalized, current+" "+strings.TrimSpace(lines[nextIndex]))
				index = nextIndex
				continue
			}
		}

		normalized = append(normalized, lines[index])
	}

	return normalized
}

func rewriteLeadingAuthorBlock(lines []string) []string {
	trimmed := dropLeadingBlankLines(lines)
	if len(trimmed) < 4 {
		return trimmed
	}

	start := findLeadingAuthorBlockStart(trimmed)
	if start < 0 || start+2 >= len(trimmed) {
		return trimmed
	}

	if !isProfileLinkLine(trimmed[start]) || isProfileLinkLine(trimmed[start+1]) {
		return trimmed
	}

	author := strings.TrimSpace(trimmed[start+1])
	dateLine, ok := trimLeadingProfileLink(trimmed[start+2], trimmed[start])
	if !ok {
		return trimmed
	}
	dateText, remainder := splitDateAndRemainder(dateLine)
	if author == "" || dateText == "" {
		return trimmed
	}

	rewritten := append([]string{}, trimmed[:start]...)
	if len(rewritten) > 0 && strings.TrimSpace(rewritten[len(rewritten)-1]) != "" {
		rewritten = append(rewritten, "")
	}
	rewritten = append(rewritten, author, dateText, "", "---")
	if remainder != "" {
		rewritten = append(rewritten, "", remainder)
	}

	rest := trimmed[start+3:]
	if len(rest) > 0 {
		rewritten = append(rewritten, rest...)
	}

	return rewritten
}

func trimLeadingProfileLink(line string, profileLine string) (string, bool) {
	line = strings.TrimSpace(line)
	profileLine = strings.TrimSpace(profileLine)
	if !strings.HasPrefix(line, profileLine) {
		return "", false
	}

	return strings.TrimSpace(strings.TrimPrefix(line, profileLine)), true
}

func findLeadingAuthorBlockStart(lines []string) int {
	limit := minInt(len(lines), 8)
	for index := 0; index < limit; index++ {
		if isProfileLinkLine(lines[index]) {
			return index
		}
	}

	return -1
}

func dropLeadingBlankLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return lines
}

func isProfileLinkLine(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "(/") && strings.HasSuffix(line, ")")
}

func splitDateAndRemainder(line string) (string, string) {
	pattern := regexp.MustCompile(`^(\d{4}年\d{1,2}月\d{1,2}日 \d{1,2}:\d{2})(?:\s+(.*))?$`)
	matches := pattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) == 0 {
		return "", ""
	}

	return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2])
}

func removeUnwantedLines(lines []string) []string {
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "ダウンロード" || strings.EqualFold(trimmed, "copy") {
			continue
		}
		if regexp.MustCompile(`^\d+$`).MatchString(trimmed) && followsHeading(filtered) {
			continue
		}
		filtered = append(filtered, line)
	}

	return filtered
}

func followsHeading(lines []string) bool {
	for index := len(lines) - 1; index >= 0; index-- {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" {
			continue
		}
		return strings.HasPrefix(trimmed, "#")
	}

	return false
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
