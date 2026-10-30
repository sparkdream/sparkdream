// Package datauri is the one data-URI detection rule shared by the chain's
// media labelling (x/common/types, rules version 2) and the off-chain
// scanners (tools/contentscan). Both sides must agree byte for byte on what
// counts as a data URI, so the rule lives here once.
//
// A body is first normalised the way a browser would read it, then matched:
//
//   - HTML character references are decoded once: numeric (&#58; &#x3a;,
//     with or without the closing semicolon) and the named ones that can
//     spell a data URI's punctuation (&colon; &semi; &comma; &sol; &Tab;
//     &NewLine;);
//   - CSS escapes are decoded (\3a , \:), since a style attribute's url()
//     is read through them;
//   - ASCII tab, CR and LF are removed (the URL parser strips them anywhere
//     in a URL, so "da<TAB>ta:" is "data:"). Because removing them also joins
//     words ("intro\ndata:"), the text is matched a second time with them
//     read as spaces; either reading counts.
//
// The normalised text contains a data URI when "data:" (case-insensitive, not
// preceded by a URI-scheme character) is followed by a media-type token and
// one of "/", ";" or "," (as in data:image/png;..., data:text,..., data:;base64,
// or data:,...), or by spaces and then a "type/" token or ";". There is no
// length bound on the media type. Prose like "Data: 5 records" or
// "metadata: x" does not match.
//
// Everything is linear in the input (Go's RE2 regexp, one decoding pass), so
// the result and cost are identical on every node.
package datauri

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// tokenChars are the RFC 7230 token characters a media type is made of.
const tokenChars = "!#$%&'*+.^_`|~0-9a-z-"

var pattern = regexp.MustCompile(`(?i)(^|[^a-z0-9+.-])data:(?:[` + tokenChars + `]*[/;,]|[ ]+(?:[` + tokenChars + `]+/|;))`)

// namedRefs are the named character references that can spell part of a
// data URI (lower-cased, without & and ;). Tab and newline decode to
// characters Normalize then drops.
var namedRefs = map[string]byte{
	"colon":   ':',
	"semi":    ';',
	"comma":   ',',
	"sol":     '/',
	"tab":     '\t',
	"newline": '\n',
}

// needsNormalize reports whether s contains anything Normalize would change.
func needsNormalize(s string) bool {
	return strings.ContainsAny(s, "&\\\t\r\n")
}

// Normalize returns s as a browser would read a URL in it: references and
// escapes decoded, tab/CR/LF removed (see the package comment).
func Normalize(s string) string {
	return normalize(s, false)
}

// normalize decodes s; tab/CR/LF are removed, or read as spaces when asSpace.
func normalize(s string, asSpace bool) string {
	if !needsNormalize(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	emit := func(r rune) {
		switch r {
		case '\t', '\r', '\n':
			if asSpace {
				b.WriteByte(' ')
			}
			return
		case 0:
			r = utf8.RuneError
		}
		b.WriteRune(r)
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '\t', '\r', '\n':
			if asSpace {
				b.WriteByte(' ')
			}
			i++
			continue
		case '&':
			if r, n, ok := charRef(s[i:]); ok {
				emit(r)
				i += n
				continue
			}
		case '\\':
			if r, n, ok := cssEscape(s[i:]); ok {
				emit(r)
				i += n
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// charRef decodes an HTML character reference at the start of s.
func charRef(s string) (rune, int, bool) {
	if len(s) < 3 {
		return 0, 0, false
	}
	if s[1] == '#' {
		i, base := 2, 10
		if i < len(s) && (s[i] == 'x' || s[i] == 'X') {
			i, base = 3, 16
		}
		start := i
		for i < len(s) && i-start < 8 && isDigit(s[i], base) {
			i++
		}
		if i == start {
			return 0, 0, false
		}
		v, err := strconv.ParseUint(s[start:i], base, 32)
		if err != nil || v > utf8.MaxRune {
			v = utf8.RuneError
		}
		if i < len(s) && s[i] == ';' {
			i++
		}
		return rune(v), i, true
	}
	end := strings.IndexByte(s[:min(len(s), 10)], ';')
	if end < 2 {
		return 0, 0, false
	}
	if c, ok := namedRefs[strings.ToLower(s[1:end])]; ok {
		return rune(c), end + 1, true
	}
	return 0, 0, false
}

// cssEscape decodes a CSS escape at the start of s: a backslash and 1-6 hex
// digits (plus one optional following whitespace), or a backslash and any
// other non-newline character standing for itself.
func cssEscape(s string) (rune, int, bool) {
	if len(s) < 2 {
		return 0, 0, false
	}
	i := 1
	for i < len(s) && i-1 < 6 && isDigit(s[i], 16) {
		i++
	}
	if i > 1 {
		v, err := strconv.ParseUint(s[1:i], 16, 32)
		if err != nil || v == 0 || v > utf8.MaxRune {
			v = utf8.RuneError
		}
		if i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == '\f') {
			i++
		}
		return rune(v), i, true
	}
	if s[1] == '\n' || s[1] == '\r' || s[1] == '\f' {
		return 0, 0, false
	}
	r, n := utf8.DecodeRuneInString(s[1:])
	return r, 1 + n, true
}

func isDigit(c byte, base int) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'):
		return true
	}
	return false
}

// Contains reports whether s contains a data URI under either reading.
func Contains(s string) bool {
	if pattern.MatchString(normalize(s, false)) {
		return true
	}
	return needsNormalize(s) && pattern.MatchString(normalize(s, true))
}

// Count returns how many data URIs s holds: the larger count of the two
// readings. A scanner that decodes fewer blobs than this has missed one.
func Count(s string) int {
	n := len(pattern.FindAllStringIndex(normalize(s, false), -1))
	if needsNormalize(s) {
		n = max(n, len(pattern.FindAllStringIndex(normalize(s, true), -1)))
	}
	return n
}
