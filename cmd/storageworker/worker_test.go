package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/tools/arweave"
	"sparkdream/tools/contentscan"
	storagetypes "sparkdream/x/storage/types"
)

const (
	testOperator = "sprkdrm1operator"
	testScanner  = "sprkdrm1scanner"
	testChainID  = "sparkdream-1"
)

func testPNG(t *testing.T, shade uint8, alpha uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	for i := 0; i < 25; i++ {
		img.SetNRGBA(i%5, i/5, color.NRGBA{shade, uint8(i * 9), 60, alpha})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeChain answers the LCD paths the worker reads, in the shapes the real
// gateway returns (strings for 64-bit integers), and applies the worker's
// messages to its requests as x/storage would.
type fakeChain struct {
	t        *testing.T
	height   int64
	requests map[int64]*request
	flaggedH map[string]bool
	canClaim bool
	quorum   int
	scanMeta string // base64 operator metadata of the one approved scanner
	sent     []sdk.Msg
	failSend map[string]error // by message type name
	// behind makes the node answer as one that has not caught up: requests
	// are read from here when present, and the operator's list is empty.
	behind map[int64]*request
}

func (f *fakeChain) reqJSON(r *request) map[string]any {
	srcs := []map[string]any{}
	for _, s := range r.Sources {
		srcs = append(srcs, map[string]any{"url": s.URL, "added_at": fmt.Sprint(int64(s.AddedAt))})
	}
	return map[string]any{
		"id": fmt.Sprint(int64(r.ID)), "requester": r.Requester, "requester_trust_level": int64(r.TrustLevel),
		"backend": "arweave", "sha256": r.Sha256, "size_bytes": fmt.Sprint(int64(r.SizeBytes)),
		"content_type": r.ContentType, "scanned": r.Scanned, "status": r.Status,
		"opened_at": fmt.Sprint(int64(r.OpenedAt)), "deadline": fmt.Sprint(int64(r.Deadline)),
		"operator": r.Operator, "claim_expires_at": fmt.Sprint(int64(r.ClaimExpiresAt)),
		"claim_extended": r.ClaimExtended, "required_attestations": int64(r.RequiredAttestations),
		"barred": r.Barred, "sources": srcs,
	}
}

func (f *fakeChain) GetJSON(_ context.Context, path string, out any) error {
	base := path
	if i := strings.Index(path, "?"); i >= 0 {
		base = path[:i]
		if !strings.Contains(path, "pagination.offset=0") {
			return json.Unmarshal([]byte(`{}`), out)
		}
	}
	reply := func(v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, out)
	}
	list := func(keep func(*request) bool) error {
		var rs []map[string]any
		for id := int64(1); id <= int64(len(f.requests))+5; id++ {
			if r, ok := f.requests[id]; ok && keep(r) {
				rs = append(rs, f.reqJSON(r))
			}
		}
		return reply(map[string]any{"requests": rs})
	}
	switch {
	case base == "/cosmos/base/tendermint/v1beta1/blocks/latest":
		return reply(map[string]any{"block": map[string]any{"header": map[string]any{"height": fmt.Sprint(f.height)}}})
	case base == "/sparkdream/storage/v1/backend/arweave":
		return reply(map[string]any{"backend": map[string]any{"name": "arweave", "service_type": "storage-arweave",
			"types": []map[string]any{{"content_type": "image/png", "scanned": true, "max_size": "1048576"}}}})
	case base == "/sparkdream/storage/v1/params":
		return reply(map[string]any{"params": map[string]any{"claim_window_blocks": "20", "min_unreachable_blocks": "3"}})
	case base == "/sparkdream/storage/v1/operator_standing/arweave/"+testOperator:
		return reply(map[string]any{"approved": true, "can_claim": f.canClaim, "reason": "operator holds too many claims"})
	case base == "/sparkdream/storage/v1/requests_by_operator/"+testOperator:
		if f.behind != nil {
			return reply(map[string]any{"requests": []any{}})
		}
		return list(func(r *request) bool { return r.Operator == testOperator && r.Status == statusClaimed })
	case base == "/sparkdream/storage/v1/requests_by_status/arweave/"+statusPending:
		return list(func(r *request) bool { return r.Status == statusPending })
	case strings.HasPrefix(base, "/sparkdream/storage/v1/request/"):
		var id int64
		if _, err := fmt.Sscan(strings.TrimPrefix(base, "/sparkdream/storage/v1/request/"), &id); err != nil {
			return err
		}
		r, ok := f.requests[id]
		if f.behind != nil {
			r, ok = f.behind[id]
		}
		if !ok {
			return fmt.Errorf("sdaptx: get %s: status 404: request not found", path)
		}
		return reply(map[string]any{"request": f.reqJSON(r)})
	case strings.HasPrefix(base, "/sparkdream/storage/v1/removed_hash/"):
		h := strings.TrimPrefix(base, "/sparkdream/storage/v1/removed_hash/")
		if !f.flaggedH[h] {
			return fmt.Errorf("sdaptx: get %s: status 404: hash is not flagged", path)
		}
		return reply(map[string]any{"removed_hash": map[string]any{"sha256": h, "flags": []map[string]any{{"scanner": testScanner}}}})
	case base == "/sparkdream/storage/v1/approved_scanners":
		return reply(map[string]any{"scanners": []map[string]any{
			{"scanner": testScanner, "stale": false},
			{"scanner": "sprkdrm1stale", "stale": true},
		}})
	case base == "/sparkdream/service/v1/operator/"+testScanner+"/content-scanner":
		return reply(map[string]any{"operator": map[string]any{"status": "OPERATOR_STATUS_ACTIVE", "metadata": f.scanMeta}})
	case base == "/sparkdream/service/v1/service_type/content-scanner":
		return reply(map[string]any{"config": map[string]any{"attestation_quorum": f.quorum, "elevated_attestation_quorum": 0}})
	}
	return fmt.Errorf("sdaptx: get %s: status 501: no such route in the test", path)
}

// send applies a worker message the way the chain would.
func (f *fakeChain) send(_ context.Context, msg sdk.Msg) error {
	name := fmt.Sprintf("%T", msg)
	if err := f.failSend[name]; err != nil {
		return err
	}
	f.sent = append(f.sent, msg)
	switch m := msg.(type) {
	case *storagetypes.MsgClaimRequest:
		r := f.requests[int64(m.RequestId)]
		if r.Status != statusPending {
			return fmt.Errorf("request is not pending")
		}
		r.Status, r.Operator = statusClaimed, m.Operator
		r.ClaimExpiresAt = num(f.height + 20)
		if r.Scanned {
			r.RequiredAttestations = num(f.quorum)
		}
	case *storagetypes.MsgExtendClaim:
		r := f.requests[int64(m.RequestId)]
		if r.ClaimExtended {
			return fmt.Errorf("claim was already extended")
		}
		r.ClaimExtended = true
		r.ClaimExpiresAt += 20
	case *storagetypes.MsgCompleteRequest:
		f.requests[int64(m.RequestId)].Status = "REQUEST_STATUS_COMPLETED"
	case *storagetypes.MsgDeclineRequest:
		r := f.requests[int64(m.RequestId)]
		r.Status, r.Operator = "REQUEST_STATUS_REJECTED", ""
	}
	return nil
}

// fakeGateway is an Arweave gateway that accepts valid transactions and
// confirms them when told to.
type fakeGateway struct {
	mu            sync.Mutex
	txs           map[string]*arweave.Transaction
	data          map[string][]byte
	confirmations map[string]int64
	submits       int
	lost          map[string]bool // ids the gateway pretends never to have seen
	serveWrong    bool
}

func newFakeGateway() *fakeGateway {
	return &fakeGateway{txs: map[string]*arweave.Transaction{}, data: map[string][]byte{}, confirmations: map[string]int64{}, lost: map[string]bool{}}
}

func (g *fakeGateway) Anchor(context.Context) (string, error) {
	return base64.RawURLEncoding.EncodeToString(make([]byte, 48)), nil
}
func (g *fakeGateway) Price(_ context.Context, size int) (string, error) {
	return fmt.Sprint(1000 + size), nil
}
func (g *fakeGateway) Submit(_ context.Context, tx *arweave.Transaction) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := tx.Verify(); err != nil {
		return err
	}
	data, err := base64.RawURLEncoding.DecodeString(tx.Data)
	if err != nil || len(data) == 0 {
		return fmt.Errorf("transaction carries no data")
	}
	root := arweave.DataRoot(data)
	if base64.RawURLEncoding.EncodeToString(root[:]) != tx.DataRoot {
		return fmt.Errorf("data does not match the data root")
	}
	g.submits++
	delete(g.lost, tx.ID)
	cp := *tx
	g.txs[tx.ID] = &cp
	g.data[tx.ID] = data
	return nil
}
func (g *fakeGateway) Confirmations(_ context.Context, id string) (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.txs[id]; !ok || g.lost[id] {
		return 0, arweave.ErrNotFound
	}
	return g.confirmations[id], nil
}
func (g *fakeGateway) Data(_ context.Context, id string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	d, ok := g.data[id]
	if !ok {
		return nil, arweave.ErrNotFound
	}
	if g.serveWrong {
		return []byte("something else"), nil
	}
	return d, nil
}
func (g *fakeGateway) confirmAll(n int64) {
	for id := range g.txs {
		g.confirmations[id] = n
	}
}

