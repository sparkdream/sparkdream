package types

import "cosmossdk.io/math"

const (
	DefaultMaxTagLength   = uint64(32)
	DefaultTagExpiration  = int64(2592000)
	DefaultMaxTotalTags   = uint64(10000)
	DefaultMinRepTierTags = uint32(1)
	DefaultMaxTagReporters = uint64(50)
)

// DefaultTagReportBond is the DREAM bond each reporter escrows to back a tag report.
var DefaultTagReportBond = math.NewInt(10)
