package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/tools/arweave"
	"sparkdream/tools/contentscan"
	storagetypes "sparkdream/x/storage/types"
)

// Worker is the reference x/storage operator (docs/x-storage-spec.md,
// "Storage operator"). Each pass it moves the requests it has claimed toward
// a completion, then looks at pending requests: it fetches the file, checks
// it, waits for the approved scanners' clean verdict, claims, and posts the
// file and the verdicts to Arweave. It completes only once both
// transactions have enough confirmations and the gateway serves the right
// bytes.
type Worker struct {
	cfg      Config
	chain    chainReader
	send     func(ctx context.Context, msg sdk.Msg) error
	operator string
	fetch    func(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error)
	gateway  gateway
	wallet   *arweave.Wallet
	now      func() time.Time

	state        state
	lastStanding string // the last "cannot claim" reason logged
	// waiting holds the last reason logged for each request the worker is
	// leaving alone, so that a reason is logged once and not on every pass.
	waiting map[int64]string
}

// waits logs why a request is left alone, once per reason.
func (w *Worker) waits(id int64, reason string) {
	if w.waiting == nil {
		w.waiting = map[int64]string{}
	}
	if w.waiting[id] == reason {
		return
	}
	if len(w.waiting) > 10_000 {
		clear(w.waiting)
	}
	w.waiting[id] = reason
	log.Printf("storageworker: request %d: waiting: %s", id, reason)
}

// chainReader is the read side of the chain client.
type chainReader interface {
	GetJSON(ctx context.Context, path string, out any) error
}

// gateway is what the worker needs from an Arweave gateway.
type gateway interface {
	Anchor(ctx context.Context) (string, error)
	Price(ctx context.Context, size int) (string, error)
	Submit(ctx context.Context, tx *arweave.Transaction) error
	Confirmations(ctx context.Context, id string) (int64, error)
	Data(ctx context.Context, id string) ([]byte, error)
}

// scanner is an approved, ACTIVE content scanner and where its feed is.
type scanner struct {
	contentscan.TrustedWorker
	feedURL string
}

// upload is the worker's note of the two transactions it posted for one
// attempt at a request. The signed transactions are kept without their data
// (no media at rest): a transaction that never reached the network is sent
// again, with its data fetched anew, under the same id, so nothing is ever
// paid for twice.
type upload struct {
	OpenedAt     int64                `json:"opened_at"`
	File         *arweave.Transaction `json:"file"`
	Verdicts     *arweave.Transaction `json:"verdicts,omitempty"`
	VerdictsJSON string               `json:"verdicts_json,omitempty"` // the published verdicts (public, small)
	Attestations []string             `json:"attestations,omitempty"`
	PostedAt     time.Time            `json:"posted_at"`
}

// state is the worker's notes, saved after every change.
type state struct {
	Uploads map[string]*upload `json:"uploads"` // by request id
}

func (w *Worker) loadState() error {
	w.state = state{Uploads: map[string]*upload{}}
	raw, err := os.ReadFile(w.cfg.StateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &w.state); err != nil {
		return fmt.Errorf("state file %s: %w", w.cfg.StateFile, err)
	}
	if w.state.Uploads == nil {
		w.state.Uploads = map[string]*upload{}
	}
	return nil
}

func (w *Worker) saveState() error {
	raw, err := json.MarshalIndent(w.state, "", " ")
	if err != nil {
		return err
	}
	// Synced before the rename: the notes are what keeps a restart from
	// paying for a second upload, so a crash must not leave them empty.
	tmp := w.cfg.StateFile + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, w.cfg.StateFile)
}

// forgetGoneAfter is how old the note of a request the chain no longer has
// must be before it is dropped. A request is pruned long after it closed; a
// node that is far behind also answers "not found".
const forgetGoneAfter = 7 * 24 * time.Hour

// over reports whether the chain shows that the attempt a note belongs to
// has ended, so the note is no longer needed. The note is what stops a
// second, paid upload, and the node answering may be behind the one that
// took the claim (a load balancer, a node catching up). So a request that
// merely looks unclaimed proves nothing; only a state that a node behind
// the claim cannot show counts:
//   - a later attempt (opened_at moved forward);
//   - completed or closed in this attempt;
//   - this operator barred from it (a lapse or its own decline).
func (w *Worker) over(ctx context.Context, id string, up *upload) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return true // not a note this worker wrote
	}
	r, err := w.getRequest(ctx, n)
	if err != nil {
		return isNotFound(err) && w.now().Sub(up.PostedAt) > forgetGoneAfter
	}
	switch {
	case int64(r.OpenedAt) != up.OpenedAt:
		return int64(r.OpenedAt) > up.OpenedAt
	case r.Status == statusClaimed && r.Operator == w.operator:
		return false
	case r.Status == statusPending || r.Status == statusClaimed:
		return barred(r, w.operator)
	}
	return true
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "status 404")
}

