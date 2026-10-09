// Command sdapverify is the independent verifier runner for the
// federation Mastodon link (phase P6; phases are defined in
// test/federation/mastodon/README.md). A separate binary
// from sdapbridge — and in production a separate ACCOUNT on a separate
// HOST with a separate network vantage point — because independence is
// the whole anti-fraud property: it shares no process, no key, and no
// fetch path with the bridge. It imports tools/apcanon (the shared
// canonicalizer — independence belongs in the fetch, not the encoder)
// and the LCD tx client, nothing else from the bridge.
//
// Loop: poll the chain for PENDING_VERIFICATION content on a peer (the
// P0.4 peer+status filters), re-fetch each content_uri anonymously (or
// signed, if the instance requires it), canonicalize, compare against
// the anchored content_hash, and submit MsgVerifyContent on a match.
//
// On MISMATCH it ALARMS and does not submit: until the dispute path has
// run live (P7) with the P0.1 bond-release fix confirmed, sending
// content to DISPUTED on a bring-up bug strands the verifier's
// committed bond. A mismatch during bring-up is far more likely a
// canonicalizer bug than operator fraud. Alarm via log line + optional
// webhook (ALARM_WEBHOOK) — page a human, let them submit
// MsgVerifyContent with the mismatched hash by hand if it is real.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/apcanon"
	"sparkdream/x/federation/types"
)

func main() {
	cfg := loadConfig()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "sdapverify:", err)
		os.Exit(2)
	}

	chain, err := sdaptx.New(sdaptx.Config{
		LCD:          cfg.LCD,
		ChainID:      cfg.ChainID,
		Bech32Prefix: cfg.Prefix,
		Denom:        cfg.Denom,
		FeeAmount:    cfg.Fee,
		GasLimit:     cfg.Gas,
		Mnemonic:     cfg.Mnemonic,

		SessionKeyFile: cfg.SessionKeyFile,
		Granter:        cfg.Granter,
	})
	if err != nil {
		log.Fatalf("sdapverify: chain client: %v", err)
	}
	if chain.Session() {
		signer := chain.SignerAddress()
		if signer == "" {
			signer = "none yet: waiting for " + cfg.SessionKeyFile
		}
		log.Printf("sdapverify: signing through a session key for %s (key %s)", chain.Address(), signer)
	}

	var signer *apcanon.HTTPSigner
	if cfg.FetchKeyID != "" {
		pem, err := os.ReadFile(cfg.FetchKeyPath)
		if err != nil {
			log.Fatalf("sdapverify: read fetch key: %v", err)
		}
		signer, err = apcanon.LoadHTTPSigner(cfg.FetchKeyID, pem)
		if err != nil {
			log.Fatalf("sdapverify: fetch signer: %v", err)
		}
	}

	// Drop configured peers the chain does not know: listing them would
	// fail every poll, forever. Unlike the bridge, a verifier needs no
	// binding; it only needs the peer to exist.
	bootCtx, cancelBoot := context.WithTimeout(context.Background(), time.Minute)
	cfg.PeerIDs = knownPeers(bootCtx, chain, cfg.PeerIDs)
	cancelBoot()
	if len(cfg.PeerIDs) == 0 {
		log.Fatalf("sdapverify: none of the configured peers exist on chain")
	}

	v := &Verifier{
		cfg:           cfg,
		self:          chain.Address(),
		chain:         chain,
		fetch:         fetchFunc(signer, cfg.AllowPrivateHosts),
		alarmed:       map[string]bool{},
		fetchFailures: map[string]int{},
		digest: func(ctx context.Context, url string) (string, error) {
			return apcanon.FetchMediaDigest(ctx, url, apcanon.FetchOptions{
				AllowPrivateHosts: cfg.AllowPrivateHosts,
				Timeout:           cfg.MediaTimeout,
				MediaMinRate:      cfg.MediaMinRate,
			})
		},
		bodyLimit: func(ctx context.Context) (int, error) {
			var out struct {
				Params struct {
					MaxContentBodySize string `json:"max_content_body_size"`
				} `json:"params"`
			}
			if err := chain.GetJSON(ctx, "/sparkdream/federation/v1/params", &out); err != nil {
				return 0, err
			}
			return strconv.Atoi(out.Params.MaxContentBodySize)
		},
		hosts: func(ctx context.Context, peerID string) ([]string, error) {
			var out struct {
				Policy struct {
					ContentHosts []string `json:"content_hosts"`
				} `json:"policy"`
			}
			err := chain.GetJSON(ctx, "/sparkdream/federation/v1/get_peer_policy/"+url.PathEscape(peerID), &out)
			return out.Policy.ContentHosts, err
		},
		broadcast: func(ctx context.Context, msg *types.MsgVerifyContent) (string, error) {
			msg.Creator = chain.Address()
			for attempt := 0; attempt < 3; attempt++ {
				res, err := chain.SignAndBroadcast(ctx, msg)
				if err != nil {
					return "", err
				}
				if !res.OK() && res.SessionUnusable() {
					return res.TxHash, fmt.Errorf("%w: %s", sdaptx.ErrNoSessionKey, res.RawLog)
				}
				if !res.OK() {
					// Only sequence races and a full mempool are worth a
					// retry. Code 13 is ErrInsufficientFee, which retrying
					// cannot fix — it just burns another attempt.
					if !sdaptx.Retryable(res.Code) {
						return res.TxHash, fmt.Errorf("tx rejected in CheckTx with code %d: %s",
							res.Code, res.RawLog)
					}
				} else {
					// CheckTx passed; confirm inclusion so a DeliverTx
					// rejection (ErrSelfVerification, an expired window, an
					// unbonded verifier) is not read as success.
					done, err := chain.Confirm(ctx, res.TxHash, cfg.ConfirmTimeout)
					if err != nil {
						return res.TxHash, err
					}
					if !done.OK() {
						return res.TxHash, fmt.Errorf("tx %s failed in DeliverTx with code %d: %s",
							res.TxHash, done.Code, done.RawLog)
					}
					return res.TxHash, nil
				}
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
				}
			}
			return "", fmt.Errorf("broadcast kept failing")
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("sdapverify: verifier=%s peers=%v lcd=%s poll=%s",
		v.self, cfg.PeerIDs, cfg.LCD, cfg.PollInterval)
	if err := v.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("sdapverify: %v", err)
	}
	log.Println("sdapverify: stopped")
}

