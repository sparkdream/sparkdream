//go:build !mainnet && !testnet && !devnet

package keeper

import "time"

// Testing values — reduced for faster governance during integration tests.
// This is the default when no build tag is specified, or with: go build -tags testparams

var GenesisNames = map[string]string{
	"sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan": "Alice",
	"sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y": "Bob",
	"sprkdrm1a0gkdyzcnsjrl2s5vlywkancparhp53fucz3zz": "Carol",
}

// GenesisHandles — see genesis_vals_mainnet.go for the design rationale.
// E2E tests rely on these being claimed at chain start.
var GenesisHandles = map[string][]string{
	"sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan": {"alice"},
	"sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y": {"bob"},
	"sprkdrm1a0gkdyzcnsjrl2s5vlywkancparhp53fucz3zz": {"carol"},
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
