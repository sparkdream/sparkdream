# x/rep Simulation Tests

This directory contains simulation operations for the x/rep module. Simulation tests are used by the Cosmos SDK to perform randomized, long-running tests that exercise the state machine with realistic transaction patterns.

## Overview

The simulation framework randomly generates and executes transactions to test:
- State transitions
- Invariants
- Edge cases
- Gas consumption
- Transaction failures and success paths

## Implementation Status

All 25 simulation operations have been fully implemented:

### Member Management
- ✅ `invite_member.go` - Invite new members with staked DREAM
- ✅ `accept_invitation.go` - Accept pending invitations

### DREAM Token Operations
- ✅ `transfer_dream.go` - Transfer DREAM between members (tips, gifts, bounties)
- ✅ `stake.go` - Create stakes on initiatives
- ✅ `unstake.go` - Remove existing stakes

### Project Management
- ✅ `propose_project.go` - Create new project proposals
- ✅ `approve_project_budget.go` - Approve project budgets (committee)
- ✅ `cancel_project.go` - Cancel projects

### Initiative Workflow
- ✅ `create_initiative.go` - Create initiatives within projects
- ✅ `assign_initiative.go` - Assign initiatives to members
- ✅ `submit_initiative_work.go` - Submit work for review
- ✅ `approve_initiative.go` - Approve initiative completion
- ✅ `abandon_initiative.go` - Abandon assigned initiatives
- ✅ `complete_initiative.go` - Complete initiatives and distribute rewards

### Challenge System
- ✅ `create_challenge.go` - Challenge completed initiatives
- ✅ `respond_to_challenge.go` - Respond to challenges
- ✅ `submit_juror_vote.go` - Submit jury votes on challenges
- ✅ `submit_expert_testimony.go` - Submit expert testimony

### Interim Compensation
- ✅ `create_interim.go` - Create interim work items
- ✅ `assign_interim.go` - Assign interim work
- ✅ `submit_interim_work.go` - Submit interim work
- ✅ `approve_interim.go` - Approve interim compensation
- ✅ `abandon_interim.go` - Abandon interim work
- ✅ `complete_interim.go` - Complete interim work

### Helpers
- ✅ `helpers.go` - Common utility functions for simulations

## Key Features

### State-Aware Operations
All simulation operations intelligently query existing state to:
- Find eligible members, projects, initiatives, etc.
- Validate preconditions before generating messages
- Return `NoOpMsg` when prerequisites aren't met

### Realistic Data Generation
- Random amounts based on balance constraints
- Appropriate status transitions
- Valid enum values (tiers, categories, types)
- Contextual field values

### Helper Functions
The `helpers.go` file provides utilities for:
- Finding entities by status (`findMember`, `findProject`, `findInitiative`, etc.)
- Filtering by criteria (DREAM balance, assignee, etc.)
- Generating random enums and tags
- Converting between types
- Ensuring member existence

## Usage

Simulations are automatically executed when running:

```bash
# Run simulations for all modules
make test-sim-full-app

# Run nondeterminism test
make test-sim-nondeterminism

# Run import/export simulation
make test-sim-import-export

# Run simulations after a software upgrade
make test-sim-after-import
```

## Configuration

Simulation weights are defined in [`x/rep/module/simulation.go`](../module/simulation.go). Each operation has a default weight of 100, which determines its relative frequency during simulation runs.

To adjust weights, modify the `defaultWeight*` constants in the WeightedOperations function.

## Testing Approach

Each simulation function follows this pattern:

1. **Find Eligible State**: Query for entities that meet preconditions
2. **Validate Prerequisites**: Check balances, status, permissions
3. **Generate Message**: Create realistic message with random data
4. **Execute Transaction**: Use `simulation.GenAndDeliverTxWithRandFees()`
5. **Handle Failures**: Return `NoOpMsg` for expected failures

## Examples

### Simple Operation (Stake)
```go
// Find a member with sufficient DREAM
staker, err := findMemberWithDream(r, ctx, k, minStake)
// Find an initiative to stake on
initiative, initID, err := findInitiative(r, ctx, k, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
// Generate random stake amount
stakeAmount := calculateStakeAmount(r, staker.DreamBalance)
// Execute transaction
msg := &types.MsgStake{...}
return simulation.GenAndDeliverTxWithRandFees(...)
```

### Complex Operation (Create Initiative)
```go
// Find active project
project, projectID, err := findProject(r, ctx, k, types.ProjectStatus_PROJECT_STATUS_ACTIVE)
// Find member creator
creator, err := findMember(r, ctx, k)
// Generate tier-appropriate budget
budget := calculateBudgetByTier(randomInitiativeTier(r))
// Create and submit
msg := &types.MsgCreateInitiative{...}
return simulation.GenAndDeliverTxWithRandFees(...)
```

## Debugging

To debug simulation failures:

1. **Enable verbose logging**: `--SimulationVerbose=true`
2. **Use specific seed**: `--SimulationSeed=<value>`
3. **Reduce operations**: `--SimulationNumBlocks=<small_value>`
4. **Check invariants**: Review invariant violations in output

## Future Enhancements

Potential improvements:
- [ ] Add operation weights based on realistic usage patterns
- [ ] Implement custom genesis generation for better initial state
- [ ] Add store decoders for better debugging
- [ ] Create proposal messages for governance simulation
- [ ] Add mutation testing for edge cases

## Related Files

- [`x/rep/module/simulation.go`](../module/simulation.go) - Operation weights and registration
- [`x/rep/keeper/`](../keeper/) - Business logic implementation
- [`x/rep/types/`](../types/) - Message and state type definitions
