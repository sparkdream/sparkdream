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

// GenesisHandles maps each founding address to the x/name handles that should
// be registered in its name at chain start. The first handle in the slice is
// auto-set as the address's primary (so reverse resolution works out of the
// box). Founders without an entry here can register handles manually post-
// genesis. Squat-prevention rationale: without this seeding, anyone who
// becomes an x/rep member could snipe a founder's canonical handle.
var GenesisHandles = map[string][]string{
	"sprkdrm1yhjdr8kxsrer3kcqpdrc2zd0kggvsj4c3vazkd": {"kingofbitchain", "kob", "kobot"},
	"sprkdrm19wsctgkpk93wkquu7t8g07gnvwzwdupshys9mu": {"valya", "valyanarts", "valya_narts"},
	"sprkdrm1emtnqs9qw9vrg5lsa58dyt8llq5fyenylmqy3p": {"cozmonika", "cozmonika_art"},
	"sprkdrm1psq079p8erng2pf37nvvvmpqpetkknpmwxx4r8": {"viorika", "viorika_art"},
	"sprkdrm1wk6eh9zrw7n6xqmyw2yqja58ekpwy3h5u4gkge": {"uyen", "uyentrangjerde", "utgdeergirl"},
	"sprkdrm1crwfn2z2230jhtlaxwphyz0xrmuwc5ntc47vak": {"houri", "hourinaeimi"},
	"sprkdrm1x39wrr0l8x5lvxzuwff65t7zkw23fyyeres2mu": {"gilda", "atefatg"},
	// N/A founders intentionally omitted; can be added by chain upgrade.
}

var FounderName = "King of Bitchain"

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
