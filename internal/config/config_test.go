package config

import "testing"

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"20GB", 20 << 30},
		{"512 MB", 512 << 20},
		{"1kb", 1 << 10},
	}
	for _, test := range tests {
		got, err := parseByteSize(test.input)
		if err != nil {
			t.Fatalf("parseByteSize(%q) error = %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parseByteSize(%q) = %d, want %d", test.input, got, test.want)
		}
	}
}

func TestParseByteSizeRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{"", "0GB", "20", "-1MB", "five GB"} {
		if _, err := parseByteSize(input); err == nil {
			t.Fatalf("parseByteSize(%q) unexpectedly succeeded", input)
		}
	}
}
