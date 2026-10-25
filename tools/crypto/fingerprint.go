package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ZkKeyFingerprint renders a short, human-comparable fingerprint of a shield
// public key: the first 8 bytes of SHA-256(zk_public_key) as hex in groups of
// four ("a1b2 c3d4 e5f6 0718"). A co-signer of a multisig member who does not
// hold the shield seed compares this against the fingerprint shown by a device
// that does, before signing a MsgRegisterZkPublicKey: the threshold controls
// whether a key is registered, not whose key it is (multisig-accounts plan
// §2.1). An empty key yields an empty fingerprint.
func ZkKeyFingerprint(zkPublicKey []byte) string {
	if len(zkPublicKey) == 0 {
		return ""
	}
	sum := sha256.Sum256(zkPublicKey)
	h := hex.EncodeToString(sum[:8])
	parts := make([]string, 0, 4)
	for i := 0; i < len(h); i += 4 {
		parts = append(parts, h[i:i+4])
	}
	return strings.Join(parts, " ")
}
