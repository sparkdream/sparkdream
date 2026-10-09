package contentscan

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Feed layout (§5.4):
//
//	<feed-root>/manifest.json         worker, latest segment, scanned_height, merkle root, verdict key
//	<feed-root>/segments/000001.jsonl append-only signed attestations, one per line
//	<feed-root>/takedown-hashes.json  sha256 (+ PDQ) hashes of removed subjects
//
// Feeds are static files: any host or mirror serves them, and anyone may
// publish a merged file (the concatenation of several workers' segments)
// because clients verify every signature themselves.

// SegmentSize is how many attestations a segment holds before rolling.
const SegmentSize = 1000

// Manifest describes a worker's feed.
type Manifest struct {
	V             int    `json:"v"`
	Worker        string `json:"worker"`
	VerdictKey    string `json:"verdict_key"` // base64 ed25519 public key
	LatestSegment int    `json:"latest_segment"`
	Count         int    `json:"count"`          // attestations in the feed
	ScannedHeight int64  `json:"scanned_height"` // chain height scanned up to
	Root          string `json:"root"`           // hex merkle root over every attestation (MerkleRoot)
}

// TakedownList is the published hash list built from removed verdicts.
type TakedownList struct {
	V       int             `json:"v"`
	Worker  string          `json:"worker"`
	Entries []TakedownEntry `json:"entries"`
}

// TakedownEntry is one removed subject. Hashes only, never media.
type TakedownEntry struct {
	SHA256   string `json:"sha256"`
	PDQ      string `json:"pdq,omitempty"`
	Category string `json:"category"`
}

// LeafHash is the merkle leaf of one attestation: sha256(0x00 || canonical
// JSON including its signature). The 0x00/0x01 prefixes follow RFC 6962 so a
// leaf can never be confused with an interior node.
func LeafHash(a Attestation) ([32]byte, error) {
	b, err := CanonicalJSON(a)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append([]byte{0x00}, b...)), nil
}

// MerkleRoot is the RFC 6962 tree hash over leaves (in feed order). The root
// of an empty feed is sha256 of the empty string. This is the root a worker
// submits in MsgSubmitCheckpoint: a feed that later fails to reproduce an
// earlier checkpoint's root over its first N entries has been rewritten.
func MerkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return sha256.Sum256(nil)
	}
	if len(leaves) == 1 {
		return leaves[0]
	}
	k := 1
	for k*2 < len(leaves) {
		k *= 2
	}
	l, r := MerkleRoot(leaves[:k]), MerkleRoot(leaves[k:])
	return sha256.Sum256(append(append([]byte{0x01}, l[:]...), r[:]...))
}

// FeedWriter appends signed attestations to a feed directory and keeps its
// manifest and takedown list current. It holds the leaf hashes in memory so
// each append recomputes the root without rereading segments.
type FeedWriter struct {
	dir      string
	manifest Manifest
	leaves   [][32]byte
	takedown map[string]TakedownEntry
}

// OpenFeed loads (or initialises) the feed in dir for worker / verdictKey.
func OpenFeed(dir, worker, verdictKeyB64 string) (*FeedWriter, error) {
	if err := os.MkdirAll(filepath.Join(dir, "segments"), 0o755); err != nil {
		return nil, err
	}
	fw := &FeedWriter{
		dir:      dir,
		manifest: Manifest{V: 1, Worker: worker, VerdictKey: verdictKeyB64, LatestSegment: 1},
		takedown: map[string]TakedownEntry{},
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "manifest.json")); err == nil {
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("contentscan: manifest: %w", err)
		}
		if m.Worker != worker {
			return nil, fmt.Errorf("contentscan: feed in %s belongs to %s, not %s", dir, m.Worker, worker)
		}
		fw.manifest.LatestSegment = max(m.LatestSegment, 1)
		fw.manifest.ScannedHeight = m.ScannedHeight
	}
	atts, err := ReadSegments(filepath.Join(dir, "segments"))
	if err != nil {
		return nil, err
	}
	for _, a := range atts {
		if err := fw.index(a); err != nil {
			return nil, err
		}
	}
	fw.manifest.Count = len(fw.leaves)
	root := MerkleRoot(fw.leaves)
	fw.manifest.Root = hex.EncodeToString(root[:])
	return fw, nil
}

func (fw *FeedWriter) index(a Attestation) error {
	leaf, err := LeafHash(a)
	if err != nil {
		return err
	}
	fw.leaves = append(fw.leaves, leaf)
	if a.Verdict == VerdictRemoved && a.Category != CategoryTest {
		fw.takedown[a.Subject.SHA256] = TakedownEntry{SHA256: a.Subject.SHA256, PDQ: a.Subject.PDQ, Category: a.Category}
	}
	return nil
}

// Append adds signed attestations to the current segment (rolling to a new
// one at SegmentSize) and rewrites the manifest and takedown list.
func (fw *FeedWriter) Append(atts ...Attestation) error {
	for _, a := range atts {
		if a.Sig == "" {
			return errors.New("contentscan: refusing to append an unsigned attestation")
		}
		if fw.manifest.Count > 0 && fw.manifest.Count%SegmentSize == 0 {
			fw.manifest.LatestSegment = fw.manifest.Count/SegmentSize + 1
		}
		line, err := CanonicalJSON(a)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(fw.segmentPath(fw.manifest.LatestSegment), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, werr := f.Write(append(line, '\n'))
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		if cerr != nil {
			return cerr
		}
		if err := fw.index(a); err != nil {
			return err
		}
		fw.manifest.Count++
	}
	root := MerkleRoot(fw.leaves)
	fw.manifest.Root = hex.EncodeToString(root[:])
	return fw.flush()
}

// SetScannedHeight records how far the worker has scanned and rewrites the
// manifest.
func (fw *FeedWriter) SetScannedHeight(h int64) error {
	if h > fw.manifest.ScannedHeight {
		fw.manifest.ScannedHeight = h
	}
	return fw.flush()
}

// Manifest returns a copy of the current manifest.
func (fw *FeedWriter) Manifest() Manifest { return fw.manifest }

// Root returns the current merkle root.
func (fw *FeedWriter) Root() [32]byte { return MerkleRoot(fw.leaves) }

func (fw *FeedWriter) segmentPath(n int) string {
	return filepath.Join(fw.dir, "segments", fmt.Sprintf("%06d.jsonl", n))
}

func (fw *FeedWriter) flush() error {
	if err := writeJSONAtomic(filepath.Join(fw.dir, "manifest.json"), fw.manifest); err != nil {
		return err
	}
	list := TakedownList{V: 1, Worker: fw.manifest.Worker}
	for _, e := range fw.takedown {
		list.Entries = append(list.Entries, e)
	}
	sort.Slice(list.Entries, func(i, j int) bool { return list.Entries[i].SHA256 < list.Entries[j].SHA256 })
	return writeJSONAtomic(filepath.Join(fw.dir, "takedown-hashes.json"), list)
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadSegments reads every attestation in a segments directory, in order.
func ReadSegments(dir string) ([]Attestation, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []Attestation
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		atts, err := ParseJSONL(raw)
		if err != nil {
			return nil, fmt.Errorf("contentscan: %s: %w", name, err)
		}
		out = append(out, atts...)
	}
	return out, nil
}

// ParseJSONL parses one attestation per non-empty line (a segment or a
// merged file).
func ParseJSONL(raw []byte) ([]Attestation, error) {
	var out []Attestation
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var a Attestation
		if err := json.Unmarshal(line, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, sc.Err()
}
