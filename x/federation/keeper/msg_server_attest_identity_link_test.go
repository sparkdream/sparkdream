package keeper_test

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	servicetypes "sparkdream/x/service/types"
)

// attestEnv is an ActivityPub peer with one bound bridge operator and a
// member's UNVERIFIED claim to @phoenix on that peer.
type attestEnv struct {
	f        *fixture
	ms       types.MsgServer
	peer     string
	operator string
	member   string
	remote   string
}

func newAttestEnv(t *testing.T) *attestEnv {
	t.Helper()
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	e := &attestEnv{f: f, ms: ms, peer: "aurora.example", remote: "@phoenix@aurora.example"}
	registerTestPeer(t, f, ms, e.peer)
	e.operator = registerTestBridge(t, f, ms, e.peer, "attest-op")
	e.member = testAddr(t, f, "attest-member")
	e.link(t, e.member)
	return e
}

func (e *attestEnv) link(t *testing.T, local string) {
	t.Helper()
	_, err := e.ms.LinkIdentity(e.f.ctx, &types.MsgLinkIdentity{Creator: local, PeerId: e.peer, RemoteIdentity: e.remote})
	require.NoError(t, err)
}

func (e *attestEnv) getLink(t *testing.T, local string) types.IdentityLink {
	t.Helper()
	link, err := e.f.keeper.IdentityLinks.Get(e.f.ctx, collections.Join(local, e.peer))
	require.NoError(t, err)
	return link
}

// msg builds the attestation an honest operator would sign for local's claim.
func (e *attestEnv) msg(t *testing.T, local string) *types.MsgAttestIdentityLink {
	t.Helper()
	return &types.MsgAttestIdentityLink{
		Operator:       e.operator,
		PeerId:         e.peer,
		LocalAddress:   local,
		RemoteIdentity: e.remote,
		Challenge:      hex.EncodeToString(e.getLink(t, local).Challenge),
		EvidenceUri:    "https://aurora.example/@phoenix/101",
	}
}

func (e *attestEnv) attest(ctx context.Context, msg *types.MsgAttestIdentityLink) error {
	_, err := e.ms.AttestIdentityLink(ctx, msg)
	return err
}

func TestAttestIdentityLink(t *testing.T) {
	e := newAttestEnv(t)
	qs := keeper.NewQueryServerImpl(e.f.keeper)
	before := e.getLink(t, e.member)

	// The claim's event carries the proof string the member publishes.
	require.True(t, hasEvent(e.f.sdkCtx(), types.EventTypeIdentityLinked, map[string]string{
		types.AttributeKeyLinkProof: types.IdentityLinkProofString(e.f.sdkCtx().ChainID(), e.member, before.Challenge),
	}))

	require.NoError(t, e.attest(e.f.ctx, e.msg(t, e.member)))

	link := e.getLink(t, e.member)
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED, link.Status)
	require.Equal(t, e.operator, link.AttestedBy)
	require.Equal(t, "https://aurora.example/@phoenix/101", link.AttestationEvidence)
	require.NotZero(t, link.VerifiedAt)

	resp, err := qs.ResolveRemoteIdentity(e.f.ctx, &types.QueryResolveRemoteIdentityRequest{PeerId: e.peer, RemoteIdentity: e.remote})
	require.NoError(t, err)
	require.Equal(t, e.member, resp.LocalAddress)
	has, _ := e.f.keeper.IdentityLinksByAttester.Has(e.f.ctx, collections.Join3(e.operator, e.peer, e.member))
	require.True(t, has)
	has, _ = e.f.keeper.UnverifiedLinkExp.Has(e.f.ctx, collections.Join3(before.UnverifiedExpiresAt, e.member, e.peer))
	require.False(t, has, "a verified link leaves the unverified expiry queue")
	require.True(t, hasEvent(e.f.sdkCtx(), types.EventTypeIdentityLinkAttested, map[string]string{
		types.AttributeKeyOperator: e.operator, types.AttributeKeyLocalAddress: e.member,
	}))

	// A second attestation of the same link is refused.
	require.ErrorIs(t, e.attest(e.f.ctx, e.msg(t, e.member)), types.ErrIdentityLinkAlreadyVerified)
}

