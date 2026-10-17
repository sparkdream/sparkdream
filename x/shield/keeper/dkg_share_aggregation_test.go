package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/share"

	"sparkdream/tools/tle"
)

// TestDKGShareAggregationRoundTrip pins SHIELD-S2-2: a multi-dealer Feldman
// DKG ceremony followed by Lagrange reconstruction at x=0 must recover the
// master secret times the epoch tag. Prior to the fix each validator's
// "decryption share" was `a_{self,0} * epoch_tag` (registration key only),
// which Lagrange-interpolates to `(Σ_j L_j(0) * a_{j,0}) * tag` ≠
// `(Σ_i a_{i,0}) * tag`.
//
// With the fix each validator computes
//
//	share_j = s_j * tag where s_j = Σ_i p_i(j)
//
// and Lagrange at x=0 recovers (Σ_i p_i(0)) * tag = master * tag.
func TestDKGShareAggregationRoundTrip(t *testing.T) {
	const (
		round     uint64 = 1
		epoch     uint64 = 7
		threshold        = 2
		nVals            = 2
	)

	storeA := NewDKGLocalKeyStore(t.TempDir())
	storeB := NewDKGLocalKeyStore(t.TempDir())

	// Each validator registers (a_{i,0}) and generates a polynomial of degree
	// threshold-1.
	privA, pubA, err := storeA.EnsureRegistrationKey(round)
	require.NoError(t, err)
	privB, pubB, err := storeB.EnsureRegistrationKey(round)
	require.NoError(t, err)

	_, commA, err := storeA.GeneratePolynomial(round, threshold)
	require.NoError(t, err)
	_, commB, err := storeB.GeneratePolynomial(round, threshold)
	require.NoError(t, err)
	marshal := func(points []kyber.Point) [][]byte {
		out := make([][]byte, len(points))
		for i, p := range points {
			out[i], err = p.MarshalBinary()
			require.NoError(t, err)
		}
		return out
	}
	commABytes, commBBytes := marshal(commA), marshal(commB)

	// A computes its evaluation at B's index (2) and ECIES-encrypts to B's
	// registration pubkey. Symmetrically B encrypts to A.
	evalAforB, err := storeA.EvaluatePolynomial(round, 2)
	require.NoError(t, err)
	evalAforBBytes, err := evalAforB.MarshalBinary()
	require.NoError(t, err)
	ctAtoB, err := ecies.Encrypt(tleSuite, pubB, evalAforBBytes, nil)
	require.NoError(t, err)

	evalBforA, err := storeB.EvaluatePolynomial(round, 1)
	require.NoError(t, err)
	evalBforABytes, err := evalBforA.MarshalBinary()
	require.NoError(t, err)
	ctBtoA, err := ecies.Encrypt(tleSuite, pubA, evalBforABytes, nil)
	require.NoError(t, err)

	// Each validator now computes their decryption share. A aggregates
	// p_A(1) + decrypt(ctBtoA); B aggregates p_B(2) + decrypt(ctAtoB).
	// Each verifies the other's evaluation against its commitments first.
	fromB := IncomingEvaluation{Dealer: "B", DealerIndex: 2, Commitments: commBBytes, Ciphertext: ctBtoA}
	fromA := IncomingEvaluation{Dealer: "A", DealerIndex: 1, Commitments: commABytes, Ciphertext: ctAtoB}
	noComplaints, err := storeA.FindComplaints(round, 1, []IncomingEvaluation{fromB})
	require.NoError(t, err)
	require.Empty(t, noComplaints)
	shareABytes, err := storeA.ComputeDecryptionShare(round, epoch, 1, []IncomingEvaluation{fromB})
	require.NoError(t, err)
	shareBBytes, err := storeB.ComputeDecryptionShare(round, epoch, 2, []IncomingEvaluation{fromA})
	require.NoError(t, err)

	// A wrong or missing evaluation is detected locally: a complaint, and no
	// share that the chain would have to reject.
	wrongEval, err := storeB.EvaluatePolynomial(round, 2) // p_B(2), not p_B(1)
	require.NoError(t, err)
	wrongBytes, _ := wrongEval.MarshalBinary()
	ctWrong, err := ecies.Encrypt(tleSuite, pubA, wrongBytes, nil)
	require.NoError(t, err)
	bad := IncomingEvaluation{Dealer: "B", DealerIndex: 2, Commitments: commBBytes, Ciphertext: ctWrong}
	complaints, err := storeA.FindComplaints(round, 1, []IncomingEvaluation{bad})
	require.NoError(t, err)
	require.Equal(t, []uint32{2}, complaints)
	_, err = storeA.ComputeDecryptionShare(round, epoch, 1, []IncomingEvaluation{bad})
	require.ErrorContains(t, err, "dealer B")
	missing := IncomingEvaluation{Dealer: "B", DealerIndex: 2, Commitments: commBBytes}
	complaints, err = storeA.FindComplaints(round, 1, []IncomingEvaluation{missing})
	require.NoError(t, err)
	require.Equal(t, []uint32{2}, complaints)
	// B's justification (the evaluation in clear) repairs A's share.
	just, err := storeB.Justify(round, 1)
	require.NoError(t, err)
	require.NoError(t, VerifyEvaluation(commBBytes, 1, just))
	repaired := IncomingEvaluation{Dealer: "B", DealerIndex: 2, Commitments: commBBytes, Ciphertext: ctWrong, Plaintext: just}
	shareAAgain, err := storeA.ComputeDecryptionShare(round, epoch, 1, []IncomingEvaluation{repaired})
	require.NoError(t, err)
	require.Equal(t, shareABytes, shareAAgain)

	// Lagrange-interpolate at x=0 to recover master * epoch_tag.
	pointA := tleSuite.Point()
	require.NoError(t, pointA.UnmarshalBinary(shareABytes))
	pointB := tleSuite.Point()
	require.NoError(t, pointB.UnmarshalBinary(shareBBytes))

	pubShares := []*share.PubShare{
		{I: 0, V: pointA}, // 0-based index for kyber
		{I: 1, V: pointB},
	}
	recovered, err := share.RecoverCommit(tleSuite, pubShares, threshold, nVals)
	require.NoError(t, err)

	// Expected: master * epoch_tag where master = a_{A,0} + a_{B,0}.
	master := tleSuite.Scalar().Add(privA, privB)
	expected := tleSuite.Point().Mul(master, tle.EpochTag(epoch))

	require.True(t, recovered.Equal(expected),
		"Lagrange-recovered point must equal master*epoch_tag")
}
