package markdownutil

import (
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantFrontmatter string
		wantBody        string
		wantErr         bool
	}{
		{
			name:            "no frontmatter",
			input:           "Hello world",
			wantFrontmatter: "",
			wantBody:        "Hello world",
		},
		{
			name: "with frontmatter",
			input: `---
name: test
description: A test skill
---
Hello world`,
			wantFrontmatter: "name: test\ndescription: A test skill",
			wantBody:        "Hello world",
		},
		{
			name:            "only frontmatter delimiters",
			input:           "---\n---",
			wantFrontmatter: "",
			wantBody:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := SplitFrontmatter(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SplitFrontmatter error = %v, wantErr %v", err, tt.wantErr)
			}
			if fm != tt.wantFrontmatter {
				t.Errorf("frontmatter = %q, want %q", fm, tt.wantFrontmatter)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestExtractDescriptionFromMarkdown(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"First line.\n\nSecond paragraph.", "First line."},
		{"# Header\n\nBody text.", "Body text."},
		{"", ""},
		{"\n\n\n", ""},
	}
	for _, tc := range tests {
		got := ExtractDescriptionFromMarkdown(tc.input)
		if got != tc.want {
			t.Errorf("ExtractDescriptionFromMarkdown(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
