package abci

import (
	"sparkdream/x/shield/types"
)

// DKGInjectionPrefix is a magic prefix prepended to the InjectedDKGData bytes
// when injecting them as pseudo-tx[0] in PrepareProposal. This prefix is:
//  1. Checked by ProcessProposal to validate the injection
//  2. Checked by the PreBlocker to identify and extract DKG data
//  3. Ensures the bytes are NOT decodable as a valid Cosmos SDK transaction
//     (so deliverTx produces a harmless decode error for tx[0])
var DKGInjectionPrefix = []byte{0xFF, 0x00, 0xDD, 0x4B, 0x47} // "DKG" in hex: 0x44 0x4B 0x47

// EncodeDKGInjection marshals InjectedDKGData with the magic prefix.
func EncodeDKGInjection(data *types.InjectedDKGData) ([]byte, error) {
	bz, err := data.Marshal()
	if err != nil {
		return nil, err
	}
	return append(DKGInjectionPrefix, bz...), nil
}

// DecodeDKGInjection strips the magic prefix and unmarshals InjectedDKGData.
// Returns nil if the bytes don't start with the magic prefix.
func DecodeDKGInjection(bz []byte) (*types.InjectedDKGData, error) {
	if len(bz) < len(DKGInjectionPrefix) {
		return nil, nil
	}
	for i, b := range DKGInjectionPrefix {
		if bz[i] != b {
			return nil, nil
		}
	}

	var data types.InjectedDKGData
	if err := data.Unmarshal(bz[len(DKGInjectionPrefix):]); err != nil {
		return nil, err
	}
	return &data, nil
}

// HasDKGInjectionPrefix checks if bytes start with the magic DKG injection prefix.
func HasDKGInjectionPrefix(bz []byte) bool {
	if len(bz) < len(DKGInjectionPrefix) {
		return false
	}
	for i, b := range DKGInjectionPrefix {
		if bz[i] != b {
			return false
		}
	}
	return true
}