func tagsOf(tx *arweave.Transaction) map[string]string {
	out := map[string]string{}
	for _, tag := range tx.Tags {
		n, _ := base64.RawURLEncoding.DecodeString(tag.Name)
		v, _ := base64.RawURLEncoding.DecodeString(tag.Value)
		out[string(n)] = string(v)
	}
	return out
}

type rig struct {
	t       *testing.T
	w       *Worker
	chain   *fakeChain
	gateway *fakeGateway
	sources map[string][]byte
	feed    []contentscan.Attestation
	key     ed25519.PrivateKey
	now     time.Time
	// who signs the verdicts, and for which chain
	scanner, chainID string
}

var (
	rigWalletOnce sync.Once
	rigWallet     *arweave.Wallet
)

func newRig(t *testing.T) *rig {
	t.Helper()
	rigWalletOnce.Do(func() {
		w, err := arweave.GenerateWallet(2048)
		if err != nil {
			t.Fatal(err)
		}
		rigWallet = w
	})
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, 32))
	meta, err := contentscan.OperatorMetadata{V: 1, VerdictKey: base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)), FeedURL: "https://feeds.example/aurora"}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{
		t: t, key: key, scanner: testScanner, chainID: testChainID, sources: map[string][]byte{}, gateway: newFakeGateway(),
		now: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC),
		chain: &fakeChain{t: t, height: 500, requests: map[int64]*request{}, flaggedH: map[string]bool{}, canClaim: true, quorum: 1,
			scanMeta: base64.StdEncoding.EncodeToString(meta), failSend: map[string]error{}},
	}
	r.w = &Worker{
		cfg: Config{Backend: "arweave", ChainID: testChainID, MinConfirmations: 10, MaxPerPass: 5,
			ResendAfter: 20 * time.Minute, MaxFeedBytes: 1 << 20, StateFile: filepath.Join(t.TempDir(), "state.json")},
		chain: r.chain, send: r.chain.send, operator: testOperator, gateway: r.gateway, wallet: rigWallet,
		now: func() time.Time { return r.now },
		fetch: func(_ context.Context, u string, maxBytes int64) ([]byte, error) {
			switch {
			case u == "https://feeds.example/aurora/manifest.json":
				return json.Marshal(contentscan.Manifest{V: 1, Worker: r.scanner, LatestSegment: 1, Count: len(r.feed)})
			case u == "https://feeds.example/aurora/segments/000001.jsonl":
				var buf bytes.Buffer
				for _, a := range r.feed {
					line, _ := json.Marshal(a)
					buf.Write(line)
					buf.WriteByte('\n')
				}
				return buf.Bytes(), nil
			}
			d, ok := r.sources[u]
			if !ok {
				return nil, fmt.Errorf("unreachable %s", u)
			}
			if int64(len(d)) > maxBytes {
				return nil, fmt.Errorf("too large")
			}
			return d, nil
		},
	}
	if err := r.w.loadState(); err != nil {
		t.Fatal(err)
	}
	return r
}

