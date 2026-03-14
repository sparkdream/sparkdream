//go:build testparams

package keeper

import "time"

// Testing values — reduced for faster governance during integration tests.
// Build with: go build -tags testparams

var GenesisNames = map[string]string{
	"sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan": "Alice",
	"sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y": "Bob",
	"sprkdrm1a0gkdyzcnsjrl2s5vlywkancparhp53fucz3zz": "Carol",
}

var FounderName = "Alice"

// All execution periods set to 1 second for fast test iteration.
var CommonsCouncilStandardMinExecution = 1 * time.Second
var CommonsMembershipMinExecution = 1 * time.Second
var CommonsOpsMinExecution = 1 * time.Second
var TechCouncilStandardMinExecution = 1 * time.Second
var TechMembershipMinExecution = 1 * time.Second
var TechOpsMinExecution = 1 * time.Second
var EcoCouncilStandardMinExecution = 1 * time.Second
var EcoMembershipMinExecution = 1 * time.Second
var EcoOpsMinExecution = 1 * time.Second
var SupervisoryMinExecution = 1 * time.Second
var CouncilUpdateCooldown = 1 * time.Second
var CommitteeUpdateCooldown = 1 * time.Second
var SupervisoryUpdateCooldown = 1 * time.Second
