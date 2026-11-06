//go:build js && wasm

// Command wasm exposes the shield client crypto to browsers, so web clients
// hash, build Merkle paths and prove with exactly the code the chain verifies
// against. Build with tools/zk/wasm/build.sh.
//
// It registers a global `sparkdreamShield` object. Every function returns a
// JSON string: the result on success, {"error": "..."} on failure. uint64
// values travel as decimal strings and bytes as base64.
//
//	version()                         → {"version", "commit"}   (the chain build it matches)
//	deriveSecret(signatureB64)        → {"secret_key": hex}
//	publicKey(secretKeyHex)           → {"public_key": b64}
//	loadKeys(provingKey, r1cs)        → {}                    (Uint8Arrays)
//	prove(inputJSON)                  → {"proof", "nullifier", "rate_limit_nullifier", "merkle_root": b64, "trust_level": n}
//	encrypt(inputJSON)                → {"encrypted_payload": b64, "submission_domain": n,
//	                                     "submission_scope": "n", "submission_type_url": s}
//
// prove input: {"secret_key": hex, "root": b64, "depth": n,
// "leaves": [{"index": "n", "hash": b64}], "min_trust_level": n,
// "domain": n, "scope": "n", "rate_limit_epoch": "n", "inner_type_url": s,
// "inner_value": b64, "owner_sequence": "n"?} where root, depth and leaves
// come from /sparkdream/rep/v1/trust_tree and inner_* is the exact inner Any
// the exec will carry (the proof is bound to it). domain is the operation's
// nullifier domain (hashed into the proof's scope). For an ownership-mode
// operation (managing anonymous content you created) pass the content's
// owner domain and scope and its current owner_sequence; the returned
// nullifier must equal the content's owner tag. scope is the operation's resolved
// nullifier scope: for an EPOCH-scoped op, current_epoch / epoch_window
// (from the op's registration); for a two-field scope path such as collect's
// "target_type,target_id" it is (target_type << 56) | target_id; for a
// fallback path such as blog reactions' "reply_id|post_id" it is
// (1 << 56) | reply_id when reply_id is non-zero, else post_id. Set
// "group_tree": true for a PROOF_DOMAIN_GROUP_TREE exec (a secret-ballot vote
// or poll answer): its rate-limit nullifier is bound to the ballot's scope as
// well as the epoch (crypto.GroupTreeRateLimitEpoch), so it is unique to the
// member and the ballot.
//
// encrypt input: {"master_public_key_g2": b64, "epoch": "n",
// "inner_type_url": s, "inner_value": b64, "proof": b64, "nullifier": b64}
// builds an encrypted-batch payload: the inner Any plus the operation proof
// and nullifier from prove(), sealed to the chain's TLE master public key
// (master_public_key_g2 of /sparkdream/shield/v1/tle_master_public_key) for
// the current shield epoch. Prove the operation with rate_limit_epoch = that
// epoch. Then prove the *submission*: call prove() again with domain =
// submission_domain, scope = submission_scope, inner_type_url =
// submission_type_url, inner_value = encrypted_payload and the same
// rate_limit_epoch; submit MsgShieldedExec with exec_mode ENCRYPTED_BATCH,
// target_epoch = epoch, encrypted_payload, and that second proof's proof,
// nullifier, rate_limit_nullifier and merkle_root, leaving inner_message
// empty. Nothing secret is needed to encrypt.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"syscall/js"

	"github.com/consensys/gnark/logger"

	"sparkdream/tools/crypto"
	"sparkdream/tools/tle"
	"sparkdream/tools/zk/prover"
)

// maxTrustLevel is the highest x/rep TrustLevel (CORE).
const maxTrustLevel = 4

// version and commit are the chain release this client was built from, set by
// build.sh with -ldflags -X (the same values the Makefile stamps into
// sparkdreamd). Clients compare version against the node's
// application_version: a prover from another release can make proofs over
// public inputs the chain no longer derives the same way, and those fail only
// at broadcast as "ZK proof verification failed".
var (
	version = "unknown"
	commit  = "unknown"
)

var shieldProver *prover.ShieldProver

