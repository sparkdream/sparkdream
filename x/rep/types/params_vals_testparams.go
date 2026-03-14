//go:build testparams

package types

import (
	"cosmossdk.io/math"
)

// Testing values — reduced for faster trust level progression during integration tests.
// Build with: go build -tags testparams
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(10),  // production: 50
		ProvisionalMinInterims:       1,                      // production: 3
		EstablishedMinRep:            math.LegacyNewDec(50),  // production: 200
		EstablishedMinInterims:       3,                      // production: 10
		TrustedMinRep:                math.LegacyNewDec(100), // production: 500
		TrustedMinSeasons:            0,                      // production: 1
		CoreMinRep:                   math.LegacyNewDec(200), // production: 1000
		CoreMinSeasons:               0,                      // production: 2
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 2,  // production: 1
		EstablishedInvitationCredits: 5,  // production: 3
		TrustedInvitationCredits:     10, // production: 5
		CoreInvitationCredits:        20, // production: 10
	}
}
