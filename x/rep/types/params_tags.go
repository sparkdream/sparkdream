package types

import "cosmossdk.io/math"

const (
	DefaultMaxTagLength    = uint64(32)
	DefaultTagExpiration   = int64(2592000)
	DefaultMaxTotalTags    = uint64(10000)
	DefaultMinRepTierTags  = uint32(1)
	DefaultMaxTagReporters = uint64(50)
)

// DefaultTagReportBond is the DREAM bond each reporter escrows to back a tag
// report: 1 DREAM. It is forfeited when the report is dismissed, so a report
// costs something if it is unfounded (REP-E-11; it was 10 micro-DREAM and
// always refunded).
var DefaultTagReportBond = math.NewInt(1_000_000)