type Config struct {
	LCD     string
	ChainID string
	Prefix  string
	Denom   string
	// PeerIDs are the peers whose pending content this runner verifies.
	PeerIDs []string
	// MediaTimeout and MediaMinRate bound each attachment download for
	// ap-canonical-v2 records (apcanon.FetchMediaDigest). Not part of the
	// rule: a download that runs out of time fails, it is never hashed.
	MediaTimeout time.Duration
	MediaMinRate int64
	Mnemonic     string
	// SessionKeyFile + Granter: sign as an x/session grantee for the
	// verifier account (Granter), whose mnemonic never reaches this host.
	// The file is re-read on every tx, so rotation is a rewrite.
	SessionKeyFile string
	Granter        string
	PollInterval   time.Duration
	// ConfirmTimeout bounds the wait for tx inclusion. SYNC-mode broadcast
	// reports CheckTx only.
	ConfirmTimeout time.Duration
	Fee            int64
	Gas            uint64
	FetchKeyID     string
	FetchKeyPath   string
	AlarmWebhook   string
	// FetchFailureAlarmAt is how many consecutive fetch failures on one
	// content id escalate to an alarm (0 = default).
	FetchFailureAlarmAt int
	// AllowPrivateHosts relaxes apcanon's SSRF guard for a local test
	// instance. Production must leave this false.
	AllowPrivateHosts bool
}

