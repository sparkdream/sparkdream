package app

import (
	_ "sparkdream/x/blog/module"
	blogmoduletypes "sparkdream/x/blog/types"
	_ "sparkdream/x/collect/module"
	collectmoduletypes "sparkdream/x/collect/types"
	_ "sparkdream/x/commons/module"
	commonsmoduletypes "sparkdream/x/commons/types"
	_ "sparkdream/x/ecosystem/module"
	ecosystemmoduletypes "sparkdream/x/ecosystem/types"
	_ "sparkdream/x/forum/module"
	forummoduletypes "sparkdream/x/forum/types"
	_ "sparkdream/x/futarchy/module"
	futarchymoduletypes "sparkdream/x/futarchy/types"
	_ "sparkdream/x/name/module"
	namemoduletypes "sparkdream/x/name/types"
	_ "sparkdream/x/rep/module"
	repmoduletypes "sparkdream/x/rep/types"
	_ "sparkdream/x/reveal/module"
	revealmoduletypes "sparkdream/x/reveal/types"
	_ "sparkdream/x/season/module"
	seasonmoduletypes "sparkdream/x/season/types"
	_ "sparkdream/x/session/module"
	sessionmoduletypes "sparkdream/x/session/types"
	_ "sparkdream/x/shield/module"
	shieldmoduletypes "sparkdream/x/shield/types"
	_ "sparkdream/x/sparkdream/module"
	sparkdreammoduletypes "sparkdream/x/sparkdream/types"
	_ "sparkdream/x/split/module"
	splitmoduletypes "sparkdream/x/split/types"

	runtimev1alpha1 "cosmossdk.io/api/cosmos/app/runtime/v1alpha1"
	appv1alpha1 "cosmossdk.io/api/cosmos/app/v1alpha1"
	authmodulev1 "cosmossdk.io/api/cosmos/auth/module/v1"
	bankmodulev1 "cosmossdk.io/api/cosmos/bank/module/v1"
	consensusmodulev1 "cosmossdk.io/api/cosmos/consensus/module/v1"
	distrmodulev1 "cosmossdk.io/api/cosmos/distribution/module/v1"
	evidencemodulev1 "cosmossdk.io/api/cosmos/evidence/module/v1"
	genutilmodulev1 "cosmossdk.io/api/cosmos/genutil/module/v1"
	govmodulev1 "cosmossdk.io/api/cosmos/gov/module/v1"
	mintmodulev1 "cosmossdk.io/api/cosmos/mint/module/v1"
	paramsmodulev1 "cosmossdk.io/api/cosmos/params/module/v1"
	slashingmodulev1 "cosmossdk.io/api/cosmos/slashing/module/v1"
	stakingmodulev1 "cosmossdk.io/api/cosmos/staking/module/v1"
	txconfigv1 "cosmossdk.io/api/cosmos/tx/config/v1"
	upgrademodulev1 "cosmossdk.io/api/cosmos/upgrade/module/v1"
	vestingmodulev1 "cosmossdk.io/api/cosmos/vesting/module/v1"
	"cosmossdk.io/depinject/appconfig"
	_ "cosmossdk.io/x/evidence" // import for side-effects
	evidencetypes "cosmossdk.io/x/evidence/types"
	_ "cosmossdk.io/x/upgrade" // import for side-effects
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	_ "github.com/cosmos/cosmos-sdk/x/auth/tx/config" // import for side-effects
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	_ "github.com/cosmos/cosmos-sdk/x/auth/vesting" // import for side-effects
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	_ "github.com/cosmos/cosmos-sdk/x/bank" // import for side-effects
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	_ "github.com/cosmos/cosmos-sdk/x/consensus" // import for side-effects
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	_ "github.com/cosmos/cosmos-sdk/x/distribution" // import for side-effects
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	_ "github.com/cosmos/cosmos-sdk/x/gov" // import for side-effects
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	_ "github.com/cosmos/cosmos-sdk/x/mint" // import for side-effects
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	_ "github.com/cosmos/cosmos-sdk/x/params" // import for side-effects
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	_ "github.com/cosmos/cosmos-sdk/x/slashing" // import for side-effects
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	_ "github.com/cosmos/cosmos-sdk/x/staking" // import for side-effects
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	_ "github.com/sparkdream/gnovm/x/gnovm/module"
	gnovmmoduletypes "github.com/sparkdream/gnovm/x/gnovm/types"
)

