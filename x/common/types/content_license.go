package types

// Spark Dream is an open-content chain: everything published to it is
// unencumbered. Locally authored content is dedicated to the public domain
// under CC0 1.0 by the act of submitting it, and content bridged in from
// elsewhere is accepted only when its author has dedicated it to the public
// domain (CC0) or it is already in the public domain (Public Domain Mark).
//
// The license is a compiled constant, not a governance parameter: it is the
// chain's founding commitment, and content published under one license cannot
// be retroactively relicensed by a vote. See docs/content-license.md.

// License identifiers. SPDX ids where SPDX has one; PDM-1.0 is the
// Creative Commons Public Domain Mark, which SPDX does not list.
const (
	LicenseCC0 = "CC0-1.0"
	LicensePDM = "PDM-1.0"
)

// ChainContentLicense is the license every piece of content submitted to
// this chain is published under.
const ChainContentLicense = LicenseCC0

// ChainContentLicenseName and ChainContentLicenseURL describe
// ChainContentLicense for display.
const (
	ChainContentLicenseName = "CC0 1.0 Universal (Public Domain Dedication)"
	ChainContentLicenseURL  = "https://creativecommons.org/publicdomain/zero/1.0/"
)

// ContentDedication is the statement shown to participants wherever they
// publish. Submitting content to the chain is the act it describes.
const ContentDedication = "Everything published on Spark Dream is dedicated to the public domain " +
	"under CC0 1.0 Universal. By submitting content you waive all copyright and related rights " +
	"in it, worldwide, to the extent the law allows, so that anyone may copy, modify, distribute " +
	"and build on it, for any purpose, without asking permission. Submit only work you have the " +
	"right to dedicate this way."

// ContentLicenseNotice is the one-line notice on every CLI command that
// publishes content.
const ContentLicenseNotice = "Published to the public domain: everything on Spark Dream is dedicated under " +
	"CC0 1.0 Universal by submitting it, so submit only work you have the right to dedicate. " +
	"See `sparkdreamd query sparkdream content-license`."

// unencumberedLicenses are the licenses content may carry to enter the chain
// from elsewhere (federation). Both impose no conditions on reuse.
var unencumberedLicenses = []string{LicenseCC0, LicensePDM}

// UnencumberedLicenses returns the licenses federated content may carry.
func UnencumberedLicenses() []string {
	return append([]string(nil), unencumberedLicenses...)
}

// openCodeLicenses are the licenses code revealed through x/reveal may be
// released under (MsgPropose.final_license): public-domain dedications and
// permissive open-source licenses, by SPDX id. Copyleft licenses are left
// out on purpose: their share-alike conditions are an encumbrance on reuse,
// which the chain's open-content commitment rules out for prose and keeps
// as narrow as possible for code.
var openCodeLicenses = []string{
	LicenseCC0,
	"Unlicense",
	"0BSD",
	"MIT",
	"Apache-2.0",
	"BSD-2-Clause",
	"BSD-3-Clause",
	"ISC",
}

// OpenCodeLicenses returns the licenses revealed code may be released under.
func OpenCodeLicenses() []string {
	return append([]string(nil), openCodeLicenses...)
}

// IsOpenCodeLicense reports whether license is one revealed code may be
// released under. Exact match on the SPDX id.
func IsOpenCodeLicense(license string) bool {
	for _, l := range openCodeLicenses {
		if l == license {
			return true
		}
	}
	return false
}

// IsUnencumberedLicense reports whether license places content in the public
// domain. Exact match: identifiers are case-sensitive SPDX-style ids.
func IsUnencumberedLicense(license string) bool {
	for _, l := range unencumberedLicenses {
		if l == license {
			return true
		}
	}
	return false
}
