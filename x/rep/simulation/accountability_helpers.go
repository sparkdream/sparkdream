package simulation

import (
	"fmt"
	"math/rand"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// getOrCreateMemberReport creates a member report if none exists.
func getOrCreateMemberReport(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, reportedMember string, reporter string) error {
	_, err := k.MemberReport.Get(ctx, reportedMember)
	if err == nil {
		return nil
	}

	now := ctx.BlockTime().Unix()
	report := types.MemberReport{
		Member:    reportedMember,
		Reason:    "Simulation report",
		Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		CreatedAt: now,
		Reporters: []string{reporter},
		TotalBond: fmt.Sprintf("%d", r.Intn(900)+100),
	}

	return k.MemberReport.Set(ctx, reportedMember, report)
}
