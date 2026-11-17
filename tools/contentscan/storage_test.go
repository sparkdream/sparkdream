package contentscan

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// storageAtt is a signed verdict about storage request 7 of file "f".
func storageAtt(t *testing.T, worker string, k ed25519.PrivateKey, verdict string, change func(*Attestation)) Attestation {
	t.Helper()
	a := att(worker, hashOf("f"), verdict, CategoryNone)
	if verdict != VerdictClean {
		a.Category = CategorySexual
	}
	a.Subject.Record = StorageRecord(7)
	if change != nil {
		change(&a)
	}
	return mustSign(t, a, k)
}

func TestStorageVerdict(t *testing.T) {
	k1, k2, k3 := testKey(1), testKey(2), testKey(3)
	workers := []TrustedWorker{
		{Address: "w1", VerdictKey: k1.Public().(ed25519.PublicKey)},
		{Address: "w2", VerdictKey: k2.Public().(ed25519.PublicKey)},
	}
	later := func(a *Attestation) { a.IssuedAt = t0.Add(time.Hour) }
	cases := []struct {
		name      string
		atts      []Attestation
		quorum    int
		want      string
		wantClean []string // workers of the verdicts a completion would list
		conflict  bool
	}{
		{"nothing", nil, 1, VerdictUnchecked, nil, false},
		{"one clean, quorum one", []Attestation{storageAtt(t, "w1", k1, VerdictClean, nil)}, 1, VerdictClean, []string{"w1"}, false},
		{"quorum zero counts as one", []Attestation{storageAtt(t, "w1", k1, VerdictClean, nil)}, 0, VerdictClean, []string{"w1"}, false},
		{"one clean, quorum two", []Attestation{storageAtt(t, "w1", k1, VerdictClean, nil)}, 2, VerdictUnchecked, nil, false},
		{"two clean, ordered by worker", []Attestation{
			storageAtt(t, "w2", k2, VerdictClean, nil), storageAtt(t, "w1", k1, VerdictClean, nil),
		}, 2, VerdictClean, []string{"w1", "w2"}, false},
		{"removed wins over clean", []Attestation{
			storageAtt(t, "w1", k1, VerdictClean, nil), storageAtt(t, "w2", k2, VerdictRemoved, nil),
		}, 1, VerdictRemoved, nil, true},
		{"held wins over clean", []Attestation{
			storageAtt(t, "w1", k1, VerdictClean, nil), storageAtt(t, "w2", k2, VerdictHeld, nil),
		}, 1, VerdictHeld, nil, true},
		{"removed wins over held", []Attestation{
			storageAtt(t, "w1", k1, VerdictHeld, nil), storageAtt(t, "w2", k2, VerdictRemoved, nil),
		}, 1, VerdictRemoved, nil, false},
		{"a worker's latest verdict replaces its earlier one", []Attestation{
			storageAtt(t, "w1", k1, VerdictRemoved, nil), storageAtt(t, "w1", k1, VerdictClean, later),
		}, 1, VerdictClean, []string{"w1"}, false},
		{"and the order they are read in does not matter", []Attestation{
			storageAtt(t, "w1", k1, VerdictClean, later), storageAtt(t, "w1", k1, VerdictRemoved, nil),
		}, 1, VerdictClean, []string{"w1"}, false},
		{"one worker cannot fill a quorum of two", []Attestation{
			storageAtt(t, "w1", k1, VerdictClean, nil), storageAtt(t, "w1", k1, VerdictClean, later),
		}, 2, VerdictUnchecked, nil, false},
		{"a worker x/storage does not count", []Attestation{storageAtt(t, "w3", k3, VerdictClean, nil)}, 1, VerdictUnchecked, nil, false},
		{"a removed verdict from such a worker is ignored too", []Attestation{
			storageAtt(t, "w1", k1, VerdictClean, nil), storageAtt(t, "w3", k3, VerdictRemoved, nil),
		}, 1, VerdictClean, []string{"w1"}, false},
		{"signed with another worker's key", []Attestation{storageAtt(t, "w1", k2, VerdictClean, nil)}, 1, VerdictUnchecked, nil, false},
		{"another chain", []Attestation{storageAtt(t, "w1", k1, VerdictClean, func(a *Attestation) { a.Subject.ChainID = "other-1" })}, 1, VerdictUnchecked, nil, false},
		{"the same file in a blog post", []Attestation{storageAtt(t, "w1", k1, VerdictClean, func(a *Attestation) { a.Subject.Record = "blog/post/1" })}, 1, VerdictUnchecked, nil, false},
		{"another request for the same file", []Attestation{storageAtt(t, "w1", k1, VerdictClean, func(a *Attestation) { a.Subject.Record = StorageRecord(8) })}, 1, VerdictUnchecked, nil, false},
		{"another file", []Attestation{storageAtt(t, "w1", k1, VerdictClean, func(a *Attestation) { a.Subject.SHA256 = hashOf("g") })}, 1, VerdictUnchecked, nil, false},
		{"scanned before the attempt opened", []Attestation{storageAtt(t, "w1", k1, VerdictClean, func(a *Attestation) { a.ScannedHeight = 89 })}, 1, VerdictUnchecked, nil, false},
		{"scanned at the height it opened", []Attestation{storageAtt(t, "w1", k1, VerdictClean, func(a *Attestation) { a.ScannedHeight = 90 })}, 1, VerdictClean, []string{"w1"}, false},
		{"expired", []Attestation{storageAtt(t, "w1", k1, VerdictClean, func(a *Attestation) { a.ExpiresAt = t0.Add(time.Minute) })}, 1, VerdictUnchecked, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, clean := StorageVerdict(tc.atts, workers, "sparkdream-1", 7, hashOf("f"), 90, tc.quorum, t0.Add(2*time.Hour))
			if res.Verdict != tc.want || res.Conflict != tc.conflict {
				t.Fatalf("got %s (conflict %v), want %s (conflict %v)", res.Verdict, res.Conflict, tc.want, tc.conflict)
			}
			var got []string
			for _, a := range clean {
				got = append(got, a.Worker)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantClean, ",") {
				t.Fatalf("clean verdicts from %v, want %v", got, tc.wantClean)
			}
			if tc.want == VerdictClean && res.CleanVotes != len(tc.wantClean) {
				t.Fatalf("clean votes %d", res.CleanVotes)
			}
		})
	}
}

