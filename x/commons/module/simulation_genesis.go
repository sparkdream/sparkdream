package commons

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"

	"sparkdream/x/commons/types"
)

// simSeatNames are the display names of the simulated founding members.
var simSeatNames = []string{"phoenix", "aurora", "zenith", "nebula", "cascade"}

// SimCommitteeNames are the committees the simulated genesis staffs with
// several sim accounts. BootstrapGovernance seats only the founder on a
// committee; these get one to four more of the founding members, so the
// committee-gated operations (IsCouncilAuthorized for "commons"/"operations"
// and "commons"/"hr", project budgets on the other pillars) have more than
// one sim account that can sign them.
var SimCommitteeNames = []string{
	"Commons Operations Committee",
	"Commons Governance Committee",
	"Technical Operations Committee",
	"Ecosystem Operations Committee",
}

// simGovernanceSeats returns the simulated founding members and the extra
// committee seats.
//
// The founding members replace the build's compiled-in founders (which are
// not sim accounts), so InitGenesis bootstraps the real "Three Pillars"
// structure around sim accounts: every founding member sits on the Commons
// Council and the founder on every council and committee. Group policy
// addresses stay module-derived (DeriveCouncilAddress); no sim account is
// ever a policy. The extra seats are council_members rows for groups the
// bootstrap creates: with an empty group_map InitGenesis bootstraps first
// and then imports council_members, and Validate only cross-checks member
// rows against an imported group_map.
//
// Up to five accounts get a seat, always leaving at least two sim accounts
// without one (fewer than three accounts seat only the founder).
func simGovernanceSeats(simState *module.SimulationState) ([]types.FoundingMember, []types.CouncilMembers) {
	n := len(simState.Accounts)
	if n == 0 {
		return nil, nil
	}
	seats := min(len(simSeatNames), max(1, n-2))
	r := simGenesisRand(simState, types.ModuleName)
	perm := r.Perm(n)

	founding := make([]types.FoundingMember, seats)
	for i := range seats {
		founding[i] = types.FoundingMember{
			Address:     simState.Accounts[perm[i]].Address.String(),
			DisplayName: simSeatNames[i],
			Founder:     i == 0,
		}
	}
	if seats < 2 {
		return founding, nil
	}

	addedAt := simState.GenTimestamp.Unix()
	council := make([]types.CouncilMembers, 0, len(SimCommitteeNames))
	for _, name := range SimCommitteeNames {
		// The founder already sits on the committee; add one to four more
		// (committees hold at most five members).
		extra := 1 + r.Intn(min(4, seats-1))
		others := r.Perm(seats - 1)[:extra]
		members := make([]types.Member, 0, extra)
		for _, o := range others {
			members = append(members, types.Member{
				Address:  founding[o+1].Address,
				Weight:   "1",
				Metadata: "Simulation seat",
				AddedAt:  addedAt,
			})
		}
		council = append(council, types.CouncilMembers{CouncilName: name, Members: members})
	}
	return founding, council
}

// simGenesisRand returns a source for the simulated genesis seeded from the
// sim accounts (themselves drawn from the simulation seed) and tag. It never
// draws from simState.Rand, so the genesis of the modules generated after
// this one, and the whole run that follows, is not shifted by it.
func simGenesisRand(simState *module.SimulationState, tag string) *rand.Rand {
	h := sha256.New()
	h.Write([]byte(tag))
	for _, acc := range simState.Accounts {
		h.Write(acc.Address)
	}
	return rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(h.Sum(nil)[:8]) >> 1))) //nolint:gosec // simulation only
}