func loadConfig() Config {
	return Config{
		LCD:     env("SDA_LCD", "https://api-dev.sparkdream.io"),
		ChainID: env("SDA_CHAIN_ID", "sparkdream-dev-1"),
		Prefix:  env("SDA_PREFIX", "sprkdrm"),
		Denom:   env("SDA_DENOM", "usparz.sparkdreamdev"),
		// SDA_PEER_IDS is a comma-separated list; SDA_PEER_ID is still read
		// when it is unset.
		PeerIDs:             splitList(env("SDA_PEER_IDS", env("SDA_PEER_ID", ""))),
		MediaTimeout:        envDuration("SDA_MEDIA_TIMEOUT", apcanon.DefaultMediaTimeout),
		MediaMinRate:        envInt("SDA_MEDIA_MIN_RATE", apcanon.DefaultMediaMinRate),
		Mnemonic:            env("SDA_MNEMONIC", ""),
		SessionKeyFile:      env("SDA_SESSION_KEY_FILE", ""),
		Granter:             env("SDA_GRANTER", ""),
		PollInterval:        envDuration("SDA_POLL", 45*time.Second),
		ConfirmTimeout:      envDuration("SDA_CONFIRM_TIMEOUT", 45*time.Second),
		FetchFailureAlarmAt: int(envInt("SDA_FETCH_FAIL_ALARM_AT", defaultFetchFailureAlarmAt)),
		Fee:                 envInt("SDA_FEE", 750000),
		Gas:                 envUint("SDA_GAS", 200000),
		FetchKeyID:          env("SDA_FETCH_KEY_ID", ""),
		FetchKeyPath:        env("SDA_FETCH_KEY", ""),
		AlarmWebhook:        os.Getenv("ALARM_WEBHOOK"),
		// Local test instances only (INSTANCE_SETUP.md); never a real peer.
		AllowPrivateHosts: envBool("SDA_ALLOW_PRIVATE_HOSTS", false),
	}
}

