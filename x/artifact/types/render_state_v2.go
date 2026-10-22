package types

import "encoding/json"

// RenderStateV2 extends v1 with x/season state and the token's nested
// contents. Its document is v1's fields in v1's order followed by the new
// ones, so a v2 renderer reads a strict superset of v1.
const RenderStateV2 uint32 = 2

// SeasonInfo is what renderers learn about the current season.
type SeasonInfo struct {
	Number uint64
	Status string
}

// MemberSeason is a member's standing in the current season.
type MemberSeason struct {
	Level, XP uint64
}

// RenderStateV2Doc is the v2 state document. Field order is the JSON
// order; never reorder, rename or remove a field.
type RenderStateV2Doc struct {
	RenderStateV1Doc
	SeasonNumber     uint64   `json:"season_number"`
	SeasonStatus     string   `json:"season_status"`
	OwnerSeasonLevel uint64   `json:"owner_season_level"`
	OwnerSeasonXP    uint64   `json:"owner_season_xp"`
	Children         []string `json:"children"`
	ContentsHash     string   `json:"contents_hash"`
}

// NewRenderStateV2 builds the v2 document. children are direct children as
// "class/token"; season and member are zero when x/season is not wired or
// the owner has no profile.
func NewRenderStateV2(v1 RenderStateV1Doc, season SeasonInfo, member MemberSeason, children []string, contentsHash string) RenderStateV2Doc {
	v1.V = RenderStateV2
	if children == nil {
		children = []string{}
	}
	return RenderStateV2Doc{
		RenderStateV1Doc: v1,
		SeasonNumber:     season.Number,
		SeasonStatus:     season.Status,
		OwnerSeasonLevel: member.Level,
		OwnerSeasonXP:    member.XP,
		Children:         children,
		ContentsHash:     contentsHash,
	}
}

// JSON encodes the document deterministically (struct field order, the
// embedded v1 fields first).
func (d RenderStateV2Doc) JSON() string {
	bz, err := json.Marshal(d)
	if err != nil {
		panic(err) // unreachable: plain struct of scalars and strings
	}
	return string(bz)
}
