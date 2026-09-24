package apcanon

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mustParse decodes with json.Number preserved, the way Fetch does.
func mustParse(t *testing.T, raw string) map[string]any {
	t.Helper()
	obj, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return obj
}

// Golden fixture set. Shapes follow real Mastodon AS2 serialization of
// its Status object (Note type). The determinism harness (P2) saves
// live-captured objects under test/federation/mastodon/fixtures/ and
// these goldens encode the same shapes so the rule is pinned before any
// instance exists. Every fixture's canonical form is asserted verbatim —
// a change to any byte of these strings is a change to the hash rule.
func TestGoldenCanonicalForms(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "plain note, never edited (updated and inReplyTo and summary absent)",
			raw: `{"@context":"https://www.w3.org/ns/activitystreams","id":"https://md.example/users/alice/statuses/100","type":"Note",` +
				`"attributedTo":"https://md.example/users/alice","content":"<p>hello world</p>","published":"2026-09-01T10:00:00Z",` +
				`"to":["https://www.w3.org/ns/activitystreams#Public"]}`,
			// Exactly eight keys, absent fields as null, @context and to dropped.
			want: `{"attachment":null,"attributedTo":"https://md.example/users/alice","content":"<p>hello world</p>",` +
				`"id":"https://md.example/users/alice/statuses/100","inReplyTo":null,"published":"2026-09-01T10:00:00Z",` +
				`"summary":null,"updated":null}`,
		},
		{
			name: "content warning (summary) + media attachments",
			raw: `{"id":"https://md.example/users/bob/statuses/200","attributedTo":"https://md.example/users/bob",` +
				`"content":"<p>picture inside</p>","summary":"cw: spoilers","published":"2026-09-02T11:00:00Z",` +
				`"attachment":[{"type":"Document","mediaType":"image/png","name":"a cat","url":"https://cdn.example/sig1.png"},` +
				`{"type":"Document","mediaType":"image/jpeg","url":"https://cdn.example/sig2.jpg"}]}`,
			// attachment[].url excluded by construction; name null when
			// absent; source order preserved.
			want: `{"attachment":[{"mediaType":"image/png","name":"a cat"},{"mediaType":"image/jpeg","name":null}],` +
				`"attributedTo":"https://md.example/users/bob","content":"<p>picture inside</p>",` +
				`"id":"https://md.example/users/bob/statuses/200","inReplyTo":null,"published":"2026-09-02T11:00:00Z",` +
				`"summary":"cw: spoilers","updated":null}`,
		},
		{
			name: "edited note (updated populated)",
			raw: `{"id":"https://md.example/users/carol/statuses/300","attributedTo":"https://md.example/users/carol",` +
				`"content":"<p>edited body</p>","published":"2026-09-03T09:00:00Z","updated":"2026-09-03T09:05:00Z"}`,
			want: `{"attachment":null,"attributedTo":"https://md.example/users/carol","content":"<p>edited body</p>",` +
				`"id":"https://md.example/users/carol/statuses/300","inReplyTo":null,"published":"2026-09-03T09:00:00Z",` +
				`"summary":null,"updated":"2026-09-03T09:05:00Z"}`,
		},
		{
			name: "reply (inReplyTo)",
			raw: `{"id":"https://md.example/users/dave/statuses/400","attributedTo":"https://md.example/users/dave",` +
				`"content":"<p>a reply</p>","published":"2026-09-04T08:00:00Z","inReplyTo":"https://md.example/users/alice/statuses/100"}`,
			want: `{"attachment":null,"attributedTo":"https://md.example/users/dave","content":"<p>a reply</p>",` +
				`"id":"https://md.example/users/dave/statuses/400","inReplyTo":"https://md.example/users/alice/statuses/100",` +
				`"published":"2026-09-04T08:00:00Z","summary":null,"updated":null}`,
		},
		{
			name: "attachment present but empty is [], not null",
			raw: `{"id":"https://md.example/users/erin/statuses/500","attributedTo":"https://md.example/users/erin",` +
				`"content":"<p>empty media</p>","published":"2026-09-05T07:00:00Z","attachment":[]}`,
			want: `{"attachment":[],"attributedTo":"https://md.example/users/erin","content":"<p>empty media</p>",` +
				`"id":"https://md.example/users/erin/statuses/500","inReplyTo":null,"published":"2026-09-05T07:00:00Z",` +
				`"summary":null,"updated":null}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Canonicalize(mustParse(t, tc.raw))
			if err != nil {
				t.Fatalf("canonicalize: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("canonical form changed:\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// The rule's core equivalence: a field absent from the source and the
// same field explicitly null must canonicalize identically — always-
// emit-null makes them equal, so an instance that starts sending
// explicit nulls (or stops) cannot move the hash of an unedited status.
func TestAbsentAndExplicitNullAreEquivalent(t *testing.T) {
	absent := `{"id":"https://x/y/1","attributedTo":"https://x/y","content":"c","published":"p"}`
	explicit := `{"id":"https://x/y/1","attributedTo":"https://x/y","content":"c","published":"p",` +
		`"summary":null,"updated":null,"inReplyTo":null,"attachment":null}`
	cAbsent, err := Canonicalize(mustParse(t, absent))
	if err != nil {
		t.Fatal(err)
	}
	cExplicit, err := Canonicalize(mustParse(t, explicit))
	if err != nil {
		t.Fatal(err)
	}
	if string(cAbsent) != string(cExplicit) {
		t.Fatalf("absent and explicit-null diverged:\n absent:   %s\n explicit: %s", cAbsent, cExplicit)
	}

	// And a populated value MUST differ from both — the equivalence
	// above is about null handling, not about ignoring the field.
	populated := `{"id":"https://x/y/1","attributedTo":"https://x/y","content":"c","published":"p","summary":"cw"}`
	cPopulated, err := Canonicalize(mustParse(t, populated))
	if err != nil {
		t.Fatal(err)
	}
	if string(cPopulated) == string(cAbsent) {
		t.Fatal("populated summary hashed the same as absent — the field would be unobservable")
	}
}

// Input key order, whitespace, and @context placement must not move the
// hash — two fetches that differ only in serialization noise are the
// same unedited status.
func TestKeyOrderAndLayoutIndependence(t *testing.T) {
	a := mustParse(t, `{"id":"https://x/1","content":"c","attributedTo":"https://x/a","published":"p"}`)
	b := mustParse(t, "\n  {\n\t\"published\":\"p\",\n\t\"attributedTo\":\"https://x/a\",\n    \"content\":\"c\",\n\t\t\"id\":\"https://x/1\"\n}\n")
	ca, err := Canonicalize(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := Canonicalize(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ca) != string(cb) {
		t.Fatalf("layout changed the canonical form:\n %s\n %s", ca, cb)
	}
}

// Attachment order is content: reordering two attachments must change
// the hash (Mastodon never reorders an unedited status, so a reorder is
// an edit the anchor should catch).
func TestAttachmentOrderIsSignificant(t *testing.T) {
	first := `{"id":"https://x/2","attachment":[{"mediaType":"image/png","name":"a"},{"mediaType":"image/png","name":"b"}]}`
	swapped := `{"id":"https://x/2","attachment":[{"mediaType":"image/png","name":"b"},{"mediaType":"image/png","name":"a"}]}`
	cf, err := Canonicalize(mustParse(t, first))
	if err != nil {
		t.Fatal(err)
	}
	cs, err := Canonicalize(mustParse(t, swapped))
	if err != nil {
		t.Fatal(err)
	}
	if string(cf) == string(cs) {
		t.Fatal("attachment reorder must change the canonical form")
	}
}

// JCS number formatting: the literal's TEXT may differ between fetches
// ("1" vs "1.0" vs "1e0") for the same value; RFC 8785 normalizes them
// all to the ES6 serialization, so the hash does not move.
func TestNumberNormalization(t *testing.T) {
	// Numbers only reach the canonical form nested inside one of the eight
	// hashed keys — AS2 permits attributedTo to be an object, and Mastodon
	// itself emits numeric fields inside attachment objects. Two spellings
	// of the same value must produce identical bytes, and key order inside
	// the nested object must not matter either.
	for _, pair := range [][2]string{
		{`{"id":"https://x/3","attributedTo":{"type":"Person","count":1}}`,
			`{"id":"https://x/3","attributedTo":{"count":1.0,"type":"Person"}}`},
		{`{"id":"https://x/3","attributedTo":{"count":1e2}}`,
			`{"id":"https://x/3","attributedTo":{"count":100}}`},
		{`{"id":"https://x/3","attributedTo":{"count":-0.0}}`,
			`{"id":"https://x/3","attributedTo":{"count":0}}`},
	} {
		a, err := Canonicalize(mustParse(t, pair[0]))
		if err != nil {
			t.Fatalf("canonicalize %s: %v", pair[0], err)
		}
		b, err := Canonicalize(mustParse(t, pair[1]))
		if err != nil {
			t.Fatalf("canonicalize %s: %v", pair[1], err)
		}
		if string(a) != string(b) {
			t.Errorf("%s and %s must canonicalize identically:\n  %s\n  %s",
				pair[0], pair[1], a, b)
		}
	}
}

// RFC 8785 serializes numbers with ECMAScript double semantics, so an
// integer beyond 2^53 is ROUNDED, not preserved — 2^53+1 canonicalizes
// identically to 2^53. That is a property of the rule, not a bug in it:
// both the bridge and the verifier run this same package, so they round
// identically and the hashes still agree. What matters is that it is
// deterministic and written down, because a third-party implementation
// that preserved the integer exactly would disagree with both of them,
// and on-chain that reads as verifier misconduct.
//
// Mastodon does not put such integers inside the eight hashed keys, so
// this is a documented edge, not an operational risk.
func TestLargeIntegersRoundDeterministically(t *testing.T) {
	// 9007199254740993 == 2^53 + 1, the smallest integer a float64 cannot
	// represent.
	const beyond = `{"id":"https://x/3","attributedTo":{"snowflake":9007199254740993}}`
	const atLimit = `{"id":"https://x/3","attributedTo":{"snowflake":9007199254740992}}`

	a, err := Canonicalize(mustParse(t, beyond))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonicalize(mustParse(t, atLimit))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("expected JCS double rounding to collapse 2^53+1 onto 2^53:\n  %s\n  %s", a, b)
	}

	// Determinism is the load-bearing property: the same input must give
	// the same bytes every time, however it rounds.
	again, err := Canonicalize(mustParse(t, beyond))
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(a) {
		t.Fatal("canonicalization of a >2^53 integer is not deterministic")
	}

	// Inside the 2^53 range nothing is lost.
	const safe = `{"id":"https://x/3","attributedTo":{"snowflake":9007199254740991}}`
	c, err := Canonicalize(mustParse(t, safe))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c), "9007199254740991") {
		t.Fatalf("an integer within the safe range must survive exactly: %s", c)
	}
}

// The 10MB cap and the never-hash-raw-bytes contract are enforced in
// Fetch; Parse rejects non-objects.
func TestParseRejectsNonObject(t *testing.T) {
	if _, err := Parse([]byte(`[1,2,3]`)); err == nil {
		t.Fatal("array top-level must be rejected")
	}
	if _, err := Parse([]byte(`"string"`)); err == nil {
		t.Fatal("string top-level must be rejected")
	}
}

// Fetch returns the parsed object and refuses non-200; a signer produces
// a header the server can actually VERIFY — the previous version of this
// test only checked that a Signature header existed, which would have
// passed on a signature over the wrong bytes.
func TestFetchAndSigning(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = "https://md.example/users/bridge#main-key"

	var verifiedSecure bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secure" {
			// No signature at all is the expected unsigned case: answer
			// 401 the way a secure-mode instance would. A signature that
			// is PRESENT but does not verify is a real failure.
			if r.Header.Get("Signature") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if err := verifyCavage(r, &key.PublicKey, keyID); err != nil {
				t.Errorf("server-side signature verification failed: %v", err)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			verifiedSecure = true
		}
		if got := r.Header.Get("Accept"); got != AcceptAS2 {
			t.Errorf("Accept = %q, want %q", got, AcceptAS2)
		}
		w.Header().Set("Content-Type", AcceptAS2)
		_, _ = w.Write([]byte(`{"id":"https://md.example/users/alice/statuses/1","content":"c"}`))
	}))
	defer srv.Close()

	// httptest binds loopback, which the destination guard refuses by
	// design (see TestFetchRefusesNonPublicDestinations).
	local := FetchOptions{AllowPrivateHosts: true}

	obj, _, err := Fetch(t.Context(), srv.URL+"/anon", local)
	if err != nil {
		t.Fatalf("anon fetch: %v", err)
	}
	if obj["id"] != "https://md.example/users/alice/statuses/1" {
		t.Fatalf("unexpected object: %v", obj)
	}

	if _, _, err := Fetch(t.Context(), srv.URL+"/secure", local); err == nil {
		t.Fatal("unsigned fetch against secure path must fail")
	}

	signed := local
	signed.Signer = &HTTPSigner{KeyID: keyID, PrivateKey: key}
	obj, _, err = Fetch(t.Context(), srv.URL+"/secure", signed)
	if err != nil {
		t.Fatalf("signed fetch: %v", err)
	}
	if obj["content"] != "c" {
		t.Fatalf("unexpected object: %v", obj)
	}
	if !verifiedSecure {
		t.Fatal("the secure path never verified a signature")
	}
}

// verifyCavage reconstructs the signing string from the request the way a
// Mastodon instance in AUTHORIZED_FETCH mode does, and checks the RSA
// signature over it. Independent of Sign's own string building on
// purpose: a test that reuses the implementation proves nothing.
func verifyCavage(r *http.Request, pub *rsa.PublicKey, wantKeyID string) error {
	header := r.Header.Get("Signature")
	if header == "" {
		return fmt.Errorf("no Signature header")
	}
	params := map[string]string{}
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return fmt.Errorf("malformed Signature component %q", part)
		}
		params[k] = strings.Trim(v, `"`)
	}
	if params["keyId"] != wantKeyID {
		return fmt.Errorf("keyId = %q, want %q", params["keyId"], wantKeyID)
	}
	if params["algorithm"] != "rsa-sha256" {
		return fmt.Errorf("algorithm = %q", params["algorithm"])
	}
	if params["headers"] == "" {
		return fmt.Errorf("no headers list")
	}

	var lines []string
	for _, h := range strings.Fields(params["headers"]) {
		switch h {
		case "(request-target)":
			lines = append(lines, "(request-target): "+
				strings.ToLower(r.Method)+" "+r.URL.RequestURI())
		case "host":
			host := r.Host
			if host == "" {
				host = r.URL.Host
			}
			lines = append(lines, "host: "+host)
		default:
			v := r.Header.Get(h)
			if v == "" {
				return fmt.Errorf("signed header %q is absent from the request", h)
			}
			lines = append(lines, h+": "+v)
		}
	}
	signingString := strings.Join(lines, "\n")

	sig, err := base64.StdEncoding.DecodeString(params["signature"])
	if err != nil {
		return fmt.Errorf("signature is not base64: %w", err)
	}
	sum := sha256.Sum256([]byte(signingString))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return fmt.Errorf("verify over %q: %w", signingString, err)
	}
	return nil
}

