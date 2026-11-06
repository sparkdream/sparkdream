package keeper

import (
	"fmt"
	"sort"
	"time"
)

// NetworkBounds are the guardian bounds that differ per network build. Every
// other floor and ceiling in msg_server.go is shared by the three networks;
// these follow each network's own gov cadence, because gov.MsgUpdateParams
// replaces the whole Params and a floor above a network's shipped value would
// force every later gov retune (of min_deposit, say) to lengthen the voting
// periods as a side effect (APP-201).
//
// The active table entry is picked by the build tag (network_bounds_*.go),
// like params_vals_*; the whole table stays compiled into every binary so a
// genesis audit can check a network's genesis against that network's bounds
// without rebuilding (NetworkBoundsFor, UseNetworkBounds).
type NetworkBounds struct {
	// GovVotingPeriodFloor is the minimum gov.voting_period.
	GovVotingPeriodFloor time.Duration
	// GovExpeditedVotingPeriodFloor is the minimum gov.expedited_voting_period.
	GovExpeditedVotingPeriodFloor time.Duration
}

// productionNetworkBounds are the testnet and mainnet floors: 6 h is short
// enough for emergencies, long enough to prevent same-day surprise proposals.
var productionNetworkBounds = NetworkBounds{
	GovVotingPeriodFloor:          6 * time.Hour,
	GovExpeditedVotingPeriodFloor: 1 * time.Hour,
}

// networkBoundsTable holds the bounds of every canonical network. The
// untagged test build uses the production entry (see
// network_bounds_testparams.go), so unit tests exercise the strict floors.
var networkBoundsTable = map[string]NetworkBounds{
	"mainnet": productionNetworkBounds,
	"testnet": productionNetworkBounds,
	// devnet ships voting_period 1 h / expedited 30 min so a proposal
	// completes inside a working session; the floors leave it room to
	// shorten further.
	"devnet": {
		GovVotingPeriodFloor:          30 * time.Minute,
		GovExpeditedVotingPeriodFloor: 10 * time.Minute,
	},
}

// NetworkBoundsFor returns the bounds compiled for the named network
// (mainnet, testnet, devnet).
func NetworkBoundsFor(network string) (NetworkBounds, error) {
	b, ok := networkBoundsTable[network]
	if !ok {
		return NetworkBounds{}, fmt.Errorf("guardian: no bounds table for network %q", network)
	}
	return b, nil
}

// NetworkNames lists the networks NetworkBoundsFor knows, sorted.
func NetworkNames() []string {
	names := make([]string, 0, len(networkBoundsTable))
	for n := range networkBoundsTable {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ActiveNetwork is the network whose bounds this build enforces: the build
// tag's network, or "mainnet" for the untagged test build.
func ActiveNetwork() string { return activeBoundsNetwork }

// activeNetworkBounds returns the bounds the build tag selected.
func activeNetworkBounds() NetworkBounds {
	return networkBoundsTable[activeBoundsNetwork]
}

// NetworkBounds returns the bounds this keeper enforces.
func (k Keeper) NetworkBounds() NetworkBounds { return k.bounds }

// UseNetworkBounds switches the keeper to the named network's bounds. It
// exists for genesis audits that check a network's shipped params against
// that network's floors from an untagged test binary; app.go never calls it
// and the build tag decides what a node enforces.
func (k *Keeper) UseNetworkBounds(network string) error {
	b, err := NetworkBoundsFor(network)
	if err != nil {
		return err
	}
	k.bounds = b
	return nil
}
