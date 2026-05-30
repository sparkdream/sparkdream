//go:build mainnet

package types

// hiddenExpirationDefault on mainnet keeps HIDDEN posts in place for 7 days
// before ExpireHiddenPosts soft-deletes them — long enough that an appeal
// can realistically be filed and resolved before the slash hooks fire.
func hiddenExpirationDefault() int64 { return 604800 } // 7 days
