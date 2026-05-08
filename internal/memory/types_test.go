package memory

import "testing"

func TestParseMemoryType(t *testing.T) {
	tests := []struct {
		input string
		want  MemoryType
	}{
		{"user", MemoryTypeUser},
		{"feedback", MemoryTypeFeedback},
		{"project", MemoryTypeProject},
		{"reference", MemoryTypeReference},
		{"invalid", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := ParseMemoryType(tc.input)
		if got != tc.want {
			t.Errorf("ParseMemoryType(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTrustingRecallSection(t *testing.T) {
	section := TrustingRecallSection()
	if !contains(section, "Before recommending from memory") {
		t.Error("expected header")
	}
}

func TestMemoryFrontmatterExample(t *testing.T) {
	ex := MemoryFrontmatterExample()
	if !contains(ex, "name:") {
		t.Error("expected name field")
	}
	if !contains(ex, "type:") {
		t.Error("expected type field")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && findSubstr(s, substr)
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