func (c *Config) Validate() error {
	var missing []string
	if len(c.PeerIDs) == 0 {
		missing = append(missing, "SDA_PEER_IDS")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
	if err := sdaptx.CheckSigningConfig(c.Mnemonic, c.SessionKeyFile, c.Granter); err != nil {
		return err
	}
	if (c.FetchKeyID == "") != (c.FetchKeyPath == "") {
		return fmt.Errorf("SDA_FETCH_KEY_ID and SDA_FETCH_KEY must be set together")
	}
	// A zero poll interval would spin against both the LCD and the remote
	// instance as fast as they answer.
	if c.PollInterval < minPollInterval {
		return fmt.Errorf("SDA_POLL must be at least %s (got %s)", minPollInterval, c.PollInterval)
	}
	return nil
}

// minPollInterval floors the poll cadence so a zero or typo'd SDA_POLL
// cannot hot-loop.
const minPollInterval = 5 * time.Second

// PendingContent is the slice of the chain's FederatedContent the runner
// needs. Every scalar is a string because that is what grpc-gateway
// emits: proto uint64 fields serialize as QUOTED strings ("id":"7") and
// proto bytes fields as BASE64 ("content_hash":"3q2+7w=="). Declaring ID
// as a Go uint64 made encoding/json reject the entire response, and
// hex-decoding the hash rejected every row individually — the runner
// verified nothing, silently, which is the worst failure shape for an
// anti-fraud component.
type PendingContent struct {
	ID             string `json:"id"`
	ContentURI     string `json:"content_uri"`
	ContentHashB64 string `json:"content_hash"`
	SubmittedBy    string `json:"submitted_by"`
	PeerID         string `json:"peer_id"`
	// CreatorIdentity is the @user@host the post is attributed to.
	CreatorIdentity string `json:"creator_identity"`
	// The displayed fields, checked against the fetched post (display.go):
	// verification binds only the hash, so without the check a record could
	// verify while showing text or links its operator made up.
	Body        string `json:"body"`
	Title       string `json:"title"`
	ContentType string `json:"content_type"`
	// License is the operator's claim that the post is public domain
	// ("CC0-1.0" / "PDM-1.0"), checked against its hashtags (display.go).
	License         string `json:"license"`
	RemoteCreatedAt string `json:"remote_created_at"` // int64, quoted by the LCD
	// ProtocolMetadataB64 is proto bytes, so base64 over the LCD. The bridge
	// writes the anchored version's AS2 `updated` into it.
	ProtocolMetadataB64 string `json:"protocol_metadata"`
	// MediaFlags is the chain's media label (uint32; json.Number accepts it
	// quoted or bare). Non-zero means list queries return body as "" and the
	// real body must be read from federated_content_body (every record with a
	// content_uri is labelled, so that is nearly all of them).
	MediaFlags json.Number `json:"media_flags"`
}

// bodyWithheld reports whether the list query blanked this record's body.
func (c PendingContent) bodyWithheld() bool {
	return c.MediaFlags != "" && c.MediaFlags != "0"
}

// anchoredUpdated returns the AS2 `updated` of the version the bridge
// hashed ("" for a never-edited post). ok is false when the metadata is
// missing or unreadable, in which case nothing can be inferred from it.
func (c PendingContent) anchoredUpdated() (updated string, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(c.ProtocolMetadataB64)
	if err != nil || len(raw) == 0 {
		return "", false
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return "", false
	}
	v, present := meta["updated"]
	if !present {
		return "", false
	}
	s, _ := v.(string) // JSON null: never edited
	return s, true
}

// editedAfterAnchor reports whether the fetched object is a LATER version
// than the one anchored: its `updated` is set and after the anchored one.
// That mismatch is an edit, not fraud; the bridge re-anchors edits as new
// records carrying supersedes_content_id.
func editedAfterAnchor(c PendingContent, obj map[string]any) bool {
	anchored, ok := c.anchoredUpdated()
	if !ok {
		return false
	}
	fetchedS, _ := obj["updated"].(string)
	fetched, err := time.Parse(time.RFC3339, fetchedS)
	if err != nil {
		return false
	}
	if anchored == "" {
		return true
	}
	a, err := time.Parse(time.RFC3339, anchored)
	if err != nil {
		return false
	}
	return fetched.After(a)
}

// AnchoredHash decodes the anchored content hash from its LCD encoding.
func (c PendingContent) AnchoredHash() ([]byte, error) {
	h, err := base64.StdEncoding.DecodeString(c.ContentHashB64)
	if err != nil {
		return nil, fmt.Errorf("content_hash %q is not base64: %w", c.ContentHashB64, err)
	}
	if len(h) != sha256.Size {
		return nil, fmt.Errorf("content_hash is %d bytes, want %d", len(h), sha256.Size)
	}
	return h, nil
}

// Verifier holds the loop seams. fetch and broadcast are interfaces in
// spirit; tests stub them via the struct fields.
type Verifier struct {
	cfg       Config
	self      string
	fetch     func(ctx context.Context, uri string) (map[string]any, error)
	broadcast func(ctx context.Context, msg *types.MsgVerifyContent) (string, error)
	list      func(ctx context.Context, peerID string) ([]PendingContent, error)
	// hosts reads the peer policy's content_hosts; nil allows the peer id
	// alone (tests).
	hosts func(ctx context.Context, peerID string) ([]string, error)
	// digest fetches and hashes a file for ap-canonical-v2 records.
	digest apcanon.MediaDigester
	// bodyLimit reads the chain's max_content_body_size; nil means the
	// default 4096 (tests).
	bodyLimit func(ctx context.Context) (int, error)
	// bodyLimitNow is the limit read at the start of the current poll.
	bodyLimitNow int
	// usernames caches each author actor's preferredUsername. Mastodon
	// usernames cannot change, so the cache never goes stale.
	usernames map[string]string

	// chain is the LCD client for the production list path, built once at
	// startup rather than per poll (each construction re-derives the key
	// from the mnemonic).
	chain *sdaptx.Client

	// alarmed latches per content id so a condition that persists across
	// every poll pages a human once, not every PollInterval forever.
	alarmed map[string]bool
	// fetchFailures counts CONSECUTIVE fetch failures per content id, so
	// a post that stops resolving escalates instead of only ever logging.
	fetchFailures map[string]int

	// alarmSink overrides alarm delivery (tests assert that the alarm
	// actually fires, not merely that nothing was submitted).
	alarmSink func(msg string)
}

// defaultFetchFailureAlarmAt is how many consecutive fetch failures
// escalate to an alarm. At the 45s default poll that is ~4 minutes of
// unreachability — past a transient blip, short of a shift.
const defaultFetchFailureAlarmAt = 5

// Run polls until cancelled. Errors back off one poll interval; a
// successful verify records nothing locally — the chain's first-verifier
// -wins rule is the dedupe.
func (v *Verifier) Run(ctx context.Context) error {
	for {
		if err := v.pollSafe(ctx); err != nil {
			log.Printf("sdapverify: poll failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(v.cfg.PollInterval):
		}
	}
}

// pollSafe turns a panic in one cycle into an error instead of killing
// the runner. A verifier that dies silently stops being an independent
// check on the bridge, which is the one thing it exists to be.
func (v *Verifier) pollSafe(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in poll cycle: %v", r)
		}
	}()
	return v.poll(ctx)
}

