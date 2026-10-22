package types

import (
	"encoding/json"
	"strconv"
)

// Renderer state bindings (docs/x-artifact-spec.md §7.16). A renderer
// declared with state_version 1 exports
//
//	func RenderState(classID uint64, tokenID uint64, seed string, blockTime int64, height int64, state string) string
//
// and receives the token's own on-chain state as a JSON document. Each
// version's field set is append-only and frozen once shipped: a renderer
// reads exactly the version it declared at class creation, so its output
// never changes because the chain learned a new field. New state goes in
// a new version.

const (
	// RendererStateFuncName is the entry point of a state-reading renderer.
	RendererStateFuncName = "RenderState"
	// RenderStateV1 is the first (and current) state version.
	RenderStateV1 uint32 = 1
	// MaxRenderStateVersion is the newest state version this chain serves
	// (RenderStateV2, render_state_v2.go).
	MaxRenderStateVersion = RenderStateV2
)

var rendererStateParams = []string{"uint64", "uint64", "string", "int64", "int64", "string"}

// RenderStateV1Doc is the v1 state document. Field order is the JSON
// order; never reorder, rename or remove a field.
type RenderStateV1Doc struct {
	V                uint32 `json:"v"`
	Owner            string `json:"owner"`
	Minter           string `json:"minter"`
	MintedAt         int64  `json:"minted_at"`
	ExpiresAt        int64  `json:"expires_at"`
	RedeemedAt       int64  `json:"redeemed_at"`
	Lock             string `json:"lock"`
	User             string `json:"user"`
	Parent           string `json:"parent"`
	Descendants      uint32 `json:"descendants"`
	DisplayedVersion uint32 `json:"displayed_version"`
	ClassSupply      uint64 `json:"class_supply"`
	ClassBurned      uint64 `json:"class_burned"`
	OwnerMember      bool   `json:"owner_member"`
	OwnerTrustLevel  int32  `json:"owner_trust_level"`
}

// NewRenderStateV1 builds the v1 document for a token. user is the renter
// ("" when not rented); member and trustLevel describe the owner in x/rep.
func NewRenderStateV1(c Class, t Token, user string, member bool, trustLevel int32) RenderStateV1Doc {
	doc := RenderStateV1Doc{
		V:                RenderStateV1,
		Owner:            t.Owner,
		Minter:           t.Minter,
		MintedAt:         t.MintedAt,
		ExpiresAt:        t.ExpiresAt,
		RedeemedAt:       t.RedeemedAt,
		Lock:             t.Lock.String(),
		User:             user,
		Descendants:      t.Descendants,
		DisplayedVersion: t.DisplayedVersion,
		ClassSupply:      c.Supply,
		ClassBurned:      c.Burned,
		OwnerMember:      member,
		OwnerTrustLevel:  trustLevel,
	}
	if t.IsNested() {
		doc.Parent = strconv.FormatUint(t.Parent.ClassId, 10) + "/" + strconv.FormatUint(t.Parent.TokenId, 10)
	}
	return doc
}

// JSON encodes the document deterministically (struct field order).
func (d RenderStateV1Doc) JSON() string {
	bz, err := json.Marshal(d)
	if err != nil {
		panic(err) // unreachable: plain struct of scalars
	}
	return string(bz)
}

// RenderStateExpr is the Gno expression a state-reading renderer is
// evaluated with.
func RenderStateExpr(classID, tokenID uint64, seed string, blockTime, height int64, state string) string {
	return RendererStateFuncName + "(" + strconv.FormatUint(classID, 10) + ", " + strconv.FormatUint(tokenID, 10) +
		", " + strconv.Quote(seed) + ", " + strconv.FormatInt(blockTime, 10) + ", " + strconv.FormatInt(height, 10) +
		", " + strconv.Quote(state) + ")"
}
