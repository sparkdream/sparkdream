// Package zenithrender is an x/artifact state renderer (state_version 2):
// RenderState receives the token's on-chain state as a JSON document and
// shows the current season and how many tokens the token holds.
package zenithrender

import (
	"strconv"
	"strings"
)

// field returns the raw value after "key": in a flat JSON document.
func field(doc, key string) string {
	i := strings.Index(doc, `"`+key+`":`)
	if i < 0 {
		return ""
	}
	rest := doc[i+len(key)+3:]
	end := strings.IndexAny(rest, ",}")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// RenderState is evaluated by x/artifact at query time as
// RenderState(classID, tokenID, seed, blockTime, height, state).
func RenderState(classID uint64, tokenID uint64, seed string, blockTime int64, height int64, state string) string {
	season := field(state, "season_number")
	held := field(state, "descendants")
	return `{"name":"Zenith #` + strconv.FormatUint(tokenID, 10) + `",` +
		`"description":"season ` + season + `, holding ` + held + `",` +
		`"attributes":[{"key":"version","value":` + strconv.Quote(field(state, "v")) + `}]}`
}