func (v *Verifier) poll(ctx context.Context) error {
	v.bodyLimitNow = defaultBodyLimit
	if v.bodyLimit != nil {
		if limit, err := v.bodyLimit(ctx); err == nil && limit > 0 {
			v.bodyLimitNow = limit
		}
	}
	stillPending := map[string]bool{}
	complete := true
	var firstErr error
	for _, peerID := range v.cfg.PeerIDs {
		// One peer's LCD error must not stop verification on the others.
		pending, err := v.listPending(ctx, peerID)
		if err == nil && v.hosts != nil {
			var contentHosts []string
			if contentHosts, err = v.hosts(ctx, peerID); err == nil {
				v.pollPeer(ctx, pending, contentHosts, stillPending)
				continue
			}
		} else if err == nil {
			v.pollPeer(ctx, pending, nil, stillPending)
			continue
		}
		complete = false
		if firstErr == nil {
			firstErr = fmt.Errorf("peer %s: %w", peerID, err)
		}
	}
	// Clear latches for anything that left the pending set: it was acted
	// on, so the next occurrence is genuinely new and must page again.
	// Only after a complete pass: a peer whose listing failed would
	// otherwise look empty and have its latches cleared, re-paging them.
	if complete {
		for id := range v.alarmed {
			if !stillPending[id] {
				delete(v.alarmed, id)
				delete(v.fetchFailures, id)
			}
		}
	}
	return firstErr
}

// pollPeer verifies one peer's pending content, recording what it saw.
func (v *Verifier) pollPeer(ctx context.Context, pending []PendingContent, contentHosts []string, stillPending map[string]bool) {
	for _, c := range pending {
		// Belt and braces: the chain rejects self-verification with 2352
		// anyway; refusing locally avoids even trying when the runner is
		// misconfigured to share the operator's key.
		if c.SubmittedBy == v.self {
			log.Printf("sdapverify: skip %s: submitted by our own address — the bridge and verifier must not share a key", c.ID)
			continue
		}
		stillPending[c.ID] = true
		v.verifyOne(ctx, c, contentHosts)
	}
}

