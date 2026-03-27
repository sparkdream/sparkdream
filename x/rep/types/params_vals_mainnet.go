//go:build mainnet

package types

import (
	"cosmossdk.io/math"
)

// Mainnet values for trust level configuration.
// Build with: go build -tags mainnet
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(50),
		ProvisionalMinInterims:       3,
		EstablishedMinRep:            math.LegacyNewDec(200),
		EstablishedMinInterims:       10,
		TrustedMinRep:                math.LegacyNewDec(500),
		TrustedMinSeasons:            1,
		CoreMinRep:                   math.LegacyNewDec(1000),
		CoreMinSeasons:               2,
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 1,
		EstablishedInvitationCredits: 3,
		TrustedInvitationCredits:     5,
		CoreInvitationCredits:        10,
	}
}