func main() {
	logger.Disable()
	js.Global().Set("sparkdreamShield", js.ValueOf(map[string]any{
		"version":      js.FuncOf(wrap(buildInfo)),
		"deriveSecret": js.FuncOf(wrap(deriveSecret)),
		"publicKey":    js.FuncOf(wrap(publicKey)),
		"loadKeys":     js.FuncOf(wrap(loadKeys)),
		"prove":        js.FuncOf(wrap(prove)),
		"encrypt":      js.FuncOf(wrap(encrypt)),
	}))
	select {}
}

func wrap(fn func(args []js.Value) (any, error)) func(js.Value, []js.Value) any {
	return func(_ js.Value, args []js.Value) (out any) {
		defer func() {
			if r := recover(); r != nil {
				out = errorJSON(fmt.Errorf("panic: %v", r))
			}
		}()
		res, err := fn(args)
		if err != nil {
			return errorJSON(err)
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return errorJSON(err)
		}
		return string(bz)
	}
}

func errorJSON(err error) string {
	bz, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(bz)
}

func buildInfo([]js.Value) (any, error) {
	return map[string]string{"version": version, "commit": commit}, nil
}

func deriveSecret(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("deriveSecret(signatureB64)")
	}
	sig, err := base64.StdEncoding.DecodeString(args[0].String())
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}
	sk, err := crypto.DeriveSecretKeyFromSignature(sig)
	if err != nil {
		return nil, err
	}
	return map[string]string{"secret_key": hex.EncodeToString(sk)}, nil
}

func publicKey(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("publicKey(secretKeyHex)")
	}
	sk, err := parseSecret(args[0].String())
	if err != nil {
		return nil, err
	}
	return map[string]string{"public_key": base64.StdEncoding.EncodeToString(crypto.DerivePublicKey(sk))}, nil
}

func loadKeys(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("loadKeys(provingKey, r1cs)")
	}
	pk := make([]byte, args[0].Get("length").Int())
	js.CopyBytesToGo(pk, args[0])
	ccs := make([]byte, args[1].Get("length").Int())
	js.CopyBytesToGo(ccs, args[1])
	p, err := prover.NewShieldProverFromTrustedBytes(pk, ccs)
	if err != nil {
		return nil, err
	}
	shieldProver = p
	return map[string]string{}, nil
}

type proveLeaf struct {
	Index string `json:"index"`
	Hash  []byte `json:"hash"`
}

type proveInput struct {
	SecretKey      string      `json:"secret_key"`
	Root           []byte      `json:"root"`
	Depth          int         `json:"depth"`
	Leaves         []proveLeaf `json:"leaves"`
	MinTrustLevel  uint64      `json:"min_trust_level"`
	Domain         uint32      `json:"domain"`
	Scope          string      `json:"scope"`
	OwnerSequence  string      `json:"owner_sequence"`
	RateLimitEpoch string      `json:"rate_limit_epoch"`
	GroupTree      bool        `json:"group_tree"`
	InnerTypeURL   string      `json:"inner_type_url"`
	InnerValue     []byte      `json:"inner_value"`
}

