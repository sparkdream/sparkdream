package types

import "cosmossdk.io/collections"

// MemberReportKey is the prefix to retrieve all MemberReport entries.
var MemberReportKey = collections.NewPrefix("memberReport/value/")