// pass runs one round: in-flight requests first, then new ones.
func (w *Worker) pass(ctx context.Context) error {
	height, err := w.height(ctx)
	if err != nil {
		return fmt.Errorf("latest height: %w", err)
	}
	var b backend
	{
		var out struct {
			Backend backend `json:"backend"`
		}
		if err := w.chain.GetJSON(ctx, "/sparkdream/storage/v1/backend/"+url.PathEscape(w.cfg.Backend), &out); err != nil {
			return fmt.Errorf("backend %s: %w", w.cfg.Backend, err)
		}
		b = out.Backend
	}
	var params storageParams
	{
		var out struct {
			Params storageParams `json:"params"`
		}
		if err := w.chain.GetJSON(ctx, "/sparkdream/storage/v1/params", &out); err != nil {
			return fmt.Errorf("params: %w", err)
		}
		params = out.Params
	}

	// 1. Requests this operator holds.
	mine, err := w.listRequests(ctx, "/sparkdream/storage/v1/requests_by_operator/"+url.PathEscape(w.operator))
	if err != nil {
		return fmt.Errorf("own requests: %w", err)
	}
	held := map[string]bool{}
	for _, r := range mine {
		if r.Status != statusClaimed {
			continue // completed: nothing left to do but wait for settlement
		}
		held[strconv.FormatInt(int64(r.ID), 10)] = true
		if err := w.advance(ctx, r, b, params, height); err != nil {
			log.Printf("storageworker: request %d: %v", r.ID, err)
		}
	}
	// Forget the notes of attempts that have ended (completed, lapsed,
	// flagged, voided). A request missing from the list above is not enough.
	changed := false
	for id, up := range w.state.Uploads {
		if !held[id] && w.over(ctx, id, up) {
			delete(w.state.Uploads, id)
			changed = true
		}
	}
	if changed {
		if err := w.saveState(); err != nil {
			return err
		}
	}

	// 2. New requests, if this operator may claim.
	var st standing
	if err := w.chain.GetJSON(ctx, "/sparkdream/storage/v1/operator_standing/"+url.PathEscape(w.cfg.Backend)+"/"+url.PathEscape(w.operator), &st); err != nil {
		return fmt.Errorf("standing: %w", err)
	}
	if !st.CanClaim {
		if st.Reason != w.lastStanding {
			log.Printf("storageworker: cannot claim now: %s", st.Reason)
			w.lastStanding = st.Reason
		}
		return nil
	}
	w.lastStanding = ""
	pending, err := w.listRequests(ctx, "/sparkdream/storage/v1/requests_by_status/"+url.PathEscape(w.cfg.Backend)+"/"+statusPending)
	if err != nil {
		return fmt.Errorf("pending requests: %w", err)
	}
	taken := 0
	for _, r := range pending {
		if taken >= w.cfg.MaxPerPass {
			break
		}
		if barred(r, w.operator) || int64(r.Deadline) < height || r.Requester == w.operator {
			continue
		}
		claimed, err := w.consider(ctx, r, b, params, height)
		if err != nil {
			log.Printf("storageworker: request %d: %v", r.ID, err)
		}
		if claimed {
			taken++
		}
	}
	return nil
}

func barred(r request, operator string) bool {
	for _, b := range r.Barred {
		if b == operator {
			return true
		}
	}
	return false
}

// errWait means the request is not ready and nothing is wrong: no clean
// quorum yet, or no source serves the file yet.
var errWait = errors.New("not ready")

// retrieve tries every source and returns the bytes with the requested
// hash. Bytes with another hash prove nothing about the file and are passed
// over. Each source is read up to the type's max_size, not the declared
// size, so that an understated size is seen as a mismatch, not as a
// truncated read.
func (w *Worker) retrieve(ctx context.Context, r request, maxSize int64) ([]byte, bool) {
	for _, s := range r.Sources {
		if s.URL == "" {
			continue
		}
		data, err := w.fetch(ctx, s.URL, maxSize)
		if err != nil {
			continue
		}
		if contentscan.SHA256Hex(data) == r.Sha256 {
			return data, true
		}
	}
	return nil, false
}

