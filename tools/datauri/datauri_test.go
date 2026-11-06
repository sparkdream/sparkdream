package datauri

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"plain":                 "plain",
		"a&#58;b":               "a:b",
		"a&#x3a;b":              "a:b",
		"a&#58b":                "a:b",
		"a&colon;b&COMMA;c":     "a:b,c",
		"da\tta":                "data",
		"da&Tab;ta":             "data",
		`\3a `:                  ":",
		`\:`:                    ":",
		"&amp;#58;":             "&amp;#58;", // decoded once, like a browser
		"&#0;":                  "�",
		"&unknown;":             "&unknown;",
		strings.Repeat("&#", 3): strings.Repeat("&#", 3),
		"\\":                    "\\",
		"&#0000000058;":         ":",
		"&#x000000003a;":        ":",
		"&#99999999999999999;":  "\uFFFD",
		"da\\\nta":              "data",
		"da\\\r\nta":            "data",
		"da\\\fta":              "data",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCount(t *testing.T) {
	body := "intro\ndata:,x and <img src=\"data&#58;image/png;base64&#44;AA\"> and data:text,y"
	if got := Count(body); got != 3 {
		t.Fatalf("Count = %d, want 3", got)
	}
	if Count("Data: 5 records, all valid") != 0 {
		t.Fatal("prose counted")
	}
}