var (
	moduleAccPerms = []*authmodulev1.ModuleAccountPermission{
		{Account: authtypes.FeeCollectorName},
		{Account: distrtypes.ModuleName},
		{Account: minttypes.ModuleName, Permissions: []string{authtypes.Minter}},
		{Account: stakingtypes.BondedPoolName, Permissions: []string{authtypes.Burner, stakingtypes.ModuleName}},
		{Account: stakingtypes.NotBondedPoolName, Permissions: []string{authtypes.Burner, stakingtypes.ModuleName}},
		{Account: govtypes.ModuleName, Permissions: []string{authtypes.Burner}},
		{Account: ibctransfertypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
		{Account: icatypes.ModuleName},
		{Account: splitmoduletypes.ModuleName},
		{Account: ecosystemmoduletypes.ModuleName},
		{Account: namemoduletypes.ModuleName},
		{Account: commonsmoduletypes.ModuleName, Permissions: []string{authtypes.Minter}},
		{Account: futarchymoduletypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
		{Account: repmoduletypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner, authtypes.Staking}},
		{Account: blogmoduletypes.ModuleName, Permissions: []string{authtypes.Burner}},
		{Account: shieldmoduletypes.ModuleName},
		{Account: forummoduletypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner, authtypes.Staking}},
		{Account: seasonmoduletypes.ModuleName},
		{Account: collectmoduletypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner, authtypes.Staking}},
		{Account: gnovmmoduletypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
		{Account: sessionmoduletypes.ModuleName},
		// this line is used by starport scaffolding # stargate/app/maccPerms
	}

	// blocked account addresses
	blockAccAddrs = []string{
		authtypes.FeeCollectorName,
		minttypes.ModuleName,
		stakingtypes.BondedPoolName,
		stakingtypes.NotBondedPoolName,
		// We allow the following module accounts to receive funds:
		// distrtypes.ModuleName,
		// govtypes.ModuleName
	}

	// application configuration (used by depinject)
	appConfig = appconfig.Compose(&appv1alpha1.Config{
		Modules: []*appv1alpha1.ModuleConfig{
			{
				Name: runtime.ModuleName,
				Config: appconfig.WrapAny(&runtimev1alpha1.Module{
					AppName: Name,
					// NOTE: upgrade module is required to be prioritized
					PreBlockers: []string{
						upgradetypes.ModuleName,
						authtypes.ModuleName,
						// this line is used by starport scaffolding # stargate/app/preBlockers
					},
					// During begin block slashing happens after distr.BeginBlocker so that
					// there is nothing left over in the validator fee pool, so as to keep the
					// CanWithdrawInvariant invariant.
					// NOTE: staking module is required if HistoricalEntries param > 0
					BeginBlockers: []string{
						minttypes.ModuleName,
						distrtypes.ModuleName,
						slashingtypes.ModuleName,
						evidencetypes.ModuleName,
						stakingtypes.ModuleName,
						// ibc modules
						ibcexported.ModuleName,
						// chain modules
						sparkdreammoduletypes.ModuleName,
						blogmoduletypes.ModuleName,
						shieldmoduletypes.ModuleName, // before split: skim gas reserve from community pool first
						splitmoduletypes.ModuleName,
						ecosystemmoduletypes.ModuleName,
						namemoduletypes.ModuleName,
						commonsmoduletypes.ModuleName,
						futarchymoduletypes.ModuleName,
						repmoduletypes.ModuleName,
						forummoduletypes.ModuleName,
						seasonmoduletypes.ModuleName,
						revealmoduletypes.ModuleName,
						collectmoduletypes.ModuleName,
						gnovmmoduletypes.ModuleName,
						sessionmoduletypes.ModuleName,
						// this line is used by starport scaffolding # stargate/app/beginBlockers
					},
					EndBlockers: []string{
						govtypes.ModuleName,
						stakingtypes.ModuleName,
						// chain modules
						sparkdreammoduletypes.ModuleName,
						blogmoduletypes.ModuleName,
						splitmoduletypes.ModuleName,
						ecosystemmoduletypes.ModuleName,
						namemoduletypes.ModuleName,
						commonsmoduletypes.ModuleName,
						futarchymoduletypes.ModuleName,
						repmoduletypes.ModuleName,
						forummoduletypes.ModuleName,
						seasonmoduletypes.ModuleName,
						revealmoduletypes.ModuleName,
						collectmoduletypes.ModuleName,
						shieldmoduletypes.ModuleName,
						gnovmmoduletypes.ModuleName,
						sessionmoduletypes.ModuleName,
						// this line is used by starport scaffolding # stargate/app/endBlockers
					},
					// The following is mostly only needed when ModuleName != StoreKey name.
					OverrideStoreKeys: []*runtimev1alpha1.StoreKeyConfig{
						{
							ModuleName: authtypes.ModuleName,
							KvStoreKey: "acc",
						},
					},
					// NOTE: The genutils module must occur after staking so that pools are
					// properly initialized with tokens from genesis accounts.
					// NOTE: The genutils module must also occur after auth so that it can access the params from auth.
					InitGenesis: []string{
						consensustypes.ModuleName,
						authtypes.ModuleName,
						banktypes.ModuleName,
						distrtypes.ModuleName,
						stakingtypes.ModuleName,
						slashingtypes.ModuleName,
						govtypes.ModuleName,
						minttypes.ModuleName,
						genutiltypes.ModuleName,
						evidencetypes.ModuleName,
						vestingtypes.ModuleName,
						upgradetypes.ModuleName,
						// ibc modules
						ibcexported.ModuleName,
						ibctransfertypes.ModuleName,
						icatypes.ModuleName,
						// chain modules
						sparkdreammoduletypes.ModuleName,
						blogmoduletypes.ModuleName,
						splitmoduletypes.ModuleName,
						ecosystemmoduletypes.ModuleName,
						namemoduletypes.ModuleName,
						commonsmoduletypes.ModuleName,
						futarchymoduletypes.ModuleName,
						repmoduletypes.ModuleName,
						forummoduletypes.ModuleName,
						seasonmoduletypes.ModuleName,
						revealmoduletypes.ModuleName,
						collectmoduletypes.ModuleName,
						shieldmoduletypes.ModuleName,
						gnovmmoduletypes.ModuleName,
						sessionmoduletypes.ModuleName,
						// this line is used by starport scaffolding # stargate/app/initGenesis
					},
				}),
			},
			{
				Name: authtypes.ModuleName,
				Config: appconfig.WrapAny(&authmodulev1.Module{
					Bech32Prefix:                AccountAddressPrefix,
					ModuleAccountPermissions:    moduleAccPerms,
					EnableUnorderedTransactions: true,
					// By default modules authority is the governance module. This is configurable with the following:
					// Authority: "group", // A custom module authority can be set using a module name
					// Authority: "cosmos1cwwv22j5ca08ggdv9c2uky355k908694z577tv", // or a specific address
				}),
			},
			{
				Name:   vestingtypes.ModuleName,
				Config: appconfig.WrapAny(&vestingmodulev1.Module{}),
			},
			{
				Name: banktypes.ModuleName,
				Config: appconfig.WrapAny(&bankmodulev1.Module{
					BlockedModuleAccountsOverride: blockAccAddrs,
				}),
			},
			{
				Name:   stakingtypes.ModuleName,
				Config: appconfig.WrapAny(&stakingmodulev1.Module{}),
			},
			{
				Name:   slashingtypes.ModuleName,
				Config: appconfig.WrapAny(&slashingmodulev1.Module{}),
			},
			{
				Name:   "tx",
				Config: appconfig.WrapAny(&txconfigv1.Config{}),
			},
			{
				Name:   genutiltypes.ModuleName,
				Config: appconfig.WrapAny(&genutilmodulev1.Module{}),
			},
			{
				Name:   upgradetypes.ModuleName,
				Config: appconfig.WrapAny(&upgrademodulev1.Module{}),
			},
			{
				Name:   distrtypes.ModuleName,
				Config: appconfig.WrapAny(&distrmodulev1.Module{}),
			},
			{
				Name:   evidencetypes.ModuleName,
				Config: appconfig.WrapAny(&evidencemodulev1.Module{}),
			},
			{
				Name: minttypes.ModuleName,
				Config: appconfig.WrapAny(&mintmodulev1.Module{
					// SECURITY: Inflation parameters are immutable.
					// Only chain upgrades can modify inflation_min, inflation_max, etc.
					// Setting authority to an impossible address prevents x/gov param updates.
					Authority: "sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe", // burn address - no private key exists
				}),
			},
			{
				Name:   govtypes.ModuleName,
				Config: appconfig.WrapAny(&govmodulev1.Module{}),
			},
			{
				Name:   consensustypes.ModuleName,
				Config: appconfig.WrapAny(&consensusmodulev1.Module{}),
			},
			{
				Name:   paramstypes.ModuleName,
				Config: appconfig.WrapAny(&paramsmodulev1.Module{}),
			},
			{
				Name:   sparkdreammoduletypes.ModuleName,
				Config: appconfig.WrapAny(&sparkdreammoduletypes.Module{}),
			},
			{
				Name:   blogmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&blogmoduletypes.Module{}),
			},
			{
				Name:   splitmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&splitmoduletypes.Module{}),
			},
			{
				Name:   ecosystemmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&ecosystemmoduletypes.Module{}),
			},
			{
				Name:   namemoduletypes.ModuleName,
				Config: appconfig.WrapAny(&namemoduletypes.Module{}),
			},
			{
				Name:   commonsmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&commonsmoduletypes.Module{}),
			},
			{
				Name:   futarchymoduletypes.ModuleName,
				Config: appconfig.WrapAny(&futarchymoduletypes.Module{}),
			},
			{
				Name:   repmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&repmoduletypes.Module{}),
			},
			{
				Name:   forummoduletypes.ModuleName,
				Config: appconfig.WrapAny(&forummoduletypes.Module{}),
			},
			{
				Name:   seasonmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&seasonmoduletypes.Module{}),
			},
			{
				Name:   revealmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&revealmoduletypes.Module{}),
			},
			{
				Name:   collectmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&collectmoduletypes.Module{}),
			},
			{
				Name:   shieldmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&shieldmoduletypes.Module{}),
			},
			{
				Name:   gnovmmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&gnovmmoduletypes.Module{}),
			},
			{
				Name:   sessionmoduletypes.ModuleName,
				Config: appconfig.WrapAny(&sessionmoduletypes.Module{}),
			},
			// this line is used by starport scaffolding # stargate/app/moduleConfig
		},
	})
)
