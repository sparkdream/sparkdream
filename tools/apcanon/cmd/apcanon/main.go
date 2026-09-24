// Command apcanon computes ap-canonical hashes (v2 by default, or v1) of ActivityPub (AS2)
// objects. It is the P1 deliverable of the Mastodon live link (phases are
// defined in test/federation/mastodon/README.md): the
// same computation the bridge daemon runs before anchoring content and
// the verifier runner runs before MsgVerifyContent, exposed for the
// manual on-chain rehearsal (P4) and the determinism harness (P2).
//
// Usage:
//
//	apcanon hash  [flags] <uri|->   # sha256 of the canonical form, hex
//	apcanon canon [flags] <uri|->   # the canonical JSON itself
//
// Flags precede the target (Go's flag package stops at the first
// non-flag argument).
//
// A URI is fetched over HTTPS as application/activity+json; "-" reads
// the raw AS2 JSON from stdin (never hashed as bytes — it is parsed and
// re-projected). Flags:
//
//	--key-id  <url>   actor key id for signed fetches (HTTP Signatures,
//	                  draft-cavage — what Mastodon secure mode verifies)
//	--key     <path>  PEM private key for signed fetches
//	--base64          print the hash base64 (MsgVerifyContent's
//	                  --content-hash flag takes base64, not hex)
//	--allow-private   permit http:// and private/loopback destinations.
//	                  Fetch refuses them by default because both daemons
//	                  fetch attacker-influenced URIs, so a remote instance
//	                  could otherwise redirect them into the host's own
//	                  network. Needed ONLY for a local test instance
//	                  (a Mastodon/GoToSocial on localhost or a LAN
//	                  address). Never for a real peer.
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"sparkdream/tools/apcanon"
)

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	if sub != "hash" && sub != "canon" {
		usage()
		os.Exit(2)
	}
	fs := flag.NewFlagSet("apcanon "+sub, flag.ExitOnError)
	keyID := fs.String("key-id", "", "actor key id (https://actor#main-key) for signed fetches")
	keyPath := fs.String("key", "", "path to PEM private key for signed fetches")
	b64 := fs.Bool("base64", false, "print hash base64 instead of hex (the chain CLI takes base64)")
	allowPrivate := fs.Bool("allow-private", false,
		"permit http:// and private/loopback destinations (local test instance only)")
	rule := fs.String("rule", apcanon.HashRuleV2,
		"hash rule: "+apcanon.HashRuleV2+" (default, digests each attachment's file) or "+apcanon.HashRuleName)
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	target := fs.Arg(0)

	obj, err := load(context.Background(), target, *keyID, *keyPath, *allowPrivate)
	if err != nil {
		fmt.Fprintln(os.Stderr, "apcanon:", err)
		os.Exit(1)
	}

	ctx := context.Background()
	digest := func(ctx context.Context, url string) (string, error) {
		return apcanon.FetchMediaDigest(ctx, url, apcanon.FetchOptions{AllowPrivateHosts: *allowPrivate})
	}
	switch sub {
	case "canon":
		var canonical []byte
		switch *rule {
		case apcanon.HashRuleV2:
			canonical, err = apcanon.CanonicalizeV2(ctx, obj, digest)
		case apcanon.HashRuleName:
			canonical, err = apcanon.Canonicalize(obj)
		default:
			err = fmt.Errorf("unknown hash rule %q", *rule)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "apcanon:", err)
			os.Exit(1)
		}
		fmt.Println(string(canonical))
	case "hash":
		hash, err := apcanon.HashFor(ctx, *rule, obj, digest)
		if err != nil {
			fmt.Fprintln(os.Stderr, "apcanon:", err)
			os.Exit(1)
		}
		if *b64 {
			fmt.Println(base64.StdEncoding.EncodeToString(hash[:]))
		} else {
			fmt.Println(hex.EncodeToString(hash[:]))
		}
	}
}

func load(ctx context.Context, target, keyID, keyPath string, allowPrivate bool) (map[string]any, error) {
	if target == "-" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		return apcanon.Parse(raw)
	}
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return nil, fmt.Errorf("target must be an http(s) URI or '-' for stdin, got %q", target)
	}
	opts := apcanon.FetchOptions{AllowPrivateHosts: allowPrivate}
	if keyID != "" || keyPath != "" {
		if keyID == "" || keyPath == "" {
			return nil, fmt.Errorf("--key-id and --key must be given together for signed fetches")
		}
		pemBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, err
		}
		signer, err := apcanon.LoadHTTPSigner(keyID, pemBytes)
		if err != nil {
			return nil, err
		}
		opts.Signer = signer
	}
	obj, _, err := apcanon.Fetch(ctx, target, opts)
	return obj, err
}

func usage() {
	// Flags must come BEFORE the target: Go's flag package stops parsing
	// at the first non-flag argument, so "hash - --base64" leaves --base64
	// unparsed and fails the NArg check. The old usage line showed flags
	// last, which is exactly backwards.
	fmt.Fprintln(os.Stderr, `usage:
  apcanon hash  [flags] <uri|->
  apcanon canon [flags] <uri|->

flags:
  --base64            print the hash base64 (the chain's --content-hash takes base64, not hex)
  --key-id URL        actor key id for signed fetches (secure-mode instances)
  --key PATH          PEM private key for signed fetches
  --allow-private     permit http:// and private/loopback targets (LOCAL TEST INSTANCE ONLY)
  --rule NAME         ap-canonical-v2 (default: also digests every attachment's file,
                      fetched from its url) or ap-canonical-v1

examples:
  apcanon hash https://mastodon.example/users/alice/statuses/1
  apcanon hash --base64 https://mastodon.example/users/alice/statuses/1
  apcanon hash --allow-private http://localhost:3000/users/alice/statuses/1
  curl -sH 'Accept: application/activity+json' <uri> | apcanon hash -`)
}