// mismatch says why bytes with the requested hash are not what was
// declared ("" when they are): a fact about the file that anyone holding it
// can check.
func mismatch(r request, data []byte) string {
	if int64(len(data)) != int64(r.SizeBytes) {
		return fmt.Sprintf("the file is %d bytes, %d were declared", len(data), r.SizeBytes)
	}
	if sniffed := http.DetectContentType(data); sniffed != r.ContentType {
		return fmt.Sprintf("the file is %s, %s was declared", sniffed, r.ContentType)
	}
	if err := contentscan.FileProfile(r.ContentType, data); err != nil {
		return err.Error()
	}
	return ""
}

// verdicts reads the approved scanners' feeds and combines their verdicts
// for the request. It returns the clean verdicts to publish when the
// combined verdict is clean at the quorum, and errWait otherwise.
func (w *Worker) verdicts(ctx context.Context, r request) ([]contentscan.Attestation, error) {
	scanners, err := w.scanners(ctx)
	if err != nil {
		return nil, err
	}
	quorum, err := w.quorum(ctx, int64(r.TrustLevel))
	if err != nil {
		return nil, err
	}
	if quorum < 1 {
		return nil, fmt.Errorf("the content-scanner quorum is 0: %w", errWait)
	}
	var all []contentscan.Attestation
	var trusted []contentscan.TrustedWorker
	for _, s := range scanners {
		trusted = append(trusted, s.TrustedWorker)
		_, atts, err := contentscan.FetchFeed(ctx, func(ctx context.Context, u string) ([]byte, error) {
			return w.fetch(ctx, u, w.cfg.MaxFeedBytes)
		}, s.feedURL)
		if err != nil {
			log.Printf("storageworker: feed of %s: %v", s.Address, err)
			continue
		}
		all = append(all, atts...)
	}
	res, clean := contentscan.StorageVerdict(all, trusted, w.cfg.ChainID, r.id(), r.Sha256, int64(r.OpenedAt), quorum, w.now())
	if res.Verdict != contentscan.VerdictClean {
		return nil, fmt.Errorf("combined verdict is %s (%d clean of %d needed): %w", res.Verdict, res.CleanVotes, quorum, errWait)
	}
	return clean, nil
}

// consider looks at one pending request and claims it when it can be
// stored. It reports whether it claimed.
func (w *Worker) consider(ctx context.Context, r request, b backend, params storageParams, height int64) (bool, error) {
	maxSize := b.maxSize(r.ContentType)
	if maxSize == 0 {
		return false, nil // the type is no longer listed
	}
	data, ok := w.retrieve(ctx, r, maxSize)
	if !ok {
		if !w.cfg.DeclineUnreachable {
			// left to expire: the request is refunded then
			w.waits(int64(r.ID), "no source serves bytes with the requested hash")
			return false, nil
		}
		var newest int64
		for _, s := range r.Sources {
			newest = max(newest, int64(s.AddedAt))
		}
		if height < newest+int64(params.MinUnreachableBlocks)+w.cfg.UnreachableGraceBlocks {
			return false, nil
		}
		log.Printf("storageworker: request %d: no source serves the file; declining as unreachable", r.ID)
		return false, w.send(ctx, &storagetypes.MsgDeclineRequest{Operator: w.operator, RequestId: r.id(), Reason: storagetypes.DeclineReason_DECLINE_REASON_UNREACHABLE})
	}
	if why := mismatch(r, data); why != "" {
		log.Printf("storageworker: request %d: declining as a mismatch: %s", r.ID, why)
		return false, w.send(ctx, &storagetypes.MsgDeclineRequest{Operator: w.operator, RequestId: r.id(), Reason: storagetypes.DeclineReason_DECLINE_REASON_MISMATCH})
	}
	if len(data) > arweave.MaxInlineData {
		// Never claim what this worker cannot post: a claim it lets lapse
		// counts against the operator.
		w.waits(int64(r.ID), fmt.Sprintf("the file is %d bytes, more than this worker posts in one transaction (%d)", len(data), arweave.MaxInlineData))
		return false, nil
	}
	if w.flagged(ctx, r.Sha256) {
		w.waits(int64(r.ID), "the hash is flagged as removed, or the flag query failed")
		return false, nil
	}
	if r.Scanned {
		if _, err := w.verdicts(ctx, r); err != nil {
			if errors.Is(err, errWait) {
				w.waits(int64(r.ID), err.Error())
				return false, nil
			}
			return false, err
		}
	}
	if err := w.send(ctx, &storagetypes.MsgClaimRequest{Operator: w.operator, RequestId: r.id()}); err != nil {
		return false, fmt.Errorf("claim: %w", err)
	}
	log.Printf("storageworker: claimed request %d (%s, %d bytes)", r.ID, r.Sha256, len(data))
	// The claim is on chain: read it back and start the upload in this pass.
	claimed, err := w.getRequest(ctx, int64(r.ID))
	if err != nil {
		return true, err
	}
	return true, w.advance(ctx, claimed, b, params, height)
}

