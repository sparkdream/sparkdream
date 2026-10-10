package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

func (f *fixture) hide(authority string, kind types.HideTargetKind, classID, tokenID uint64) (uint64, error) {
	resp, err := f.msg.HideContent(f.ctx, &types.MsgHideContent{
		Authority: authority, TargetKind: kind, ClassId: classID, TokenId: tokenID,
		Reason: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	if err != nil {
		return 0, err
	}
	return resp.HideId, nil
}

func (f *fixture) hideRecord(id uint64) types.HideRecord {
	f.t.Helper()
	hr, err := f.keeper.HideRecords.Get(f.ctx, id)
	require.NoError(f.t, err)
	return hr
}

func TestHideTokenAuthority(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 1)

	_, err := f.hide(f.dave.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = f.msg.HideContent(f.ctx, &types.MsgHideContent{Authority: f.ops.String(), TargetKind: types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, ClassId: id, TokenId: 1})
	require.ErrorIs(t, err, types.ErrInvalidReason)

	// Sentinel path reserves the bond and records the action.
	hid, err := f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.NoError(t, err)
	hr := f.hideRecord(hid)
	require.Equal(t, f.sentinel.String(), hr.Sentinel)
	require.Equal(t, f.params().SentinelHideCommitDream, hr.CommittedAmount)
	require.Equal(t, f.params().SentinelHideCommitDream, f.rep.reserved[f.sentinel.String()])
	require.Contains(t, f.rep.actions, reptypes.ActionKindArtifactHide)
	require.Equal(t, types.ContentStatus_CONTENT_STATUS_HIDDEN, f.token(id, 1).Status)

	_, err = f.hide(f.ops.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.ErrorIs(t, err, types.ErrAlreadyHidden)

	// Queries withhold the metadata; ownership is unchanged.
	q, err := f.query.Token(f.ctx, &types.QueryTokenRequest{ClassId: id, TokenId: 1})
	require.NoError(t, err)
	require.True(t, q.Token.Withheld)
	require.Empty(t, q.Token.Token.Metadata.Uri)
	require.Equal(t, f.bob.String(), q.Token.Token.Owner)

	// Hidden tokens cannot be listed or edited but can be transferred and burned.
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: spark(1), Duration: 60})
	require.ErrorIs(t, err, types.ErrContentHidden)
	_, err = f.msg.UpdateToken(f.ctx, &types.MsgUpdateToken{Owner: f.alice.String(), ClassId: id, TokenId: 1, Metadata: meta("evade")})
	require.ErrorIs(t, err, types.ErrContentHidden)
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(), Entries: []types.TransferEntry{{ClassId: id, TokenId: 1, Recipient: f.alice.String()}}})
	require.NoError(t, err)
	f.checkInvariants()
}

func TestHideCancelsListing(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 1)
	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: spark(1), Duration: 600})
	require.NoError(t, err)
	_, err = f.hide(f.ops.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.NoError(t, err)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(id, 1).Lock)
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.dave.String(), ClassId: id, TokenId: 1, ExpectedPrice: spark(1), ExpectedNonce: 1})
	require.ErrorIs(t, err, types.ErrListingNotFound)
	f.checkInvariants()
}

