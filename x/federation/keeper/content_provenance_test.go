package keeper_test

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func submitURI(ms types.MsgServer, f *fixture, op, peer, uri, body string, supersedes *types.ContentRef) (*types.MsgSubmitFederatedContentResponse, error) {
	hash := sha256.Sum256([]byte(body))
	return ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		Operator: op, PeerId: peer, RemoteContentId: "1", ContentType: "blog_post",
		CreatorIdentity: "@alice@" + peer, Body: body, ContentUri: uri,
		ContentHash: hash[:], Supersedes: supersedes,
	})
}

// A bridge bonded for one peer cannot anchor another instance's post under
// that peer's name.
func TestSubmitRejectsForeignContentHost(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.example")
	op := registerTestBridge(t, f, ms, "phoenix.example", "host-op")

	_, err := submitURI(ms, f, op, "phoenix.example", "https://aurora.example/users/a/statuses/1", "foreign", nil)
	require.ErrorIs(t, err, types.ErrContentHostMismatch)

	_, err = submitURI(ms, f, op, "phoenix.example", "http://phoenix.example:3000/ap/users/1/statuses/2", "own host", nil)
	require.NoError(t, err, "the peer's own host (any port) is accepted")

	_, err = submitURI(ms, f, op, "phoenix.example", "", "no uri", nil)
	require.NoError(t, err, "an empty content_uri is still accepted; it can never be verified")
}

// A WEB_DOMAIN split instance lists its object host in content_hosts.
func TestSubmitAcceptsPolicyContentHost(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.example")
	op := registerTestBridge(t, f, ms, "phoenix.example", "alias-op")
	uri := "https://social.phoenix.example/users/a/statuses/1"

	_, err := submitURI(ms, f, op, "phoenix.example", uri, "split before", nil)
	require.ErrorIs(t, err, types.ErrContentHostMismatch)

	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority, PeerId: "phoenix.example",
		Policy: types.PeerPolicy{
			InboundContentTypes: []string{"blog_post"},
			ContentHosts:        []string{"social.phoenix.example"},
			AllowedIdentities:   []string{types.AllIdentities},
		},
	})
	require.NoError(t, err)

	_, err = submitURI(ms, f, op, "phoenix.example", uri, "split after", nil)
	require.NoError(t, err)
}

func TestUpdatePeerPolicyValidatesContentHosts(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.example")
	registerTestNOSTRPeer(t, f, ms, "zenith-relay")

	for _, hosts := range [][]string{{"Social.Phoenix.example"}, {"phoenix.example:3000"}, {"a.example", "a.example"}} {
		_, err := ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
			Authority: f.authority, PeerId: "phoenix.example",
			Policy: types.PeerPolicy{InboundContentTypes: []string{"blog_post"}, ContentHosts: hosts},
		})
		require.ErrorIs(t, err, types.ErrInvalidParamValue, "hosts %v", hosts)
	}

	_, err := ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority, PeerId: "zenith-relay",
		Policy: types.PeerPolicy{InboundContentTypes: []string{"blog_post"}, ContentHosts: []string{"zenith.example"}},
	})
	require.ErrorIs(t, err, types.ErrPeerTypeMismatch, "content_hosts are ActivityPub-only")
}

// An edit anchored while the original is still pending retires the original:
// it can never be verified, and the expiry sweep must not count it against
// the operator.
func TestSupersedeRetiresPendingPredecessor(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.example")
	op := registerTestBridge(t, f, ms, "phoenix.example", "edit-op")
	uri := "https://phoenix.example/users/a/statuses/1"

	v1, err := submitURI(ms, f, op, "phoenix.example", uri, "original", nil)
	require.NoError(t, err)
	v2, err := submitURI(ms, f, op, "phoenix.example", uri, "edited", &types.ContentRef{ContentId: v1.ContentId})
	require.NoError(t, err)

	old, err := f.keeper.Content.Get(f.ctx, v1.ContentId)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_SUPERSEDED, old.Status)
	require.Equal(t, v2.ContentId, old.SupersededBy)
	neu, err := f.keeper.Content.Get(f.ctx, v2.ContentId)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION, neu.Status)
	require.NotNil(t, neu.Supersedes)
	require.Equal(t, v1.ContentId, neu.Supersedes.ContentId)

	// Push the predecessor's verification deadline into the past and sweep.
	require.NoError(t, f.keeper.VerificationWindow.Remove(f.ctx, collections.Join(int64(0), v1.ContentId)))
	require.NoError(t, f.keeper.VerificationWindow.Set(f.ctx, collections.Join(int64(1), v1.ContentId)))
	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Now())
	require.NoError(t, f.keeper.EndBlocker(sdkCtx))

	old, err = f.keeper.Content.Get(sdkCtx, v1.ContentId)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_SUPERSEDED, old.Status,
		"the expiry sweep must leave a superseded record alone")
	binding, err := f.keeper.BridgeBindings.Get(sdkCtx, collections.Join(op, "phoenix.example"))
	require.NoError(t, err)
	require.Zero(t, binding.ContentUnverified, "a superseded record must not count as unverified")
	require.Zero(t, binding.EpochUnverified)
}