// request adds a pending request for data served at one source.
func (r *rig) request(id int64, data []byte, served bool) *request {
	url := fmt.Sprintf("https://files.example/%d.png", id)
	if served {
		r.sources[url] = data
	}
	req := &request{
		ID: num(id), Requester: "sprkdrm1requester", TrustLevel: 2, Backend: "arweave",
		Sha256: contentscan.SHA256Hex(data), SizeBytes: num(len(data)), ContentType: "image/png", Scanned: true,
		Status: statusPending, OpenedAt: 400, Deadline: 900,
	}
	req.Sources = append(req.Sources, struct {
		URL     string `json:"url"`
		AddedAt num    `json:"added_at"`
	}{URL: url, AddedAt: 400})
	r.chain.requests[id] = req
	return req
}

// verdict publishes a signed verdict for a request in the scanner's feed.
func (r *rig) verdict(req *request, verdict string, scannedHeight int64) contentscan.Attestation {
	a := contentscan.Attestation{
		V: contentscan.AttestationVersion, Worker: r.scanner, Verdict: verdict, Category: contentscan.CategoryNone,
		Ruleset: contentscan.Ruleset{Version: "test"}, ScannedHeight: scannedHeight,
		IssuedAt: r.now.Add(time.Duration(len(r.feed)) * time.Second),
	}
	if verdict != contentscan.VerdictClean {
		a.Category = contentscan.CategorySexual
	}
	a.Subject.ChainID = r.chainID
	a.Subject.Record = contentscan.StorageRecord(uint64(req.ID))
	a.Subject.SHA256 = req.Sha256
	signed, err := contentscan.Sign(a, r.key)
	if err != nil {
		r.t.Fatal(err)
	}
	r.feed = append(r.feed, signed)
	return signed
}

