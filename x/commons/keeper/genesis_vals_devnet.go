//go:build devnet

package keeper

import "time"

// Devnet values — accelerated but human-observable governance timers.
// Build with: go build -tags devnet

var GenesisNames = map[string]string{
	"sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan": "Alice",
	"sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y": "Bob",
	"sprkdrm1a0gkdyzcnsjrl2s5vlywkancparhp53fucz3zz": "Carol",
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
