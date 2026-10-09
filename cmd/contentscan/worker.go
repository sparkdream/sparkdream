package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/contentscan"
	servicetypes "sparkdream/x/service/types"
)

// chainClient is what the worker needs from sdaptx.Client.
type chainClient interface {
	lister
	SignAndBroadcast(ctx context.Context, msgs ...sdk.Msg) (sdaptx.BroadcastResult, error)
}

// Worker follows the chain, judges every subject once (per stored version,
// or per URI until its verdict expires), appends signed verdicts to its
// feed and checkpoints the feed root on chain (docs/content-scanning.md §5).
type Worker struct {
	cfg      Config
	chain    chainClient
	fetch    func(ctx context.Context, rawURL string) ([]byte, error)
	feed     *contentscan.FeedWriter
	detector contentscan.Detector
	key      ed25519.PrivateKey
	operator string
	now      func() time.Time

	// done maps a subject key to when its verdict stops counting (zero:
	// never; a body version or content-addressed reference is immutable).
	done           map[string]time.Time
	lastCheckpoint int64
}

// loadDone rebuilds the done-set from the worker's own feed and its state
// file, so a restart does not re-issue verdicts. The state file holds keys
// the feed cannot reproduce (a reference body's chain hash).
func (w *Worker) loadDone(atts []contentscan.Attestation) {
	for _, a := range atts {
		for _, k := range attestationKeys(a) {
			w.done[k] = a.ExpiresAt
		}
	}
	if w.cfg.StateFile == "" {
		return
	}
	raw, err := os.ReadFile(w.cfg.StateFile)
	if err != nil {
		return
	}
	var st workerState
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	for k, exp := range st.Done {
		w.done[k] = exp
	}
	w.lastCheckpoint = st.LastCheckpoint
}

// workerState is the worker's private bookkeeping (never published: the
// feed directory is public, this file is not).
type workerState struct {
	Done           map[string]time.Time `json:"done"`
	LastCheckpoint int64                `json:"last_checkpoint"`
}

func (w *Worker) saveState() error {
	if w.cfg.StateFile == "" {
		return nil
	}
	b, err := json.Marshal(workerState{Done: w.done, LastCheckpoint: w.lastCheckpoint})
	if err != nil {
		return err
	}
	tmp := w.cfg.StateFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, w.cfg.StateFile)
}

// attestationKeys mirrors subject.key for an issued attestation.
func attestationKeys(a contentscan.Attestation) []string {
	var keys []string
	switch {
	case a.Subject.URI != "":
		keys = append(keys, "uri#"+a.Subject.URI)
	case a.Subject.Record != "":
		keys = append(keys, a.Subject.Record+"#"+a.Subject.SHA256)
		if a.Subject.CID != "" {
			keys = append(keys, a.Subject.Record+"#"+a.Subject.CID)
		}
	}
	return keys
}

func (w *Worker) isDone(s subject) bool {
	exp, ok := w.done[s.key()]
	if !ok {
		return false
	}
	// Re-scan a URI once 90% of its verdict lifetime has passed, so a fresh
	// verdict is out before the old one lapses.
	if !exp.IsZero() && w.now().After(exp.Add(-w.cfg.URITTL/10)) {
		return false
	}
	return true
}

// latestHeight reads the chain's latest block height.
func (w *Worker) latestHeight(ctx context.Context) (int64, error) {
	var out struct {
		Block struct {
			Header struct {
				Height string `json:"height"`
			} `json:"header"`
		} `json:"block"`
	}
	if err := w.chain.GetJSON(ctx, "/cosmos/base/tendermint/v1beta1/blocks/latest", &out); err != nil {
		return 0, err
	}
	return strconv.ParseInt(out.Block.Header.Height, 10, 64)
}

// poll runs one scan pass.
func (w *Worker) poll(ctx context.Context) error {
	height, err := w.latestHeight(ctx)
	if err != nil {
		return fmt.Errorf("latest height: %w", err)
	}
	queue, err := w.collect(ctx)
	if err != nil {
		return err
	}

	var atts []contentscan.Attestation
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if w.isDone(s) {
			continue
		}
		a, extra, err := w.judge(ctx, s, height)
		queue = append(queue, extra...)
		if errors.Is(err, contentscan.ErrNoClassifier) {
			continue // stays unchecked: no clean verdict without a classifier
		}
		if err != nil {
			log.Printf("contentscan: %s %s: %v", s.kind, firstNonEmpty(s.record, s.uri), err)
			continue // retried next poll
		}
		signed, err := contentscan.Sign(a, w.key)
		if err != nil {
			return err
		}
		atts = append(atts, signed)
		for _, k := range append(attestationKeys(signed), s.key()) {
			w.done[k] = signed.ExpiresAt
		}
	}
	if len(atts) > 0 {
		if err := w.feed.Append(atts...); err != nil {
			return err
		}
		log.Printf("contentscan: issued %d verdicts at height %d", len(atts), height)
	}
	if err := w.feed.SetScannedHeight(height); err != nil {
		return err
	}
	cpErr := w.maybeCheckpoint(ctx, height)
	if err := w.saveState(); err != nil {
		return err
	}
	return cpErr
}

