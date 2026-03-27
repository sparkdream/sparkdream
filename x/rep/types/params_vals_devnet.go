//go:build devnet

package types

import (
	"cosmossdk.io/math"
)

// Devnet values — reduced thresholds for faster progression with realistic gameplay.
// Build with: go build -tags devnet
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(25),  // production: 50
		ProvisionalMinInterims:       2,                      // production: 3
		EstablishedMinRep:            math.LegacyNewDec(100), // production: 200
		EstablishedMinInterims:       5,                      // production: 10
		TrustedMinRep:                math.LegacyNewDec(250), // production: 500
		TrustedMinSeasons:            1,                      // production: 1
		CoreMinRep:                   math.LegacyNewDec(500), // production: 1000
		CoreMinSeasons:               1,                      // production: 2
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 2,  // production: 1
		EstablishedInvitationCredits: 4,  // production: 3
		TrustedInvitationCredits:     7,  // production: 5
		CoreInvitationCredits:        15, // production: 10
	}
}