func TestSentinelSelfCorrectAndCouncilUnhide(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 2)

	hid, err := f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.NoError(t, err)
	_, err = f.msg.UnhideContent(f.ctx, &types.MsgUnhideContent{Authority: f.dave.String(), HideId: hid})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = f.msg.UnhideContent(f.ctx, &types.MsgUnhideContent{Authority: f.sentinel.String(), HideId: hid})
	require.NoError(t, err)
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_UNHIDDEN, f.hideRecord(hid).Outcome)
	require.Equal(t, types.ContentStatus_CONTENT_STATUS_ACTIVE, f.token(id, 1).Status)
	require.True(t, f.rep.reservedOf(f.sentinel.String()).IsZero(), "bond released")
	require.Empty(t, f.rep.outcomes, "self-correct records no verdict")

	// Past the self-correct window only the council can unhide, and that
	// counts as an overturn.
	hid, err = f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 2)
	require.NoError(t, err)
	f.advance(10, f.params().SentinelUnhideWindowBlocks+1)
	_, err = f.msg.UnhideContent(f.ctx, &types.MsgUnhideContent{Authority: f.sentinel.String(), HideId: hid})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = f.msg.UnhideContent(f.ctx, &types.MsgUnhideContent{Authority: f.ops.String(), HideId: hid})
	require.NoError(t, err)
	require.Equal(t, []string{reptypes.ActionKindArtifactHide + ":false"}, f.rep.outcomes)
	f.checkInvariants()
}

func TestAppealRoutesThroughRep(t *testing.T) {
	f := initFixture(t)
	target := keeper.NewRepAppealTarget(f.keeper)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 2)

	hid, err := f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.NoError(t, err)
	_, err = f.msg.AppealHide(f.ctx, &types.MsgAppealHide{Appellant: f.alice.String(), HideId: hid})
	require.ErrorIs(t, err, types.ErrNotAppellant, "token hides are appealed by the holder")
	bobB := f.bank.bal(f.bob)
	resp, err := f.msg.AppealHide(f.ctx, &types.MsgAppealHide{Appellant: f.bob.String(), HideId: hid, Reason: "not spam"})
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp.AppealId)
	require.Equal(t, bobB, f.bank.bal(f.bob), "x/rep, not artifact, charges the appeal bond")
	require.Equal(t, []string{reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE.String() + ":" + u64s(hid)}, f.rep.appeals)
	require.Contains(t, f.rep.actions, reptypes.ActionKindArtifactAppealFiled)
	hr := f.hideRecord(hid)
	require.True(t, hr.Appealed)
	require.Equal(t, uint64(1), hr.AppealId)
	_, err = f.msg.AppealHide(f.ctx, &types.MsgAppealHide{Appellant: f.bob.String(), HideId: hid})
	require.ErrorIs(t, err, types.ErrAppealWindowClosed)

	// x/rep owns the deadline now: the artifact EndBlocker must not scrub.
	f.advance(60, f.params().HideExpiryBlocks+5)
	f.endBlock()
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_PENDING, f.hideRecord(hid).Outcome)

	// x/rep reads the sentinel and the reserved bond from artifact.
	sentinel, err := target.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid))
	require.NoError(t, err)
	require.Equal(t, f.sentinel.String(), sentinel)
	committed, err := target.GetActionCommittedAmount(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid))
	require.NoError(t, err)
	require.Equal(t, f.params().SentinelHideCommitDream, committed)

	// Verdict OVERTURNED: x/rep calls ReverseSentinelAction, then the hook.
	require.NoError(t, target.ReverseSentinelAction(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid)))
	require.NoError(t, target.OnAppealOutcome(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid), reptypes.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED))
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_OVERTURNED, f.hideRecord(hid).Outcome)
	require.Equal(t, types.ContentStatus_CONTENT_STATUS_ACTIVE, f.token(id, 1).Status)
	// Once closed, x/rep sees no sentinel (no double release/slash).
	sentinel, _ = target.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid))
	require.Empty(t, sentinel)
	f.checkInvariants()

	// Verdict UPHELD: the metadata is scrubbed, ownership kept.
	hid2, err := f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 2)
	require.NoError(t, err)
	_, err = f.msg.AppealHide(f.ctx, &types.MsgAppealHide{Appellant: f.bob.String(), HideId: hid2})
	require.NoError(t, err)
	require.NoError(t, target.OnSentinelActionResolved(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid2)))
	require.NoError(t, target.OnAppealOutcome(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid2), reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD))
	tok := f.token(id, 2)
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_UPHELD, f.hideRecord(hid2).Outcome)
	require.Equal(t, types.ContentStatus_CONTENT_STATUS_HIDDEN, tok.Status)
	require.Equal(t, types.TokenMetadata{}, tok.Metadata, "scrubbed")
	require.Equal(t, f.bob.String(), tok.Owner, "ownership survives moderation")
	f.checkInvariants()

	// Wrong action type or junk targets are rejected, unknown ids ignored.
	_, err = target.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE, u64s(hid2))
	require.Error(t, err)
	_, err = target.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, "x")
	require.Error(t, err)
	require.NoError(t, target.ReverseSentinelAction(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, "999"))
}