// The destination guard: both daemons fetch URIs that come from inbound
// federation traffic, so a remote instance must not be able to point the
// fetcher at the daemon host's own network or at cloud metadata.
func TestFetchRefusesNonPublicDestinations(t *testing.T) {
	for _, uri := range []string{
		"http://example.com/note",         // plaintext
		"https://127.0.0.1/note",          // loopback
		"https://169.254.169.254/latest/", // cloud metadata
		"https://10.0.0.5/note",           // RFC1918
		"https://[::1]/note",              // loopback v6
		"file:///etc/passwd",              // not http at all
		"https:///note",                   // no host
	} {
		if _, _, err := Fetch(t.Context(), uri, FetchOptions{}); err == nil {
			t.Errorf("Fetch(%q) must be refused", uri)
		}
	}
}

// Every redirect hop is re-checked against the destination guard, not
// just the URI the caller passed in — otherwise a cooperating instance
// could answer 302 and send the fetcher anywhere. Uses a scheme the
// guard rejects unconditionally so the assertion holds regardless of the
// AllowPrivateHosts relaxation the loopback test server needs.
func TestFetchRechecksRedirectHops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "ftp://example.com/note")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	_, _, err := Fetch(t.Context(), srv.URL+"/note", FetchOptions{AllowPrivateHosts: true})
	if err == nil {
		t.Fatal("a redirect to a refused destination must fail the fetch")
	}
	if !strings.Contains(err.Error(), "scheme must be https") {
		t.Fatalf("want the destination guard to reject the hop, got %v", err)
	}
}