func (v *Verifier) verifyOne(ctx context.Context, c PendingContent, contentHosts []string) {
	want, err := c.AnchoredHash()
	if err != nil {
		log.Printf("sdapverify: %s: anchored hash unusable: %v", c.ID, err)
		return
	}
	// Provenance before content: a matching hash of a post that does not
	// belong to this peer would vouch for misattributed content. The chain
	// refuses such submissions now; this also covers records anchored before
	// it did. Checked before fetching, so the runner never fetches a URL
	// the peer does not own.
	err = types.ContentURIHostAllowed(c.PeerID, contentHosts, c.ContentURI)
	if err == nil && c.CreatorIdentity != "" {
		err = types.CreatorIdentityHostAllowed(c.PeerID, contentHosts, c.CreatorIdentity)
	}
	if err != nil {
		v.alarmOnce(ctx, c, fmt.Sprintf(
			"PROVENANCE content=%s uri=%q submitted_by=%s — %v. Not verifying: the post is not this peer's.",
			c.ID, c.ContentURI, c.SubmittedBy, err))
		return
	}

	obj, err := v.fetch(ctx, c.ContentURI)
	if err != nil {
		// A post that cannot be fetched is the "anchor then withdraw"
		// fraud shape as well as an ordinary outage, so repeated failure
		// has to reach a human rather than scrolling past in a log.
		n := v.noteFailure(c.ID)
		log.Printf("sdapverify: %s: fetch %s failed (%d consecutive): %v",
			c.ID, c.ContentURI, n, err)
		if n == v.fetchFailureAlarmAt() {
			v.alarmOnce(ctx, c, fmt.Sprintf(
				"UNFETCHABLE content=%s uri=%s submitted_by=%s — %d consecutive fetch failures. "+
					"Either the instance is down or the operator anchored content that no longer resolves; "+
					"a human must decide which.", c.ID, c.ContentURI, c.SubmittedBy, n))
		}
		return
	}

	// Recompute under the rule the record was anchored with. ap-canonical-v2
	// also digests every attachment's file; the digests are kept for the
	// display check below.
	rule := c.hashRule()
	digests := map[string]string{}
	var digester apcanon.MediaDigester
	if v.digest != nil {
		digester = func(ctx context.Context, url string) (string, error) {
			if d, ok := digests[url]; ok {
				return d, nil
			}
			d, err := v.digest(ctx, url)
			if err == nil {
				digests[url] = d
			}
			return d, err
		}
	}
	hash, err := apcanon.HashFor(ctx, rule, obj, digester)
	if err != nil {
		if rule != "" && rule != apcanon.HashRuleName && rule != apcanon.HashRuleV2 {
			v.alarmOnce(ctx, c, fmt.Sprintf("UNKNOWN RULE content=%s hash_rule=%q — this runner cannot check it; upgrade it.", c.ID, rule))
			return
		}
		// A file that will not download is the same "anchor then withdraw"
		// shape as an unfetchable post, but with a different usual cause (a
		// large file on a slow link): its own alarm, with the remedy.
		n := v.noteFailure(c.ID)
		log.Printf("sdapverify: %s: hash under %s failed (%d consecutive): %v", c.ID, rule, n, err)
		if n == v.fetchFailureAlarmAt() {
			if errors.Is(err, apcanon.ErrMediaUnavailable) {
				v.alarmOnce(ctx, c, fmt.Sprintf("MEDIA UNFETCHABLE content=%s uri=%s — an attachment's file failed %d times (%v). "+
					"A large file on a slow link: raise SDA_MEDIA_TIMEOUT or lower SDA_MEDIA_MIN_RATE. A 404: the origin removed it "+
					"after anchoring, and a human must decide whether that is fraud.", c.ID, c.ContentURI, n, err))
			} else {
				v.alarmOnce(ctx, c, fmt.Sprintf("UNFETCHABLE content=%s uri=%s — %d consecutive failures hashing it (%v)",
					c.ID, c.ContentURI, n, err))
			}
		}
		return
	}
	// Reset only once everything the hash needs has been read: clearing on
	// the post fetch alone meant a file that failed every poll never got
	// past one consecutive failure, and its alarm could never fire.
	v.clearFailures(c.ID)

	if !bytes.Equal(hash[:], want) {
		// The post was edited after this record was anchored: the anchored
		// version is simply gone, and the bridge anchors the new one as its
		// own record. Neither verify (the anchored bytes can no longer be
		// checked) nor page a human for every edit. Skipping never produces
		// a VERIFIED record, so a doctored hash that claims an old `updated`
		// gains nothing -- it expires unverified like any other.
		if editedAfterAnchor(c, obj) {
			if !v.alarmed[c.ID] {
				log.Printf("sdapverify: %s: superseded -- %s was edited after it was anchored (now updated=%v); "+
					"not verifying the old version", c.ID, c.ContentURI, obj["updated"])
			}
			v.latch(c.ID)
			return
		}
		v.alarmOnce(ctx, c, fmt.Sprintf(
			"HASH MISMATCH content=%s uri=%s submitted_by=%s got=%s anchored=%s — "+
				"canonicalizer bug or operator fraud? A human must decide; refusing to send content "+
				"to DISPUTED during bring-up.",
			c.ID, c.ContentURI, c.SubmittedBy,
			hex.EncodeToString(hash[:]), hex.EncodeToString(want)))
		return
	}

	// The hash matches the post; now the record must SHOW that post.
	problems := checkDisplay(c, rule, obj, digests, v.bodyLimitNow)
	authorProblem, err := v.checkAuthor(ctx, c, obj)
	if err != nil {
		log.Printf("sdapverify: %s: %v; retrying next poll", c.ID, err)
		return
	}
	if authorProblem != "" {
		problems = append(problems, authorProblem)
	}
	if len(problems) > 0 {
		v.alarmOnce(ctx, c, fmt.Sprintf(
			"MISREPRESENTED content=%s uri=%s submitted_by=%s — the hash matches but the record does not show the post: %s. "+
				"Not verifying: a VERIFIED record would vouch for what it displays.",
			c.ID, c.ContentURI, c.SubmittedBy, strings.Join(problems, "; ")))
		return
	}

	contentID, err := strconv.ParseUint(c.ID, 10, 64)
	if err != nil {
		log.Printf("sdapverify: %s: unparseable content id: %v", c.ID, err)
		return
	}
	txHash, err := v.broadcast(ctx, &types.MsgVerifyContent{
		ContentId:   contentID,
		ContentHash: want,
	})
	if err != nil {
		log.Printf("sdapverify: %s: broadcast failed: %v", c.ID, err)
		return
	}
	oversize := 0
	for _, d := range digests {
		if d == apcanon.DigestOversize {
			oversize++
		}
	}
	if oversize > 0 {
		log.Printf("sdapverify: verified %s (%s) tx=%s; %d attachment(s) over %d MiB are described but not attested",
			c.ID, c.ContentURI, txHash, oversize, apcanon.MaxMediaBytes>>20)
		return
	}
	log.Printf("sdapverify: verified %s (%s) tx=%s", c.ID, c.ContentURI, txHash)
}

