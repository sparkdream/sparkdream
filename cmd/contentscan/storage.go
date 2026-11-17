package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"

	"sparkdream/tools/contentscan"
	storagetypes "sparkdream/x/storage/types"
)

// kindStorage is an x/storage request for a permanent copy
// (docs/x-storage-spec.md, docs/content-scanning.md §8.7). The subject is the
// request, not the bare hash: a completion counts only verdicts whose record
// is its own request and that were scanned at or after its opened_at.
const kindStorage = "storage"

type storageBackend struct {
	Name string `json:"name"`
}

type storageRequest struct {
	ID          string `json:"id"`
	Sha256      string `json:"sha256"`
	ContentType string `json:"content_type"`
	Scanned     bool   `json:"scanned"`
	OpenedAt    string `json:"opened_at"`
	Sources     []struct {
		URL string `json:"url"`
	} `json:"sources"`
}

// errNoSource means no source of a storage request served the requested
// bytes. The request stays unchecked and is tried again on the next poll: a
// member may add a working source.
var errNoSource = errors.New("no source served bytes with the requested hash")

// collectStorage lists the scanned storage requests that are still waiting
// for a copy. A chain without the module answers the first query with an
// error; that is logged once per poll and the rest of the scan goes on.
func (w *Worker) collectStorage(ctx context.Context) []subject {
	var out []subject
	var backends []string
	if err := pageAll(ctx, w.chain, "/sparkdream/storage/v1/backends", func(raw json.RawMessage) (int, error) {
		var resp struct {
			Backends []storageBackend `json:"backends"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return 0, err
		}
		for _, b := range resp.Backends {
			backends = append(backends, b.Name)
		}
		return len(resp.Backends), nil
	}); err != nil {
		log.Printf("contentscan: storage backends: %v", err)
		return nil
	}
	w.storageBackends = backends
	for _, b := range backends {
		for _, status := range []string{"REQUEST_STATUS_PENDING", "REQUEST_STATUS_CLAIMED"} {
			path := "/sparkdream/storage/v1/requests_by_status/" + url.PathEscape(b) + "/" + status
			if err := pageAll(ctx, w.chain, path, func(raw json.RawMessage) (int, error) {
				var resp struct {
					Requests []storageRequest `json:"requests"`
				}
				if err := json.Unmarshal(raw, &resp); err != nil {
					return 0, err
				}
				for _, r := range resp.Requests {
					if !r.Scanned {
						continue
					}
					s := subject{
						kind: kindStorage, record: "storage/request/" + r.ID,
						contentType: r.ContentType, wantSHA256: r.Sha256, openedAt: r.OpenedAt,
					}
					for _, src := range r.Sources {
						if src.URL != "" {
							s.sources = append(s.sources, src.URL)
						}
					}
					out = append(out, s)
				}
				return len(resp.Requests), nil
			}); err != nil {
				log.Printf("contentscan: storage requests of %s: %v", b, err)
			}
		}
	}
	return out
}

// judgeStorage judges one storage request. It tries each source until one
// serves bytes with the requested hash; bytes with another hash prove
// nothing about the file and are passed over. A file outside the storage
// profile of its type gets no verdict (ErrUnsupportedMedia): the profile
// guarantees that the pixels judged here are the pixels a viewer sees.
func (w *Worker) judgeStorage(ctx context.Context, s subject, a contentscan.Attestation) (contentscan.Attestation, error) {
	var data []byte
	for _, src := range s.sources {
		got, err := w.fetch(ctx, src)
		if err != nil {
			continue
		}
		if contentscan.SHA256Hex(got) == s.wantSHA256 {
			data = got
			break
		}
	}
	if data == nil {
		return a, errNoSource
	}
	if err := contentscan.FileProfile(s.contentType, data); err != nil {
		return a, fmt.Errorf("%w: %v", contentscan.ErrUnsupportedMedia, err)
	}
	a.Subject.SHA256 = s.wantSHA256
	var err error
	a.Verdict, a.Category, err = w.detector.Judge(ctx, []contentscan.Blob{{Data: data, MediaType: s.contentType, Source: "storage"}})
	return a, err
}

// notFound reports whether a query failed because the chain has no such
// record, as opposed to a node that could not answer.
func notFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "status 404")
}

// hasStorageRequest reports whether a backend holds a live or settled
// request for the hash. An error means the worker could not tell.
func (w *Worker) hasStorageRequest(ctx context.Context, sha256Hex string) (bool, error) {
	if len(w.storageBackends) == 0 {
		// The backend list has not been read yet (the first query after a
		// start failed): "no request" would drop flags that are still owed.
		return false, errors.New("the storage backends are not known yet")
	}
	for _, b := range w.storageBackends {
		var resp struct {
			Request struct {
				ID string `json:"id"`
			} `json:"request"`
		}
		path := "/sparkdream/storage/v1/request_by_hash/" + url.PathEscape(b) + "/" + url.PathEscape(sha256Hex)
		err := w.chain.GetJSON(ctx, path, &resp)
		switch {
		case err == nil && resp.Request.ID != "":
			return true, nil
		case err != nil && !notFound(err):
			return false, err
		}
	}
	return false, nil
}

// flagOnChain reports whether this scanner's flag is recorded on the hash.
func (w *Worker) flagOnChain(ctx context.Context, sha256Hex string) (bool, error) {
	var resp struct {
		RemovedHash struct {
			Flags []struct {
				Scanner string `json:"scanner"`
			} `json:"flags"`
		} `json:"removed_hash"`
	}
	err := w.chain.GetJSON(ctx, "/sparkdream/storage/v1/removed_hash/"+url.PathEscape(sha256Hex), &resp)
	if err != nil {
		if notFound(err) {
			return false, nil
		}
		return false, err
	}
	for _, f := range resp.RemovedHash.Flags {
		if f.Scanner == w.operator {
			return true, nil
		}
	}
	return false, nil
}

// pendingFlag is a flag this scanner owes x/storage: the hash of the signed
// removed verdict, and when the flag may be sent (again).
type pendingFlag struct {
	AttestationHash string    `json:"attestation_hash"`
	Next            time.Time `json:"next"`
}

// flagRetry is how long a sent flag is given to show up on chain before it
// is sent again. A flag the chain refuses for now (the per-window cap, an
// approval not yet given) is retried at this pace too.
const flagRetry = 10 * time.Minute

// flagRemoved notes that x/storage must be told a hash is removed content,
// so no permanent copy of it is made and an existing one stops being pointed
// at. It is called for a removed verdict on a storage request, and for a
// removed verdict on anything else whose bytes a storage request names. The
// flag carries the hash of the signed verdict, so anyone can find it in the
// feed. Nothing is sent here: sendFlags does that, and keeps the note until
// the flag is on chain.
func (w *Worker) flagRemoved(ctx context.Context, s subject, signed contentscan.Attestation) {
	if !w.cfg.Storage || signed.Verdict != contentscan.VerdictRemoved || signed.Subject.SHA256 == "" {
		return
	}
	sha := signed.Subject.SHA256
	if strings.ToLower(sha) != sha || len(sha) != 64 {
		return
	}
	if s.kind != kindStorage {
		// When the worker cannot tell, the note is kept: sendFlags asks again.
		if has, err := w.hasStorageRequest(ctx, sha); err == nil && !has {
			return
		}
	}
	hash, err := contentscan.AttestationHash(signed)
	if err != nil {
		log.Printf("contentscan: flag %s: %v", sha, err)
		return
	}
	if w.flags == nil {
		w.flags = map[string]*pendingFlag{}
	}
	if w.flags[sha] == nil {
		w.flags[sha] = &pendingFlag{AttestationHash: hash}
	}
}

// sendFlags sends the flags this scanner owes and drops each note once the
// flag is read back from the chain. A broadcast that was accepted proves
// nothing: the transaction can still fail in its block, or never be in one,
// and a removed file would then be stored for good. A note is also dropped
// when no request names the hash any more (another scanner's flag closed
// it, or it expired): a later request for the same bytes is a new subject
// and gets its own verdict and flag.
func (w *Worker) sendFlags(ctx context.Context) {
	hashes := make([]string, 0, len(w.flags))
	for sha := range w.flags {
		hashes = append(hashes, sha)
	}
	sort.Strings(hashes)
	for _, sha := range hashes {
		f := w.flags[sha]
		on, err := w.flagOnChain(ctx, sha)
		if err != nil {
			log.Printf("contentscan: flag %s: %v", sha, err)
			continue
		}
		if on {
			log.Printf("contentscan: %s is flagged as removed", sha)
			delete(w.flags, sha)
			continue
		}
		has, err := w.hasStorageRequest(ctx, sha)
		if err != nil {
			log.Printf("contentscan: flag %s: %v", sha, err)
			continue
		}
		if !has {
			delete(w.flags, sha)
			continue
		}
		if w.now().Before(f.Next) {
			continue // sent not long ago: give it time to be in a block
		}
		f.Next = w.now().Add(flagRetry)
		res, err := w.chain.SignAndBroadcast(ctx, &storagetypes.MsgFlagRemoved{
			Scanner:         w.operator,
			Sha256:          sha,
			AttestationHash: f.AttestationHash,
		})
		switch {
		case err != nil:
			log.Printf("contentscan: flag %s: %v", sha, err)
		case res.Code != 0:
			log.Printf("contentscan: flag %s refused: code %d %s", sha, res.Code, res.RawLog)
		default:
			log.Printf("contentscan: sent the flag for %s (tx %s)", sha, res.TxHash)
		}
	}
}
