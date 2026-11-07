package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"
)

// Member key migration lifecycle: request / reveal / force open a
// MemberMigration, the new address accepts, the EndBlocker finalizes after
// the path's delay (migration_finalize.go). docs/x-rep-spec.md, "Member key
// migration".

// SetMigrationHooks registers the cross-module hooks (commons, name, season,
// session, federation, service, artifact). Called from app.go after
// depinject.
func (k Keeper) SetMigrationHooks(h types.MemberMigrationHooks) {
	k.late.migrationHooks = h
}

// AddReservedAddress marks an address that can never be a migration target:
// module accounts and module-derived escrows, the x/shield public submitter
// whose private key is public, and so on. Wired from app.go.
func (k Keeper) AddReservedAddress(addr string) {
	if k.late.reservedAddresses == nil {
		k.late.reservedAddresses = map[string]bool{}
	}
	k.late.reservedAddresses[addr] = true
}

// SetVaultChecker late-wires the x/vault "is this a shared vault address"
// check (multisig-accounts plan). Until x/vault exists it stays nil and
// reports false.
func (k Keeper) SetVaultChecker(fn func(ctx context.Context, addr string) bool) {
	k.late.isVault = fn
}

// MigrationHooks returns the wired cross-module migration hooks, for the
// app-level guard that walks them (app/migration_hooks_guard_test.go).
func (k Keeper) MigrationHooks() types.MemberMigrationHooks {
	return k.migrationHooks()
}

func (k Keeper) migrationHooks() types.MemberMigrationHooks {
	if k.late.migrationHooks == nil {
		return types.MultiMemberMigrationHooks{}
	}
	return k.late.migrationHooks
}

// HasOpenMemberReport reports whether a report against member is PENDING,
// ESCALATED or META_APPEALED. The key path is refused while one is open, so
// a member about to be zeroed cannot start a migration first.
func (k Keeper) HasOpenMemberReport(ctx context.Context, member string) bool {
	report, err := k.MemberReport.Get(ctx, member)
	if err != nil {
		return false
	}
	switch report.Status {
	case types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED,
		types.MemberReportStatus_MEMBER_REPORT_STATUS_META_APPEALED:
		return true
	}
	return false
}

// IsHRCommitteePolicy reports whether addr is the Commons HR (governance)
// committee policy address or the gov authority: the only authorities that
// may force or cancel a council migration.
func (k Keeper) IsHRCommitteePolicy(ctx context.Context, addr string) bool {
	if k.IsGovAuthority(addr) {
		return true
	}
	if k.commonsKeeper == nil {
		return false
	}
	return k.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, addr, "commons", "hr")
}

// checkMigrationSource verifies the member a migration moves away from:
// an existing, non-tombstoned, non-zeroed member.
func (k Keeper) checkMigrationSource(ctx context.Context, from string) (types.Member, error) {
	if k.IsTombstoned(ctx, from) {
		return types.Member{}, types.ErrMemberTombstoned
	}
	m, err := k.Member.Get(ctx, from)
	if err != nil {
		return types.Member{}, types.ErrMemberNotFound
	}
	if m.Status == types.MemberStatus_MEMBER_STATUS_ZEROED {
		return types.Member{}, types.ErrMigrationZeroedMember
	}
	return m, nil
}

// checkTargetFresh enforces the freshness rules of plan §7.1 on the new
// address: no identity of its own anywhere (member record, pending
// invitation, tombstone, pending migration target), and a personal address
// (no module or module-derived account, no council or committee policy, no
// reserved address, no vault). Module-held identity (names, season profile,
// session grants as granter, operator records, federation links) is checked
// by the BeforeMemberMigration hooks the caller runs next.
func (k Keeper) checkTargetFresh(ctx context.Context, from, to string) error {
	return k.checkTargetFreshOpts(ctx, from, to, false)
}

// checkTargetFreshOpts is checkTargetFresh with the pending-invitation rule
// optional. RECOVERY and COUNCIL opens pass allowPendingInvitation: their
// target is public before the open (a recovery reveal in the mempool, a force
// in a commons proposal), so an invitation filed in that window must not be
// able to fail the open; openMigration voids it instead (REP-R-02).
func (k Keeper) checkTargetFreshOpts(ctx context.Context, from, to string, allowPendingInvitation bool) error {
	if to == from {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address equals the old one")
	}
	if _, err := k.addressCodec.StringToBytes(to); err != nil {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "invalid new address")
	}
	if has, _ := k.Member.Has(ctx, to); has {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address is already a member")
	}
	if !allowPendingInvitation {
		if _, pending := k.pendingInvitationTo(ctx, to); pending {
			return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address has a pending invitation")
		}
	}
	if k.IsTombstoned(ctx, to) {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address is a tombstone")
	}
	if has, _ := k.PendingMigrationByTarget.Has(ctx, to); has {
		return errorsmod.Wrap(types.ErrMigrationTargetNotFresh, "new address is already the target of a pending migration")
	}
	if err := k.IsPersonalAddress(ctx, to); err != nil {
		return errorsmod.Wrapf(types.ErrMigrationTargetNotFresh, "new address %v", err)
	}
	return nil
}

