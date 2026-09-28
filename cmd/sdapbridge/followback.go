package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"

	"sparkdream/internal/sdaptx"
)

// Follow-back: on an open peer ("*", no curation collection) the bridge
// manages no follows (curation.go), so an author who opts in by following
// the bridge account would never reach its home timeline. For the bridge's
// OWN instance, whose accounts are wallet sign-ins, the bridge follows back
// each local follower whose wallet is an active x/rep member of THIS chain,
// and unfollows the ones it followed back once they unfollow it or stop
// being members. Remote followers are never followed back: an open peer
// would otherwise let any fediverse account anchor content on the chain.
//
// It runs only when the instance's sign-ups are closed (registrations
// "none"): then a local account exists only through wallet sign-in. With
// open or approval-based sign-ups there is no automatic follow-back.
//
// The member's address comes from the instance's
// /api/v1/sparkdream/wallet_addresses (zz_sparkdream_wallet_login.rb), which
// answers only a bridge token, and only for accounts that follow that
// bridge. Follows the bridge did not make here (curated, or made by hand)
// are never touched.

// Membership is a chain's answer about one address.
type memberSource func(ctx context.Context, addr []byte) (bool, error)

// FetchMembership reads x/rep membership: true for an ACTIVE member, false
// for none or any other status. A lookup that fails is an error, never a no.
func FetchMembership(chain *sdaptx.Client, prefix string) memberSource {
	return func(ctx context.Context, addr []byte) (bool, error) {
		bech, err := bech32.ConvertAndEncode(prefix, addr)
		if err != nil {
			return false, err
		}
		var resp struct {
			Member struct {
				Status json.RawMessage `json:"status"`
			} `json:"member"`
		}
		if err := chain.GetJSON(ctx, "/sparkdream/rep/v1/member/"+url.PathEscape(bech), &resp); err != nil {
			if strings.Contains(err.Error(), "status 404") {
				return false, nil
			}
			return false, err
		}
		return memberActive(resp.Member.Status), nil
	}
}

// memberActive reads x/rep's MemberStatus from LCD JSON: a name, a number,
// or absent (proto3's zero, ACTIVE).
func memberActive(raw json.RawMessage) bool {
	s := strings.Trim(string(raw), `"`)
	return s == "" || s == "null" || s == "0" || s == "MEMBER_STATUS_ACTIVE"
}

// syncFollowBacks brings the bridge's follow-backs in line with its local
// member followers. Errors cost this pass only.
func (b *Bridge) syncFollowBacks(ctx context.Context) error {
	if !b.cfg.FollowBack || b.members == nil {
		return nil
	}
	host := b.cfg.instanceHost()
	own := b.peerOfHandle(handleFromAcct("bridge", host))
	if own == "" {
		return nil // the instance is not one of this bridge's peers
	}
	// a curated peer: curation decides whom the bridge follows
	if g, ok := b.gateFor(ctx, own); !ok || !g.open {
		return nil
	}
	open, err := b.masto.RegistrationsOpen(ctx)
	if err != nil {
		return err
	}
	if open {
		if time.Since(b.followBackOffLogged) > time.Hour {
			b.followBackOffLogged = time.Now()
			log.Printf("sdapbridge: follow-back off: %s accepts sign-ups, so a local account is not necessarily a member", host)
		}
		return nil
	}

	self, err := b.self(ctx)
	if err != nil {
		return err
	}
	followers, err := b.masto.Followers(ctx, self)
	if err != nil {
		return err
	}
	following, err := b.masto.Following(ctx, self)
	if err != nil {
		return err
	}
	follows := map[string]bool{}
	for _, a := range following {
		follows[a.ID] = true
	}
	localFollowers := map[string]Account{}
	for _, a := range followers {
		if a.ID != self && !strings.Contains(a.Acct, "@") {
			localFollowers[a.ID] = a
		}
	}

	// the accounts to ask about: followers not yet followed, and the
	// follow-backs still in place (still members?)
	var ask []string
	for id := range localFollowers {
		if !follows[id] || b.state.FollowedBack(id) {
			ask = append(ask, id)
		}
	}
	addrs := map[string]string{}
	if len(ask) > 0 {
		if addrs, err = b.masto.WalletAddresses(ctx, ask); err != nil {
			return err
		}
	}
	member := func(id string) (bool, error) {
		addr, err := hex.DecodeString(addrs[id])
		if err != nil || len(addr) != 20 {
			return false, nil // not a wallet account
		}
		return b.members(ctx, addr)
	}

	for id, a := range localFollowers {
		if follows[id] {
			continue
		}
		ok, err := member(id)
		if err != nil {
			log.Printf("sdapbridge: follow-back: membership of @%s: %v", a.Acct, err)
			continue
		}
		if !ok {
			continue
		}
		if err := b.masto.Follow(ctx, id); err != nil {
			log.Printf("sdapbridge: follow-back @%s: %v", a.Acct, err)
			continue
		}
		b.state.RecordFollowBack(id)
		follows[id] = true
		log.Printf("sdapbridge: followed back @%s: a member of this chain who follows the bridge", a.Acct)
	}

	for _, id := range b.state.FollowBacks() {
		if !follows[id] {
			b.state.ForgetFollowBack(id) // unfollowed by other means
			continue
		}
		reason := ""
		if _, still := localFollowers[id]; !still {
			reason = "no longer follows the bridge"
		} else {
			ok, err := member(id)
			if err != nil {
				log.Printf("sdapbridge: follow-back: membership of account %s: %v", id, err)
				continue // undetermined: leave it
			}
			if !ok {
				reason = "no longer an active member of this chain"
			}
		}
		if reason == "" {
			continue
		}
		if err := b.masto.Unfollow(ctx, id); err != nil {
			log.Printf("sdapbridge: undo follow-back of account %s: %v", id, err)
			continue
		}
		b.state.ForgetFollowBack(id)
		log.Printf("sdapbridge: unfollowed account %s: %s", id, reason)
	}
	return b.state.Save()
}

// RegistrationsOpen reports whether the instance accepts sign-ups (open, or
// with approval), from /api/v2/instance.
func (m *MastodonClient) RegistrationsOpen(ctx context.Context) (bool, error) {
	var out struct {
		Registrations struct {
			Enabled bool `json:"enabled"`
		} `json:"registrations"`
	}
	if err := m.getJSON(ctx, "/api/v2/instance", &out); err != nil {
		return false, err
	}
	return out.Registrations.Enabled, nil
}

// maxWalletLookup is the instance endpoint's per-request cap.
const maxWalletLookup = 100

// WalletAddresses returns the wallet address (hex) of each account among
// ids that signed in with a wallet and follows this bridge, keyed by
// account id. Accounts it does not return are not wallet accounts, or do
// not follow the bridge.
func (m *MastodonClient) WalletAddresses(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for start := 0; start < len(ids); start += maxWalletLookup {
		end := min(start+maxWalletLookup, len(ids))
		q := url.Values{}
		for _, id := range ids[start:end] {
			q.Add("id[]", id)
		}
		var page map[string]string
		if err := m.getJSON(ctx, "/api/v1/sparkdream/wallet_addresses?"+q.Encode(), &page); err != nil {
			return nil, fmt.Errorf("wallet addresses: %w", err)
		}
		for k, v := range page {
			out[k] = v
		}
	}
	return out, nil
}