// noteFailure records a consecutive fetch failure and returns the new
// count.
func (v *Verifier) noteFailure(contentID string) int {
	if v.fetchFailures == nil {
		v.fetchFailures = map[string]int{}
	}
	v.fetchFailures[contentID]++
	return v.fetchFailures[contentID]
}

func (v *Verifier) clearFailures(contentID string) {
	delete(v.fetchFailures, contentID)
}

func (v *Verifier) fetchFailureAlarmAt() int {
	if v.cfg.FetchFailureAlarmAt > 0 {
		return v.cfg.FetchFailureAlarmAt
	}
	return defaultFetchFailureAlarmAt
}

// alarmOnce latches: a condition that persists across every poll must not
// re-page every PollInterval forever. The latch clears when the content
// stops being returned as pending (i.e. someone acted on it).
func (v *Verifier) alarmOnce(ctx context.Context, c PendingContent, msg string) {
	if v.alarmed[c.ID] {
		return
	}
	v.latch(c.ID)
	v.alarm(ctx, msg)
}

// latch marks a content id as reported, so a condition that persists
// across polls is logged or paged once. Shares alarmOnce's latch, which
// poll clears when the content leaves the pending set.
func (v *Verifier) latch(contentID string) {
	if v.alarmed == nil {
		v.alarmed = map[string]bool{}
	}
	v.alarmed[contentID] = true
}