// pendingInvitationTo returns the PENDING invitation addressed to addr, if any.
func (k Keeper) pendingInvitationTo(ctx context.Context, addr string) (types.Invitation, bool) {
	invID, err := k.InvitationsByInvitee.Get(ctx, addr)
	if err != nil {
		return types.Invitation{}, false
	}
	inv, err := k.Invitation.Get(ctx, invID)
	if err != nil || inv.Status != types.InvitationStatus_INVITATION_STATUS_PENDING {
		return types.Invitation{}, false
	}
	return inv, true
}

// voidPendingInvitationTo closes a PENDING invitation to a RECOVERY or COUNCIL
// migration target as REVOKED (REP-R-02): the inviter gets the stake and the
// invitation credit back and the invitee slot is freed, exactly as if the
// inviter had revoked it, and an invitation_voided_by_migration event names
// the migration. The invitee never accepted, so nothing is lost but the
// inviter's intent, which a migration of a proven member outranks.
func (k Keeper) voidPendingInvitationTo(ctx context.Context, to string, migrationID uint64) error {
	inv, pending := k.pendingInvitationTo(ctx, to)
	if !pending {
		return nil
	}
	return k.closePendingInvitation(ctx, inv, types.InvitationStatus_INVITATION_STATUS_REVOKED, "invitation_voided_by_migration",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", migrationID)))
}

// IsPersonalAddress fails when addr cannot belong to a person: a reserved
// module or system address, a module account, an interchain account, a
// council or committee policy or an x/vault shared account. Membership is a person, so every entry point
// that creates or moves an identity (invitation, acceptance, migration target)
// runs this gate; x/vault runs it on signer sets.
func (k Keeper) IsPersonalAddress(ctx context.Context, addr string) error {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return fmt.Errorf("is not a valid address: %w", err)
	}
	if k.late.reservedAddresses[addr] {
		return fmt.Errorf("is a reserved module or system address")
	}
	if k.authKeeper != nil {
		if acc := k.authKeeper.GetAccount(ctx, addrBytes); acc != nil {
			if _, isModule := acc.(sdk.ModuleAccountI); isModule {
				return fmt.Errorf("is a module account")
			}
			// An interchain account is driven by a controller chain's
			// module, not by a person holding a key here.
			if _, isICA := acc.(*icatypes.InterchainAccount); isICA {
				return fmt.Errorf("is an interchain account")
			}
		}
	}
	if k.commonsKeeper != nil && k.commonsKeeper.IsGroupPolicyAddress(ctx, addr) {
		return fmt.Errorf("is a group policy")
	}
	if k.late.isVault != nil && k.late.isVault(ctx, addr) {
		return fmt.Errorf("is a vault")
	}
	return nil
}

// checkAliasCaps refuses a path once the member's alias counts hit the
// compiled ceilings. The KEY path keeps AliasRecoveryReserve recent slots for
// recovery and council migrations.
func (k Keeper) checkAliasCaps(ctx context.Context, from string, path types.MigrationPath) error {
	total, recent := k.aliasCounts(ctx, from)
	if total >= types.MaxLifetimeAliases {
		return errorsmod.Wrapf(types.ErrMigrationAliasCap, "%d lifetime aliases", total)
	}
	limit := types.MaxRecentAliases
	if path == types.MigrationPath_MIGRATION_PATH_KEY {
		limit -= types.AliasRecoveryReserve
	}
	if recent >= limit {
		return errorsmod.Wrapf(types.ErrMigrationAliasCap, "%d recent aliases (limit %d for this path)", recent, limit)
	}
	return nil
}

// RequestKeyMigration opens a KEY migration: the member signs the request,
// the new address accepts, finalization follows key_migration_delay later.
func (k Keeper) RequestKeyMigration(ctx context.Context, from, to string) (uint64, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if !params.MigrationEnabled {
		return 0, types.ErrMigrationDisabled
	}
	if !params.KeyPathEnabled {
		return 0, types.ErrKeyPathDisabled
	}
	m, err := k.checkMigrationSource(ctx, from)
	if err != nil {
		return 0, err
	}
	if k.HasOpenMemberReport(ctx, from) {
		return 0, types.ErrMigrationOpenReport
	}
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if m.LastMigratedAt > 0 && now < m.LastMigratedAt+params.MigrationCooldown {
		return 0, errorsmod.Wrapf(types.ErrMigrationCooldown, "until %d", m.LastMigratedAt+params.MigrationCooldown)
	}
	inWindow := 0
	for _, t := range m.KeyMigrationTimes {
		if t > now-params.KeyMigrationWindow {
			inWindow++
		}
	}
	if commontypes.Saturate[uint32](inWindow) >= params.MaxKeyMigrationsPerWindow {
		return 0, errorsmod.Wrapf(types.ErrMigrationWindowCap, "%d within key_migration_window", inWindow)
	}
	return k.openMigration(ctx, from, to, types.MigrationPath_MIGRATION_PATH_KEY, "")
}

