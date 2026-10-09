package apcanon

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	commontypes "sparkdream/x/common/types"
)

// The open-content rule: Spark Dream anchors only posts their author has
// dedicated to the public domain. Mastodon has no license field, so the
// dedication is a hashtag in the post itself:
//
//	#cc0           CC0 1.0 dedication     -> "CC0-1.0"
//	#publicdomain  Public Domain Mark     -> "PDM-1.0"
//
// When a post carries both, CC0 wins: it is the author's own waiver, where
// the mark is a statement about the work.
//
// License reads the hashtag from `content` (the rendered post HTML), never
// from the `tag` array. `content` is one of the hashed fields under every
// ap-canonical rule, so a record whose hash verifies is also bound to the
// hashtag that licensed it; `tag` is not hashed. Like the hash rule, this
// function IS the contract between the bridge (which claims a license on
// MsgSubmitFederatedContent) and the verifier (which refuses a record whose
// claim the post does not back): both daemons must call it rather than
// re-implement it, or an honest record looks like a misrepresented one.

var (
	// inlineTag matches the markup Mastodon wraps a hashtag in
	// (<a ... rel="tag">#<span>cc0</span></a>). Removed without a gap, so
	// the "#" and the name rejoin.
	inlineTag = regexp.MustCompile(`(?i)</?(a|span)(\s[^>]*)?>`)
	// blockTag matches every other element (<p>, <br>, ...). Replaced by a
	// space, so text from adjacent paragraphs never fuses into a hashtag.
	blockTag = regexp.MustCompile(`<[^>]*>`)
	// hashtag matches a "#" and the whole run of hashtag characters
	// (letters, digits, underscore, middle dot) after it, so #cc0art and
	// #publicdomainday are names of their own, not #cc0 / #publicdomain.
	hashtag = regexp.MustCompile(`#([\p{L}\p{N}_·]+)`)
)

// License returns the public-domain license an AS2 object's author declared
// by hashtag ("CC0-1.0" or "PDM-1.0"), or "" when the post declares none and
// must not be anchored.
func License(obj map[string]any) string {
	content, _ := obj["content"].(string)
	return LicenseFromHTML(content)
}

// LicenseFromHTML applies License's rule to a post body.
func LicenseFromHTML(html string) string {
	text := blockTag.ReplaceAllString(inlineTag.ReplaceAllString(html, ""), " ")
	found := ""
	for _, m := range hashtag.FindAllStringSubmatchIndex(text, -1) {
		// A "#" inside a word (foo#cc0) or a URL fragment is not a hashtag.
		if m[0] > 0 {
			if r, _ := utf8.DecodeLastRuneInString(text[:m[0]]); isHashtagRune(r) || r == '#' || r == '/' {
				continue
			}
		}
		switch name := text[m[2]:m[3]]; {
		case strings.EqualFold(name, "cc0"):
			return commontypes.LicenseCC0
		case strings.EqualFold(name, "publicdomain"):
			found = commontypes.LicensePDM
		}
	}
	return found
}

func isHashtagRune(r rune) bool {
	return r == '_' || r == '·' || unicode.IsLetter(r) || unicode.IsNumber(r)
}