// alarm reports WITHOUT submitting. Page a human: during bring-up a
// mismatch is far more likely a canonicalizer bug than operator fraud,
// and a wrong DISPUTED strands the verifier's committed bond.
func (v *Verifier) alarm(ctx context.Context, msg string) {
	log.Printf("sdapverify: %s", msg)
	if v.alarmSink != nil {
		v.alarmSink(msg)
		return
	}
	if v.cfg.AlarmWebhook == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"text": "sdapverify " + msg})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.cfg.AlarmWebhook, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		log.Printf("sdapverify: alarm webhook failed: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
}

func (v *Verifier) listPending(ctx context.Context, peerID string) ([]PendingContent, error) {
	if v.list != nil {
		return v.list(ctx, peerID)
	}
	if v.chain == nil {
		return nil, fmt.Errorf("sdapverify: no chain client configured")
	}
	// Production path: the P0.4 peer+status filters over the LCD. Offset
	// and total are both in MATCH space (see the query's own comment), so
	// paging by offset += limit terminates.
	const pageSize = 50
	var all []PendingContent
	for offset := uint64(0); ; offset += pageSize {
		var out struct {
			Content    []PendingContent `json:"content"`
			Pagination struct {
				Total string `json:"total"`
			} `json:"pagination"`
		}
		path := fmt.Sprintf(
			"/sparkdream/federation/v1/list_federated_content"+
				"?peer_id=%s&status=FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"+
				"&pagination.offset=%d&pagination.limit=%d",
			url.QueryEscape(peerID), offset, pageSize)
		if err := v.chain.GetJSON(ctx, path, &out); err != nil {
			return nil, err
		}
		// Media-labelled records come back with body "" (docs/content-scanning.md
		// §3.4). checkDisplay compares the body to the fetched post, so read the
		// stored body before judging, or every labelled record would look forged.
		for i := range out.Content {
			if !out.Content[i].bodyWithheld() {
				continue
			}
			var body struct {
				Body string `json:"body"`
			}
			bodyPath := "/sparkdream/federation/v1/federated_content_body/" + url.PathEscape(out.Content[i].ID)
			if err := v.chain.GetJSON(ctx, bodyPath, &body); err != nil {
				return nil, err
			}
			out.Content[i].Body = body.Body
		}
		all = append(all, out.Content...)
		if uint64(len(out.Content)) < pageSize {
			return all, nil
		}
	}
}

func fetchFunc(signer *apcanon.HTTPSigner, allowPrivate bool) func(ctx context.Context, uri string) (map[string]any, error) {
	return func(ctx context.Context, uri string) (map[string]any, error) {
		obj, _, err := apcanon.Fetch(ctx, uri, apcanon.FetchOptions{
			Signer:            signer,
			AllowPrivateHosts: allowPrivate,
		})
		return obj, err
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envInt(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		var out int64
		if _, err := fmt.Sscanf(v, "%d", &out); err == nil {
			return out
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	switch os.Getenv(k) {
	case "1", "true", "TRUE", "yes":
		return true
	case "0", "false", "FALSE", "no":
		return false
	}
	return def
}

func envUint(k string, def uint64) uint64 {
	if v := os.Getenv(k); v != "" {
		var out uint64
		if _, err := fmt.Sscanf(v, "%d", &out); err == nil {
			return out
		}
	}
	return def
}

// splitList parses a comma-separated env value, dropping blanks.
func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// knownPeers keeps the configured peers that exist on chain.
func knownPeers(ctx context.Context, chain *sdaptx.Client, ids []string) []string {
	var out []string
	for _, id := range ids {
		var peer struct {
			Peer struct {
				ID string `json:"id"`
			} `json:"peer"`
		}
		if err := chain.GetJSON(ctx, "/sparkdream/federation/v1/get_peer/"+url.PathEscape(id), &peer); err != nil || peer.Peer.ID != id {
			log.Printf("sdapverify: dropping peer %s: not registered on chain (%v)", id, err)
			continue
		}
		out = append(out, id)
	}
	return out
}
