package main

import (
	"os"
	"path/filepath"
	"testing"
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