// A tampered verdict does not count: the signature covers the subject.
func TestStorageVerdictRejectsTampering(t *testing.T) {
	k := testKey(1)
	workers := []TrustedWorker{{Address: "w1", VerdictKey: k.Public().(ed25519.PublicKey)}}
	a := storageAtt(t, "w1", k, VerdictClean, func(a *Attestation) { a.Subject.Record = StorageRecord(8) })
	a.Subject.Record = StorageRecord(7) // moved to another request after signing
	if res, clean := StorageVerdict([]Attestation{a}, workers, "sparkdream-1", 7, hashOf("f"), 0, 1, t0); res.Verdict != VerdictUnchecked || clean != nil {
		t.Fatalf("counted a verdict moved to another request: %s", res.Verdict)
	}
}

// FetchFeed reads what a FeedWriter wrote.
func TestFetchFeed(t *testing.T) {
	dir := t.TempDir()
	k := testKey(1)
	fromDir := func(_ context.Context, u string) ([]byte, error) {
		rel, ok := strings.CutPrefix(u, "https://feeds.example/aurora/")
		if !ok {
			return nil, errors.New("outside the feed: " + u)
		}
		return os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	}
	fw, err := OpenFeed(dir, "w1", "key")
	if err != nil {
		t.Fatal(err)
	}
	// A feed with no manifest yet is an error; one with a manifest and no
	// verdict has no segment file and reads as empty.
	if _, _, err := FetchFeed(context.Background(), fromDir, "https://feeds.example/aurora/"); err == nil {
		t.Fatal("no error for a feed without a manifest")
	}
	if err := fw.SetScannedHeight(5); err != nil {
		t.Fatal(err)
	}
	m, atts, err := FetchFeed(context.Background(), fromDir, "https://feeds.example/aurora/")
	if err != nil || m.Worker != "w1" || len(atts) != 0 {
		t.Fatalf("empty feed: %+v %v %v", m, atts, err)
	}
	a1 := storageAtt(t, "w1", k, VerdictClean, nil)
	a2 := storageAtt(t, "w1", k, VerdictHeld, func(a *Attestation) { a.IssuedAt = t0.Add(time.Minute) })
	if err := fw.Append(a1, a2); err != nil {
		t.Fatal(err)
	}
	m, atts, err = FetchFeed(context.Background(), fromDir, "https://feeds.example/aurora")
	if err != nil || m.Count != 2 || len(atts) != 2 {
		t.Fatalf("feed: %+v, %d verdicts, %v", m, len(atts), err)
	}
	if m.Root != fw.Manifest().Root || m.Root == "" {
		t.Fatalf("manifest root %q, writer has %q", m.Root, fw.Manifest().Root)
	}
	h1, _ := AttestationHash(a1)
	if got, _ := AttestationHash(atts[0]); got != h1 {
		t.Fatal("a verdict read from the feed hashes differently from the one written")
	}

	// Failures are errors, never an empty feed.
	if _, _, err := FetchFeed(context.Background(), func(context.Context, string) ([]byte, error) { return nil, errors.New("down") }, "https://x"); err == nil {
		t.Fatal("no error for an unreachable manifest")
	}
	bad := func(body string) func(context.Context, string) ([]byte, error) {
		return func(_ context.Context, u string) ([]byte, error) {
			if strings.HasSuffix(u, "manifest.json") {
				return []byte(body), nil
			}
			return nil, errors.New("no such segment")
		}
	}
	for _, body := range []string{`not json`, `{"count":1,"latest_segment":20000}`, `{"count":1,"latest_segment":-1}`, `{"count":1,"latest_segment":1}`} {
		if _, _, err := FetchFeed(context.Background(), bad(body), "https://x"); err == nil {
			t.Fatalf("no error for manifest %s", body)
		}
	}
}
