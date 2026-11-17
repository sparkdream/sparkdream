package contentscan

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// StorageRecord is the subject record of an x/storage request.
func StorageRecord(requestID uint64) string {
	return "storage/request/" + strconv.FormatUint(requestID, 10)
}

// StorageVerdict applies the combining rule (§4.4) to an x/storage request
// (docs/x-storage-spec.md, "Claims, completion and settlement"). Only a
// verdict about this very request counts: its chain_id is chainID, its
// record is the request's, its sha256 is the file's, and it was scanned at
// or after openedAt, the height the request's current attempt started. That
// leaves out a verdict on the bare hash, one made for the same image in a
// post, and one from before a void or a reopen.
//
// workers must be the scanners x/storage counts: approved there and ACTIVE.
// Among their latest verdicts, removed wins, then held, then clean at
// quorum; anything else is unchecked. When the result is clean, clean holds
// one verdict per worker, ordered by worker address: these are the verdicts
// a completion lists (by AttestationHash) and publishes.
func StorageVerdict(atts []Attestation, workers []TrustedWorker, chainID string, requestID uint64, sha256Hex string, openedAt int64, quorum int, now time.Time) (res Result, clean []Attestation) {
	if quorum < 1 {
		quorum = 1
	}
	keys := make(map[string]TrustedWorker, len(workers))
	for _, w := range workers {
		keys[w.Address] = w
	}
	record := StorageRecord(requestID)
	latest := map[string]Attestation{}
	for _, a := range atts {
		w, trusted := keys[a.Worker]
		if !trusted || a.Subject.ChainID != chainID || a.Subject.Record != record ||
			a.Subject.SHA256 != sha256Hex || a.ScannedHeight < openedAt {
			continue
		}
		if !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt) {
			continue
		}
		if Verify(a, w.VerdictKey) != nil {
			continue
		}
		if prev, ok := latest[a.Worker]; ok && !a.IssuedAt.After(prev.IssuedAt) {
			continue
		}
		latest[a.Worker] = a
	}
	addrs := make([]string, 0, len(latest))
	for w := range latest {
		addrs = append(addrs, w)
	}
	sort.Strings(addrs)
	var removed, held *Attestation
	for _, w := range addrs {
		a := latest[w]
		switch a.Verdict {
		case VerdictRemoved:
			if removed == nil {
				removed = &a
			}
		case VerdictHeld:
			if held == nil {
				held = &a
			}
		case VerdictClean:
			clean = append(clean, a)
		}
	}
	res.CleanVotes = len(clean)
	switch {
	case removed != nil:
		res.Verdict, res.Category, res.Conflict = VerdictRemoved, removed.Category, len(clean) > 0
	case held != nil:
		res.Verdict, res.Category, res.Conflict = VerdictHeld, held.Category, len(clean) > 0
	case len(clean) >= quorum:
		res.Verdict = VerdictClean
		return res, clean
	default:
		res.Verdict = VerdictUnchecked
	}
	return res, nil
}

// maxFeedSegments bounds how much of a feed FetchFeed reads.
const maxFeedSegments = 10_000

// FetchFeed reads a worker's published feed: its manifest, then every
// segment. fetch is the caller's guarded fetcher (Fetch with a FetchLimits
// client); feedURL is the feed_url of the worker's operator metadata.
func FetchFeed(ctx context.Context, fetch func(ctx context.Context, rawURL string) ([]byte, error), feedURL string) (Manifest, []Attestation, error) {
	base := strings.TrimRight(feedURL, "/")
	raw, err := fetch(ctx, base+"/manifest.json")
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("contentscan: feed manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, nil, fmt.Errorf("contentscan: feed manifest: %w", err)
	}
	if m.LatestSegment < 0 || m.LatestSegment > maxFeedSegments {
		return m, nil, fmt.Errorf("contentscan: feed manifest names %d segments", m.LatestSegment)
	}
	if m.Count == 0 {
		return m, nil, nil // a new feed has no segment file yet
	}
	var out []Attestation
	for n := 1; n <= m.LatestSegment; n++ {
		seg, err := fetch(ctx, fmt.Sprintf("%s/segments/%06d.jsonl", base, n))
		if err != nil {
			return m, nil, fmt.Errorf("contentscan: feed segment %d: %w", n, err)
		}
		atts, err := ParseJSONL(seg)
		if err != nil {
			return m, nil, fmt.Errorf("contentscan: feed segment %d: %w", n, err)
		}
		out = append(out, atts...)
	}
	return m, out, nil
}
