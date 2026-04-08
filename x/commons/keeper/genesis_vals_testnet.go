//go:build testnet

package keeper

import "time"

// Testnet values — 2x devnet timers, closer to production cadence.
// Build with: go build -tags testnet

var GenesisNames = map[string]string{
	"sprkdrm1yhjdr8kxsrer3kcqpdrc2zd0kggvsj4c3vazkd": "King of Bitchain",
	"sprkdrm19wsctgkpk93wkquu7t8g07gnvwzwdupshys9mu": "Valya",
	"sprkdrm1emtnqs9qw9vrg5lsa58dyt8llq5fyenylmqy3p": "Cozmonika",
	"sprkdrm1psq079p8erng2pf37nvvvmpqpetkknpmwxx4r8": "Viorika",
	"sprkdrm1wk6eh9zrw7n6xqmyw2yqja58ekpwy3h5u4gkge": "Uyen",
	"sprkdrm1dqpr060l2pxy08j7q4gaahnmchs7qlhmf2w4y9": "N/A",
	"sprkdrm1crwfn2z2230jhtlaxwphyz0xrmuwc5ntc47vak": "Houri",
	"sprkdrm1x39wrr0l8x5lvxzuwff65t7zkw23fyyeres2mu": "Gilda",
	"sprkdrm1jqyzam9sewlmf704c84ysmkvhaqy8l0tpwysfs": "N/A",
}

var FounderName = "King of Bitchain"

// --- COMMONS PILLAR ---
var CommonsCouncilStandardMinExecution = 10 * time.Minute // production: 72h
var CommonsMembershipMinExecution = 1 * time.Hour         // production: 504h
var CommonsOpsMinExecution = 10 * time.Minute             // production: 24h

// --- TECHNICAL PILLAR ---
var TechCouncilStandardMinExecution = 10 * time.Minute // production: 72h
var TechMembershipMinExecution = 30 * time.Minute      // production: 168h
var TechOpsMinExecution = 10 * time.Minute             // production: 24h

// --- ECOSYSTEM PILLAR ---
var EcoCouncilStandardMinExecution = 10 * time.Minute // production: 72h
var EcoMembershipMinExecution = 30 * time.Minute      // production: 168h
var EcoOpsMinExecution = 10 * time.Minute             // production: 24h

// --- SUPERVISORY BOARD ---
var SupervisoryMinExecution = 10 * time.Minute // production: 24h

// --- UPDATE COOLDOWNS ---
var CouncilUpdateCooldown = 30 * time.Minute   // production: 168h
var CommitteeUpdateCooldown = 10 * time.Minute // production: 24h
var SupervisoryUpdateCooldown = 2 * time.Hour  // production: 720h
