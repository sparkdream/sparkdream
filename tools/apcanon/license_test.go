package apcanon

import "testing"

func TestLicenseFromHTML(t *testing.T) {
	const tagCC0 = `<a href="https://phoenix.example/tags/cc0" class="mention hashtag" rel="tag">#<span>cc0</span></a>`
	const tagPD = `<a href="https://phoenix.example/tags/publicdomain" class="mention hashtag" rel="tag">#<span>PublicDomain</span></a>`
	for _, tc := range []struct {
		name, html, want string
	}{
		{"mastodon cc0 markup", `<p>My sketch ` + tagCC0 + `</p>`, "CC0-1.0"},
		{"mastodon publicdomain markup", `<p>Scan of an 1860 map ` + tagPD + `</p>`, "PDM-1.0"},
		{"plain text", "hello #CC0", "CC0-1.0"},
		{"start of text", "#cc0 hello", "CC0-1.0"},
		{"cc0 wins over pdm, either order", "#publicdomain #cc0", "CC0-1.0"},
		{"cc0 wins, markup", `<p>` + tagPD + ` ` + tagCC0 + `</p>`, "CC0-1.0"},
		{"pdm alone", "#publicdomain", "PDM-1.0"},
		{"after a paragraph break", `<p>first</p><p>#cc0</p>`, "CC0-1.0"},
		{"no tag", "<p>just a post</p>", ""},
		{"longer hashtag", "#cc0art #publicdomainday", ""},
		{"underscore suffix", "#cc0_", ""},
		{"inside a word", "foo#cc0", ""},
		{"url fragment", "https://phoenix.example/page#cc0", ""},
		{"double hash", "##cc0", ""},
		{"text without hash", "this is cc0 and public domain", ""},
		{"empty", "", ""},
	} {
		if got := LicenseFromHTML(tc.html); got != tc.want {
			t.Errorf("%s: LicenseFromHTML(%q) = %q, want %q", tc.name, tc.html, got, tc.want)
		}
	}
}

func TestLicenseReadsContentOnly(t *testing.T) {
	// The tag array is not hashed, so it must not license a post on its own.
	obj := map[string]any{
		"content": "<p>no dedication here</p>",
		"tag":     []any{map[string]any{"type": "Hashtag", "name": "#cc0"}},
		"summary": "#cc0",
	}
	if got := License(obj); got != "" {
		t.Fatalf("License = %q, want \"\"", got)
	}
	if got := License(map[string]any{"content": "#cc0"}); got != "CC0-1.0" {
		t.Fatalf("License = %q, want CC0-1.0", got)
	}
	if got := License(map[string]any{}); got != "" {
		t.Fatalf("License(no content) = %q", got)
	}
}
