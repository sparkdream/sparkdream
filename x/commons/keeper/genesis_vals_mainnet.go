//go:build mainnet

package keeper

import "time"

// Mainnet values for genesis bootstrap.
// Build with: go build -tags mainnet

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

var FounderName = "Alice"

// --- COMMONS PILLAR ---
var CommonsCouncilStandardMinExecution = 72 * time.Hour // 3 Days
var CommonsMembershipMinExecution = 504 * time.Hour     // 21 Days
var CommonsOpsMinExecution = 24 * time.Hour             // 1 Day

// --- TECHNICAL PILLAR ---
var TechCouncilStandardMinExecution = 72 * time.Hour // 3 Days
var TechMembershipMinExecution = 168 * time.Hour     // 7 Days
var TechOpsMinExecution = 24 * time.Hour             // 1 Day

// --- ECOSYSTEM PILLAR ---
var EcoCouncilStandardMinExecution = 72 * time.Hour // 3 Days
var EcoMembershipMinExecution = 168 * time.Hour     // 7 Days
var EcoOpsMinExecution = 24 * time.Hour             // 1 Day

// --- SUPERVISORY BOARD ---
var SupervisoryMinExecution = 24 * time.Hour // 1 Day

// --- UPDATE COOLDOWNS ---
var CouncilUpdateCooldown = 168 * time.Hour     // 7 Days
var CommitteeUpdateCooldown = 24 * time.Hour    // 1 Day
var SupervisoryUpdateCooldown = 720 * time.Hour // 30 Days
