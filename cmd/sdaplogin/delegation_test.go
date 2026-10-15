package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"
)

// testService is the host the harness's redirect URI names.
const testService = "social.phoenix.example"

// granterAddr derives a second test address, the member who grants.
func granterAddr(seed string) (bech string, raw []byte) {
	sum := sha256.Sum256([]byte(seed))
	raw = sum[:20]
	bech, _ = bech32.ConvertAndEncode("sprkdrm", raw)
	return bech, raw
}

// grantJSON renders an x/session Grant the way the LCD does.
func grantJSON(id, granter, grantee, typ, status, service string, expires time.Time) string {
	return fmt.Sprintf(`{"id":%q,"granter":%q,"grantee":%q,"type":%q,"status":%q,"expires_at":%q,"sign_in":{"service":%q}}`,
		id, granter, grantee, typ, status, expires.UTC().Format(time.RFC3339), service)
}

// delegationHarness: the harness key holds a SIGN_IN grant (id 7) from a
// member named kingofbitchain, and is not a member itself.
func delegationHarness(t *testing.T) (*harness, string, []byte) {
	t.Helper()
	h := newHarness(t, "")
	granter, raw := granterAddr("granter")
	h.lcd.mu.Lock()
	h.lcd.members[granter] = `{"status":"MEMBER_STATUS_ACTIVE","trust_level":"TRUST_LEVEL_ESTABLISHED"}`
	h.lcd.names[granter] = "kingofbitchain"
	h.lcd.grants["7"] = grantJSON("7", granter, h.addr, signInGrantType, grantStatusActive, testService, time.Now().Add(time.Hour))
	h.lcd.mu.Unlock()
	return h, granter, raw
}

func (h *harness) accountChoices(t *testing.T, request string) (int, []accountChoice) {
	t.Helper()
	q := url.Values{"request": {request}, "chain": {"fleet-phoenix"}, "address": {h.addr}}
	resp, err := http.Get(h.http.URL + "/authorize/accounts?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Accounts []accountChoice `json:"accounts"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Accounts
}

func TestDelegatedLoginFlow(t *testing.T) {
	h, granter, raw := delegationHarness(t)
	request, challenge := h.openPage(t)

	status, accounts := h.accountChoices(t, request)
	if status != http.StatusOK || len(accounts) != 1 || accounts[0].Address != granter || accounts[0].Name != "kingofbitchain" {
		t.Fatalf("accounts: %d %+v", status, accounts)
	}

	status, out := h.verifyAs(t, request, h.sign(t, signedText(challenge, granter)), granter)
	if status != http.StatusOK {
		t.Fatalf("verify: %d %v", status, out)
	}
	code, _ := url.Parse(out["redirect"])
	status, tok := h.exchange(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {code.Query().Get("code")}, "redirect_uri": {testRedirect},
		"code_verifier": {verifier},
	}, true)
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, tok)
	}
	claims := checkIDToken(t, h, tok["id_token"].(string))
	for k, want := range map[string]any{
		"sub":                 hex.EncodeToString(raw), // the granter's account, not the signer's
		"preferred_username":  "kingofbitchain",
		"sparkdream_delegate": h.addr,
		"sparkdream_grant":    "7",
		"sparkdream_chain":    "fleet-phoenix",
	} {
		if claims[k] != want {
			t.Errorf("id_token %s = %v, want %v", k, claims[k], want)
		}
	}

	// userinfo, which Mastodon's OIDC client reads, carries the delegation too
	req, _ := http.NewRequest(http.MethodGet, h.http.URL+"/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok["access_token"].(string))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&info)
	resp.Body.Close()
	if info["sub"] != hex.EncodeToString(raw) || info["sparkdream_delegate"] != h.addr || info["sparkdream_grant"] != "7" {
		t.Fatalf("userinfo: %v", info)
	}
}

func TestDelegatedLoginRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(h *harness, granter string)
		// signPlain signs the bare challenge instead of the one naming the account
		signPlain bool
		status    int
		want      string
	}{
		{"signature does not name the account", nil, true, http.StatusForbidden, "signature rejected"},
		{"no grant", func(h *harness, _ string) { delete(h.lcd.grants, "7") }, false, http.StatusForbidden, "no active sign-in grant"},
		{"grant for another service", func(h *harness, g string) {
			h.lcd.grants["7"] = grantJSON("7", g, h.addr, signInGrantType, grantStatusActive, "other.example", time.Now().Add(time.Hour))
		}, false, http.StatusForbidden, "no active sign-in grant"},
		{"expired grant", func(h *harness, g string) {
			h.lcd.grants["7"] = grantJSON("7", g, h.addr, signInGrantType, grantStatusActive, testService, time.Now().Add(-time.Minute))
		}, false, http.StatusForbidden, "no active sign-in grant"},
		{"session key, not a sign-in grant", func(h *harness, g string) {
			h.lcd.grants["7"] = grantJSON("7", g, h.addr, "GRANT_TYPE_SESSION_KEY", grantStatusActive, testService, time.Now().Add(time.Hour))
		}, false, http.StatusForbidden, "no active sign-in grant"},
		{"granter no longer a member", func(h *harness, g string) { delete(h.lcd.members, g) }, false, http.StatusForbidden, "not an active member"},
		{"granter has no name", func(h *harness, g string) { delete(h.lcd.names, g) }, false, http.StatusForbidden, "no primary name"},
		{"chain down", func(h *harness, _ string) { h.lcd.down = true }, false, http.StatusBadGateway, "could not reach"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, granter, _ := delegationHarness(t)
			if tc.mutate != nil {
				h.lcd.mu.Lock()
				tc.mutate(h, granter)
				h.lcd.mu.Unlock()
			}
			request, challenge := h.openPage(t)
			text := signedText(challenge, granter)
			if tc.signPlain {
				text = challenge
			}
			status, out := h.verifyAs(t, request, h.sign(t, text), granter)
			if status != tc.status || !strings.Contains(out["error"], tc.want) {
				t.Fatalf("verify: %d %v, want %d containing %q", status, out, tc.status, tc.want)
			}
		})
	}
}

