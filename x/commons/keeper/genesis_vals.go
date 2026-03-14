//go:build !testparams

package keeper

import "time"

// Production values for genesis bootstrap.
// To use relaxed testing values, build with: go build -tags testparams

var GenesisNames = map[string]string{
	// Populate with mainnet genesis addresses before launch.
	// "sprkdrm1...": "Alice",
	// "sprkdrm1...": "Bob",
}

var FounderName = "Alice"

// --- COMMONS PILLAR ---
var CommonsCouncilStandardMinExecution = 72 * time.Hour  // 3 Days
var CommonsMembershipMinExecution = 504 * time.Hour      // 21 Days
var CommonsOpsMinExecution = 24 * time.Hour              // 1 Day

// --- TECHNICAL PILLAR ---
var TechCouncilStandardMinExecution = 72 * time.Hour     // 3 Days
var TechMembershipMinExecution = 168 * time.Hour         // 7 Days
var TechOpsMinExecution = 24 * time.Hour                 // 1 Day

// --- ECOSYSTEM PILLAR ---
var EcoCouncilStandardMinExecution = 72 * time.Hour      // 3 Days
var EcoMembershipMinExecution = 168 * time.Hour          // 7 Days
var EcoOpsMinExecution = 24 * time.Hour                  // 1 Day

// --- SUPERVISORY BOARD ---
var SupervisoryMinExecution = 24 * time.Hour             // 1 Day

// --- UPDATE COOLDOWNS ---
var CouncilUpdateCooldown = 168 * time.Hour              // 7 Days
var CommitteeUpdateCooldown = 24 * time.Hour             // 1 Day
var SupervisoryUpdateCooldown = 720 * time.Hour          // 30 Days