// ForceMigration opens a COUNCIL migration on behalf of the HR committee or
// governance. An open member report does not block it: a theft victim should
// not wait on a report. The new address must still accept, and the old
// address may object to a jury during the delay. Signed by x/gov for the
// member and new address of a migration whose objection is escalated to
// governance, it rejects that objection instead of opening a new migration.
func (k Keeper) ForceMigration(ctx context.Context, authority, from, to, evidenceURI string) (uint64, error) {
	if !k.IsHRCommitteePolicy(ctx, authority) {
		return 0, errorsmod.Wrap(types.ErrMigrationNotAuthorized, "only the Commons HR committee policy or governance may force a migration")
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if !params.MigrationEnabled {
		return 0, types.ErrMigrationDisabled
	}
	// Governance re-forcing the migration an inconclusive jury escalated to
	// it rejects the objection: the bond goes to the new address and the
	// migration finalizes. Only x/gov decides an escalation; the HR committee
	// filed the migration and cannot overrule the objection to it.
	if pendingID, err := k.PendingMigrationByMember.Get(ctx, from); err == nil {
		if pending, err := k.MemberMigrations.Get(ctx, pendingID); err == nil && isEscalatedObjection(pending) {
			if !k.IsGovAuthority(authority) {
				return 0, errorsmod.Wrap(types.ErrMigrationNotAuthorized, "the objection to this migration is escalated to governance")
			}
			if pending.ToAddress != to {
				return 0, errorsmod.Wrapf(types.ErrMigrationPending,
					"migration %d to %s awaits a governance decision; cancel it first to force another target", pending.Id, pending.ToAddress)
			}
			if err := k.rejectMigrationObjection(ctx, &pending, "gov"); err != nil {
				return 0, err
			}
			return pending.Id, nil
		}
	}
	if _, err := k.checkMigrationSource(ctx, from); err != nil {
		return 0, err
	}
	return k.openMigration(ctx, from, to, types.MigrationPath_MIGRATION_PATH_COUNCIL, evidenceURI)
}

// openMigration is the shared open path. It runs the freshness and cap
// checks, applies precedence (RECOVERY > COUNCIL > KEY) against any pending
// migration of the member, asks every module hook for a veto, and writes a
// REQUESTED migration with its accept-window expiry.
func (k Keeper) openMigration(ctx context.Context, from, to string, path types.MigrationPath, evidenceURI string) (uint64, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if _, err := k.checkMigrationSource(ctx, from); err != nil {
		return 0, err
	}
	// RECOVERY and COUNCIL targets are public before the open, so a pending
	// invitation to the target does not fail them: it is voided below once
	// every other check has passed. The KEY path keeps the refusal.
	nonKey := path != types.MigrationPath_MIGRATION_PATH_KEY
	if err := k.checkTargetFreshOpts(ctx, from, to, nonKey); err != nil {
		return 0, err
	}
	if err := k.checkAliasCaps(ctx, from, path); err != nil {
		return 0, err
	}

	// Precedence against the member's pending migration, if any.
	if pendingID, err := k.PendingMigrationByMember.Get(ctx, from); err == nil {
		pending, err := k.MemberMigrations.Get(ctx, pendingID)
		if err == nil && types.IsPendingMigrationStatus(pending.Status) {
			if types.MigrationPathRank(pending.Path) >= types.MigrationPathRank(path) {
				return 0, errorsmod.Wrapf(types.ErrMigrationPending, "migration %d (%s) is pending", pending.Id, pending.Path)
			}
			if err := k.cancelMigration(ctx, &pending, types.MigrationStatus_MIGRATION_STATUS_CANCELLED,
				fmt.Sprintf("superseded by a %s migration", path)); err != nil {
				return 0, err
			}
		}
	}

	fromAddr, err := k.addressCodec.StringToBytes(from)
	if err != nil {
		return 0, err
	}
	toAddr, err := k.addressCodec.StringToBytes(to)
	if err != nil {
		return 0, err
	}
	if err := k.migrationHooks().BeforeMemberMigration(ctx, fromAddr, toAddr, path); err != nil {
		return 0, errorsmod.Wrap(types.ErrMigrationVetoed, err.Error())
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	id, err := k.MemberMigrationSeq.Next(ctx)
	if err != nil {
		return 0, err
	}
	id++ // ids start at 1; 0 is "none" in queries and events
	if nonKey {
		if err := k.voidPendingInvitationTo(ctx, to, id); err != nil {
			return 0, err
		}
	}
	m := types.MemberMigration{
		Id:            id,
		FromAddress:   from,
		ToAddress:     to,
		Path:          path,
		Status:        types.MigrationStatus_MIGRATION_STATUS_REQUESTED,
		RequestedAt:   now,
		EvidenceUri:   evidenceURI,
		ObjectionBond: math.ZeroInt(),
		AcceptBy:      now + params.MigrationAcceptWindow,
	}
	if err := k.MemberMigrations.Set(ctx, id, m); err != nil {
		return 0, err
	}
	if err := k.PendingMigrationByMember.Set(ctx, from, id); err != nil {
		return 0, err
	}
	// The request does not reserve the target: nobody has proven control of
	// it yet, and a reservation here would let any member (or a thief holding
	// the old key) squat a victim's recovery target with a request of their
	// own. The target is reserved when it accepts.
	if err := k.MigrationRequestsByTarget.Set(ctx, collections.Join(to, id)); err != nil {
		return 0, err
	}
	if err := k.MigrationAcceptExpiry.Set(ctx, collections.Join(m.AcceptBy, id)); err != nil {
		return 0, err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migration_requested",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("from", from),
		sdk.NewAttribute("to", to),
		sdk.NewAttribute("path", path.String()),
		sdk.NewAttribute("accept_by", fmt.Sprintf("%d", m.AcceptBy)),
	))
	return id, nil
}

// AcceptMigration is signed by the new address and completes every path: it
// proves control of the target, sets executes_at and enqueues finalization.
func (k Keeper) AcceptMigration(ctx context.Context, signer string, id uint64) error {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return types.ErrMigrationNotFound
	}
	if m.ToAddress != signer {
		return errorsmod.Wrap(types.ErrMigrationNotAuthorized, "only the new address may accept")
	}
	if m.Status != types.MigrationStatus_MIGRATION_STATUS_REQUESTED {
		return errorsmod.Wrapf(types.ErrMigrationWrongStatus, "status %s", m.Status)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	// Re-verify what may have changed since the request.
	if _, err := k.checkMigrationSource(ctx, m.FromAddress); err != nil {
		return err
	}
	if err := k.checkTargetFreshAtAccept(ctx, m); err != nil {
		return err
	}
	fromAddr, _ := k.addressCodec.StringToBytes(m.FromAddress)
	toAddr, _ := k.addressCodec.StringToBytes(m.ToAddress)
	if err := k.migrationHooks().BeforeMemberMigration(ctx, fromAddr, toAddr, m.Path); err != nil {
		return errorsmod.Wrap(types.ErrMigrationVetoed, err.Error())
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	delay := params.KeyMigrationDelay
	switch m.Path {
	case types.MigrationPath_MIGRATION_PATH_RECOVERY:
		delay = params.RecoveryMigrationDelay
	case types.MigrationPath_MIGRATION_PATH_COUNCIL:
		delay = params.CouncilMigrationDelay
	}
	if err := k.MigrationAcceptExpiry.Remove(ctx, collections.Join(acceptBy(m, params), id)); err != nil {
		return err
	}
	if err := k.MigrationRequestsByTarget.Remove(ctx, collections.Join(m.ToAddress, id)); err != nil {
		return err
	}
	// The new address proved control: reserve it.
	if err := k.PendingMigrationByTarget.Set(ctx, m.ToAddress, id); err != nil {
		return err
	}
	m.Status = types.MigrationStatus_MIGRATION_STATUS_ACCEPTED
	m.AcceptedAt = now
	m.ExecutesAt = now + delay
	if err := k.MemberMigrations.Set(ctx, id, m); err != nil {
		return err
	}
	if err := k.MigrationQueue.Set(ctx, collections.Join(m.ExecutesAt, id)); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migration_accepted",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("from", m.FromAddress),
		sdk.NewAttribute("to", m.ToAddress),
		sdk.NewAttribute("path", m.Path.String()),
		sdk.NewAttribute("executes_at", fmt.Sprintf("%d", m.ExecutesAt)),
	))
	return nil
}

