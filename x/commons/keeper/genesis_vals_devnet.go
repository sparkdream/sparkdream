//go:build devnet

package keeper

import "time"

// Devnet values — accelerated but human-observable governance timers.
// Build with: go build -tags devnet

// GenesisNames is the fallback founder set for a devnet-tagged binary
// started from a genesis that carries no commons.founding_members. The
// canonical devnet genesis DOES carry that block (see
// deploy/config/network/devnet/config.yml), which overrides all three of
// GenesisNames / GenesisHandles / FounderName at once — so these values
// only matter for a chain someone inits from this build tag alone. Keep
// them in step with deploy/scripts/regenerate-network-genesis.py's
// DEVNET_ACCOUNTS regardless: a stale fallback bootstraps governance
// around addresses that hold no keys.
//
// hal is absent on purpose — it is the non-member test fixture.
var GenesisNames = map[string]string{
	"sprkdrm1jct5shlz26j2qgdeyzj2nwe5e79djprhcgpwrc": "Alice",
	"sprkdrm1ctk6qnsg5rf7ddupvkxth8q6h7d49h80vcjgg0": "Bob",
	"sprkdrm17tce9p442054fkwl9zrntt7k7h3fs92fcwjxke": "Carol",
	"sprkdrm1m6xjdt97j9a67smsy6n6h4vqgdkjy7jm8hvq8x": "Dave",
	"sprkdrm1jzxgmcv3xgqlagkfrppaf8jvpvf3lyqpkhljn9": "Erin",
	"sprkdrm1tfl0ys2wws5shrfre259drs5llh4ug83ne7q9e": "Felix",
	"sprkdrm1kllgzkzvjyq09r8unw4l69wl993u0tex5gd3v0": "Gwen",
}

// GenesisHandles — see genesis_vals_mainnet.go for the design rationale.
var GenesisHandles = map[string][]string{
	"sprkdrm1jct5shlz26j2qgdeyzj2nwe5e79djprhcgpwrc": {"alice"},
	"sprkdrm1ctk6qnsg5rf7ddupvkxth8q6h7d49h80vcjgg0": {"bob"},
	"sprkdrm17tce9p442054fkwl9zrntt7k7h3fs92fcwjxke": {"carol"},
	"sprkdrm1m6xjdt97j9a67smsy6n6h4vqgdkjy7jm8hvq8x": {"dave"},
	"sprkdrm1jzxgmcv3xgqlagkfrppaf8jvpvf3lyqpkhljn9": {"erin"},
	"sprkdrm1tfl0ys2wws5shrfre259drs5llh4ug83ne7q9e": {"felix"},
	"sprkdrm1kllgzkzvjyq09r8unw4l69wl993u0tex5gd3v0": {"gwen"},
}

var FounderName = "Alice"

// --- COMMONS PILLAR ---
var CommonsCouncilStandardMinExecution = 5 * time.Minute // production: 72h
var CommonsMembershipMinExecution = 30 * time.Minute     // production: 504h
var CommonsOpsMinExecution = 5 * time.Minute             // production: 24h

// --- TECHNICAL PILLAR ---
var TechCouncilStandardMinExecution = 5 * time.Minute // production: 72h
var TechMembershipMinExecution = 15 * time.Minute     // production: 168h
var TechOpsMinExecution = 5 * time.Minute             // production: 24h

// --- ECOSYSTEM PILLAR ---
var EcoCouncilStandardMinExecution = 5 * time.Minute // production: 72h
var EcoMembershipMinExecution = 15 * time.Minute     // production: 168h
var EcoOpsMinExecution = 5 * time.Minute             // production: 24h

// --- SUPERVISORY BOARD ---
var SupervisoryMinExecution = 5 * time.Minute // production: 24h

// --- UPDATE COOLDOWNS ---
var CouncilUpdateCooldown = 15 * time.Minute  // production: 168h
var CommitteeUpdateCooldown = 5 * time.Minute // production: 24h
var SupervisoryUpdateCooldown = 1 * time.Hour // production: 720h
