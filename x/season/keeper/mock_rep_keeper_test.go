package keeper_test

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// mockRepKeeper mocks the x/rep keeper for testing season transitions
type mockRepKeeper struct {
	// Balances maps address to DREAM balance
	Balances map[string]math.Int
	// ReputationScores maps address to tag -> score
	ReputationScores map[string]map[string]string
	// LifetimeReputation maps address to tag -> score (archived)
	LifetimeReputation map[string]map[string]string
	// CompletedInitiatives maps address to count
	CompletedInitiatives map[string]uint64
	// Members tracks membership status
	Members map[string]bool
	// ArchiveCallCount tracks how many times ArchiveSeasonalReputation was called
	ArchiveCallCount int
	// LastArchivedAddresses tracks addresses that had reputation archived
	LastArchivedAddresses []string
}

// newMockRepKeeper creates a new mock rep keeper with empty state
func newMockRepKeeper() *mockRepKeeper {
	return &mockRepKeeper{
		Balances:              make(map[string]math.Int),
		ReputationScores:      make(map[string]map[string]string),
		LifetimeReputation:    make(map[string]map[string]string),
		CompletedInitiatives:  make(map[string]uint64),
		Members:               make(map[string]bool),
		LastArchivedAddresses: []string{},
	}
}

// SetMember sets up a member with the given data
func (m *mockRepKeeper) SetMember(addr string, balance int64, reputation map[string]string, initiatives uint64) {
	m.Members[addr] = true
	m.Balances[addr] = math.NewInt(balance)
	if reputation != nil {
		m.ReputationScores[addr] = reputation
	} else {
		m.ReputationScores[addr] = make(map[string]string)
	}
	m.CompletedInitiatives[addr] = initiatives
	// Initialize lifetime reputation as empty
	if _, exists := m.LifetimeReputation[addr]; !exists {
		m.LifetimeReputation[addr] = make(map[string]string)
	}
}

// GetBalance returns the DREAM balance for an address
func (m *mockRepKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress) (math.Int, error) {
	// Try bech32 string first
	if balance, ok := m.Balances[addr.String()]; ok {
		return balance, nil
	}
	// Also try raw string (for tests that use simple string addresses)
	rawStr := string(addr)
	if balance, ok := m.Balances[rawStr]; ok {
		return balance, nil
	}
	return math.ZeroInt(), nil
}

// BurnDREAM burns DREAM tokens from an address
func (m *mockRepKeeper) BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if balance, ok := m.Balances[addr.String()]; ok {
		m.Balances[addr.String()] = balance.Sub(amount)
	}
	return nil
}

// LockDREAM locks DREAM tokens for an address
func (m *mockRepKeeper) LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	return nil
}

// UnlockDREAM unlocks DREAM tokens for an address
func (m *mockRepKeeper) UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	return nil
}

// IsMember returns whether an address is a member
func (m *mockRepKeeper) IsMember(ctx context.Context, addr string) bool {
	return m.Members[addr]
}

// GetMember returns member data (as interface{} to match the interface)
func (m *mockRepKeeper) GetMember(ctx context.Context, addr string) (interface{}, error) {
	if !m.Members[addr] {
		return nil, nil
	}
	return struct{ Address string }{Address: addr}, nil
}

// GetReputationScores returns all reputation scores for a member
func (m *mockRepKeeper) GetReputationScores(ctx context.Context, addr string) (map[string]string, error) {
	if scores, ok := m.ReputationScores[addr]; ok {
		// Return a copy to prevent modification
		result := make(map[string]string)
		for k, v := range scores {
			result[k] = v
		}
		return result, nil
	}
	return make(map[string]string), nil
}

// ArchiveSeasonalReputation archives seasonal reputation to lifetime and resets
func (m *mockRepKeeper) ArchiveSeasonalReputation(ctx context.Context, addr string) (map[string]string, error) {
	m.ArchiveCallCount++
	m.LastArchivedAddresses = append(m.LastArchivedAddresses, addr)

	scores, ok := m.ReputationScores[addr]
	if !ok {
		return make(map[string]string), nil
	}

	// Copy to result before clearing
	result := make(map[string]string)
	for k, v := range scores {
		result[k] = v
	}

	// Archive to lifetime
	if m.LifetimeReputation[addr] == nil {
		m.LifetimeReputation[addr] = make(map[string]string)
	}
	for tag, score := range scores {
		// For simplicity, just overwrite (real impl would add)
		m.LifetimeReputation[addr][tag] = score
	}

	// Clear seasonal scores
	m.ReputationScores[addr] = make(map[string]string)

	return result, nil
}

// GetCompletedInitiativesCount returns the count of completed initiatives
func (m *mockRepKeeper) GetCompletedInitiativesCount(ctx context.Context, addr string) (uint64, error) {
	if count, ok := m.CompletedInitiatives[addr]; ok {
		return count, nil
	}
	return 0, nil
}