// Past verification, a predecessor keeps its status and only gains the link.
func TestSupersedeVerifiedPredecessorKeepsStatus(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.example")
	op := registerTestBridge(t, f, ms, "phoenix.example", "ver-op")
	uri := "https://phoenix.example/users/a/statuses/2"

	v1, err := submitURI(ms, f, op, "phoenix.example", uri, "original", nil)
	require.NoError(t, err)
	old, err := f.keeper.Content.Get(f.ctx, v1.ContentId)
	require.NoError(t, err)
	old.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
	require.NoError(t, f.keeper.Content.Set(f.ctx, v1.ContentId, old))

	v2, err := submitURI(ms, f, op, "phoenix.example", uri, "edited", &types.ContentRef{ContentId: v1.ContentId})
	require.NoError(t, err)
	old, err = f.keeper.Content.Get(f.ctx, v1.ContentId)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, old.Status)
	require.Equal(t, v2.ContentId, old.SupersededBy)
}

// An operator can only supersede its own record of the same content_uri on
// the same peer, once.
func TestSupersedeRejections(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.example")
	registerTestPeer(t, f, ms, "aurora.example")
	op := registerTestBridge(t, f, ms, "phoenix.example", "own-op")
	require.Equal(t, op, registerTestBridge(t, f, ms, "aurora.example", "own-op"))
	other := registerTestBridge(t, f, ms, "phoenix.example", "other-op")
	uri := "https://phoenix.example/users/a/statuses/3"

	v1, err := submitURI(ms, f, op, "phoenix.example", uri, "v1", nil)
	require.NoError(t, err)
	ref := &types.ContentRef{ContentId: v1.ContentId}

	cases := []struct {
		name, op, peer, uri, body string
		ref                       *types.ContentRef
	}{
		{"missing record", op, "phoenix.example", uri, "a", &types.ContentRef{ContentId: 9999}},
		{"another operator's record", other, "phoenix.example", uri, "b", ref},
		{"different content_uri", op, "phoenix.example", "https://phoenix.example/users/a/statuses/4", "c", ref},
		{"empty content_uri", op, "phoenix.example", "", "d", ref},
		{"different peer", op, "aurora.example", "https://aurora.example/users/a/statuses/3", "e", ref},
	}
	for _, c := range cases {
		_, err := submitURI(ms, f, c.op, c.peer, c.uri, c.body, c.ref)
		require.ErrorIs(t, err, types.ErrInvalidSupersede, c.name)
	}

	_, err = submitURI(ms, f, op, "phoenix.example", uri, "v2", ref)
	require.NoError(t, err)
	_, err = submitURI(ms, f, op, "phoenix.example", uri, "v2 again", ref)
	require.ErrorIs(t, err, types.ErrInvalidSupersede, "a record is superseded once; chain from the newest")
}

// SUPERSEDED is terminal: moderation could otherwise vouch for bytes that
// no longer exist at the source.
func TestModerateSupersededIsTerminal(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.example")
	op := registerTestBridge(t, f, ms, "phoenix.example", "mod-op")
	uri := "https://phoenix.example/users/a/statuses/5"

	v1, err := submitURI(ms, f, op, "phoenix.example", uri, "v1", nil)
	require.NoError(t, err)
	_, err = submitURI(ms, f, op, "phoenix.example", uri, "v2", &types.ContentRef{ContentId: v1.ContentId})
	require.NoError(t, err)

	_, err = ms.ModerateContent(f.ctx, &types.MsgModerateContent{
		Authority: f.authority, ContentId: v1.ContentId,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, Reason: "test",
	})
	require.ErrorIs(t, err, types.ErrContentTerminal)
}

// The attribution is held to the same rule: a bridge cannot anchor this
// peer's post as someone else's on another instance.
func TestSubmitRejectsForeignCreatorHost(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.example")
	op := registerTestBridge(t, f, ms, "phoenix.example", "creator-op")
	submit := func(identity, body string) error {
		hash := sha256.Sum256([]byte(body))
		_, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
			Operator: op, PeerId: "phoenix.example", RemoteContentId: "1", ContentType: "blog_post",
			CreatorIdentity: identity, ContentUri: "https://phoenix.example/users/a/statuses/1", ContentHash: hash[:],
		})
		return err
	}

	require.ErrorIs(t, submit("@alice@aurora.example", "misattributed"), types.ErrCreatorHostMismatch)
	require.ErrorIs(t, submit("alice", "no host"), types.ErrCreatorHostMismatch)
	require.NoError(t, submit("@alice@phoenix.example", "own"))
	require.NoError(t, submit("", "unattributed"), "an empty creator_identity stays accepted")
}

// Indexers learn about the retirement from content_superseded, with the
// successor's id and the predecessor's resulting status.
func TestSupersedeEmitsEvent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.example")
	op := registerTestBridge(t, f, ms, "phoenix.example", "event-op")
	uri := "https://phoenix.example/users/a/statuses/7"

	v1, err := submitURI(ms, f, op, "phoenix.example", uri, "v1", nil)
	require.NoError(t, err)
	v2, err := submitURI(ms, f, op, "phoenix.example", uri, "v2", &types.ContentRef{ContentId: v1.ContentId})
	require.NoError(t, err)

	var found bool
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		if ev.Type != types.EventTypeContentSuperseded {
			continue
		}
		attrs := map[string]string{}
		for _, a := range ev.Attributes {
			attrs[a.Key] = a.Value
		}
		require.Equal(t, fmt.Sprint(v1.ContentId), attrs[types.AttributeKeyContentID])
		require.Equal(t, fmt.Sprint(v2.ContentId), attrs[types.AttributeKeySupersededBy])
		require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_SUPERSEDED.String(), attrs[types.AttributeKeyNewStatus])
		found = true
	}
	require.True(t, found, "content_superseded was not emitted")
}
