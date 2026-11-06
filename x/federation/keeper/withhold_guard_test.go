package keeper_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/testutil/contentguard"
	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// TestEveryContentQueryWithholdsFlaggedBodies is the content-withholding guard
// (docs/content-scanning.md §3.4, review theme 5 of CSC-102): it walks the
// federation Query service, and every query whose response holds a
// FederatedContent must be exercised here against a media-flagged record and
// return it with its body blanked. A new query that returns content fails
// until a case is added. FederatedContentBody serves the body and returns no
// record.
func TestEveryContentQueryWithholdsFlaggedBodies(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "guard-peer")
	opStr := registerTestBridge(t, f, ms, "guard-peer", "guard-op")

	hash := sha256.Sum256([]byte("inline"))
	resp, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		License:  "CC0-1.0",
		Operator: opStr, PeerId: "guard-peer", RemoteContentId: "inline",
		ContentType: "blog_post", CreatorIdentity: "@aurora@guard-peer",
		Title: "t", Body: `<p><img src="data:image/png;base64,iVBORw0KGgo="></p>`, ContentHash: hash[:],
	})
	require.NoError(t, err)

	cases := map[string]func() (any, error){
		"GetFederatedContent": func() (any, error) {
			return qs.GetFederatedContent(f.ctx, &types.QueryGetFederatedContentRequest{Id: resp.ContentId})
		},
		"ListFederatedContent": func() (any, error) {
			return qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{PeerId: "guard-peer"})
		},
	}

	methods := contentguard.QueriesReturning(t, "sparkdream.federation.v1.Query",
		"sparkdream.federation.v1.FederatedContent")
	require.NotEmpty(t, methods)
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			call, ok := cases[m]
			require.True(t, ok, "query %s returns FederatedContent: add a withholding case for it", m)
			resp, err := call()
			require.NoError(t, err)
			n := contentguard.Withheld(t, resp,
				func(c types.FederatedContent) bool { return c.MediaFlags != 0 },
				func(c types.FederatedContent) string { return c.Body })
			require.Positive(t, n, "query %s did not return the flagged record", m)
		})
	}
}
