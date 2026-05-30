package keeper

import (
	"context"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// IssueWarning records a MemberWarning against `member` with the given reason
// and evidence. It is intended for keeper-internal callers (other modules'
// EndBlocker / abci paths via expected_keepers). The caller supplies its own
// `issuedBy` identifier — typically a module account address so the warning's
// origin is auditable.
//
// warning_number is per-member: it counts existing warnings against `member`
// and assigns N+1. This mirrors the pattern in MsgResolveMemberReport so
// queries (ListMemberWarning, GetMemberStanding) read a consistent sequence
// regardless of which path produced the warning.
//
// Reason should be a stable short identifier (e.g. "promoted_hidden_content")
// rather than free-form prose, so downstream consumers can filter on it.
func (k Keeper) IssueWarning(ctx context.Context, member string, issuedBy string, reason string, evidencePostIDs []uint64) error {
	if _, err := k.addressCodec.StringToBytes(member); err != nil {
		return errorsmod.Wrap(err, "invalid member address")
	}
	if reason == "" {
		return types.ErrEmptyWarningReason
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	warningID, err := k.MemberWarningSeq.Next(ctx)
	if err != nil {
		return errorsmod.Wrap(err, "failed to allocate warning id")
	}

	// Count existing warnings for this member to derive warning_number.
	// O(N) over all warnings; matches the existing msg_server_resolve_member_report
	// pattern. Acceptable while volume is low; revisit with a per-member
	// counter index if it becomes a hotspot.
	var warningCount uint64
	iter, err := k.MemberWarning.Iterate(ctx, nil)
	if err == nil {
		defer iter.Close()
		for ; iter.Valid(); iter.Next() {
			w, valErr := iter.Value()
			if valErr != nil {
				continue
			}
			if w.Member == member {
				warningCount++
			}
		}
	}

	warning := types.MemberWarning{
		Id:              warningID,
		Member:          member,
		Reason:          reason,
		IssuedAt:        sdkCtx.BlockTime().Unix(),
		IssuedBy:        issuedBy,
		WarningNumber:   warningCount + 1,
		EvidencePostIds: evidencePostIDs,
	}

	if err := k.MemberWarning.Set(ctx, warningID, warning); err != nil {
		return errorsmod.Wrap(err, "failed to store member warning")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_warning_issued",
		sdk.NewAttribute("warning_id", strconv.FormatUint(warningID, 10)),
		sdk.NewAttribute("member", member),
		sdk.NewAttribute("issued_by", issuedBy),
		sdk.NewAttribute("reason", reason),
		sdk.NewAttribute("warning_number", strconv.FormatUint(warningCount+1, 10)),
	))

	return nil
}