// maybeCheckpoint submits MsgSubmitCheckpoint every CheckpointEvery blocks.
func (w *Worker) maybeCheckpoint(ctx context.Context, height int64) error {
	if w.cfg.CheckpointEvery <= 0 || height-w.lastCheckpoint < w.cfg.CheckpointEvery {
		return nil
	}
	root := w.feed.Root()
	res, err := w.chain.SignAndBroadcast(ctx, &servicetypes.MsgSubmitCheckpoint{
		Operator:    w.operator,
		ServiceType: servicetypes.ContentScannerServiceType,
		Height:      height,
		Root:        root[:],
	})
	if err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	if res.Code != 0 {
		return fmt.Errorf("checkpoint rejected: code %d %s", res.Code, res.RawLog)
	}
	w.lastCheckpoint = height
	log.Printf("contentscan: checkpoint height=%d root=%s tx=%s", height, hex.EncodeToString(root[:]), res.TxHash)
	return nil
}

// base builds the unsigned attestation skeleton.
func (w *Worker) base(height int64) contentscan.Attestation {
	return contentscan.Attestation{
		V:             contentscan.AttestationVersion,
		Worker:        w.operator,
		Ruleset:       w.detector.Ruleset,
		ScannedHeight: height,
		IssuedAt:      w.now().UTC().Truncate(time.Second),
	}
}

// judge fetches, decodes and judges one subject. extra holds URL subjects
// discovered inside a decoded body.
func (w *Worker) judge(ctx context.Context, s subject, height int64) (contentscan.Attestation, []subject, error) {
	a := w.base(height)
	a.Subject.ChainID = w.cfg.ChainID
	a.Subject.Record = s.record

	switch s.kind {
	case kindBody:
		body, chainHash, err := w.readBody(ctx, s)
		if err != nil {
			return a, nil, err
		}
		sum := contentscan.SHA256Hex([]byte(body))
		if chainHash != "" && chainHash != sum {
			return a, nil, fmt.Errorf("body does not match chain body_hash")
		}
		a.Subject.SHA256 = sum
		text, err := contentscan.DecodeBody(s.contentType, body, w.cfg.MaxFetchBytes)
		if err != nil {
			return a, nil, err
		}
		var extra []subject
		for _, u := range contentscan.ExtractURLs(text) {
			extra = append(extra, subject{kind: kindURI, uri: u})
		}
		a.Verdict, a.Category, err = w.detector.Judge(ctx, contentscan.ExtractDataURIs(text, w.cfg.MaxFetchBytes))
		return a, extra, err

	case kindRef:
		id, _, err := w.readBody(ctx, s)
		if err != nil {
			return a, nil, err
		}
		gw := w.cfg.gateway(s.contentType)
		if gw == "" {
			return a, nil, fmt.Errorf("no gateway configured for %s", s.contentType)
		}
		data, err := w.fetch(ctx, strings.ReplaceAll(gw, "{id}", strings.TrimSpace(id)))
		if err != nil {
			return a, nil, err
		}
		a.Subject.CID = strings.TrimSpace(id)
		a.Subject.SHA256 = contentscan.SHA256Hex(data)
		a.Verdict, a.Category, err = w.detector.Judge(ctx, blobsOf(data, w.cfg.MaxFetchBytes))
		return a, nil, err

	default: // kindURI
		target, contentAddressed := w.cfg.resolveURI(s.uri)
		if target == "" {
			return a, nil, fmt.Errorf("unsupported URI scheme")
		}
		data, err := w.fetch(ctx, target)
		if err != nil {
			return a, nil, err
		}
		a.Subject.Record = "" // a URI verdict is keyed by URI and bytes, wherever it appears
		a.Subject.URI = s.uri
		a.Subject.SHA256 = contentscan.SHA256Hex(data)
		if !contentAddressed {
			// The same URL can serve different bytes later (§4.1).
			a.ExpiresAt = a.IssuedAt.Add(w.cfg.URITTL)
		}
		a.Verdict, a.Category, err = w.detector.Judge(ctx, blobsOf(data, w.cfg.MaxFetchBytes))
		return a, nil, err
	}
}

// blobsOf turns fetched bytes into blobs: the bytes themselves, plus any data
// URIs inside them when they are text.
func blobsOf(data []byte, maxBytes int64) []contentscan.Blob {
	mt := http.DetectContentType(data)
	blobs := []contentscan.Blob{{Data: data, MediaType: mt, Source: "fetch"}}
	if !contentscan.IsMedia(mt) {
		blobs = append(blobs, contentscan.ExtractDataURIs(data, maxBytes)...)
	}
	return blobs
}

// readBody reads a body query, returning the body and the chain's body_hash
// (hex, "" when the response has none).
func (w *Worker) readBody(ctx context.Context, s subject) (string, string, error) {
	var resp map[string]json.RawMessage
	if err := w.chain.GetJSON(ctx, s.bodyPath, &resp); err != nil {
		return "", "", err
	}
	var body string
	if raw, ok := resp[s.bodyField]; ok {
		if err := json.Unmarshal(raw, &body); err != nil {
			return "", "", err
		}
	}
	var hashB64 string
	if raw, ok := resp["body_hash"]; ok {
		_ = json.Unmarshal(raw, &hashB64)
	}
	if hashB64 == "" {
		return body, "", nil
	}
	h, err := base64.StdEncoding.DecodeString(hashB64)
	if err != nil {
		return "", "", err
	}
	return body, hex.EncodeToString(h), nil
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