func tags(r request, extra ...arweave.Tag) []arweave.Tag {
	return append([]arweave.Tag{
		arweave.NewTag("App-Name", "SparkDream"),
		arweave.NewTag("SHA-256", r.Sha256),
	}, extra...)
}

// advance moves a request this operator has claimed one step: post the
// transactions, resend one the network lost, extend the claim, or complete.
func (w *Worker) advance(ctx context.Context, r request, b backend, params storageParams, height int64) error {
	if r.Operator != w.operator || r.Status != statusClaimed {
		return nil
	}
	id := strconv.FormatInt(int64(r.ID), 10)
	up := w.state.Uploads[id]
	if up != nil && up.OpenedAt != int64(r.OpenedAt) {
		if int64(r.OpenedAt) < up.OpenedAt {
			// The node shows an attempt older than the one already posted
			// for: it is behind. Signing again here would pay twice.
			return errors.New("the node answering is behind this worker's notes; waiting")
		}
		up = nil // notes of an earlier attempt at this request
	}
	if w.flagged(ctx, r.Sha256) {
		return nil // the chain closes the request; nothing more to post
	}

	if up == nil {
		data, ok := w.retrieve(ctx, r, b.maxSize(r.ContentType))
		if !ok {
			return errors.New("claimed, but no source serves the file any more; the claim will lapse")
		}
		if why := mismatch(r, data); why != "" {
			return w.send(ctx, &storagetypes.MsgDeclineRequest{Operator: w.operator, RequestId: r.id(), Reason: storagetypes.DeclineReason_DECLINE_REASON_MISMATCH})
		}
		up = &upload{OpenedAt: int64(r.OpenedAt), PostedAt: w.now().UTC()}
		var verdictsData []byte
		if r.Scanned {
			clean, err := w.verdicts(ctx, r)
			if err != nil {
				return fmt.Errorf("claimed, but the clean quorum is gone: %w", err)
			}
			if len(clean) < int(r.RequiredAttestations) {
				return fmt.Errorf("claimed with a quorum of %d, only %d clean verdicts", r.RequiredAttestations, len(clean))
			}
			// The chain refuses a completion with more attestations than
			// its cap, and more scanners than that may be approved.
			clean = capClean(clean, int(params.MaxAttestationsPerCompletion))
			for _, a := range clean {
				h, err := contentscan.AttestationHash(a)
				if err != nil {
					return err
				}
				up.Attestations = append(up.Attestations, h)
			}
			if verdictsData, err = json.Marshal(clean); err != nil {
				return err
			}
			up.VerdictsJSON = string(verdictsData)
		}
		// Sign both, note them, and only then post: a crash between the
		// note and the post resends the same transactions.
		file, err := w.sign(ctx, data, tags(r, arweave.NewTag("Content-Type", r.ContentType)))
		if err != nil {
			return err
		}
		up.File = file
		if verdictsData != nil {
			v, err := w.sign(ctx, verdictsData, tags(r,
				arweave.NewTag("Content-Type", "application/json"),
				arweave.NewTag("Type", "verdicts"),
				arweave.NewTag("Request-Id", id)))
			if err != nil {
				return err
			}
			up.Verdicts = v
		}
		w.state.Uploads[id] = stripped(up)
		if err := w.saveState(); err != nil {
			return err
		}
		if err := w.gateway.Submit(ctx, up.File); err != nil {
			return fmt.Errorf("post file: %w", err)
		}
		if up.Verdicts != nil {
			if err := w.gateway.Submit(ctx, up.Verdicts); err != nil {
				return fmt.Errorf("post verdicts: %w", err)
			}
		}
		log.Printf("storageworker: request %d: posted file %s", r.ID, up.File.ID)
		return nil
	}

	// Already posted: check both transactions.
	ready := true
	for _, part := range []struct {
		name string
		tx   *arweave.Transaction
		data func() ([]byte, bool)
	}{
		{"file", up.File, func() ([]byte, bool) { return w.retrieve(ctx, r, b.maxSize(r.ContentType)) }},
		{"verdicts", up.Verdicts, func() ([]byte, bool) { return []byte(up.VerdictsJSON), up.VerdictsJSON != "" }},
	} {
		if part.tx == nil {
			continue
		}
		n, err := w.gateway.Confirmations(ctx, part.tx.ID)
		switch {
		case errors.Is(err, arweave.ErrNotFound):
			// Never arrived, or dropped before it was mined. Send the same
			// signed transaction again; while it is still in the mempool
			// the gateway may not know it yet, so give it a while first.
			ready = false
			if w.now().Sub(up.PostedAt) < w.cfg.ResendAfter {
				continue
			}
			data, ok := part.data()
			if !ok {
				return fmt.Errorf("%s transaction %s is gone and its data cannot be fetched again", part.name, part.tx.ID)
			}
			again := *part.tx
			again.Data = arweave.EncodeData(data)
			if err := w.gateway.Submit(ctx, &again); err != nil {
				return fmt.Errorf("resend %s: %w", part.name, err)
			}
			up.PostedAt = w.now().UTC()
			if err := w.saveState(); err != nil {
				return err
			}
			log.Printf("storageworker: request %d: sent %s transaction %s again", r.ID, part.name, part.tx.ID)
		case err != nil:
			return fmt.Errorf("%s status: %w", part.name, err)
		case n < w.cfg.MinConfirmations:
			ready = false
		}
	}

	if !ready {
		// The storage network can take longer than the claim. Extend once,
		// near the end; if that runs out too the claim lapses and is not
		// completed.
		if !r.ClaimExtended && int64(r.ClaimExpiresAt)-height <= w.extendMargin(params) {
			if err := w.send(ctx, &storagetypes.MsgExtendClaim{Operator: w.operator, RequestId: r.id()}); err != nil {
				return fmt.Errorf("extend claim: %w", err)
			}
			log.Printf("storageworker: request %d: claim extended", r.ID)
		}
		return nil
	}

	// Confirmed. A transaction that was mined is not proof that the gateway
	// serves the file: check the bytes.
	served, err := w.gateway.Data(ctx, up.File.ID)
	if err != nil {
		return fmt.Errorf("gateway does not serve the copy yet: %w", err)
	}
	if contentscan.SHA256Hex(served) != r.Sha256 {
		return fmt.Errorf("gateway serves other bytes for %s", up.File.ID)
	}
	if r.Scanned {
		// Read the combined verdict once more just before completing: a
		// verdict may have turned since the claim.
		if _, err := w.verdicts(ctx, r); err != nil {
			return fmt.Errorf("not completing: %w", err)
		}
	}
	msg := &storagetypes.MsgCompleteRequest{Operator: w.operator, RequestId: r.id(), Locator: up.File.ID, Attestations: up.Attestations}
	if up.Verdicts != nil {
		msg.VerdictsLocator = up.Verdicts.ID
	}
	if err := w.send(ctx, msg); err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	log.Printf("storageworker: request %d completed: %s", r.ID, up.File.ID)
	return nil
}