func (r *rig) pass() {
	r.t.Helper()
	if err := r.w.pass(context.Background()); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) sentTypes() string {
	var names []string
	for _, m := range r.chain.sent {
		names = append(names, strings.TrimPrefix(fmt.Sprintf("%T", m), "*types.Msg"))
	}
	return strings.Join(names, ",")
}

// The whole life of a request the worker stores: claim after a clean
// verdict, post the file and the verdicts, wait for confirmations, extend
// the claim near its end, complete once the gateway serves the bytes.
func TestWorkerStoresARequest(t *testing.T) {
	r := newRig(t)
	data := testPNG(t, 10, 255)
	req := r.request(1, data, true)
	att := r.verdict(req, contentscan.VerdictClean, 450)

	r.pass()
	if got := r.sentTypes(); got != "ClaimRequest" {
		t.Fatalf("first pass sent %q", got)
	}
	if r.gateway.submits != 2 {
		t.Fatalf("want the file and the verdicts posted, got %d transactions", r.gateway.submits)
	}
	up := r.w.state.Uploads["1"]
	if up == nil || up.File == nil || up.Verdicts == nil || up.File.Data != "" || up.Verdicts.Data != "" {
		t.Fatalf("notes must hold both signed transactions without their data: %+v", up)
	}
	fileTags := tagsOf(r.gateway.txs[up.File.ID])
	if fileTags["App-Name"] != "SparkDream" || fileTags["SHA-256"] != req.Sha256 || fileTags["Content-Type"] != "image/png" {
		t.Fatalf("file tags: %v", fileTags)
	}
	vTags := tagsOf(r.gateway.txs[up.Verdicts.ID])
	if vTags["Type"] != "verdicts" || vTags["Request-Id"] != "1" || vTags["SHA-256"] != req.Sha256 {
		t.Fatalf("verdicts tags: %v", vTags)
	}
	// What was published is the signed verdict, and its hash is what the
	// completion will list: anyone can check one against the other.
	var published []contentscan.Attestation
	if err := json.Unmarshal(r.gateway.data[up.Verdicts.ID], &published); err != nil || len(published) != 1 {
		t.Fatalf("published verdicts: %v %s", err, r.gateway.data[up.Verdicts.ID])
	}
	wantHash, _ := contentscan.AttestationHash(att)
	if gotHash, _ := contentscan.AttestationHash(published[0]); gotHash != wantHash || up.Attestations[0] != wantHash {
		t.Fatalf("attestation hash: published %s, listed %v, want %s", gotHash, up.Attestations, wantHash)
	}
	if !bytes.Equal(r.gateway.data[up.File.ID], data) {
		t.Fatal("the posted file is not the requested bytes")
	}

	// Not confirmed yet: nothing more is sent, nothing is posted again.
	r.chain.height = 505
	r.pass()
	if got := r.sentTypes(); got != "ClaimRequest" || r.gateway.submits != 2 {
		t.Fatalf("while pending: sent %q, %d submits", got, r.gateway.submits)
	}
	// Near the end of the claim, still short of confirmations: extend, once.
	r.gateway.confirmAll(3)
	r.chain.height = 516
	r.pass()
	r.pass()
	if got := r.sentTypes(); got != "ClaimRequest,ExtendClaim" {
		t.Fatalf("near the claim's end: sent %q", got)
	}
	// Confirmed and served: complete.
	r.gateway.confirmAll(10)
	r.pass()
	if got := r.sentTypes(); got != "ClaimRequest,ExtendClaim,CompleteRequest" {
		t.Fatalf("after confirmation: sent %q", got)
	}
	done := r.chain.sent[2].(*storagetypes.MsgCompleteRequest)
	if done.Locator != up.File.ID || done.VerdictsLocator != up.Verdicts.ID || len(done.Attestations) != 1 || done.Attestations[0] != wantHash {
		t.Fatalf("completion: %+v", done)
	}
	if storagetypes.ValidateLocator(storagetypes.LocatorFormat_LOCATOR_FORMAT_ARWEAVE_TX, done.Locator) != nil ||
		storagetypes.ValidateHash(done.Attestations[0]) != nil {
		t.Fatalf("the chain would refuse this completion: %+v", done)
	}
	// The next pass forgets the notes of a request it no longer holds.
	r.pass()
	if len(r.w.state.Uploads) != 0 {
		t.Fatalf("notes kept after completion: %v", r.w.state.Uploads)
	}
	if r.gateway.submits != 2 {
		t.Fatalf("posted %d transactions for one request", r.gateway.submits)
	}
}