func TestAttestIdentityLinkOperatorGates(t *testing.T) {
	t.Run("not a bridge operator", func(t *testing.T) {
		e := newAttestEnv(t)
		m := e.msg(t, e.member)
		m.Operator = testAddr(t, e.f, "zenith-stranger")
		require.ErrorIs(t, e.attest(e.f.ctx, m), types.ErrBridgeNotFound)
	})
	t.Run("bound to another peer only", func(t *testing.T) {
		e := newAttestEnv(t)
		registerTestPeer(t, e.f, e.ms, "zenith.example")
		m := e.msg(t, e.member)
		m.Operator = registerTestBridge(t, e.f, e.ms, "zenith.example", "zenith-op")
		require.ErrorIs(t, e.attest(e.f.ctx, m), types.ErrBridgeNotFound)
	})
	t.Run("suspended binding", func(t *testing.T) {
		e := newAttestEnv(t)
		key := collections.Join(e.operator, e.peer)
		b, err := e.f.keeper.BridgeBindings.Get(e.f.ctx, key)
		require.NoError(t, err)
		b.Suspended = true
		require.NoError(t, e.f.keeper.BridgeBindings.Set(e.f.ctx, key, b))
		require.ErrorIs(t, e.attest(e.f.ctx, e.msg(t, e.member)), types.ErrBridgeNotActive)
	})
	t.Run("removed binding", func(t *testing.T) {
		e := newAttestEnv(t)
		require.NoError(t, e.f.keeper.BridgeBindings.Remove(e.f.ctx, collections.Join(e.operator, e.peer)))
		require.ErrorIs(t, e.attest(e.f.ctx, e.msg(t, e.member)), types.ErrBridgeNotFound)
	})
	t.Run("operator unbonding in x/service", func(t *testing.T) {
		e := newAttestEnv(t)
		sk := wireServiceKeeper(t, e.f)
		sk.GetOperatorFn = func(context.Context, []byte, string) (servicetypes.Operator, bool) {
			return servicetypes.Operator{Status: servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING}, true
		}
		require.ErrorIs(t, e.attest(e.f.ctx, e.msg(t, e.member)), types.ErrBindingNotActive)
	})
	t.Run("self-attestation", func(t *testing.T) {
		e := newAttestEnv(t)
		e.link(t, e.operator)
		require.ErrorIs(t, e.attest(e.f.ctx, e.msg(t, e.operator)), types.ErrSelfAttestation)
	})
	t.Run("suspended peer", func(t *testing.T) {
		e := newAttestEnv(t)
		setPeerStatus(t, e.f, e.peer, types.PeerStatus_PEER_STATUS_SUSPENDED)
		require.ErrorIs(t, e.attest(e.f.ctx, e.msg(t, e.member)), types.ErrPeerNotActive)
	})
}

func TestAttestIdentityLinkClaimGates(t *testing.T) {
	t.Run("missing claim", func(t *testing.T) {
		e := newAttestEnv(t)
		m := e.msg(t, e.member)
		m.LocalAddress = testAddr(t, e.f, "aurora-nobody")
		require.ErrorIs(t, e.attest(e.f.ctx, m), types.ErrIdentityLinkNotFound)
	})
	t.Run("different remote identity", func(t *testing.T) {
		e := newAttestEnv(t)
		m := e.msg(t, e.member)
		m.RemoteIdentity = "@zenith@aurora.example"
		require.ErrorIs(t, e.attest(e.f.ctx, m), types.ErrIdentityLinkNotFound)
	})
	t.Run("expired claim", func(t *testing.T) {
		e := newAttestEnv(t)
		m := e.msg(t, e.member)
		params, err := e.f.keeper.Params.Get(e.f.ctx)
		require.NoError(t, err)
		late := e.f.sdkCtx().WithBlockTime(e.f.sdkCtx().BlockTime().Add(params.UnverifiedLinkTtl + time.Second))
		require.ErrorIs(t, e.attest(late, m), types.ErrIdentityLinkExpired)
	})
	t.Run("wrong challenge", func(t *testing.T) {
		e := newAttestEnv(t)
		m := e.msg(t, e.member)
		m.Challenge = hex.EncodeToString(make([]byte, 32))
		require.ErrorIs(t, e.attest(e.f.ctx, m), types.ErrAttestationChallengeMismatch)
		m.Challenge = "not-hex"
		require.ErrorIs(t, e.attest(e.f.ctx, m), types.ErrAttestationChallengeMismatch)
	})
	t.Run("stale challenge after relink", func(t *testing.T) {
		e := newAttestEnv(t)
		stale := e.msg(t, e.member)
		_, err := e.ms.UnlinkIdentity(e.f.ctx, &types.MsgUnlinkIdentity{Creator: e.member, PeerId: e.peer})
		require.NoError(t, err)
		later := e.f.sdkCtx().WithBlockHeight(e.f.sdkCtx().BlockHeight() + 1).WithBlockTime(e.f.sdkCtx().BlockTime().Add(time.Minute))
		_, err = e.ms.LinkIdentity(later, &types.MsgLinkIdentity{Creator: e.member, PeerId: e.peer, RemoteIdentity: e.remote})
		require.NoError(t, err)
		require.ErrorIs(t, e.attest(later, stale), types.ErrAttestationChallengeMismatch)
	})
	t.Run("evidence off the peer's hosts", func(t *testing.T) {
		e := newAttestEnv(t)
		m := e.msg(t, e.member)
		m.EvidenceUri = "https://zenith.example/@phoenix/1"
		require.ErrorIs(t, e.attest(e.f.ctx, m), types.ErrInvalidAttestationEvidence)
		m.EvidenceUri = ""
		require.ErrorIs(t, e.attest(e.f.ctx, m), types.ErrInvalidAttestationEvidence)
	})
	t.Run("remote identity off the peer's hosts", func(t *testing.T) {
		e := newAttestEnv(t)
		e.remote = "@phoenix@zenith.example"
		other := testAddr(t, e.f, "aurora-offhost")
		e.link(t, other)
		require.ErrorIs(t, e.attest(e.f.ctx, e.msg(t, other)), types.ErrCreatorHostMismatch)
	})
	t.Run("spark dream peer verifies over IBC", func(t *testing.T) {
		e := newAttestEnv(t)
		registerTestIBCPeer(t, e.f, e.ms, "phoenix-chain")
		seedBinding(t, e.f, e.operator, "phoenix-chain", types.ServiceTypeFederationBridgeActivityPub)
		m := e.msg(t, e.member)
		m.PeerId = "phoenix-chain"
		require.ErrorIs(t, e.attest(e.f.ctx, m), types.ErrPeerTypeMismatch)
	})
}