// checkTargetFreshAtAccept re-runs the freshness check for an existing
// migration, ignoring the migration's own pending-target entry.
func (k Keeper) checkTargetFreshAtAccept(ctx context.Context, m types.MemberMigration) error {
	owned := false
	if cur, err := k.PendingMigrationByTarget.Get(ctx, m.ToAddress); err == nil && cur == m.Id {
		owned = true
		if err := k.PendingMigrationByTarget.Remove(ctx, m.ToAddress); err != nil {
			return err
		}
	}
	freshErr := k.checkTargetFresh(ctx, m.FromAddress, m.ToAddress)
	if owned {
		if err := k.PendingMigrationByTarget.Set(ctx, m.ToAddress, m.Id); err != nil {
			return err
		}
	}
	return freshErr
}

// acceptBy returns the migration's accept deadline. Migrations written
// before accept_by existed fall back to the param-derived value.
func acceptBy(m types.MemberMigration, params types.Params) int64 {
	if m.AcceptBy != 0 {
		return m.AcceptBy
	}
	return m.RequestedAt + params.MigrationAcceptWindow
}

// IsReservedMigrationTarget reports whether addr must not take on an identity
// of its own because a migration is about to move one into it: it has
// accepted a migration, or it is named by a pending RECOVERY or COUNCIL
// request. Those requests need a recovery preimage or the HR committee, so
// nobody can reserve an address for free; KEY requests do not count, since
// any member can name any address in one. x/rep refuses invitations to a
// reserved address (a thief must not void a recovery by inviting its target).
func (k Keeper) IsReservedMigrationTarget(ctx context.Context, addr string) bool {
	if has, _ := k.PendingMigrationByTarget.Has(ctx, addr); has {
		return true
	}
	reserved := false
	if err := k.MigrationRequestsByTarget.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](addr),
		func(key collections.Pair[string, uint64]) (bool, error) {
			if m, err := k.MemberMigrations.Get(ctx, key.K2()); err == nil &&
				m.Status == types.MigrationStatus_MIGRATION_STATUS_REQUESTED &&
				m.Path != types.MigrationPath_MIGRATION_PATH_KEY {
				reserved = true
				return true, nil
			}
			return false, nil
		}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("IsReservedMigrationTarget: MigrationRequestsByTarget.Walk failed", "err", err)
	}
	return reserved
}

