package types_test

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The member-key-migration alias contract (docs/x-rep-spec.md, "Member key
// migration"): an address stored on a record (author, staker, sentinel,
// owner, ...) stays the old address after its member migrates, so every
// ownership check must go through CanActAs(signer, stored) and every
// self-dealing ban through SameMember(a, b). A raw `stored == signer`
// silently locks a migrated member out of its own records or lets it
// self-deal across the migration; gating member treatment on IsMember (true
// for a ZEROED member) instead of IsActiveMember is the sibling mistake.
//
// TestAliasContractGuard walks every keeper and ante source file under x/
// and fails on either pattern outside the allowlist below. It is a syntactic
// heuristic in the spirit of the amino-name guards: a new hit is either a
// bug to convert to the alias helpers, or a comparison that is not about
// member identity, which goes in the allowlist with the reason.

// identityFields are struct fields that hold a member's address on a stored
// record.
var identityFields = map[string]bool{
	"Creator": true, "Owner": true, "Author": true, "Staker": true, "Sentinel": true,
	"Curator": true, "Verifier": true, "Challenger": true, "Assignee": true, "Inviter": true,
	"InvitedBy": true, "Funder": true, "Appellant": true, "Proposer": true, "Submitter": true,
	"SubmittedBy": true, "Reporter": true, "Member": true, "Holder": true, "Recipient": true,
	"Granter": true, "Grantee": true, "Contributor": true, "Claimant": true, "Sponsor": true,
	"Issuer": true, "Seller": true, "Buyer": true, "Renter": true, "Bidder": true,
	"Collaborator": true, "Endorser": true, "Voter": true, "Juror": true, "Reviewer": true,
	"Objector": true, "Operator": true, "Controller": true, "Address": true, "Defendant": true,
}

// signerIdents are local names that hold the acting (signing) address.
var signerIdents = map[string]bool{
	"signer": true, "creator": true, "sender": true, "caller": true, "authority": true,
	"actor": true, "voter": true, "staker": true, "author": true, "owner": true,
}

// aliasGuardAllowlist: "<path relative to x/>|<expression>" -> reason.
// An entry the code no longer matches fails the test, so fixing a site means
// deleting its entry.
var aliasGuardAllowlist = map[string]string{
	// Index consistency, not authorization: the author index key is checked
	// against the record it points at.
	"blog/keeper/promotion_queue.go|post.Creator != creator":  "index consistency check",
	"blog/keeper/promotion_queue.go|reply.Creator != creator": "index consistency check",
	"forum/keeper/promotion_queue.go|post.Author != author":   "index consistency check",
	// Policy, vault and operator addresses are not member identities.
	"commons/keeper/msg_server_cancel_recurring_spend.go|grant.Granter != msg.Authority":                    "granter is a council policy address",
	"service/keeper/msg_server_open_controller_transfer_case.go|msg.ProposedNewController == op.Controller": "controller is a group policy address",
	"vault/keeper/vault.go|signer != v.Address":                                                             "the vault's own address",
	"federation/keeper/msg_server_submit_federated_content.go|prev.SubmittedBy != msg.Operator":             "operator address is the x/service identity; operators retire and re-register instead of migrating",
	// Re-keyed by the owning module's migration hook, so the stored address
	// is always the current one.
	"session/keeper/msg_server_decline_grant.go|msg.Grantee != grant.Grantee":          "session re-keys grants (granter and grantee side) at migration",
	"session/keeper/msg_server_accept_grant.go|msg.Grantee != grant.Grantee":           "session re-keys grants (granter and grantee side) at migration",
	"session/keeper/msg_server_pull_allowance.go|grant.Grantee != msg.Grantee":         "session re-keys grants at migration",
	"session/keeper/msg_server_pull_allowance.go|msg.Recipient == grant.Granter":       "session re-keys grants at migration",
	"session/keeper/msg_server_retry_scheduled_oneshot.go|msg.Caller != grant.Grantee": "session re-keys grants at migration",
	"session/keeper/msg_server_retry_scheduled_oneshot.go|msg.Caller != grant.Granter": "session re-keys grants at migration",
	"session/keeper/msg_server_retry_scheduled_oneshot.go|msg.Caller == grant.Granter": "session re-keys grants at migration",
	"session/keeper/msg_server_revoke_grant.go|msg.Granter != grant.Granter":           "session re-keys grants at migration",
	// Existence checks, not gates.
	"season/keeper/content_validation.go|k.late.repKeeper.IsMember": "a jury content reference must name an existing member record",
	"season/keeper/keeper_helpers.go|k.late.repKeeper.IsMember":     "IsMember helper (record existence); gates use IsActiveMember",
}

func TestAliasContractGuard(t *testing.T) {
	root := filepath.Join("..", "..")
	var hits []string
	fset := token.NewFileSet()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "simulation" || info.Name() == "testutil" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || strings.HasSuffix(rel, ".pb.go") || strings.HasSuffix(rel, ".pb.gw.go") {
			return nil
		}
		if !strings.Contains(rel, "/keeper/") && !strings.Contains(rel, "/ante/") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.BinaryExpr:
				if e.Op != token.EQL && e.Op != token.NEQ {
					return true
				}
				if (isStoredIdentity(e.X) && isSigner(e.Y)) || (isStoredIdentity(e.Y) && isSigner(e.X)) {
					hits = append(hits, rel+"|"+exprString(fset, e))
				}
			case *ast.CallExpr:
				// A raw member-record lookup by a stored address: it misses the
				// record once that member migrates (use GetMember, which
				// resolves the alias, or Canonical first).
				if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Get" && len(e.Args) == 2 {
					if coll, ok := sel.X.(*ast.SelectorExpr); ok && coll.Sel.Name == "Member" && isStoredIdentity(e.Args[1]) {
						hits = append(hits, rel+"|"+exprString(fset, e))
					}
				}
				// Gating member treatment on another module's IsMember.
				if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "IsMember" && !strings.HasPrefix(rel, "rep/") {
					if inner, ok := sel.X.(*ast.SelectorExpr); ok && strings.Contains(strings.ToLower(inner.Sel.Name), "rep") {
						hits = append(hits, rel+"|"+exprString(fset, e.Fun))
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(hits)
	seen := map[string]bool{}
	var unexpected []string
	for _, h := range hits {
		seen[h] = true
		if _, ok := aliasGuardAllowlist[h]; !ok {
			unexpected = append(unexpected, h)
		}
	}
	for _, h := range unexpected {
		t.Errorf("raw member-address comparison or IsMember gate (use CanActAs / SameMember / IsActiveMember, or allowlist with a reason): %s", h)
	}
	for k := range aliasGuardAllowlist {
		if !seen[k] {
			t.Errorf("stale alias guard allowlist entry (the code no longer matches; remove it): %s", k)
		}
	}
}

// isStoredIdentity: <record>.<IdentityField>, where <record> is not msg (a
// msg field is the signer's own input, not stored state).
func isStoredIdentity(e ast.Expr) bool {
	e = unwrapString(e)
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || !identityFields[sel.Sel.Name] {
		return false
	}
	if id, ok := sel.X.(*ast.Ident); ok && (id.Name == "msg" || id.Name == "req") {
		return false
	}
	return true
}

// isSigner: msg.<Field>, a signer-named local, or either's .String().
func isSigner(e ast.Expr) bool {
	e = unwrapString(e)
	switch x := e.(type) {
	case *ast.Ident:
		return signerIdents[x.Name]
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && id.Name == "msg" {
			return true
		}
	}
	return false
}

func unwrapString(e ast.Expr) ast.Expr {
	if c, ok := e.(*ast.CallExpr); ok && len(c.Args) == 0 {
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "String" {
			return sel.X
		}
	}
	return e
}

func exprString(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	_ = printer.Fprint(&b, fset, e)
	return b.String()
}