func prove(args []js.Value) (any, error) {
	if shieldProver == nil {
		return nil, errors.New("call loadKeys first")
	}
	if len(args) != 1 {
		return nil, errors.New("prove(inputJSON)")
	}
	var in proveInput
	if err := json.Unmarshal([]byte(args[0].String()), &in); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	sk, err := parseSecret(in.SecretKey)
	if err != nil {
		return nil, err
	}
	scope, err := strconv.ParseUint(in.Scope, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("scope: %w", err)
	}
	epoch, err := strconv.ParseUint(in.RateLimitEpoch, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("rate_limit_epoch: %w", err)
	}
	// An ownership-mode exec (managing anonymously created content) binds the
	// owner's current sequence into the message hash.
	msgHash := crypto.ShieldMessageHash(in.InnerTypeURL, in.InnerValue)
	if in.OwnerSequence != "" {
		seq, err := strconv.ParseUint(in.OwnerSequence, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("owner_sequence: %w", err)
		}
		msgHash = crypto.ShieldOwnedMessageHash(in.InnerTypeURL, in.InnerValue, seq)
	}

	leaves := make(map[uint64][]byte, len(in.Leaves))
	for _, l := range in.Leaves {
		idx, err := strconv.ParseUint(l.Index, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("leaf index: %w", err)
		}
		leaves[idx] = l.Hash
	}

	// The leaf commits to the member's current trust level; find which one.
	pk := crypto.DerivePublicKey(sk)
	var (
		leafIndex  uint64
		trustLevel uint64
		found      bool
	)
	for t := uint64(0); t <= maxTrustLevel && !found; t++ {
		want := crypto.ComputeLeaf(pk, t)
		for idx, h := range leaves {
			if bytes.Equal(crypto.PadTo32(h), want) {
				leafIndex, trustLevel, found = idx, t, true
				break
			}
		}
	}
	if !found {
		return nil, errors.New("your anonymous identity is not in the trust tree yet; it is added at the end of the block after registration")
	}
	if trustLevel < in.MinTrustLevel {
		return nil, fmt.Errorf("this action needs trust level %d; yours is %d", in.MinTrustLevel, trustLevel)
	}

	path, err := crypto.SparseMerkleProof(leaves, in.Depth, leafIndex)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(path.Root, crypto.PadTo32(in.Root)) {
		return nil, errors.New("trust tree export is inconsistent with its root; fetch it again")
	}

	out, err := shieldProver.GenerateProof(&prover.ShieldProofInput{
		SecretKey:      sk,
		TrustLevel:     trustLevel,
		MinTrustLevel:  in.MinTrustLevel,
		Domain:         in.Domain,
		Scope:          scope,
		RateLimitEpoch: epoch,
		GroupTree:      in.GroupTree,
		MessageHash:    msgHash,
		MerkleRoot:     path.Root,
		MerkleProof:    path,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"proof":                out.ProofBytes,
		"nullifier":            out.Nullifier,
		"rate_limit_nullifier": out.RateLimitNullifier,
		"merkle_root":          out.MerkleRoot,
		"trust_level":          trustLevel,
	}, nil
}

type encryptInput struct {
	MasterPublicKeyG2 []byte `json:"master_public_key_g2"`
	Epoch             string `json:"epoch"`
	InnerTypeURL      string `json:"inner_type_url"`
	InnerValue        []byte `json:"inner_value"`
	Proof             []byte `json:"proof"`
	Nullifier         []byte `json:"nullifier"`
}

// encrypt seals an inner message and its proof for encrypted-batch
// submission. The plaintext is the MsgShieldedExec fragment the chain
// decodes at execution (tle.EncodeExecFragment); the ciphertext format and
// KDF are tools/tle's, shared with the chain.
func encrypt(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("encrypt(inputJSON)")
	}
	var in encryptInput
	if err := json.Unmarshal([]byte(args[0].String()), &in); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	epoch, err := strconv.ParseUint(in.Epoch, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("epoch: %w", err)
	}
	if len(in.Proof) == 0 || len(in.Nullifier) != 32 {
		return nil, errors.New("proof and 32-byte nullifier are required (prove() first)")
	}
	fragment, err := tle.EncodeExecFragment(in.InnerTypeURL, in.InnerValue, in.Proof, in.Nullifier)
	if err != nil {
		return nil, err
	}
	payload, err := tle.Encrypt(in.MasterPublicKeyG2, epoch, fragment)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"encrypted_payload":   payload,
		"submission_domain":   tle.BatchSubmissionDomain,
		"submission_scope":    strconv.FormatUint(tle.BatchSubmissionScope(payload), 10),
		"submission_type_url": tle.BatchSubmissionTypeURL,
	}, nil
}

func parseSecret(skHex string) ([]byte, error) {
	sk, err := hex.DecodeString(skHex)
	if err != nil || len(sk) != 32 {
		return nil, errors.New("secret key must be 32 bytes of hex")
	}
	if !crypto.IsCanonicalFieldElement(sk) {
		return nil, errors.New("secret key is not a canonical field element")
	}
	return sk, nil
}
