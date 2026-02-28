package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/vote/types"
)

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	Schema collections.Schema
	Params collections.Item[types.Params]

	authKeeper    types.AuthKeeper
	bankKeeper    types.BankKeeper
	repKeeper     types.RepKeeper
	seasonKeeper  types.SeasonKeeper
	stakingKeeper types.StakingKeeper

	VotingProposalSeq     collections.Sequence
	VotingProposal        collections.Map[uint64, types.VotingProposal]
	VoterRegistration     collections.Map[string, types.VoterRegistration]
	AnonymousVote         collections.Map[string, types.AnonymousVote]
	SealedVote            collections.Map[string, types.SealedVote]
	VoterTreeSnapshot     collections.Map[uint64, types.VoterTreeSnapshot]
	UsedNullifier         collections.Map[string, types.UsedNullifier]
	UsedProposalNullifier collections.Map[string, types.UsedProposalNullifier]
	TleValidatorShare     collections.Map[string, types.TleValidatorShare]
	TleDecryptionShare    collections.Map[string, types.TleDecryptionShare]
	EpochDecryptionKey    collections.Map[uint64, types.EpochDecryptionKey]
	SrsState              collections.Item[types.SrsState]
	TleEpochParticipation collections.Map[uint64, types.TleEpochParticipation]
	TleValidatorLiveness  collections.Map[string, types.TleValidatorLiveness]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	repKeeper types.RepKeeper,
	seasonKeeper types.SeasonKeeper,
	stakingKeeper types.StakingKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,

		authKeeper:        authKeeper,
		bankKeeper:        bankKeeper,
		repKeeper:         repKeeper,
		seasonKeeper:      seasonKeeper,
		stakingKeeper:     stakingKeeper,
		Params:            collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		VotingProposal:    collections.NewMap(sb, types.VotingProposalKey, "votingProposal", collections.Uint64Key, codec.CollValue[types.VotingProposal](cdc)),
		VotingProposalSeq: collections.NewSequence(sb, types.VotingProposalCountKey, "votingProposalSequence"),
		VoterRegistration: collections.NewMap(sb, types.VoterRegistrationKey, "voterRegistration", collections.StringKey, codec.CollValue[types.VoterRegistration](cdc)), AnonymousVote: collections.NewMap(sb, types.AnonymousVoteKey, "anonymousVote", collections.StringKey, codec.CollValue[types.AnonymousVote](cdc)), SealedVote: collections.NewMap(sb, types.SealedVoteKey, "sealedVote", collections.StringKey, codec.CollValue[types.SealedVote](cdc)), VoterTreeSnapshot: collections.NewMap(sb, types.VoterTreeSnapshotKey, "voterTreeSnapshot", collections.Uint64Key, codec.CollValue[types.VoterTreeSnapshot](cdc)), UsedNullifier: collections.NewMap(sb, types.UsedNullifierKey, "usedNullifier", collections.StringKey, codec.CollValue[types.UsedNullifier](cdc)), UsedProposalNullifier: collections.NewMap(sb, types.UsedProposalNullifierKey, "usedProposalNullifier", collections.StringKey, codec.CollValue[types.UsedProposalNullifier](cdc)), TleValidatorShare: collections.NewMap(sb, types.TleValidatorShareKey, "tleValidatorShare", collections.StringKey, codec.CollValue[types.TleValidatorShare](cdc)), TleDecryptionShare: collections.NewMap(sb, types.TleDecryptionShareKey, "tleDecryptionShare", collections.StringKey, codec.CollValue[types.TleDecryptionShare](cdc)), EpochDecryptionKey: collections.NewMap(sb, types.EpochDecryptionKeyKey, "epochDecryptionKey", collections.Uint64Key, codec.CollValue[types.EpochDecryptionKey](cdc)), SrsState: collections.NewItem(sb, types.SrsStateKey, "srsState", codec.CollValue[types.SrsState](cdc)), TleEpochParticipation: collections.NewMap(sb, types.TleEpochParticipationKey, "tleEpochParticipation", collections.Uint64Key, codec.CollValue[types.TleEpochParticipation](cdc)), TleValidatorLiveness: collections.NewMap(sb, types.TleValidatorLivenessKey, "tleValidatorLiveness", collections.StringKey, codec.CollValue[types.TleValidatorLiveness](cdc))}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// VerifyAnonymousActionProof verifies a ZK proof for anonymous actions (posting, replying, etc.).
// It delegates to the Groth16 verifier using the AnonActionVerifyingKey from module params.
// If no verifying key is configured, verification is skipped (development mode).
func (k Keeper) VerifyAnonymousActionProof(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error {
	if len(proof) == 0 {
		return fmt.Errorf("empty proof")
	}
	if len(nullifier) == 0 {
		return fmt.Errorf("empty nullifier")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get vote params: %w", err)
	}

	// Scope is 0 for anonymous actions (the scope is embedded in the nullifier by the prover)
	return verifyAnonActionProof(ctx, params.AnonActionVerifyingKey, merkleRoot, nullifier, proof, minTrustLevel, 0)
}

// GetActiveVoterZkPublicKeys returns the addresses and ZK public keys of all active voter registrations.
// Used by x/rep to build the member trust tree.
func (k Keeper) GetActiveVoterZkPublicKeys(ctx context.Context) ([]string, [][]byte, error) {
	var addresses []string
	var zkPubKeys [][]byte

	err := k.VoterRegistration.Walk(ctx, nil, func(addr string, reg types.VoterRegistration) (bool, error) {
		if reg.Active {
			addresses = append(addresses, addr)
			zkPubKeys = append(zkPubKeys, reg.ZkPublicKey)
		}
		return false, nil
	})
	if err != nil {
		return nil, nil, err
	}

	return addresses, zkPubKeys, nil
}

// GetVoterZkPublicKey returns the ZK public key for a single active voter registration.
// Used by x/rep for incremental trust tree updates.
func (k Keeper) GetVoterZkPublicKey(ctx context.Context, address string) ([]byte, error) {
	reg, err := k.VoterRegistration.Get(ctx, address)
	if err != nil {
		return nil, err
	}
	if !reg.Active {
		return nil, fmt.Errorf("voter registration not active: %s", address)
	}
	return reg.ZkPublicKey, nil
}

// VerifyMembershipProof verifies a ZK proof that the prover is a registered
// voter without revealing their identity. It builds a fresh Merkle tree from
// active voter registrations and delegates to the Groth16 verifier using the
// ProposalVerifyingKey from module params.
//
// This is intended for cross-module use (e.g., x/rep anonymous challenges).
func (k Keeper) VerifyMembershipProof(ctx context.Context, proof []byte, nullifier []byte) error {
	if len(proof) == 0 {
		return fmt.Errorf("empty proof")
	}
	if len(nullifier) == 0 {
		return fmt.Errorf("empty nullifier")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get vote params: %w", err)
	}

	merkleRoot, voterCount, err := k.buildTreeSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("failed to build voter tree: %w", err)
	}
	if voterCount == 0 {
		return fmt.Errorf("no registered voters")
	}

	return verifyProposalProof(ctx, params.ProposalVerifyingKey, merkleRoot, nullifier, proof)
}
