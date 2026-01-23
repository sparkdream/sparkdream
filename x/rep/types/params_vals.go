package types

import (
	"cosmossdk.io/math"
)

////////////////////////////
// BEGIN PRODUCTION VALUES

func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		// Trust level upgrade requirements
		ProvisionalMinRep:      math.LegacyNewDec(50),   // 50 rep to reach PROVISIONAL
		ProvisionalMinInterims: 3,                       // 3 completed interims
		EstablishedMinRep:      math.LegacyNewDec(200),  // 200 rep to reach ESTABLISHED
		EstablishedMinInterims: 10,                      // 10 completed interims
		TrustedMinRep:          math.LegacyNewDec(500),  // 500 rep to reach TRUSTED
		TrustedMinSeasons:      1,                       // 1 season of membership
		CoreMinRep:             math.LegacyNewDec(1000), // 1000 rep to reach CORE
		CoreMinSeasons:         2,                       // 2 seasons of membership
		// Invitation credits per trust level (max per season)
		NewInvitationCredits:         0,  // NEW cannot invite
		ProvisionalInvitationCredits: 1,  // Can invite 1 per season
		EstablishedInvitationCredits: 3,  // Moderate invite ability
		TrustedInvitationCredits:     5,  // Solid contributors
		CoreInvitationCredits:        10, // Founders/long-term members
	}
}

// END PRODUCTION VALUES
////////////////////////////

////////////////////////////
// BEGIN TESTING VALUES
/*
// These values are reduced to allow faster trust level progression during integration testing.
// To switch to production values, comment this section and uncomment the production section above.
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		// Trust level upgrade requirements - REDUCED for testing
		ProvisionalMinRep:      math.LegacyNewDec(10),  // 10 rep (production: 50)
		ProvisionalMinInterims: 1,                      // 1 interim (production: 3)
		EstablishedMinRep:      math.LegacyNewDec(50),  // 50 rep (production: 200)
		EstablishedMinInterims: 3,                      // 3 interims (production: 10)
		TrustedMinRep:          math.LegacyNewDec(100), // 100 rep (production: 500)
		TrustedMinSeasons:      0,                      // 0 seasons (production: 1)
		CoreMinRep:             math.LegacyNewDec(200), // 200 rep (production: 1000)
		CoreMinSeasons:         0,                      // 0 seasons (production: 2)
		// Invitation credits per trust level (max per season) - INCREASED for testing
		NewInvitationCredits:         0,  // NEW cannot invite
		ProvisionalInvitationCredits: 2,  // (production: 1)
		EstablishedInvitationCredits: 5,  // (production: 3)
		TrustedInvitationCredits:     10, // (production: 5)
		CoreInvitationCredits:        20, // (production: 10)
	}
}
*/
// END TESTING VALUES
////////////////////////////