// One verified owner per remote identity: two members may both claim it,
// but once one is attested the other's attestation is refused.
func TestAttestIdentityLinkDuplicateVerifiedIdentity(t *testing.T) {
	e := newAttestEnv(t)
	rival := testAddr(t, e.f, "aurora-rival")
	e.link(t, rival)
	rivalMsg := e.msg(t, rival)

	require.NoError(t, e.attest(e.f.ctx, e.msg(t, e.member)))
	require.ErrorIs(t, e.attest(e.f.ctx, rivalMsg), types.ErrRemoteIdentityAlreadyClaimed)

	owner, err := e.f.keeper.IdentityLinksByRemote.Get(e.f.ctx, collections.Join(e.peer, e.remote))
	require.NoError(t, err)
	require.Equal(t, e.member, owner)
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED, e.getLink(t, rival).Status)
}

// Unlinking an attested link clears the verified reverse index and the
// per-attester index, and frees the identity for another member.
func TestUnlinkAttestedIdentityClearsIndexes(t *testing.T) {
	e := newAttestEnv(t)
	rival := testAddr(t, e.f, "aurora-rival")
	e.link(t, rival)
	require.NoError(t, e.attest(e.f.ctx, e.msg(t, e.member)))

	_, err := e.ms.UnlinkIdentity(e.f.ctx, &types.MsgUnlinkIdentity{Creator: e.member, PeerId: e.peer})
	require.NoError(t, err)

	has, _ := e.f.keeper.IdentityLinksByRemote.Has(e.f.ctx, collections.Join(e.peer, e.remote))
	require.False(t, has)
	has, _ = e.f.keeper.IdentityLinksByAttester.Has(e.f.ctx, collections.Join3(e.operator, e.peer, e.member))
	require.False(t, has)

	// The rival's still-pending claim can now be attested.
	require.NoError(t, e.attest(e.f.ctx, e.msg(t, rival)))
}

