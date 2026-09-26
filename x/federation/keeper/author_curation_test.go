package keeper_test

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// fakeCollect stands in for x/collect: collection id -> active link URIs.
// A collection missing from the map does not exist (or is not active).
type fakeCollect map[uint64][]string

func (c fakeCollect) ActiveLinkURIs(_ context.Context, id uint64) ([]string, bool, error) {
	uris, ok := c[id]
	return uris, ok, nil
}

// The two author gates on bridged content: allowed_identities ("*" = any
// author, empty = nobody) and, when set, a curated x/collect collection.
// Both must pass; blocked_identities still wins over either.
func TestAuthorCuration(t *testing.T) {
	const peer = "phoenix.example"
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, peer)
	op := registerTestBridge(t, f, ms, peer, "curation-op")
	f.keeper.SetCollectKeeper(fakeCollect{
		0: {"https://phoenix.example/@alice", "@Carol@phoenix.example"}, // id 0 is a real collection id
		7: {},
	})

	n := 0
	submit := func(identity string) error {
		n++
		hash := sha256.Sum256([]byte{byte(n), byte(n >> 8)})
		_, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
			Operator: op, PeerId: peer, RemoteContentId: "1", ContentType: "blog_post",
			CreatorIdentity: identity, ContentUri: "https://phoenix.example/users/a/statuses/1", ContentHash: hash[:],
		})
		return err
	}
	setPolicy := func(allowed []string, curation *types.IdentityCuration, blocked ...string) error {
		_, err := ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
			Authority: f.authority, PeerId: peer,
			Policy: types.PeerPolicy{
				InboundContentTypes: []string{"blog_post"},
				AllowedIdentities:   allowed,
				Curation:            curation,
				BlockedIdentities:   blocked,
			},
		})
		return err
	}

	// empty: a new external peer anchors nobody until the committee decides
	require.NoError(t, setPolicy(nil, nil))
	require.ErrorIs(t, submit("@alice@phoenix.example"), types.ErrIdentityNotAllowed)

	// a plain allowlist, however the handle is written
	require.NoError(t, setPolicy([]string{"@Alice@phoenix.example", "https://phoenix.example/users/bob"}, nil))
	require.NoError(t, submit("@alice@phoenix.example"))
	require.NoError(t, submit("@bob@phoenix.example"))
	require.ErrorIs(t, submit("@carol@phoenix.example"), types.ErrIdentityNotAllowed)
	require.ErrorIs(t, submit(""), types.ErrIdentityNotAllowed, "unattributed content needs an open policy")

	// "*" and no collection: open
	require.NoError(t, setPolicy([]string{types.AllIdentities}, nil))
	require.NoError(t, submit("@carol@phoenix.example"))
	require.NoError(t, submit(""))

	// "*" plus a collection: the collection alone decides
	require.NoError(t, setPolicy([]string{types.AllIdentities}, &types.IdentityCuration{CollectionId: 0}))
	require.NoError(t, submit("@alice@phoenix.example"), "listed by profile URL")
	require.NoError(t, submit("@carol@phoenix.example"), "listed by handle, any case")
	require.ErrorIs(t, submit("@bob@phoenix.example"), types.ErrIdentityNotCurated)
	require.ErrorIs(t, submit(""), types.ErrIdentityNotCurated)

	// both gates: listed AND curated
	require.NoError(t, setPolicy([]string{"@bob@phoenix.example", "@alice@phoenix.example"}, &types.IdentityCuration{CollectionId: 0}))
	require.NoError(t, submit("@alice@phoenix.example"))
	require.ErrorIs(t, submit("@bob@phoenix.example"), types.ErrIdentityNotCurated, "allowed but not curated")
	require.ErrorIs(t, submit("@carol@phoenix.example"), types.ErrIdentityNotAllowed, "curated but not allowed")

	// blocked still wins
	require.NoError(t, setPolicy([]string{types.AllIdentities}, nil, "@alice@phoenix.example"))
	require.ErrorIs(t, submit("@alice@phoenix.example"), types.ErrIdentityBlocked)

	// an empty collection admits nobody
	require.NoError(t, setPolicy([]string{types.AllIdentities}, &types.IdentityCuration{CollectionId: 7}))
	require.ErrorIs(t, submit("@alice@phoenix.example"), types.ErrIdentityNotCurated)
}

func TestAuthorCurationPolicyValidation(t *testing.T) {
	const peer = "phoenix.example"
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, peer)
	update := func(p types.PeerPolicy) error {
		p.InboundContentTypes = []string{"blog_post"}
		_, err := ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{Authority: f.authority, PeerId: peer, Policy: p})
		return err
	}

	// no x/collect wired: a curation collection cannot be named at all
	require.ErrorIs(t, update(types.PeerPolicy{Curation: &types.IdentityCuration{CollectionId: 1}}), types.ErrCurationUnavailable)

	f.keeper.SetCollectKeeper(fakeCollect{1: {"@alice@phoenix.example"}})
	require.NoError(t, update(types.PeerPolicy{Curation: &types.IdentityCuration{CollectionId: 1}}))
	require.ErrorIs(t, update(types.PeerPolicy{Curation: &types.IdentityCuration{CollectionId: 99}}), types.ErrCurationUnavailable, "a typo'd id is refused")

	require.ErrorIs(t, update(types.PeerPolicy{AllowedIdentities: []string{"alice"}}), types.ErrInvalidAllowedIdentity)
	require.ErrorIs(t, update(types.PeerPolicy{AllowedIdentities: []string{"https://phoenix.example/about"}}), types.ErrInvalidAllowedIdentity)
	many := make([]string, types.MaxAllowedIdentities+1)
	for i := range many {
		many[i] = "@u" + string(rune('a'+i%26)) + "@phoenix.example"
	}
	require.ErrorIs(t, update(types.PeerPolicy{AllowedIdentities: many}), types.ErrInvalidParamValue)
}