// capClean keeps at most limit clean verdicts (all of them when limit is 0).
// Which ones does not matter: each is a clean verdict of an approved
// scanner, and the quorum is checked before this.
func capClean(clean []contentscan.Attestation, limit int) []contentscan.Attestation {
	if limit > 0 && len(clean) > limit {
		return clean[:limit]
	}
	return clean
}

// extendMargin is how close to its end a claim is extended: a quarter of
// the claim window, and at least two blocks.
func (w *Worker) extendMargin(params storageParams) int64 {
	return max(int64(params.ClaimWindowBlocks)/4, 2)
}

// sign builds and signs a data transaction at the gateway's current anchor
// and price.
func (w *Worker) sign(ctx context.Context, data []byte, tags []arweave.Tag) (*arweave.Transaction, error) {
	anchor, err := w.gateway.Anchor(ctx)
	if err != nil {
		return nil, err
	}
	reward, err := w.gateway.Price(ctx, len(data))
	if err != nil {
		return nil, err
	}
	tx, err := arweave.NewDataTx(data, tags, anchor, reward)
	if err != nil {
		return nil, err
	}
	if err := w.wallet.Sign(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// stripped returns the note as it is saved: the signed transactions without
// their data.
func stripped(up *upload) *upload {
	out := *up
	if up.File != nil {
		f := *up.File
		f.Data = ""
		out.File = &f
	}
	if up.Verdicts != nil {
		v := *up.Verdicts
		v.Data = ""
		out.Verdicts = &v
	}
	return &out
}
