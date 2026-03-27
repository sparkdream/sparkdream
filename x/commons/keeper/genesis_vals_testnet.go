//go:build testnet

package keeper

import "time"

// Testnet values — 2x devnet timers, closer to production cadence.
// Build with: go build -tags testnet

var GenesisNames = map[string]string{
	"sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan": "Alice",
	"sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y": "Bob",
	"sprkdrm1a0gkdyzcnsjrl2s5vlywkancparhp53fucz3zz": "Carol",
}

var FounderName = "Alice"

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
var CouncilUpdateCooldown = 30 * time.Minute     // production: 168h
var CommitteeUpdateCooldown = 10 * time.Minute   // production: 24h
var SupervisoryUpdateCooldown = 2 * time.Hour    // production: 720h