// What the worker will not claim.
func TestWorkerWaitsOrDeclines(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *rig) *request
		want  string // messages sent
	}{
		{"no verdict yet", func(r *rig) *request { return r.request(1, testPNG(t, 1, 255), true) }, ""},
		{"held", func(r *rig) *request {
			req := r.request(1, testPNG(t, 2, 255), true)
			r.verdict(req, contentscan.VerdictHeld, 450)
			return req
		}, ""},
		{"removed after clean", func(r *rig) *request {
			req := r.request(1, testPNG(t, 3, 255), true)
			r.verdict(req, contentscan.VerdictClean, 450)
			r.verdict(req, contentscan.VerdictRemoved, 460)
			return req
		}, ""},
		{"verdict from before the request reopened", func(r *rig) *request {
			req := r.request(1, testPNG(t, 4, 255), true)
			r.verdict(req, contentscan.VerdictClean, 399) // opened_at is 400
			return req
		}, ""},
		{"verdict for another request with the same file", func(r *rig) *request {
			req := r.request(1, testPNG(t, 5, 255), true)
			other := *req
			other.ID = 7
			r.verdict(&other, contentscan.VerdictClean, 450)
			return req
		}, ""},
		{"quorum of two, one scanner", func(r *rig) *request {
			req := r.request(1, testPNG(t, 6, 255), true)
			r.verdict(req, contentscan.VerdictClean, 450)
			r.chain.quorum = 2
			return req
		}, ""},
		{"flagged hash", func(r *rig) *request {
			req := r.request(1, testPNG(t, 7, 255), true)
			r.verdict(req, contentscan.VerdictClean, 450)
			r.chain.flaggedH[req.Sha256] = true
			return req
		}, ""},
		{"no source serves it", func(r *rig) *request {
			req := r.request(1, testPNG(t, 8, 255), false)
			r.verdict(req, contentscan.VerdictClean, 450)
			return req
		}, ""},
		{"a source serves other bytes", func(r *rig) *request {
			req := r.request(1, testPNG(t, 9, 255), false)
			r.sources["https://files.example/1.png"] = testPNG(t, 99, 255)
			r.verdict(req, contentscan.VerdictClean, 450)
			return req
		}, ""},
		{"barred from the request", func(r *rig) *request {
			req := r.request(1, testPNG(t, 10, 255), true)
			r.verdict(req, contentscan.VerdictClean, 450)
			req.Barred = []string{testOperator}
			return req
		}, ""},
		{"its own request", func(r *rig) *request {
			req := r.request(1, testPNG(t, 11, 255), true)
			r.verdict(req, contentscan.VerdictClean, 450)
			req.Requester = testOperator
			return req
		}, ""},
		{"cannot claim now", func(r *rig) *request {
			req := r.request(1, testPNG(t, 12, 255), true)
			r.verdict(req, contentscan.VerdictClean, 450)
			r.chain.canClaim = false
			return req
		}, ""},
		{"wrong declared size", func(r *rig) *request {
			req := r.request(1, testPNG(t, 13, 255), true)
			req.SizeBytes = 1
			return req
		}, "DeclineRequest"},
		{"outside the file profile", func(r *rig) *request {
			return r.request(1, testPNG(t, 14, 0), true) // transparent
		}, "DeclineRequest"},
		{"not the declared type", func(r *rig) *request {
			req := r.request(1, []byte("%PDF-1.4 not an image at all"), true)
			return req
		}, "DeclineRequest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			tc.setup(r)
			r.pass()
			if got := r.sentTypes(); got != tc.want {
				t.Fatalf("sent %q, want %q", got, tc.want)
			}
			if r.gateway.submits != 0 {
				t.Fatalf("posted %d transactions", r.gateway.submits)
			}
			if tc.want == "DeclineRequest" {
				if d := r.chain.sent[0].(*storagetypes.MsgDeclineRequest); d.Reason != storagetypes.DeclineReason_DECLINE_REASON_MISMATCH {
					t.Fatalf("decline reason %s", d.Reason)
				}
			}
		})
	}
}

