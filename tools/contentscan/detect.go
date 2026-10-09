package contentscan

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Detection, layered (§5.2). Open tools only, no commercial services:
//
//   - exact hash: a takedown list (the system's own removed verdicts, plus
//     known-abuse lists if a membership is ever obtained) -> removed;
//   - canary list: harmless council test items whose correct verdict is
//     known -> removed / category test (never shown to readers);
//   - classifier: a pinned open-weight model served over HTTP -> held, never
//     removed (classifier output alone is probabilistic);
//   - CSAM heuristic: sexual score combined with an age estimate -> held /
//     csam.
//
// Perceptual hashing (PDQ, TMK+PDQF) plugs in as another Classifier-style
// stage when a Go implementation is vendored; until then resized copies of
// removed material are caught only by the classifier.

// Blob is one piece of media found in a subject, held only in memory.
type Blob struct {
	Data      []byte
	MediaType string // sniffed or declared
	Source    string // "body", "data-uri", "fetch"
}

// SHA256Hex returns the lowercase hex sha256 of b.
func SHA256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// DecodeBody turns a stored body into the bytes a client would render:
// GZIP/ZSTD bodies are base64 then decompressed (capped at maxBytes); text
// bodies are returned as is. contentType is the chain enum name (e.g.
// "CONTENT_TYPE_GZIP"). Off-chain reference types are not decoded here; the
// body is a CID/id the caller resolves through a gateway.
func DecodeBody(contentType, body string, maxBytes int64) ([]byte, error) {
	switch contentType {
	case "CONTENT_TYPE_GZIP", "CONTENT_TYPE_ZSTD":
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body))
		if err != nil {
			return nil, fmt.Errorf("contentscan: compressed body is not base64: %w", err)
		}
		var r io.Reader
		if contentType == "CONTENT_TYPE_GZIP" {
			zr, err := gzip.NewReader(bytes.NewReader(raw))
			if err != nil {
				return nil, err
			}
			defer zr.Close()
			r = zr
		} else {
			zr, err := zstd.NewReader(bytes.NewReader(raw), zstd.WithDecoderMaxMemory(uint64(max(maxBytes, 1<<20))))
			if err != nil {
				return nil, err
			}
			defer zr.Close()
			r = zr
		}
		return readCapped(r, maxBytes)
	default:
		return []byte(body), nil
	}
}

// dataURIRe finds RFC 2397 data URIs in rendered text. It is broader than
// the chain's labelling rule only in that it captures the payload too.
var dataURIRe = regexp.MustCompile(`(?i)data:([^\s,;"'()<>]*)((?:;[^\s,;"'()<>]*)*),([^\s"'()<>]*)`)

// ExtractDataURIs decodes every data URI in text into a blob.
func ExtractDataURIs(text []byte, maxBytes int64) []Blob {
	var out []Blob
	for _, m := range dataURIRe.FindAllSubmatch(text, -1) {
		mediaType, params, payload := string(m[1]), strings.ToLower(string(m[2])), string(m[3])
		var data []byte
		if strings.Contains(params, ";base64") {
			d, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				d, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(payload, "="))
				if err != nil {
					continue
				}
			}
			data = d
		} else {
			d, err := url.PathUnescape(payload)
			if err != nil {
				continue
			}
			data = []byte(d)
		}
		if maxBytes > 0 && int64(len(data)) > maxBytes {
			data = data[:maxBytes]
		}
		if mediaType == "" {
			mediaType = http.DetectContentType(data)
		}
		out = append(out, Blob{Data: data, MediaType: mediaType, Source: "data-uri"})
	}
	return out
}

// urlRe finds media URLs a renderer would load: HTML src/poster/srcset
// attributes and Markdown image syntax.
var urlRe = regexp.MustCompile(`(?i)(?:\b(?:src|poster|srcset|href)\s*=\s*["']?|!\[[^\]]*\]\()\s*((?:https?|ipfs|ar)://[^\s"'()<>]+)`)

