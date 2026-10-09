package keeper

import (
	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/types"
)

// applyContentMediaLabels recomputes a federated record's chain-owned media
// labels: the body is scanned for data URIs and a set content_uri marks an
// external reference. Call it on submission, on IBC receive and on genesis
// import. See docs/content-scanning.md §3.
func applyContentMediaLabels(content *types.FederatedContent) {
	l := commontypes.LabelFederatedContent(content.Body, content.ContentUri)
	content.MediaFlags, content.MediaRulesVersion = l.Flags, l.RulesVersion
}

// withholdContentBody blanks the body of a media-flagged record for list and
// show queries. Clients fetch it through FederatedContentBody once scanner
// verdicts clear it.
func withholdContentBody(content types.FederatedContent) types.FederatedContent {
	if content.MediaFlags != 0 {
		content.Body = ""
	}
	return content
}
