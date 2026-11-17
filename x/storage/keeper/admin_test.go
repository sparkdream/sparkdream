package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/storage/types"
)

func pairKey(a string, k types.CounterKind) collections.Pair[string, int32] {
	return collections.Join(a, int32(k))
}

func TestApproveOperatorRules(t *testing.T) {
	f := initFixture(t)
	c := f.council("council")
	other := f.council("other-council")
	op := f.member("operator", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	approve := func(authority, backend, operator string) error {
		_, err := f.srv.ApproveOperator(f.ctx, &types.MsgApproveOperator{Authority: authority, Backend: backend, Operator: operator})
		return err
	}
	// Not an x/service operator yet.
	require.ErrorIs(t, approve(c, types.ArweaveBackend, op), types.ErrOperatorNotActive)
	f.register(op, types.ArweaveServiceType, c)
	require.ErrorIs(t, approve(c, "nowhere", op), types.ErrBackendNotFound)
	// Signed by a group that is not the operator's controller, or by the operator.
	require.ErrorIs(t, approve(other, types.ArweaveBackend, op), types.ErrUnauthorized)
	require.ErrorIs(t, approve(op, types.ArweaveBackend, op), types.ErrUnauthorized)
	require.NoError(t, approve(c, types.ArweaveBackend, op))
	require.NoError(t, approve(c, types.ArweaveBackend, op)) // idempotent

	// Governance can approve in a controller's place; the approval records
	// the operator's controller.
	op2 := f.member("operator2", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	f.register(op2, types.ArweaveServiceType, other)
	require.NoError(t, approve(f.gov, types.ArweaveBackend, op2))
	a, err := f.keeper.ApprovedOperators.Get(f.ctx, collections.Join(types.ArweaveBackend, op2))
	require.NoError(t, err)
	require.Equal(t, other, a.Controller)

	f.setParams(func(p *types.Params) { p.MaxOperatorsPerBackend = 2 })
	op3 := f.member("operator3", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	f.register(op3, types.ArweaveServiceType, c)
	require.ErrorIs(t, approve(c, types.ArweaveBackend, op3), types.ErrTooManyOperators)

	remove := func(authority, operator string) error {
		_, err := f.srv.RemoveOperator(f.ctx, &types.MsgRemoveOperator{Authority: authority, Backend: types.ArweaveBackend, Operator: operator})
		return err
	}
	require.ErrorIs(t, remove(other, op), types.ErrUnauthorized)
	require.ErrorIs(t, remove(c, op3), types.ErrOperatorNotApproved)
	require.NoError(t, remove(c, op))
	require.NoError(t, remove(f.gov, op2))
}

// An operator whose approval was removed may still complete what it claimed.
func TestRemovedOperatorMayComplete(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	other := f.request(w.requester, "other")
	f.claim(w.operator, id)
	_, err := f.srv.RemoveOperator(f.ctx, &types.MsgRemoveOperator{Authority: w.council, Backend: types.ArweaveBackend, Operator: w.operator})
	require.NoError(t, err)
	require.ErrorIs(t, f.claimErr(w.operator, other), types.ErrOperatorNotApproved)
	_, err = f.srv.ExtendClaim(f.ctx, &types.MsgExtendClaim{Operator: w.operator, RequestId: id})
	require.ErrorIs(t, err, types.ErrOperatorNotApproved)
	f.complete(w.operator, id)
}

func TestApproveScannerRules(t *testing.T) {
	f := initFixture(t)
	c := f.council("council")
	other := f.council("other-council")
	s := addr("scanner")
	approve := func(authority, scanner string) error {
		_, err := f.srv.ApproveScanner(f.ctx, &types.MsgApproveScanner{Authority: authority, Scanner: scanner})
		return err
	}
	require.ErrorIs(t, approve(c, s), types.ErrOperatorNotActive)
	f.register(s, types.ContentScannerServiceType, c)
	require.ErrorIs(t, approve(other, s), types.ErrUnauthorized)
	require.NoError(t, approve(c, s))

	f.setParams(func(p *types.Params) { p.MaxApprovedScanners = 1 })
	s2 := addr("scanner2")
	f.register(s2, types.ContentScannerServiceType, c)
	require.ErrorIs(t, approve(c, s2), types.ErrTooManyScanners)

	// Removing a scanner leaves its flags in place.
	require.NoError(t, f.flagErr(s, "photo"))
	_, err := f.srv.RemoveScanner(f.ctx, &types.MsgRemoveScanner{Authority: other, Scanner: s})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = f.srv.RemoveScanner(f.ctx, &types.MsgRemoveScanner{Authority: c, Scanner: s})
	require.NoError(t, err)
	has, err := f.keeper.RemovedHashes.Has(f.ctx, hashOf("photo"))
	require.NoError(t, err)
	require.True(t, has)
	require.ErrorIs(t, f.flagErr(s, "other"), types.ErrScannerNotApproved)
	_, err = f.srv.RemoveScanner(f.ctx, &types.MsgRemoveScanner{Authority: c, Scanner: s})
	require.ErrorIs(t, err, types.ErrScannerNotApproved)
}

func TestUpdateBackend(t *testing.T) {
	f := initFixture(t)
	f.service.configs["storage-other"] = f.service.configs[types.ArweaveServiceType]
	f.service.unbonding["storage-other"] = 1 << 40
	update := func(authority string, mut func(b *types.Backend)) error {
		b := f.backend()
		mut(&b)
		_, err := f.srv.UpdateBackend(f.ctx, &types.MsgUpdateBackend{Authority: authority, Backend: b})
		return err
	}
	require.ErrorIs(t, update(addr("someone"), func(*types.Backend) {}), types.ErrInvalidSigner)
	require.ErrorIs(t, update(f.gov, func(b *types.Backend) { b.ServiceType = "storage-other" }), types.ErrBackendFieldImmutable)
	require.ErrorIs(t, update(f.gov, func(b *types.Backend) { b.MinFee = math.NewInt(1) }), types.ErrInvalidBackend) // below the floor
	require.ErrorIs(t, update(f.gov, func(b *types.Backend) { b.DeclinesToClose = 0 }), types.ErrInvalidBackend)
	require.ErrorIs(t, update(f.gov, func(b *types.Backend) { b.DeclinesToClose = 33 }), types.ErrInvalidBackend)
	require.ErrorIs(t, update(f.gov, func(b *types.Backend) { b.Types = append(b.Types, b.Types[0]) }), types.ErrInvalidBackend)
	require.ErrorIs(t, update(f.gov, func(b *types.Backend) { b.Types[0].MaxSize = 0 }), types.ErrInvalidBackend)

	require.NoError(t, update(f.gov, func(b *types.Backend) { b.Enabled = false; b.DeclinesToClose = 2 }))
	require.False(t, f.backend().Enabled)

	// A new name creates a backend.
	require.NoError(t, update(f.gov, func(b *types.Backend) { b.Name = "second"; b.ServiceType = "storage-other" }))
	_, err := f.keeper.Backends.Get(f.ctx, "second")
	require.NoError(t, err)
	// A service type that does not exist is refused.
	require.ErrorIs(t, update(f.gov, func(b *types.Backend) { b.Name = "third"; b.ServiceType = "missing" }), types.ErrInvalidBackend)
}

// Windows that an operator could outlast by unbonding are refused.
func TestWindowsMustFitTheUnbondingPeriod(t *testing.T) {
	f := initFixture(t)
	p := f.params()
	f.service.unbonding[types.ArweaveServiceType] = p.HoldSpan() // not strictly longer
	_, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.gov, Params: p})
	require.ErrorIs(t, err, types.ErrInvalidParams)
	b := f.backend()
	_, err = f.srv.UpdateBackend(f.ctx, &types.MsgUpdateBackend{Authority: f.gov, Backend: b})
	require.ErrorIs(t, err, types.ErrInvalidParams)

	f.service.unbonding[types.ArweaveServiceType] = p.HoldSpan() + 1
	_, err = f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.gov, Params: p})
	require.NoError(t, err)
}

