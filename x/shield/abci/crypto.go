package abci

import (
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/sign/schnorr"
)

// schnorrVerify verifies a Schnorr signature using the BN256 G1 suite.
func schnorrVerify(pubKey kyber.Point, msg, sig []byte) error {
	return schnorr.Verify(tleSuite, pubKey, msg, sig)
}