// CancelMigration cancels a pending migration. Who may cancel: the old or new
// address for KEY migrations; the HR committee policy for KEY and COUNCIL
// migrations; governance for any path (the only authority over a RECOVERY
// migration, whose commitment predates any compromise). Cancelling an
// OBJECTED migration refunds the objector, so governance cancelling a
// migration whose objection was escalated to it upholds the objection.
func (k Keeper) CancelMigration(ctx context.Context, signer string, id uint64) error {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return types.ErrMigrationNotFound
	}
	if !types.IsPendingMigrationStatus(m.Status) {
		return errorsmod.Wrapf(types.ErrMigrationWrongStatus, "status %s", m.Status)
	}
	allowed := k.IsGovAuthority(signer)
	switch m.Path {
	case types.MigrationPath_MIGRATION_PATH_KEY:
		allowed = allowed || signer == m.FromAddress || signer == m.ToAddress || k.IsHRCommitteePolicy(ctx, signer)
	case types.MigrationPath_MIGRATION_PATH_COUNCIL:
		allowed = allowed || k.IsHRCommitteePolicy(ctx, signer)
	}
	if !allowed {
		return errorsmod.Wrapf(types.ErrMigrationNotAuthorized, "%s may not cancel a %s migration", signer, m.Path)
	}
	return k.cancelMigration(ctx, &m, types.MigrationStatus_MIGRATION_STATUS_CANCELLED, "cancelled by "+signer)
}

// cancelMigration moves a pending migration to a terminal status, clears its
// queue and index entries and refunds an objection bond to the objector.
func (k Keeper) cancelMigration(ctx context.Context, m *types.MemberMigration, status types.MigrationStatus, reason string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if err := k.clearPendingIndexes(ctx, *m, params); err != nil {
		return err
	}
	if m.Status == types.MigrationStatus_MIGRATION_STATUS_OBJECTED && m.ObjectionBond.IsPositive() {
		if err := k.payObjectionBond(ctx, m, m.Objector); err != nil {
			return err
		}
	}
	m.Status = status
	m.FailureReason = reason
	if err := k.MemberMigrations.Set(ctx, m.Id, *m); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("member_migration_cancelled",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", m.Id)),
		sdk.NewAttribute("from", m.FromAddress),
		sdk.NewAttribute("to", m.ToAddress),
		sdk.NewAttribute("status", status.String()),
		sdk.NewAttribute("reason", reason),
	))
	return nil
}

// clearPendingIndexes removes a migration's pending pointers, accept-expiry
// and queue entries (whichever exist for its status).
func (k Keeper) clearPendingIndexes(ctx context.Context, m types.MemberMigration, params types.Params) error {
	if err := k.PendingMigrationByMember.Remove(ctx, m.FromAddress); err != nil {
		return err
	}
	if cur, err := k.PendingMigrationByTarget.Get(ctx, m.ToAddress); err == nil && cur == m.Id {
		if err := k.PendingMigrationByTarget.Remove(ctx, m.ToAddress); err != nil {
			return err
		}
	}
	if err := k.MigrationRequestsByTarget.Remove(ctx, collections.Join(m.ToAddress, m.Id)); err != nil {
		return err
	}
	if err := k.MigrationAcceptExpiry.Remove(ctx, collections.Join(acceptBy(m, params), m.Id)); err != nil {
		return err
	}
	if m.ExecutesAt > 0 {
		if err := k.MigrationQueue.Remove(ctx, collections.Join(m.ExecutesAt, m.Id)); err != nil {
			return err
		}
	}
	return k.ObjectedMigrations.Remove(ctx, m.Id)
}

