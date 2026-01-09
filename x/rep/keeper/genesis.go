package keeper

import (
	"context"

	"sparkdream/x/rep/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	for _, elem := range genState.MemberMap {
		if err := k.Member.Set(ctx, elem.Address, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.InvitationList {
		if err := k.Invitation.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.InvitationSeq.Set(ctx, genState.InvitationCount); err != nil {
		return err
	}
	for _, elem := range genState.ProjectList {
		if err := k.Project.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.ProjectSeq.Set(ctx, genState.ProjectCount); err != nil {
		return err
	}
	for _, elem := range genState.InitiativeList {
		if err := k.Initiative.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.InitiativeSeq.Set(ctx, genState.InitiativeCount); err != nil {
		return err
	}
	for _, elem := range genState.StakeList {
		if err := k.Stake.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.StakeSeq.Set(ctx, genState.StakeCount); err != nil {
		return err
	}
	for _, elem := range genState.ChallengeList {
		if err := k.Challenge.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.ChallengeSeq.Set(ctx, genState.ChallengeCount); err != nil {
		return err
	}
	for _, elem := range genState.JuryReviewList {
		if err := k.JuryReview.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.JuryReviewSeq.Set(ctx, genState.JuryReviewCount); err != nil {
		return err
	}
	for _, elem := range genState.InterimList {
		if err := k.Interim.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.InterimSeq.Set(ctx, genState.InterimCount); err != nil {
		return err
	}
	for _, elem := range genState.InterimTemplateMap {
		if err := k.InterimTemplate.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	return k.Params.Set(ctx, genState.Params)
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.Member.Walk(ctx, nil, func(_ string, val types.Member) (stop bool, err error) {
		genesis.MemberMap = append(genesis.MemberMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	err = k.Invitation.Walk(ctx, nil, func(key uint64, elem types.Invitation) (bool, error) {
		genesis.InvitationList = append(genesis.InvitationList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.InvitationCount, err = k.InvitationSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Project.Walk(ctx, nil, func(key uint64, elem types.Project) (bool, error) {
		genesis.ProjectList = append(genesis.ProjectList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.ProjectCount, err = k.ProjectSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Initiative.Walk(ctx, nil, func(key uint64, elem types.Initiative) (bool, error) {
		genesis.InitiativeList = append(genesis.InitiativeList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.InitiativeCount, err = k.InitiativeSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Stake.Walk(ctx, nil, func(key uint64, elem types.Stake) (bool, error) {
		genesis.StakeList = append(genesis.StakeList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.StakeCount, err = k.StakeSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Challenge.Walk(ctx, nil, func(key uint64, elem types.Challenge) (bool, error) {
		genesis.ChallengeList = append(genesis.ChallengeList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.ChallengeCount, err = k.ChallengeSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.JuryReview.Walk(ctx, nil, func(key uint64, elem types.JuryReview) (bool, error) {
		genesis.JuryReviewList = append(genesis.JuryReviewList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.JuryReviewCount, err = k.JuryReviewSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Interim.Walk(ctx, nil, func(key uint64, elem types.Interim) (bool, error) {
		genesis.InterimList = append(genesis.InterimList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.InterimCount, err = k.InterimSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.InterimTemplate.Walk(ctx, nil, func(_ string, val types.InterimTemplate) (stop bool, err error) {
		genesis.InterimTemplateMap = append(genesis.InterimTemplateMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return genesis, nil
}
