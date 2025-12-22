package keeper

import "time"

////////////////////////////
// BEGIN PRODUCTION VALUES

// Note: Ensure these addresses are valid in your mainnet genesis
var GenesisNames = map[string]string{
	// "sprkdrm1...": "Alice",
	// "sprkdrm1...": "Bob",
}

// The account that initializes the "Founder" role in committees
var FounderName = "Alice"

// --- COMMONS PILLAR ---
// General Council Decisions (Spend > Threshold, Policy Changes)
var CommonsCouncilStandardMinExecution = 72 * time.Hour // 3 Days

// HR/Recruiting (Adding Council Members) - SLOW
// Supervisory council (meta-group) needs ample time to review changes to Council membership
var CommonsMembershipMinExecution = 504 * time.Hour // 21 Days

// Day-to-Day Spending - FAST
var CommonsOpsMinExecution = 24 * time.Hour // 1 Day

// --- TECHNICAL PILLAR ---
// Chain Upgrades & Parameters
var TechCouncilStandardMinExecution = 72 * time.Hour // 3 Days

// HR/Recruiting (Adding Tech Council Members) - SLOW
var TechMembershipMinExecution = 168 * time.Hour // 7 Days

// Operational Spending - FAST
var TechOpsMinExecution = 24 * time.Hour // 1 Day

// --- ECOSYSTEM PILLAR ---
// Treasury & Incentives
var EcoCouncilStandardMinExecution = 72 * time.Hour // 3 Days

// HR/Recruiting (Adding Eco Council Members) - SLOW
var EcoMembershipMinExecution = 168 * time.Hour // 7 Days

// Operational Spending - FAST
var EcoOpsMinExecution = 24 * time.Hour // 1 Day

// --- UPDATE COOLDOWNS (Prevents Instability) ---
var CouncilUpdateCooldown = 168 * time.Hour     // 7 Days
var CommitteeUpdateCooldown = 24 * time.Hour    // 1 Day
var SupervisoryUpdateCooldown = 720 * time.Hour // 30 Days

// END PRODUCTION VALUES
////////////////////////////

////////////////////////////
// BEGIN TESTING VALUES
/*
var GenesisNames = map[string]string{
	"sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan": "Alice",
	"sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y": "Bob",
	"sprkdrm1a0gkdyzcnsjrl2s5vlywkancparhp53fucz3zz": "Carol",
}

var FounderName = "Alice"

// Min Execution
var CommonsCouncilStandardMinExecution = 1 * time.Second
var CommonsMembershipMinExecution = 1 * time.Second
var CommonsOpsMinExecution = 1 * time.Second
var TechCouncilStandardMinExecution = 1 * time.Second
var TechMembershipMinExecution = 1 * time.Second
var TechOpsMinExecution = 1 * time.Second
var EcoCouncilStandardMinExecution = 1 * time.Second
var EcoMembershipMinExecution = 1 * time.Second
var EcoOpsMinExecution = 1 * time.Second

// Cooldowns
var CouncilUpdateCooldown = 1 * time.Second
var CommitteeUpdateCooldown = 1 * time.Second
var SupervisoryUpdateCooldown = 1 * time.Second
*/
// END TESTING VALUES
////////////////////////////