// An unreachable request is left to expire unless the operator opts in, and
// then only after the chain's retry period.
func TestWorkerUnreachableDecline(t *testing.T) {
	r := newRig(t)
	r.request(1, testPNG(t, 1, 255), false)
	r.w.cfg.DeclineUnreachable = true
	r.chain.height = 402 // sources added at 400, min_unreachable_blocks 3
	r.pass()
	if got := r.sentTypes(); got != "" {
		t.Fatalf("declined too early: %q", got)
	}
	r.chain.height = 403
	r.pass()
	if got := r.sentTypes(); got != "DeclineRequest" {
		t.Fatalf("sent %q", got)
	}
	if d := r.chain.sent[0].(*storagetypes.MsgDeclineRequest); d.Reason != storagetypes.DeclineReason_DECLINE_REASON_UNREACHABLE {
		t.Fatalf("decline reason %s", d.Reason)
	}
}

// A restart resends the same transactions and never pays for a second
// upload: the notes hold the signed transactions, and a transaction the
// network lost goes out again under the same id.
func TestWorkerRestartDoesNotPayTwice(t *testing.T) {
	r := newRig(t)
	req := r.request(1, testPNG(t, 20, 255), true)
	r.verdict(req, contentscan.VerdictClean, 450)
	r.pass()
	first := r.w.state.Uploads["1"]
	fileID, verdictsID := first.File.ID, first.Verdicts.ID

	// A new process with the same state file.
	raw, err := os.ReadFile(r.w.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"data":"`+arweave.EncodeData(r.gateway.data[fileID])) {
		t.Fatal("the state file holds the file's bytes")
	}
	again := *r.w
	again.state = state{}
	if err := again.loadState(); err != nil {
		t.Fatal(err)
	}
	r.w = &again

	// The gateway still knows both: nothing is posted again.
	r.pass()
	if r.gateway.submits != 2 {
		t.Fatalf("a restart posted again: %d submits", r.gateway.submits)
	}
	// The network lost the file transaction. It is left alone for a while
	// (it may only be slow to appear), then sent again under the same id.
	r.gateway.lost[fileID] = true
	r.pass()
	if r.gateway.submits != 2 {
		t.Fatal("resent before the wait was over")
	}
	r.now = r.now.Add(21 * time.Minute)
	r.pass()
	if r.gateway.submits != 3 || len(r.gateway.txs) != 2 {
		t.Fatalf("want the same transaction sent again: %d submits, %d distinct transactions", r.gateway.submits, len(r.gateway.txs))
	}
	if _, ok := r.gateway.txs[fileID]; !ok || r.w.state.Uploads["1"].File.ID != fileID || r.w.state.Uploads["1"].Verdicts.ID != verdictsID {
		t.Fatal("the resent transaction has a new id: it would be paid for twice")
	}

	r.gateway.confirmAll(12)
	r.pass()
	if got := r.sentTypes(); got != "ClaimRequest,CompleteRequest" {
		t.Fatalf("sent %q", got)
	}
}

