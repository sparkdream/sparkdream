// Package aurorarender is an x/artifact renderer: a pure package whose
// Render returns the displayed TokenMetadata as JSON. It reads only its
// arguments (block time and height among them), so it works with today's
// gnovm bindings and never touches chain state.
package aurorarender

import "strconv"

// Render is evaluated by x/artifact at query time as
// Render(classID, tokenID, seed, blockTime, height).
func Render(classID uint64, tokenID uint64, seed string, blockTime int64, height int64) string {
	phase := "dawn"
	if height%2 == 1 {
		phase = "dusk"
	}
	return `{"name":"Aurora #` + strconv.FormatUint(tokenID, 10) + ` (` + phase + `)",` +
		`"description":"rendered at height ` + strconv.FormatInt(height, 10) + `",` +
		`"attributes":[{"key":"phase","value":"` + phase + `"},{"key":"class","value":"` + strconv.FormatUint(classID, 10) + `"}]}`
}
