package keeper

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/share"

	"sparkdream/x/vote/types"
)

// tleSuite is the BN256 G1 suite used for all TLE operations.
// BN256 is the same curve family as the gnark BN254 ZK circuits.
var tleSuite = bn256.NewSuiteG1()

// decryptTLEPayload decrypts a timelock-encrypted vote reveal using the
// epoch decryption key (the reconstructed master private scalar).
//
// The plaintext format is: voteOption (4 bytes, big-endian uint32) || salt (32 bytes).
// The nullifier is not stored in the encrypted payload (it's already in the
// SealedVote record), so nil is returned for it.
func decryptTLEPayload(encryptedReveal, decryptionKey []byte) (nullifier []byte, voteOption uint32, salt []byte, err error) {
	// Unmarshal the epoch private key scalar.
	scalar := tleSuite.Scalar()
	if err := scalar.UnmarshalBinary(decryptionKey); err != nil {
		return nil, 0, nil, fmt.Errorf("invalid decryption key: %w", err)
	}

	// Decrypt using ECIES (nil hash = SHA256 default).
	plaintext, err := ecies.Decrypt(tleSuite, scalar, encryptedReveal, nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("ECIES decryption failed: %w", err)
	}

	// Parse plaintext: 4 bytes voteOption + 32 bytes salt = 36 bytes.
	if len(plaintext) < 36 {
		return nil, 0, nil, fmt.Errorf("plaintext too short: expected 36 bytes, got %d", len(plaintext))
	}

	voteOption = binary.BigEndian.Uint32(plaintext[:4])
	salt = plaintext[4:36]

	return nil, voteOption, salt, nil
}

// verifyCorrectnessProof verifies that a validator's submitted decryption share
// (private key scalar) corresponds to their registered public key share.
//
// This is a simplified correctness check: compute scalar * G and compare
// against the registered public key point. The correctnessProof field is
// unused in this design (the scalar-to-point check is sufficient).
func verifyCorrectnessProof(_ context.Context, shareBytes, _ []byte, registeredPublicKeyShare []byte) error {
	// Unmarshal the private key share scalar.
	scalar := tleSuite.Scalar()
	if err := scalar.UnmarshalBinary(shareBytes); err != nil {
		return fmt.Errorf("invalid share scalar: %w", err)
	}

	// Compute scalar * G (nil base point = generator).
	computed := tleSuite.Point().Mul(scalar, nil)

	// Unmarshal the registered public key share point.
	registered := tleSuite.Point()
	if err := registered.UnmarshalBinary(registeredPublicKeyShare); err != nil {
		return fmt.Errorf("invalid registered public key: %w", err)
	}

	// Check equality.
	if !computed.Equal(registered) {
		return types.ErrInvalidCorrectnessProof
	}

	return nil
}

// tryReconstructEpochKey attempts to reconstruct the epoch decryption key
// via Lagrange interpolation when enough validator shares have been submitted.
// Called after each SubmitDecryptionShare.
func tryReconstructEpochKey(ctx context.Context, k Keeper, epoch uint64) error {
	// Already reconstructed?
	has, err := k.EpochDecryptionKey.Has(ctx, epoch)
	if err != nil {
		return err
	}
	if has {
		return nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	// Count total validators with registered shares.
	var totalValidators int
	err = k.TleValidatorShare.Walk(ctx, nil, func(_ string, _ types.TleValidatorShare) (bool, error) {
		totalValidators++
		return false, nil
	})
	if err != nil {
		return err
	}

	if totalValidators == 0 {
		return nil
	}

	// Compute threshold: ceil(totalValidators * numerator / denominator).
	threshold := int(math.Ceil(
		float64(totalValidators) * float64(params.TleThresholdNumerator) / float64(params.TleThresholdDenominator),
	))
	if threshold < 1 {
		threshold = 1
	}

	// Collect decryption shares for this epoch.
	var priShares []*share.PriShare

	err = k.TleDecryptionShare.Walk(ctx, nil, func(_ string, ds types.TleDecryptionShare) (bool, error) {
		if ds.Epoch != epoch {
			return false, nil
		}

		// Get the validator's registered share index.
		valShare, valErr := k.TleValidatorShare.Get(ctx, ds.Validator)
		if valErr != nil {
			return false, nil // skip unregistered
		}

		// Unmarshal the private key share scalar.
		scalar := tleSuite.Scalar()
		if unmarshalErr := scalar.UnmarshalBinary(ds.Share); unmarshalErr != nil {
			return false, nil // skip malformed
		}

		// Convert 1-based ShareIndex to 0-based kyber index.
		// kyber's PriShare.I=0 corresponds to evaluation at x=1.
		priShares = append(priShares, &share.PriShare{
			I: int(valShare.ShareIndex - 1),
			V: scalar,
		})

		return false, nil
	})
	if err != nil {
		return err
	}

	if len(priShares) < threshold {
		return nil // not enough shares yet
	}

	// Reconstruct the secret via Lagrange interpolation.
	recovered, err := share.RecoverSecret(tleSuite, priShares, threshold, totalValidators)
	if err != nil {
		return fmt.Errorf("secret recovery failed: %w", err)
	}

	// Marshal the recovered scalar.
	keyBytes, err := recovered.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshal recovered key: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Store the epoch decryption key.
	if err := k.EpochDecryptionKey.Set(ctx, epoch, types.EpochDecryptionKey{
		Epoch:         epoch,
		DecryptionKey: keyBytes,
		AvailableAt:   sdkCtx.BlockHeight(),
	}); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventEpochDecryptionKeyAvail,
		sdk.NewAttribute(types.AttributeEpoch, fmt.Sprintf("%d", epoch)),
	))

	return nil
}