// A slashed (dissolved) operator's attestations lose their bond: the hook
// queues them and the EndBlocker returns the links to UNVERIFIED with a
// fresh TTL, keeping the challenge so another operator can attest the same
// published proof.
func TestDissolvedOperatorAttestationsRevert(t *testing.T) {
	e := newAttestEnv(t)
	// The registered binding has no x/service reverse index in this
	// fixture; seed it as x/service would have.
	require.NoError(t, e.f.keeper.BindingsByOperator.Set(e.f.ctx,
		collections.Join3(types.ServiceTypeFederationBridgeActivityPub, e.operator, e.peer)))
	require.NoError(t, e.attest(e.f.ctx, e.msg(t, e.member)))
	challenge := e.getLink(t, e.member).Challenge

	opBytes, err := e.f.addressCodec.StringToBytes(e.operator)
	require.NoError(t, err)
	keeper.NewFederationServiceHooks(e.f.keeper).AfterOperatorDissolved(e.f.ctx, sdk.AccAddress(opBytes), types.ServiceTypeFederationBridgeActivityPub)

	queued, _ := e.f.keeper.AttesterRevocations.Has(e.f.ctx, collections.Join(e.operator, e.peer))
	require.True(t, queued)
	// Until the EndBlocker runs the link is still verified.
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED, e.getLink(t, e.member).Status)

	ctx := e.f.sdkCtx().WithEventManager(sdk.NewEventManager())
	require.NoError(t, e.f.keeper.EndBlocker(ctx))

	link := e.getLink(t, e.member)
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED, link.Status)
	require.Empty(t, link.AttestedBy)
	require.Empty(t, link.AttestationEvidence)
	require.Equal(t, challenge, link.Challenge)
	params, _ := e.f.keeper.Params.Get(e.f.ctx)
	require.Equal(t, ctx.BlockTime().Unix()+int64(params.UnverifiedLinkTtl.Seconds()), link.UnverifiedExpiresAt)
	has, _ := e.f.keeper.UnverifiedLinkExp.Has(e.f.ctx, collections.Join3(link.UnverifiedExpiresAt, e.member, e.peer))
	require.True(t, has)
	has, _ = e.f.keeper.IdentityLinksByRemote.Has(e.f.ctx, collections.Join(e.peer, e.remote))
	require.False(t, has)
	has, _ = e.f.keeper.IdentityLinksByAttester.Has(e.f.ctx, collections.Join3(e.operator, e.peer, e.member))
	require.False(t, has)
	queued, _ = e.f.keeper.AttesterRevocations.Has(e.f.ctx, collections.Join(e.operator, e.peer))
	require.False(t, queued)
	require.True(t, hasEvent(ctx, types.EventTypeIdentityAttestationRevoked, map[string]string{
		types.AttributeKeyOperator: e.operator, types.AttributeKeyLocalAddress: e.member,
	}))

	// Another bound operator can attest the same proof.
	other := registerTestBridge(t, e.f, e.ms, e.peer, "attest-op-2")
	m := e.msg(t, e.member)
	m.Operator = other
	require.NoError(t, e.attest(e.f.ctx, m))
	require.Equal(t, other, e.getLink(t, e.member).AttestedBy)
}

// A retired (honestly exited) operator's attestations stand.
func TestRetiredOperatorAttestationsStand(t *testing.T) {
	e := newAttestEnv(t)
	require.NoError(t, e.f.keeper.BindingsByOperator.Set(e.f.ctx,
		collections.Join3(types.ServiceTypeFederationBridgeActivityPub, e.operator, e.peer)))
	require.NoError(t, e.attest(e.f.ctx, e.msg(t, e.member)))

	opBytes, err := e.f.addressCodec.StringToBytes(e.operator)
	require.NoError(t, err)
	keeper.NewFederationServiceHooks(e.f.keeper).AfterOperatorRetired(e.f.ctx, sdk.AccAddress(opBytes), types.ServiceTypeFederationBridgeActivityPub)
	require.NoError(t, e.f.keeper.EndBlocker(e.f.sdkCtx()))

	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED, e.getLink(t, e.member).Status)
	queued, _ := e.f.keeper.AttesterRevocations.Has(e.f.ctx, collections.Join(e.operator, e.peer))
	require.False(t, queued)
}

// The attestation fields and the revocation queue survive export/import,
// and the per-attester index is rebuilt from the links.
func TestGenesisRoundTripsAttestedLinks(t *testing.T) {
	e := newAttestEnv(t)
	require.NoError(t, e.attest(e.f.ctx, e.msg(t, e.member)))
	require.NoError(t, e.f.keeper.AttesterRevocations.Set(e.f.ctx, collections.Join("zenith-op", e.peer)))

	gs, err := e.f.keeper.ExportGenesis(e.f.ctx)
	require.NoError(t, err)
	gs.PortId = types.PortID // the unit fixture never binds the port
	require.NoError(t, gs.Validate())

	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *gs))
	link, err := f2.keeper.IdentityLinks.Get(f2.ctx, collections.Join(e.member, e.peer))
	require.NoError(t, err)
	require.Equal(t, e.operator, link.AttestedBy)
	require.Equal(t, "https://aurora.example/@phoenix/101", link.AttestationEvidence)
	has, _ := f2.keeper.IdentityLinksByAttester.Has(f2.ctx, collections.Join3(e.operator, e.peer, e.member))
	require.True(t, has)
	owner, err := f2.keeper.IdentityLinksByRemote.Get(f2.ctx, collections.Join(e.peer, e.remote))
	require.NoError(t, err)
	require.Equal(t, e.member, owner)

	got, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, []types.AttesterRevocation{{Operator: "zenith-op", PeerId: e.peer}}, got.AttesterRevocationQueue)
}
