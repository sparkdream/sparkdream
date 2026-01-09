package testutil

import (
	"cosmossdk.io/math"
)

// DecPtr converts a string to a LegacyDec pointer.
// It panics on error, so use this ONLY for tests or hardcoded constants.
func DecPtr(s string) *math.LegacyDec {
	d, err := math.LegacyNewDecFromStr(s)
	if err != nil {
		panic(err)
	}
	return &d
}

func IntPtr(n int64) *math.Int {
	i := math.NewInt(n)
	return &i
}
