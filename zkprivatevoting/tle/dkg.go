// Package tle provides client-side tooling for Threshold Timelock Encryption (TLE).
//
// Validators use this package to:
//   - Run a dealer-based DKG ceremony to generate key shares
//   - Prepare TLE share registration data for MsgRegisterTLEShare
//   - Prepare epoch decryption shares for MsgSubmitDecryptionShare
//
// Voters use this package to:
//   - ECIES-encrypt sealed vote payloads using the master public key
//   - Compute MiMC vote commitments matching the on-chain verifier
//   - Generate random salts for vote sealing
package tle

import (
	"fmt"

	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/share"
)

// suite is the BN256 G1 suite used for all TLE operations.
// Must match the suite used on-chain in x/vote/keeper/tle.go.
var suite = bn256.NewSuiteG1()

// ValidatorShare holds a single validator's DKG output.
type ValidatorShare struct {
	Index          int    // 1-based share index (matches TleValidatorShare.ShareIndex)
	PrivateScalar  []byte // marshalled kyber scalar — KEEP SECRET
	PublicKeyShare []byte // marshalled kyber G1 point — submitted on-chain
}

// DKGOutput contains the complete output of a DKG ceremony.
type DKGOutput struct {
	MasterPublicKey []byte            // aggregated public key for voter ECIES encryption
	ValidatorShares []*ValidatorShare // per-validator shares (index 1..N)
	Threshold       int               // minimum shares for reconstruction
	TotalValidators int               // total number of validators
}

// RunDKG performs a simplified dealer-based DKG ceremony.
//
// A trusted dealer generates a master secret and splits it into shares using
// Shamir's secret sharing over a polynomial of degree threshold-1. Each
// validator receives a private scalar share and a corresponding public key
// share (scalar * G).
//
// In production, this should be replaced with a proper distributed protocol
// (e.g., Pedersen DKG or FROST) where no single party knows the master secret.
//
// Parameters:
//   - threshold: minimum shares needed to reconstruct the epoch key (e.g., 2)
//   - totalValidators: total number of validators participating (e.g., 3)
//
// The threshold ratio should match the on-chain params:
//
//	TleThresholdNumerator / TleThresholdDenominator ≈ threshold / totalValidators
func RunDKG(threshold, totalValidators int) (*DKGOutput, error) {
	if threshold < 1 {
		return nil, fmt.Errorf("threshold must be >= 1, got %d", threshold)
	}
	if totalValidators < threshold {
		return nil, fmt.Errorf("totalValidators (%d) must be >= threshold (%d)", totalValidators, threshold)
	}

	// Generate master secret and polynomial of degree threshold-1.
	secret := suite.Scalar().Pick(suite.RandomStream())
	poly := share.NewPriPoly(suite, threshold, secret, suite.RandomStream())

	// Get n shares evaluated at x=1..n (kyber convention: PriShare.I=0 → x=1).
	shares := poly.Shares(totalValidators)

	// Master public key = secret * G (base point).
	masterPub := suite.Point().Mul(secret, nil)
	masterPubBytes, err := masterPub.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal master public key: %w", err)
	}

	// Build per-validator shares.
	valShares := make([]*ValidatorShare, totalValidators)
	for i, s := range shares {
		privBytes, err := s.V.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal private scalar for validator %d: %w", i+1, err)
		}

		// Public key share = s_i * G.
		pubPoint := suite.Point().Mul(s.V, nil)
		pubBytes, err := pubPoint.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal public key share for validator %d: %w", i+1, err)
		}

		valShares[i] = &ValidatorShare{
			Index:          i + 1, // 1-based to match on-chain TleValidatorShare.ShareIndex
			PrivateScalar:  privBytes,
			PublicKeyShare: pubBytes,
		}
	}

	return &DKGOutput{
		MasterPublicKey: masterPubBytes,
		ValidatorShares: valShares,
		Threshold:       threshold,
		TotalValidators: totalValidators,
	}, nil
}

// AggregateMasterPublicKey computes the master public key from any threshold
// number of validator public key shares using Lagrange interpolation on points.
//
// This is useful when the master public key isn't stored on-chain (e.g., for
// verification) or when validators want to independently verify the DKG output.
//
// Parameters:
//   - pubShares: slice of (index, publicKeyShareBytes) pairs — at least threshold shares
//   - threshold: the DKG threshold
//   - totalValidators: total number of validators
//
// Each pubShare index must be 1-based (matching ValidatorShare.Index).
func AggregateMasterPublicKey(pubShares []*ValidatorShare, threshold, totalValidators int) ([]byte, error) {
	if len(pubShares) < threshold {
		return nil, fmt.Errorf("need at least %d shares, got %d", threshold, len(pubShares))
	}

	// Convert to kyber PubShare format.
	kyberShares := make([]*share.PubShare, len(pubShares))
	for i, vs := range pubShares {
		point := suite.Point()
		if err := point.UnmarshalBinary(vs.PublicKeyShare); err != nil {
			return nil, fmt.Errorf("invalid public key share for validator %d: %w", vs.Index, err)
		}
		kyberShares[i] = &share.PubShare{
			I: vs.Index - 1, // convert 1-based to 0-based for kyber
			V: point,
		}
	}

	// Lagrange interpolation on points to recover masterPub = secret * G.
	recovered, err := share.RecoverCommit(suite, kyberShares, threshold, totalValidators)
	if err != nil {
		return nil, fmt.Errorf("failed to recover master public key: %w", err)
	}

	return recovered.MarshalBinary()
}