func TestDelegatedLoginNeedsSignerMembershipOnlyForOwnAccount(t *testing.T) {
	// the harness key is not a member: signing in to its own account fails,
	// while the granted account works (TestDelegatedLoginFlow)
	h, _, _ := delegationHarness(t)
	request, challenge := h.openPage(t)
	status, out := h.verify(t, request, h.sign(t, challenge))
	if status != http.StatusForbidden || !strings.Contains(out["error"], "not an active member") {
		t.Fatalf("own account: %d %v", status, out)
	}
}

func TestAccountChoices(t *testing.T) {
	h, granter, _ := delegationHarness(t)
	other, _ := granterAddr("nameless")
	h.lcd.mu.Lock()
	// a second granter without a primary name has no handle to offer
	h.lcd.grants["8"] = grantJSON("8", other, h.addr, signInGrantType, grantStatusActive, testService, time.Now().Add(time.Hour))
	// a grant for another service is not offered
	h.lcd.grants["9"] = grantJSON("9", other, h.addr, signInGrantType, grantStatusActive, "other.example", time.Now().Add(time.Hour))
	h.lcd.mu.Unlock()

	request, _ := h.openPage(t)
	if status, accounts := h.accountChoices(t, request); status != http.StatusOK || len(accounts) != 1 || accounts[0].Address != granter {
		t.Fatalf("accounts: %d %+v", status, accounts)
	}

	// a chain that cannot answer (or predates the grant type) offers none
	h.lcd.mu.Lock()
	h.lcd.down = true
	h.lcd.mu.Unlock()
	if status, accounts := h.accountChoices(t, request); status != http.StatusOK || len(accounts) != 0 {
		t.Fatalf("chain down: %d %+v", status, accounts)
	}

	// only for an open login page
	if status, _ := h.accountChoices(t, "no-such-request"); status != http.StatusBadRequest {
		t.Fatalf("unknown request: %d", status)
	}
}

func TestSignInGrantStatus(t *testing.T) {
	h, _, raw := delegationHarness(t)
	ask := func(chain, id, granterHex, grantee string) (int, string) {
		t.Helper()
		q := url.Values{"granter": {granterHex}, "grantee": {grantee}}
		resp, err := http.Get(h.http.URL + "/sign-in-grant/" + chain + "/" + id + "?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out["status"]
	}
	granterHex := hex.EncodeToString(raw)

	if _, st := ask("fleet-phoenix", "7", granterHex, h.addr); st != "active" {
		t.Fatalf("live grant: %s", st)
	}
	_, otherRaw := granterAddr("someone else")
	if _, st := ask("fleet-phoenix", "7", hex.EncodeToString(otherRaw), h.addr); st != "inactive" {
		t.Fatalf("grant of another granter: %s", st)
	}
	if _, st := ask("fleet-phoenix", "7", granterHex, "sprkdrm1someoneelse"); st != "inactive" {
		t.Fatalf("grant to another grantee: %s", st)
	}
	if _, st := ask("unlinked-chain", "7", granterHex, h.addr); st != "inactive" {
		t.Fatalf("unlinked chain: %s", st)
	}
	if status, _ := ask("fleet-phoenix", "seven", granterHex, h.addr); status != http.StatusBadRequest {
		t.Fatalf("bad id: %d", status)
	}

	h.lcd.mu.Lock()
	h.lcd.down = true
	h.lcd.mu.Unlock()
	if _, st := ask("fleet-phoenix", "7", granterHex, h.addr); st != "unknown" {
		t.Fatalf("chain down: %s", st)
	}

	h.lcd.mu.Lock()
	h.lcd.down = false
	delete(h.lcd.grants, "7") // revoked: x/session deletes it
	h.lcd.mu.Unlock()
	if _, st := ask("fleet-phoenix", "7", granterHex, h.addr); st != "inactive" {
		t.Fatalf("revoked grant: %s", st)
	}
}