func TestHideExpiryAndAppealTimeout(t *testing.T) {
	f := initFixture(t)
	target := keeper.NewRepAppealTarget(f.keeper)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 2)
	p := f.params()

	// Unappealed: scrubbed at hide_expiry_blocks, bond released.
	hid1, err := f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.NoError(t, err)
	// Appealed: x/rep times out -> restored, bond released, no slash.
	hid2, err := f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 2)
	require.NoError(t, err)
	_, err = f.msg.AppealHide(f.ctx, &types.MsgAppealHide{Appellant: f.bob.String(), HideId: hid2})
	require.NoError(t, err)

	f.advance(60, p.HideExpiryBlocks)
	f.endBlock()
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_EXPIRED, f.hideRecord(hid1).Outcome)
	require.Equal(t, types.TokenMetadata{}, f.token(id, 1).Metadata)
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_PENDING, f.hideRecord(hid2).Outcome)

	require.NoError(t, target.OnAppealOutcome(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid2), reptypes.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT))
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_APPEAL_TIMEOUT, f.hideRecord(hid2).Outcome)
	require.Equal(t, types.ContentStatus_CONTENT_STATUS_ACTIVE, f.token(id, 2).Status)
	require.True(t, f.rep.reservedOf(f.sentinel.String()).IsZero())
	require.Empty(t, f.rep.slashed)
	f.checkInvariants()
}

func TestBurnHiddenTokenClosesHide(t *testing.T) {
	f := initFixture(t)
	target := keeper.NewRepAppealTarget(f.keeper)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 1)
	hid, err := f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.NoError(t, err)
	_, err = f.msg.AppealHide(f.ctx, &types.MsgAppealHide{Appellant: f.bob.String(), HideId: hid})
	require.NoError(t, err)
	bobB := f.bank.bal(f.bob)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_TARGET_BURNED, f.hideRecord(hid).Outcome)
	require.Equal(t, bobB.Add(f.params().TokenDeposit), f.bank.bal(f.bob))
	require.True(t, f.rep.reservedOf(f.sentinel.String()).IsZero())
	// A late x/rep verdict is a no-op and reports no sentinel to slash.
	sentinel, err := target.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid))
	require.NoError(t, err)
	require.Empty(t, sentinel)
	require.NoError(t, target.ReverseSentinelAction(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE, u64s(hid)))
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_TARGET_BURNED, f.hideRecord(hid).Outcome)
	f.checkInvariants()
}