// A redirect chain is bounded. Go's default of 10 is more than an AS2
// object ever legitimately needs and is a cheap amplification primitive.
func TestFetchBoundsRedirectChain(t *testing.T) {
	// Redirect forever, relative so the handler needs no self-reference.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer srv.Close()

	_, _, err := Fetch(t.Context(), srv.URL+"/note", FetchOptions{AllowPrivateHosts: true})
	if err == nil {
		t.Fatal("an unbounded redirect loop must fail")
	}
	if !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("want a redirect-cap error, got %v", err)
	}
}

// A cavage signature is bound to (request-target) and host, so it is
// valid only for the hop it was made for. Carrying it across a redirect
// leaks the keyId and a valid signature to whatever host the redirector
// named — Go strips Authorization on a cross-host redirect but not a
// custom Signature header, so Fetch has to do it.
func TestFetchDropsSignatureOnCrossHostRedirect(t *testing.T) {
	var sawSignature bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Signature") != "" {
			sawSignature = true
		}
		w.Header().Set("Content-Type", AcceptAS2)
		_, _ = w.Write([]byte(`{"id":"https://other.example/1"}`))
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/note", http.StatusFound)
	}))
	defer redirector.Close()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Fetch(t.Context(), redirector.URL+"/note", FetchOptions{
		AllowPrivateHosts: true,
		Signer:            &HTTPSigner{KeyID: "https://md.example/users/bridge#main-key", PrivateKey: key},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if sawSignature {
		t.Fatal("the signature must not follow the redirect to another host")
	}
}

// An oversized body is an ERROR, not a silent truncation: two fetchers
// with different caps would otherwise hash different documents, and
// on-chain that is indistinguishable from verifier misconduct.
func TestFetchRefusesOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", AcceptAS2)
		_, _ = w.Write([]byte(`{"id":"x","content":"`))
		chunk := strings.Repeat("a", 1<<16)
		for written := 0; written <= maxAS2Body; written += len(chunk) {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	_, _, err := Fetch(t.Context(), srv.URL+"/big", FetchOptions{AllowPrivateHosts: true})
	if err == nil {
		t.Fatal("an oversized object must be refused, never truncated and hashed")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want a size error, got %v", err)
	}
}

// Hash is a pure function of the canonical form: same object, same
// digest; any edit class in the hashed set moves it. This is the P2
// property in miniature — the full harness runs it against a live
// instance over hours.
func TestHashStableAndSensitive(t *testing.T) {
	base := `{"id":"https://x/10","attributedTo":"https://x/a","content":"<p>one</p>","published":"p","attachment":null}`
	h1, err := Hash(mustParse(t, base))
	if err != nil {
		t.Fatal(err)
	}
	h2, err := Hash(mustParse(t, base))
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatal("same object hashed twice must agree")
	}

	// Every edit class Mastodon can apply to a status flips the hash:
	// body, CW add, media add, and `updated` (populated by the instance
	// on any edit) all land in the hashed set.
	edits := map[string]string{
		"body edit": `{"id":"https://x/10","attributedTo":"https://x/a","content":"<p>two</p>","published":"p"}`,
		"cw add":    `{"id":"https://x/10","attributedTo":"https://x/a","content":"<p>one</p>","published":"p","summary":"cw"}`,
		"media add": `{"id":"https://x/10","attributedTo":"https://x/a","content":"<p>one</p>","published":"p","attachment":[{"mediaType":"image/png","name":null}]}`,
		"updated":   `{"id":"https://x/10","attributedTo":"https://x/a","content":"<p>one</p>","published":"p","updated":"2026-09-06T00:00:00Z"}`,
	}
	for name, raw := range edits {
		h, err := Hash(mustParse(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		if h == h1 {
			t.Fatalf("%s did not change the hash", name)
		}
	}

	// Unhashed noise must NOT move it: url (signed CDN link — same media
	// with and without it), @context, to/cc audience, and arbitrary extra
	// properties.
	withMedia := `{"id":"https://x/10","attributedTo":"https://x/a","content":"<p>one</p>","published":"p","attachment":[{"mediaType":"image/png","name":null}]}`
	hMedia, err := Hash(mustParse(t, withMedia))
	if err != nil {
		t.Fatal(err)
	}
	noise := map[string]string{
		"signed media url": `{"id":"https://x/10","attributedTo":"https://x/a","content":"<p>one</p>","published":"p","attachment":[{"mediaType":"image/png","name":null,"url":"https://cdn/x?sig=abc"}]}`,
		"@context":         `{"@context":["https://www.w3.org/ns/activitystreams",{"sensitive":"as:sensitive"}],"id":"https://x/10","attributedTo":"https://x/a","content":"<p>one</p>","published":"p"}`,
		"audience":         `{"id":"https://x/10","attributedTo":"https://x/a","content":"<p>one</p>","published":"p","to":["https://www.w3.org/ns/activitystreams#Public"],"cc":["https://x/a/followers"]}`,
		"extra property":   `{"id":"https://x/10","attributedTo":"https://x/a","content":"<p>one</p>","published":"p","sensitive":false,"sparkdream_extra":12345}`,
	}
	for name, raw := range noise {
		h, err := Hash(mustParse(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		want := h1
		if name == "signed media url" {
			want = hMedia
		}
		if h != want {
			t.Fatalf("%s moved the hash — unedited status must hash stably", name)
		}
	}

	// Sanity: the hash equals sha256 of the canonical bytes.
	c, err := Canonicalize(mustParse(t, base))
	if err != nil {
		t.Fatal(err)
	}
	var want [32]byte
	want = sha256.Sum256(c)
	if h1 != want {
		t.Fatal("Hash is not sha256(Canonicalize)")
	}
}

// JSON round-trip of the projected form (used by the CLI's canon
// output) must survive encoding/json exactly.
func TestCanonicalFormIsValidJSON(t *testing.T) {
	raw := `{"id":"https://x/20","attributedTo":{"a":1},"attachment":[{"mediaType":"image/png","name":"n"}],"x":true}`
	c, err := Canonicalize(mustParse(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	var check map[string]any
	if err := json.Unmarshal(c, &check); err != nil {
		t.Fatalf("canonical form is not valid JSON: %v (%s)", err, c)
	}
	if len(check) != 8 {
		t.Fatalf("projected form has %d keys, want 8: %s", len(check), c)
	}
}