// A node that is behind the claim shows the request as unclaimed, or not at
// all. The note of the upload must survive that: without it the next pass
// would sign and pay for the same file again.
func TestWorkerKeepsItsNotesWhenTheNodeIsBehind(t *testing.T) {
	r := newRig(t)
	req := r.request(1, testPNG(t, 20, 255), true)
	r.verdict(req, contentscan.VerdictClean, 450)
	r.pass()
	fileID := r.w.state.Uploads["1"].File.ID
	r.chain.canClaim = false // look only at what the worker holds

	// Behind the claim: the request is still pending there.
	before := *req
	before.Status, before.Operator, before.ClaimExpiresAt = statusPending, "", 0
	r.chain.behind = map[int64]*request{1: &before}
	r.pass()
	// Behind the request itself: not found.
	r.chain.behind = map[int64]*request{}
	r.pass()
	// Showing an earlier attempt, claimed by this operator then.
	earlier := *req
	earlier.OpenedAt = 300
	r.chain.behind = nil
	r.chain.requests[1] = &earlier
	r.pass()
	r.chain.requests[1] = req

	r.pass()
	up := r.w.state.Uploads["1"]
	if up == nil || up.File.ID != fileID || r.gateway.submits != 2 || len(r.gateway.txs) != 2 {
		t.Fatalf("a node that was behind cost a second upload: note %v, %d submits, %d transactions", up != nil, r.gateway.submits, len(r.gateway.txs))
	}
	raw, err := os.ReadFile(r.w.cfg.StateFile)
	if err != nil || !strings.Contains(string(raw), fileID) {
		t.Fatalf("the saved notes lost the upload: %v", err)
	}
}

// The note is dropped once the chain shows the attempt is over.
func TestWorkerForgetsFinishedAttempts(t *testing.T) {
	for name, end := range map[string]func(r *rig, req *request){
		"lapsed": func(_ *rig, req *request) {
			req.Status, req.Operator, req.Barred = statusPending, "", []string{testOperator}
		},
		"flagged while claimed": func(_ *rig, req *request) { req.Status = "REQUEST_STATUS_REJECTED" },
		"voided and reopened": func(_ *rig, req *request) {
			req.Status, req.Operator, req.OpenedAt = statusPending, "", 600
		},
		"pruned long ago": func(r *rig, _ *request) {
			delete(r.chain.requests, 1)
			r.now = r.now.Add(forgetGoneAfter + time.Hour)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			req := r.request(1, testPNG(t, 20, 255), true)
			r.verdict(req, contentscan.VerdictClean, 450)
			r.pass()
			if r.w.state.Uploads["1"] == nil {
				t.Fatal("no note after the upload")
			}
			r.chain.canClaim = false
			end(r, req)
			r.pass()
			if r.w.state.Uploads["1"] != nil {
				t.Fatal("the note of a finished attempt was kept")
			}
		})
	}
}

// A confirmed transaction is not enough: the gateway must serve the bytes,
// and the verdict must still be clean.
func TestWorkerDoesNotCompleteABadCopy(t *testing.T) {
	r := newRig(t)
	req := r.request(1, testPNG(t, 30, 255), true)
	r.verdict(req, contentscan.VerdictClean, 450)
	r.pass()
	r.gateway.confirmAll(10)

	r.gateway.serveWrong = true
	r.pass()
	if got := r.sentTypes(); got != "ClaimRequest" {
		t.Fatalf("completed a copy the gateway does not serve: %q", got)
	}
	r.gateway.serveWrong = false

	// The scanner turned its verdict to held after the claim.
	r.verdict(req, contentscan.VerdictHeld, 470)
	r.pass()
	if got := r.sentTypes(); got != "ClaimRequest" {
		t.Fatalf("completed without a clean verdict: %q", got)
	}

	// The hash is flagged: the chain closes the request; the worker stops.
	r.chain.flaggedH[req.Sha256] = true
	r.pass()
	if got := r.sentTypes(); got != "ClaimRequest" {
		t.Fatalf("acted on a flagged request: %q", got)
	}
}

// A claim that the chain refuses (another operator got there first) posts
// nothing to Arweave.
func TestWorkerRefusedClaimPostsNothing(t *testing.T) {
	r := newRig(t)
	req := r.request(1, testPNG(t, 40, 255), true)
	r.verdict(req, contentscan.VerdictClean, 450)
	r.chain.failSend["*types.MsgClaimRequest"] = fmt.Errorf("request is not in a status that allows this")
	r.pass()
	if r.gateway.submits != 0 || len(r.w.state.Uploads) != 0 {
		t.Fatalf("posted after a refused claim: %d submits", r.gateway.submits)
	}
}