func TestClassHideDrainsListingsAndScrubs(t *testing.T) {
	f := initFixture(t)
	// Long inbox TTL so the pending mint outlives the hide expiry.
	f.setParams(func(p *types.Params) { p.MaxExpirationsPerBlock = 10; p.PendingTtl = 3600 })
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.alice, 25)
	for i := uint64(1); i <= 25; i++ {
		_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: i, Price: spark(1), Duration: 6000})
		require.NoError(t, err)
	}
	// A pending mint in the class must be scrubbed too.
	_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	f.mint(f.alice, id, f.dave, 1)

	hid, err := f.hide(f.ops.String(), types.HideTargetKind_HIDE_TARGET_KIND_CLASS, id, 0)
	require.NoError(t, err)
	require.Equal(t, "", f.hideRecord(hid).Sentinel, "council hide has no sentinel")

	// Nothing is sellable while the drain runs.
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.bob.String(), ClassId: id, TokenId: 25, ExpectedPrice: spark(1), ExpectedNonce: 1})
	require.ErrorIs(t, err, types.ErrContentHidden)
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id, Entries: []types.MintEntry{{}}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrContentHidden)

	// Class queries withhold; token queries in a hidden class withhold too.
	cq, err := f.query.Class(f.ctx, &types.QueryClassRequest{ClassId: id})
	require.NoError(t, err)
	require.True(t, cq.Class.Withheld)
	require.Empty(t, cq.Class.Class.Name)
	tq, err := f.query.Token(f.ctx, &types.QueryTokenRequest{ClassId: id, TokenId: 3})
	require.NoError(t, err)
	require.True(t, tq.Token.Withheld)

	f.endBlock() // 10 listings
	f.endBlock() // 20
	count := 0
	_ = f.keeper.Listings.Walk(f.ctx, nil, func(_ keeperPairT, _ types.Listing) (bool, error) { count++; return false, nil })
	require.Equal(t, 5, count)
	f.endBlock()
	has, _ := f.keeper.ClassCancelQueue.Has(f.ctx, id)
	require.False(t, has, "queue done")
	for i := uint64(1); i <= 25; i++ {
		require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(id, i).Lock)
	}

	// Unappealed class hide expires: class and every token scrubbed.
	f.advance(60, f.params().HideExpiryBlocks)
	for i := 0; i < 5; i++ {
		f.endBlock()
	}
	c := f.class(id)
	require.Empty(t, c.Name)
	require.True(t, c.MetadataFrozen)
	for i := uint64(1); i <= 25; i++ {
		require.Equal(t, types.TokenMetadata{}, f.token(id, i).Metadata, "token %d", i)
	}
	pm, err := f.keeper.PendingMints.Get(f.ctx, keeperPair(id, 26))
	require.NoError(t, err)
	require.Equal(t, types.TokenMetadata{}, pm.Metadata)
	has, _ = f.keeper.ClassScrubQueue.Has(f.ctx, id)
	require.False(t, has)
}

func TestUnhideClassStopsDrain(t *testing.T) {
	f := initFixture(t)
	f.setParams(func(p *types.Params) { p.MaxExpirationsPerBlock = 10 })
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.alice, 15)
	for i := uint64(1); i <= 15; i++ {
		_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: i, Price: spark(1), Duration: 6000})
		require.NoError(t, err)
	}
	hid, err := f.hide(f.ops.String(), types.HideTargetKind_HIDE_TARGET_KIND_CLASS, id, 0)
	require.NoError(t, err)
	f.endBlock()
	_, err = f.msg.UnhideContent(f.ctx, &types.MsgUnhideContent{Authority: f.ops.String(), HideId: hid})
	require.NoError(t, err)
	f.endBlock()
	count := 0
	_ = f.keeper.Listings.Walk(f.ctx, nil, func(_ keeperPairT, _ types.Listing) (bool, error) { count++; return false, nil })
	require.Equal(t, 5, count, "listings not yet drained survive the unhide")
}

func TestSentinelLimits(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 3)
	f.setParams(func(p *types.Params) { p.MaxHidesPerSentinelPerDay = 1 })
	_, err := f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.NoError(t, err)
	_, err = f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 2)
	require.ErrorIs(t, err, types.ErrSentinelRateLimit)

	f.setParams(func(p *types.Params) { p.MaxHidesPerSentinelPerDay = 50 })
	f.rep.cooldown[f.sentinel.String()] = f.ctx.BlockTime().Unix() + 100
	_, err = f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 2)
	require.ErrorIs(t, err, types.ErrSentinelCooldown)

	f.rep.cooldown[f.sentinel.String()] = 0
	f.rep.sentinels[f.sentinel.String()] = f.params().SentinelHideCommitDream.SubRaw(1)
	_, err = f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 2)
	require.ErrorIs(t, err, types.ErrInsufficientBond)
}