// payObjectionBond releases the escrowed objection bond to recipient.
func (k Keeper) payObjectionBond(ctx context.Context, m *types.MemberMigration, recipient string) error {
	to, err := k.addressCodec.StringToBytes(recipient)
	if err != nil {
		return err
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), m.ObjectionBond))
	if err := k.bankKeeper.SendCoins(ctx, AppealBondEscrowAddress(), to, coins); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("migration_objection_bond_paid",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", m.Id)),
		sdk.NewAttribute("recipient", recipient),
		sdk.NewAttribute("amount", m.ObjectionBond.String()),
	))
	m.ObjectionBond = math.ZeroInt()
	return nil
}

// ObjectMigration lets the old address contest a COUNCIL migration during
// its delay. It does not cancel the migration: in the stolen-key case the
// thief holds the key, so a veto would let the thief block recovery. The
// objector posts migration_objection_bond and an x/rep jury decides; an
// inconclusive jury escalates to x/gov (resolveMigrationObjection). The bond
// is paid to the new address if the migration is upheld and refunded
// otherwise, never burned (on the stolen-key path it is the victim's SPARK).
func (k Keeper) ObjectMigration(ctx context.Context, member string, id uint64, reason string) error {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return types.ErrMigrationNotFound
	}
	if m.FromAddress != member {
		return errorsmod.Wrap(types.ErrMigrationNotAuthorized, "only the migrating address may object")
	}
	if m.Path != types.MigrationPath_MIGRATION_PATH_COUNCIL {
		return errorsmod.Wrapf(types.ErrMigrationWrongStatus, "only council migrations can be objected to (path %s)", m.Path)
	}
	if m.Status != types.MigrationStatus_MIGRATION_STATUS_ACCEPTED {
		return errorsmod.Wrapf(types.ErrMigrationWrongStatus, "status %s", m.Status)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	memberAddr, err := k.addressCodec.StringToBytes(member)
	if err != nil {
		return err
	}
	bond := params.MigrationObjectionBond
	if bond.IsPositive() {
		coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), bond))
		if err := k.bankKeeper.SendCoins(ctx, memberAddr, AppealBondEscrowAddress(), coins); err != nil {
			return errorsmod.Wrap(types.ErrMigrationObjectionBond, err.Error())
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"type":         types.MigrationObjectionInitiativeType,
		"migration_id": id,
		"from":         m.FromAddress,
		"to":           m.ToAddress,
		"reason":       reason,
	})
	deadline := now + types.DefaultAppealDeadline
	reviewID, err := k.CreateAppealInitiative(ctx, types.MigrationObjectionInitiativeType, payload, deadline)
	if err != nil {
		return errorsmod.Wrap(err, "failed to open objection jury review")
	}
	// Seat a jury that excludes both parties, drawn in the next BeginBlocker
	// so the objector cannot pick it (REP-R-102). Best effort, like appeals:
	// a review nobody can be seated on resolves INCONCLUSIVE at its deadline.
	if err := k.openAppealJury(ctx, reviewID, []string{m.FromAddress, m.ToAddress}); err != nil {
		return err
	}

	// Leave the queue while the jury sits; the verdict re-enqueues or
	// cancels.
	if err := k.MigrationQueue.Remove(ctx, collections.Join(m.ExecutesAt, id)); err != nil {
		return err
	}
	m.Status = types.MigrationStatus_MIGRATION_STATUS_OBJECTED
	m.JuryReviewId = reviewID
	m.Objector = member
	m.ObjectionBond = bond
	if err := k.MemberMigrations.Set(ctx, id, m); err != nil {
		return err
	}
	if err := k.ObjectedMigrations.Set(ctx, id); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migration_objected",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("from", m.FromAddress),
		sdk.NewAttribute("to", m.ToAddress),
		sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", reviewID)),
		sdk.NewAttribute("bond", bond.String()),
	))
	return nil
}

// resolveMigrationObjection applies a jury verdict to an OBJECTED migration.
// The objector is the "challenger": UPHOLD_CHALLENGE sustains the objection
// (migration cancelled, bond refunded); REJECT_CHALLENGE upholds the
// migration (finalizes at the next EndBlock, bond paid to the new address).
// INCONCLUSIVE (including a jury that could not be seated) neither cancels
// nor refunds: on a chain too thin to seat a jury that would let a thief
// holding the old key stall every council migration for free. The objection
// is escalated to x/gov instead (escalateMigrationObjection).
func (k Keeper) resolveMigrationObjection(ctx context.Context, review types.JuryReview, verdict types.Verdict) error {
	var payload struct {
		MigrationID uint64 `json:"migration_id"`
	}
	if err := json.Unmarshal([]byte(review.ReviewDeliverable), &payload); err != nil {
		return fmt.Errorf("decode migration objection payload: %w", err)
	}
	m, err := k.MemberMigrations.Get(ctx, payload.MigrationID)
	if err != nil {
		return types.ErrMigrationNotFound
	}
	if m.Status != types.MigrationStatus_MIGRATION_STATUS_OBJECTED || m.JuryReviewId != review.Id || m.EscalatedUntil != 0 {
		return nil // already resolved, superseded or escalated; nothing to apply
	}
	switch verdict {
	case types.Verdict_VERDICT_REJECT_CHALLENGE:
		return k.rejectMigrationObjection(ctx, &m, fmt.Sprintf("jury:%d", review.Id))
	case types.Verdict_VERDICT_UPHOLD_CHALLENGE:
		return k.cancelMigration(ctx, &m, types.MigrationStatus_MIGRATION_STATUS_CANCELLED,
			fmt.Sprintf("objection upheld by jury review %d", review.Id))
	default:
		return k.escalateMigrationObjection(ctx, &m)
	}
}

