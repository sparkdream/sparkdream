package simulation

import (
	"fmt"

	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	zkcrypto "sparkdream/tools/crypto"
)

// zkKeyMessage is the fixed message a wallet signs to derive its shield
// secret key; version varies it so a member can rotate keys.
func zkKeyMessage(version int) []byte {
	return []byte(fmt.Sprintf("sparkdream/shield/key-derivation/v1#%d", version))
}

// zkPublicKey derives acc's ZK public key the way a wallet does: it signs a
// fixed message with the account key, turns the signature into a shield
// secret key and hashes that to the public key.
func zkPublicKey(acc simtypes.Account, version int) ([]byte, error) {
	sig, err := acc.PrivKey.Sign(zkKeyMessage(version))
	if err != nil {
		return nil, err
	}
	sk, err := zkcrypto.DeriveSecretKeyFromSignature(sig)
	if err != nil {
		return nil, err
	}
	return zkcrypto.DerivePublicKey(sk), nil
}
