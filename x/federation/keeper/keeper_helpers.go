package keeper

import (
	"bytes"
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/federation/types"
	servicetypes "sparkdream/x/service/types"
)

// isGovernance checks if the given address bytes match the module authority (governance).
func (k Keeper) isGovernance(authorityBytes []byte) bool {
	return bytes.Equal(k.authority, authorityBytes)
}

// isCouncilAuthorized checks if the address is authorized via governance or Commons Council.
func (k Keeper) IsCouncilAuthorized(ctx context.Context, addr string, council, committee string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	if k.isGovernance(addrBytes) {
		return true
	}
	if k.late.commonsKeeper == nil {
		return false
	}
	return k.late.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
}

// IsGovAuthority reports whether the bech32 address string matches the
// configured x/gov authority bytes. Used by Phase 1 federation→service
// migration messages (UpdatePeerController, ResyncBridgeCount,
// PruneOrphanBindings) that accept gov authority.
func (k Keeper) IsGovAuthority(addr string) bool {
	authorityBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return bytes.Equal(k.authority, authorityBytes)
}

// countBridgesForPeer counts the number of bindings registered for a
// peer. Status filtering is no longer needed: bindings only exist for
// non-terminal operators (the AfterOperatorDissolved/Retired hooks
// prune them on terminal transitions).
func (k Keeper) countBridgesForPeer(ctx context.Context, peerID string) (uint64, error) {
	var count uint64
	rng := collections.NewPrefixedPairRange[string, string](peerID)
	err := k.BridgesByPeer.Walk(ctx, rng, func(_ collections.Pair[string, string]) (bool, error) {
		count++
		return false, nil
	})
	return count, err
}

// getPeerRequireActive gets a peer and verifies it is ACTIVE.
func (k Keeper) GetPeerRequireActive(ctx context.Context, peerID string) (types.Peer, error) {
	peer, err := k.Peers.Get(ctx, peerID)
	if err != nil {
		return types.Peer{}, types.ErrPeerNotFound
	}
	if peer.Status != types.PeerStatus_PEER_STATUS_ACTIVE {
		return types.Peer{}, types.ErrPeerNotActive
	}
	return peer, nil
}

// repMembers returns the wired rep keeper's membership/alias surface, or
// false when rep is unwired or a test double does not implement it.
func (k Keeper) repMembers() (types.RepMemberKeeper, bool) {
	if k.late == nil || k.late.repKeeper == nil {
		return nil, false
	}
	rm, ok := k.late.repKeeper.(types.RepMemberKeeper)
	return rm, ok
}

// canActAs reports whether signer may exercise rights recorded against
// stored, through x/rep's alias contract (a member who migrated keys acts
// for records written under the old address; a tombstoned key acts for
// nobody). Plain equality when rep is unwired.
func (k Keeper) canActAs(ctx context.Context, signer, stored string) bool {
	if rm, ok := k.repMembers(); ok {
		return rm.CanActAs(ctx, signer, stored)
	}
	return signer != "" && signer == stored
}

// sameMember reports whether two stored addresses belong to the same member
// (self-dealing bans). Plain equality when rep is unwired.
func (k Keeper) sameMember(ctx context.Context, a, b string) bool {
	if rm, ok := k.repMembers(); ok {
		return rm.SameMember(ctx, a, b)
	}
	return a != "" && a == b
}

// isCouncilPolicy reports whether addr is the x/gov authority or the policy
// address of the Commons Council or its Operations Committee — an executed
// proposal, never a single member's signature. Value-moving and
// verdict-setting federation messages use this rather than
// IsCouncilAuthorized, which also accepts any individual committee member.
func (k Keeper) isCouncilPolicy(ctx context.Context, addr string) bool {
	if k.IsGovAuthority(addr) {
		return true
	}
	if k.late == nil || k.late.commonsKeeper == nil {
		return false
	}
	return k.late.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, addr, "commons", "operations")
}

// operatorServiceActive reports whether operator's x/service record for the
// peer type's service_type is ACTIVE with a bond that meets the type's
// current min_bond. (After gov raises min_bond the stored status changes
// only on the operator's next x/service message; until then the bond check
// keeps an under-collateralized bridge from submitting or earning.) True
// when x/service is unwired (standalone tests) or the peer type has no
// service-backed bridges.
func (k Keeper) operatorServiceActive(ctx context.Context, operator string, peerType types.PeerType) bool {
	sk := k.serviceKeeper()
	if sk == nil {
		return true
	}
	serviceType, err := serviceTypeForPeer(peerType)
	if err != nil {
		return true
	}
	opBytes, err := k.addressCodec.StringToBytes(operator)
	if err != nil {
		return false
	}
	op, ok := sk.GetOperator(ctx, opBytes, serviceType)
	if !ok || op.Status != servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE {
		return false
	}
	if cfg, found := sk.GetServiceTypeConfig(ctx, serviceType); found && op.BondAmount.LT(cfg.MinBondAmount) {
		return false
	}
	return true
}

// operatorIsMember reports whether operator is an active x/rep member.
// Bridge operators must be members so the alias-aware self-dealing checks
// (verification, arbitration, attestation) see the person behind a bridge:
// otherwise one person runs a bridge from a fresh address and verifies its
// submissions from their member address, collecting verified-volume operator
// pay for self-confirmed work. A member who is later zeroed (or whose key is
// tombstoned) stops passing, which suspends the bridge in practice. False
// when x/rep is unwired.
func (k Keeper) operatorIsMember(ctx context.Context, operator string) bool {
	rm, ok := k.repMembers()
	if !ok {
		return false
	}
	addr, err := k.addressCodec.StringToBytes(operator)
	if err != nil {
		return false
	}
	return rm.IsActiveMember(ctx, addr)
}

// requireMemberOperator is operatorIsMember as an error.
func (k Keeper) requireMemberOperator(ctx context.Context, operator string) error {
	if !k.operatorIsMember(ctx, operator) {
		return errorsmod.Wrapf(types.ErrOperatorNotMember, "operator %s", operator)
	}
	return nil
}

// requireActiveOperator checks that operator is an active member and that
// operatorServiceActive holds.
func (k Keeper) requireActiveOperator(ctx context.Context, operator string, peerType types.PeerType) error {
	if err := k.requireMemberOperator(ctx, operator); err != nil {
		return err
	}
	if !k.operatorServiceActive(ctx, operator, peerType) {
		return errorsmod.Wrapf(types.ErrBindingNotActive, "operator %s", operator)
	}
	return nil
}