// ExtractURLs returns the distinct external URLs in a rendered body, in
// order. Clients treat every one as unchecked until it has a verdict (§6).
func ExtractURLs(text []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range urlRe.FindAllSubmatch(text, -1) {
		u := string(m[1])
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// IsMedia reports whether a media type is something the classifier must
// see (images, video, audio, archives, anything not plain text).
func IsMedia(mediaType string) bool {
	mt := strings.ToLower(mediaType)
	return !(strings.HasPrefix(mt, "text/") || mt == "application/json")
}

// HashList maps sha256 hex to a category.
type HashList struct {
	entries map[string]string
}

// NewHashList builds a list from sha256 -> category.
func NewHashList(entries map[string]string) *HashList {
	l := &HashList{entries: map[string]string{}}
	for h, c := range entries {
		l.entries[strings.ToLower(h)] = c
	}
	return l
}

// LoadHashList reads a takedown-hashes.json file or a plain list (one hex
// sha256 per line, optionally followed by whitespace and a category).
// defaultCategory applies to plain-list lines without one.
func LoadHashList(path, defaultCategory string) (*HashList, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	l := &HashList{entries: map[string]string{}}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var tl TakedownList
		if err := json.Unmarshal(trimmed, &tl); err != nil {
			return nil, fmt.Errorf("contentscan: %s: %w", path, err)
		}
		for _, e := range tl.Entries {
			l.entries[strings.ToLower(e.SHA256)] = e.Category
		}
		return l, nil
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		cat := defaultCategory
		if len(fields) > 1 {
			cat = fields[1]
		}
		l.entries[strings.ToLower(fields[0])] = cat
	}
	return l, sc.Err()
}

// Lookup returns the category for a hash.
func (l *HashList) Lookup(sha256Hex string) (string, bool) {
	if l == nil {
		return "", false
	}
	c, ok := l.entries[sha256Hex]
	return c, ok
}

// Classifier scores one blob. Implementations must be deterministic for a
// given model hash (CPU inference, fixed precision) so independent workers
// agree.
type Classifier interface {
	Classify(ctx context.Context, b Blob) (map[string]float64, error)
}

// HTTPClassifier posts the blob's bytes to a local model server and expects
// {"scores": {"sexual": 0.1, "minor": 0.0, "violence": 0.0, ...}}. The server
// runs the pinned open-weight models named in the ruleset.
type HTTPClassifier struct {
	URL    string
	Client *http.Client
}

// Classify implements Classifier.
func (c HTTPClassifier) Classify(ctx context.Context, b Blob) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(b.Data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", b.MediaType)
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("contentscan: classifier status %d", resp.StatusCode)
	}
	var out struct {
		Scores map[string]float64 `json:"scores"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out.Scores, nil
}

// ErrNoClassifier means media was found but no classifier is configured, so
// the worker cannot honestly say clean; it issues no verdict and the subject
// stays unchecked.
var ErrNoClassifier = errors.New("contentscan: media needs a classifier before it can be called clean")

// Detector judges the blobs found in one subject.
type Detector struct {
	Takedown   *HashList
	Canary     *HashList
	Classifier Classifier
	Ruleset    Ruleset
}

// Advisory categories a classifier may raise (held, operator chooses what to
// do with them).
var advisoryCategories = []string{CategorySexual, CategoryViolence, CategorySpam}

// Judge returns the verdict and category for a subject whose rendered media
// is blobs. A subject with no media at all is clean. Order of precedence:
// canary, takedown list, CSAM heuristic, advisory categories.
func (d Detector) Judge(ctx context.Context, blobs []Blob) (verdict, category string, err error) {
	for _, b := range blobs {
		h := SHA256Hex(b.Data)
		if _, ok := d.Canary.Lookup(h); ok {
			return VerdictRemoved, CategoryTest, nil
		}
		if c, ok := d.Takedown.Lookup(h); ok {
			return VerdictRemoved, c, nil
		}
	}
	held := ""
	for _, b := range blobs {
		if !IsMedia(b.MediaType) {
			continue
		}
		if d.Classifier == nil {
			return "", "", ErrNoClassifier
		}
		scores, err := d.Classifier.Classify(ctx, b)
		if err != nil {
			return "", "", err
		}
		t := d.Ruleset.Thresholds
		if over(scores, t, CategorySexual) && over(scores, t, "minor") {
			return VerdictHeld, CategoryCSAM, nil
		}
		for _, c := range advisoryCategories {
			if held == "" && over(scores, t, c) {
				held = c
			}
		}
	}
	if held != "" {
		return VerdictHeld, held, nil
	}
	return VerdictClean, CategoryNone, nil
}

// over reports whether scores[key] reaches its threshold. A category without
// a configured threshold never triggers.
func over(scores, thresholds map[string]float64, key string) bool {
	t, ok := thresholds[key]
	if !ok {
		return false
	}
	return scores[key] >= t
}