// A file larger than one Arweave transaction carries is never claimed.
func TestWorkerLeavesWhatItCannotPost(t *testing.T) {
	r := newRig(t)
	// Noise does not compress: 2000 x 2000 opaque pixels make about 12 MiB.
	img := image.NewNRGBA(image.Rect(0, 0, 2000, 2000))
	rand.New(rand.NewSource(1)).Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	big := buf.Bytes()
	if len(big) <= arweave.MaxInlineData {
		t.Fatalf("the test file is only %d bytes", len(big))
	}
	req := r.request(1, big, true)
	r.verdict(req, contentscan.VerdictClean, 450)
	// The type's max_size in the fake chain is 1 MiB; raise what a fetch
	// may return by serving the request through a larger limit.
	fetch := r.w.fetch
	r.w.fetch = func(ctx context.Context, u string, _ int64) ([]byte, error) { return fetch(ctx, u, 1<<40) }
	r.pass()
	if got := r.sentTypes(); got != "" || r.gateway.submits != 0 {
		t.Fatalf("sent %q, %d submits", got, r.gateway.submits)
	}
}

// The worker claims at most max_per_pass requests in a pass.
func TestWorkerMaxPerPass(t *testing.T) {
	r := newRig(t)
	r.w.cfg.MaxPerPass = 2
	for id := int64(1); id <= 4; id++ {
		req := r.request(id, testPNG(t, uint8(50+id), 255), true)
		r.verdict(req, contentscan.VerdictClean, 450)
	}
	r.pass()
	if got := r.sentTypes(); got != "ClaimRequest,ClaimRequest" {
		t.Fatalf("sent %q", got)
	}
}

func TestLoadConfig(t *testing.T) {
	for _, k := range []string{"STW_ARWEAVE_WALLET", "STW_MNEMONIC", "STW_SESSION_KEY_FILE", "STW_GRANTER"} {
		t.Setenv(k, "")
	}
	if _, err := loadConfig(); err == nil {
		t.Fatal("accepted a config with no wallet")
	}
	t.Setenv("STW_ARWEAVE_WALLET", "wallet.json")
	if _, err := loadConfig(); err == nil {
		t.Fatal("accepted a config with no signing key")
	}
	t.Setenv("STW_SESSION_KEY_FILE", "key")
	if _, err := loadConfig(); err == nil {
		t.Fatal("accepted a session key without a granter")
	}
	t.Setenv("STW_GRANTER", "sprkdrm1operator")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	// The default fee and gas meet the networks' minimum gas price (0.025).
	if float64(cfg.Fee)/float64(cfg.Gas) < 0.025 {
		t.Fatalf("default fee %d for %d gas is below the minimum gas price", cfg.Fee, cfg.Gas)
	}
	if cfg.MinConfirmations != 10 || cfg.Backend != "arweave" || cfg.DeclineUnreachable {
		t.Fatalf("defaults: %+v", cfg)
	}
	t.Setenv("STW_MNEMONIC", "words")
	if _, err := loadConfig(); err == nil {
		t.Fatal("accepted both a mnemonic and a session key")
	}
}

// The wallet command creates a wallet file once and reads it back.
func TestWalletCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wallet.json")
	raw, err := rigWalletJWK(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// A gateway that answers, and one that fails: a failure is reported,
	// not fatal.
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "12345") }))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", http.StatusBadGateway) }))
	defer bad.Close()
	for _, u := range []string{good.URL, bad.URL} {
		if err := walletCmd(path, u); err != nil {
			t.Fatal(err)
		}
	}
	// A file that is not a wallet is an error, and is left as it is.
	junk := filepath.Join(t.TempDir(), "junk.json")
	if err := os.WriteFile(junk, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := walletCmd(junk, good.URL); err == nil {
		t.Fatal("accepted a file that is not a wallet")
	}
	if got, _ := os.ReadFile(junk); string(got) != "{}" {
		t.Fatal("overwrote a file that is not a wallet")
	}
}

func rigWalletJWK(t *testing.T) ([]byte, error) {
	newRig(t)
	return rigWallet.JWK()
}

// More scanners may be approved than a completion may list.
func TestCapClean(t *testing.T) {
	clean := make([]contentscan.Attestation, 12)
	for _, tc := range []struct{ limit, want int }{{8, 8}, {12, 12}, {20, 12}, {0, 12}} {
		if got := len(capClean(clean, tc.limit)); got != tc.want {
			t.Errorf("limit %d: kept %d, want %d", tc.limit, got, tc.want)
		}
	}
}