// rejectMigrationObjection settles an objection against the objector: the
// bond is paid to the new address and the migration re-enters the queue to
// finalize at the next EndBlock. by names the deciding authority ("jury:<id>",
// "gov" or "escalation_expired") on the event.
func (k Keeper) rejectMigrationObjection(ctx context.Context, m *types.MemberMigration, by string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if m.ObjectionBond.IsPositive() {
		if err := k.payObjectionBond(ctx, m, m.ToAddress); err != nil {
			return err
		}
	}
	m.Status = types.MigrationStatus_MIGRATION_STATUS_ACCEPTED
	m.ExecutesAt = sdkCtx.BlockTime().Unix()
	m.EscalatedUntil = 0
	if err := k.MemberMigrations.Set(ctx, m.Id, *m); err != nil {
		return err
	}
	if err := k.ObjectedMigrations.Remove(ctx, m.Id); err != nil {
		return err
	}
	if err := k.MigrationQueue.Set(ctx, collections.Join(m.ExecutesAt, m.Id)); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migration_objection_rejected",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", m.Id)),
		sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", m.JuryReviewId)),
		sdk.NewAttribute("by", by),
	))
	return nil
}

// escalateMigrationObjection hands an objection the jury could not decide to
// x/gov. The migration stays OBJECTED (out of the finalization queue) and the
// objector's bond stays in escrow. Governance decides with the existing
// messages: MsgCancelMigration upholds the objection (cancel, bond refunded),
// MsgForceMigration naming the same member and new address rejects it
// (finalize, bond paid to the new address). If governance has not decided
// within MigrationObjectionEscalationWindow, SweepObjectedMigrations rejects
// the objection.
func (k Keeper) escalateMigrationObjection(ctx context.Context, m *types.MemberMigration) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	m.EscalatedUntil = sdkCtx.BlockTime().Unix() + types.MigrationObjectionEscalationWindow
	if err := k.MemberMigrations.Set(ctx, m.Id, *m); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("member_migration_objection_escalated",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", m.Id)),
		sdk.NewAttribute("from", m.FromAddress),
		sdk.NewAttribute("to", m.ToAddress),
		sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", m.JuryReviewId)),
		sdk.NewAttribute("escalated_until", fmt.Sprintf("%d", m.EscalatedUntil)),
	))
	return nil
}

// isEscalatedObjection reports whether m is an OBJECTED council migration
// whose objection waits on x/gov.
func isEscalatedObjection(m types.MemberMigration) bool {
	return m.Status == types.MigrationStatus_MIGRATION_STATUS_OBJECTED && m.EscalatedUntil != 0
}

// SweepObjectedMigrations tallies objection reviews whose deadline passed
// without a verdict, so a silent jury cannot park a migration forever, and
// rejects escalated objections governance left undecided past their
// escalation window. It walks only the OBJECTED migrations
// (ObjectedMigrations), not every pending one.
func (k Keeper) SweepObjectedMigrations(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	var due, expired []uint64
	err := k.ObjectedMigrations.Walk(ctx, nil, func(id uint64) (bool, error) {
		m, err := k.MemberMigrations.Get(ctx, id)
		if err != nil || m.Status != types.MigrationStatus_MIGRATION_STATUS_OBJECTED {
			return false, nil
		}
		if m.EscalatedUntil != 0 {
			if m.EscalatedUntil <= now {
				expired = append(expired, id)
			}
			return false, nil
		}
		review, err := k.GetJuryReview(ctx, m.JuryReviewId)
		if err != nil || review.Verdict != types.Verdict_VERDICT_PENDING || review.Deadline > now {
			return false, nil
		}
		due = append(due, m.JuryReviewId)
		return false, nil
	})
	if err != nil {
		return err
	}
	for _, reviewID := range due {
		if err := k.TallyJuryVotes(ctx, reviewID); err != nil {
			sdkCtx.Logger().Error("failed to tally expired migration objection", "review_id", reviewID, "error", err)
		}
	}
	for _, id := range expired {
		cacheCtx, write := sdkCtx.CacheContext()
		m, err := k.MemberMigrations.Get(cacheCtx, id)
		if err == nil {
			err = k.rejectMigrationObjection(cacheCtx, &m, "escalation_expired")
		}
		if err != nil {
			sdkCtx.Logger().Error("failed to expire escalated migration objection", "migration_id", id, "error", err)
			continue
		}
		write()
	}
	return nil
}

