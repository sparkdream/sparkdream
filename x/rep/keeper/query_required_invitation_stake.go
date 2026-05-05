package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RequiredInvitationStake reports the minimum DREAM stake an inviter must
// lock for their next invitation. The required stake escalates with each
// invitation already made this season per InvitationCostMultiplier, so
// frontends should call this immediately before constructing
// MsgInviteMember and use the returned `required_stake` as the floor.
//
// The query lazily applies the seasonal credit reset (without persisting
// state) so the answer reflects what the chain will actually enforce on
// the next MsgInviteMember.
func (q queryServer) RequiredInvitationStake(ctx context.Context, req *types.QueryRequiredInvitationStakeRequest) (*types.QueryRequiredInvitationStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if _, err := q.k.addressCodec.StringToBytes(req.Inviter); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid inviter address")
	}

	member, err := q.k.Member.Get(ctx, req.Inviter)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrMemberNotFound.Error())
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Mirror the lazy seasonal reset performed by EnsureInvitationCreditsReset
	// without mutating state — queries must be read-only.
	currentSeason, err := q.k.GetCurrentSeason(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	maxCredits := GetMaxInvitationCredits(params.TrustLevelConfig, member.TrustLevel)
	if member.LastCreditResetSeason < currentSeason {
		member.InvitationCredits = maxCredits
	}

	required := computeRequiredInvitationStake(params, member)

	var used uint32
	if maxCredits > member.InvitationCredits {
		used = maxCredits - member.InvitationCredits
	}

	multiplier := math.LegacyOneDec()
	if used > 0 && params.InvitationCostMultiplier.GT(math.LegacyOneDec()) {
		multiplier = params.InvitationCostMultiplier.Power(uint64(used))
	}

	return &types.QueryRequiredInvitationStakeResponse{
		RequiredStake:    required,
		BaseStake:        params.MinInvitationStake,
		CostMultiplier:   multiplier,
		CreditsUsed:      used,
		CreditsRemaining: member.InvitationCredits,
		TrustLevel:       member.TrustLevel,
	}, nil
}