func TestUpdateParamsAgainstState(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.scanner("scanner2", w.council)
	f.operator("second", w.council)
	try := func(mut func(p *types.Params)) error {
		p := f.params()
		mut(&p)
		_, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.gov, Params: p})
		return err
	}
	require.ErrorIs(t, try(func(p *types.Params) { p.MaxApprovedScanners = 1 }), types.ErrInvalidParams)
	require.ErrorIs(t, try(func(p *types.Params) { p.MaxOperatorsPerBackend = 1 }), types.ErrInvalidParams)
	f.setBackend(func(b *types.Backend) { b.DeclinesToClose = 4 })
	require.ErrorIs(t, try(func(p *types.Params) { p.MaxBarredPerRequest = 3 }), types.ErrInvalidParams)
	require.NoError(t, try(func(p *types.Params) { p.MaxBarredPerRequest = 4 }))
}

// Prices are set by the Operations Committee inside the governance bounds,
// for one backend, and nothing else.
func TestUpdateOperationalParams(t *testing.T) {
	f := initFixture(t)
	ops := addr("ops-committee")
	f.commons.ops[ops] = true
	b := f.backend()
	set := func(authority string, price, minFee math.Int) error {
		_, err := f.srv.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: authority, Backend: types.ArweaveBackend, PricePerByte: price, MinFee: minFee})
		return err
	}
	require.ErrorIs(t, set(addr("someone"), b.PricePerByte, b.MinFee), types.ErrInvalidSigner)
	// Below the floor: the committee cannot make the council pay for every copy.
	require.ErrorIs(t, set(ops, math.ZeroInt(), b.MinFee), types.ErrPriceOutOfBounds)
	require.ErrorIs(t, set(ops, b.PricePerByte, math.ZeroInt()), types.ErrPriceOutOfBounds)
	require.ErrorIs(t, set(ops, b.MaxPricePerByte.AddRaw(1), b.MinFee), types.ErrPriceOutOfBounds)

	require.NoError(t, set(ops, math.NewInt(20), b.MinFee.MulRaw(2)))
	got := f.backend()
	require.Equal(t, int64(20), got.PricePerByte.Int64())
	b.PricePerByte, b.MinFee = got.PricePerByte, got.MinFee
	require.True(t, b.Equal(got)) // nothing else changed
	require.NoError(t, set(f.gov, math.NewInt(10), b.MinFee))
}