// AddTombstoneSweeper registers a module callback that MsgSweepTombstone runs
// before moving the tombstone's spendable SPARK: it collects module-held
// funds of the tombstone (x/service: matured exit bonds) for the canonical
// address. Wired from app.go.
func (k Keeper) AddTombstoneSweeper(fn func(ctx context.Context, tombstone, canonical sdk.AccAddress) error) {
	k.late.tombstoneSweepers = append(k.late.tombstoneSweepers, fn)
}

// SweepTombstone moves a tombstone's spendable SPARK to its canonical
// address. Unbonding completes to the tombstone through UndelegateCoins,
// which bypasses the send restriction, so SPARK keeps landing there after
// finalization; on the RECOVERY and COUNCIL paths the tombstone cannot
// MsgSend it, so the canonical address collects it here. On those paths it
// also starts unbonding any delegation still at the tombstone (finalization
// starts them all, but a validator at its unbonding-entry cap, or more than
// MaxTombstoneUndelegations delegations, leaves some behind); once that
// unbonding matures, the next sweep collects the released SPARK.
func (k Keeper) SweepTombstone(ctx context.Context, signer, tombstone string) (sdk.Coins, error) {
	t, found := k.GetTombstone(ctx, tombstone)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrNotTombstoneOwner, "%s is not a tombstone", tombstone)
	}
	if t.Canonical != signer {
		return nil, errorsmod.Wrapf(types.ErrNotTombstoneOwner, "canonical address is %s", t.Canonical)
	}
	from, err := k.addressCodec.StringToBytes(tombstone)
	if err != nil {
		return nil, err
	}
	to, err := k.addressCodec.StringToBytes(signer)
	if err != nil {
		return nil, err
	}
	if t.Path != types.MigrationPath_MIGRATION_PATH_KEY {
		k.undelegateTombstone(ctx, from, "sweep")
	}
	for _, sweep := range k.late.tombstoneSweepers {
		if err := sweep(ctx, from, to); err != nil {
			return nil, err
		}
	}
	spendable := k.bankKeeper.SpendableCoins(ctx, from)
	if spendable.IsZero() {
		return sdk.NewCoins(), nil
	}
	if err := k.bankKeeper.SendCoins(ctx, from, to, spendable); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("tombstone_swept",
		sdk.NewAttribute("tombstone", tombstone),
		sdk.NewAttribute("canonical", signer),
		sdk.NewAttribute("amount", spendable.String()),
	))
	return spendable, nil
}

// GetMemberMigration returns a migration by id.
func (k Keeper) GetMemberMigration(ctx context.Context, id uint64) (types.MemberMigration, bool) {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return types.MemberMigration{}, false
	}
	return m, true
}

// GetPendingMigration returns the member's pending migration, if any.
func (k Keeper) GetPendingMigration(ctx context.Context, member string) (types.MemberMigration, bool) {
	id, err := k.PendingMigrationByMember.Get(ctx, member)
	if err != nil {
		return types.MemberMigration{}, false
	}
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil || !types.IsPendingMigrationStatus(m.Status) {
		return types.MemberMigration{}, false
	}
	return m, true
}

// GetPendingMigrationByTarget returns the pending migration whose new address
// is to: the accepted one if any, otherwise the most recent request naming
// it.
func (k Keeper) GetPendingMigrationByTarget(ctx context.Context, to string) (types.MemberMigration, bool) {
	if id, err := k.PendingMigrationByTarget.Get(ctx, to); err == nil {
		if m, err := k.MemberMigrations.Get(ctx, id); err == nil && types.IsPendingMigrationStatus(m.Status) {
			return m, true
		}
	}
	var found types.MemberMigration
	ok := false
	if err := k.MigrationRequestsByTarget.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](to),
		func(key collections.Pair[string, uint64]) (bool, error) {
			if m, err := k.MemberMigrations.Get(ctx, key.K2()); err == nil && types.IsPendingMigrationStatus(m.Status) {
				found, ok = m, true // ascending ids: the last match is the newest
			}
			return false, nil
		}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("GetPendingMigrationByTarget: MigrationRequestsByTarget.Walk failed", "err", err)
	}
	return found, ok
}

// CheckMigrationTarget runs the full target-freshness check for a prospective
// migration, including every module's BeforeMemberMigration veto. Used by the
// simulation to avoid delivering a request the handler would refuse.
func (k Keeper) CheckMigrationTarget(ctx context.Context, from, to string, path types.MigrationPath) error {
	if err := k.checkTargetFresh(ctx, from, to); err != nil {
		return err
	}
	fromAddr, err := k.addressCodec.StringToBytes(from)
	if err != nil {
		return err
	}
	toAddr, err := k.addressCodec.StringToBytes(to)
	if err != nil {
		return err
	}
	return k.migrationHooks().BeforeMemberMigration(ctx, fromAddr, toAddr, path)
}
