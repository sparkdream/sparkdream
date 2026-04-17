package app

import (
	"io"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	upgradekeeper "cosmossdk.io/x/upgrade/keeper"

	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	cosmos_ante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	consensuskeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	icacontrollerkeeper "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/controller/keeper"
	icahostkeeper "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/host/keeper"
	ibctransferkeeper "github.com/cosmos/ibc-go/v10/modules/apps/transfer/keeper"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"
	gnovmante "github.com/sparkdream/gnovm/x/gnovm/ante"
	gnovmmodulekeeper "github.com/sparkdream/gnovm/x/gnovm/keeper"

	"sparkdream/docs"
	blogmodulekeeper "sparkdream/x/blog/keeper"
	collectmodulekeeper "sparkdream/x/collect/keeper"
	commonsante "sparkdream/x/commons/ante"
	commonsmodulekeeper "sparkdream/x/commons/keeper"
	ecosystemmodulekeeper "sparkdream/x/ecosystem/keeper"
	federationmodulekeeper "sparkdream/x/federation/keeper"
	forummodulekeeper "sparkdream/x/forum/keeper"
	futarchymodulekeeper "sparkdream/x/futarchy/keeper"
	futarchymoduletypes "sparkdream/x/futarchy/types"
	namemodulekeeper "sparkdream/x/name/keeper"
	repmodulekeeper "sparkdream/x/rep/keeper"
	revealmodulekeeper "sparkdream/x/reveal/keeper"
	seasonmodulekeeper "sparkdream/x/season/keeper"
	sessionante "sparkdream/x/session/ante"
	sessionmodulekeeper "sparkdream/x/session/keeper"
	shieldabci "sparkdream/x/shield/abci"
	shieldante "sparkdream/x/shield/ante"
	shieldmodulekeeper "sparkdream/x/shield/keeper"

	sparkdreammodulekeeper "sparkdream/x/sparkdream/keeper"
	splitmodulekeeper "sparkdream/x/split/keeper"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cast"
)

const (
	// Name is the name of the application.
	Name = "sparkdream"
	// AccountAddressPrefix is the prefix for accounts addresses.
	AccountAddressPrefix = "sprkdrm"
	// ChainCoinType is the coin type of the chain.
	ChainCoinType = 118
)

// DefaultNodeHome default home directories for the application daemon
var DefaultNodeHome string

var (
	_ runtime.AppI            = (*App)(nil)
	_ servertypes.Application = (*App)(nil)
)

// App extends an ABCI application, but with most of its parameters exported.
// They are exported for convenience in creating helper functions, as object
// capabilities aren't needed for testing.
type App struct {
	*runtime.App
	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	txConfig          client.TxConfig
	interfaceRegistry codectypes.InterfaceRegistry

	// keepers
	// only keepers required by the app are exposed
	// the list of all modules is available in the app_config
	AuthKeeper            authkeeper.AccountKeeper
	BankKeeper            bankkeeper.Keeper
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	MintKeeper            mintkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             *govkeeper.Keeper
	UpgradeKeeper         *upgradekeeper.Keeper
	ConsensusParamsKeeper consensuskeeper.Keeper
	ParamsKeeper          paramskeeper.Keeper

	// ibc keepers
	IBCKeeper           *ibckeeper.Keeper
	ICAControllerKeeper icacontrollerkeeper.Keeper
	ICAHostKeeper       icahostkeeper.Keeper
	TransferKeeper      ibctransferkeeper.Keeper

	SparkdreamKeeper sparkdreammodulekeeper.Keeper
	BlogKeeper       blogmodulekeeper.Keeper
	SplitKeeper      splitmodulekeeper.Keeper
	EcosystemKeeper  ecosystemmodulekeeper.Keeper
	NameKeeper       namemodulekeeper.Keeper
	CommonsKeeper    commonsmodulekeeper.Keeper
	FutarchyKeeper   futarchymodulekeeper.Keeper
	RepKeeper        repmodulekeeper.Keeper
	SeasonKeeper     seasonmodulekeeper.Keeper
	ShieldKeeper     shieldmodulekeeper.Keeper
	// this line is used by starport scaffolding # stargate/app/keeperDeclaration

	// simulation manager
	sm               *module.SimulationManager
	ForumKeeper      forummodulekeeper.Keeper
	RevealKeeper     revealmodulekeeper.Keeper
	CollectKeeper    collectmodulekeeper.Keeper
	GnoVMKeeper      gnovmmodulekeeper.Keeper
	SessionKeeper    sessionmodulekeeper.Keeper
	FederationKeeper federationmodulekeeper.Keeper
}

func init() {

	sdk.DefaultBondDenom = "uspark"

	var err error
	clienthelpers.EnvPrefix = Name
	DefaultNodeHome, err = clienthelpers.GetNodeHomeDirectory("." + Name)
	if err != nil {
		panic(err)
	}
}

// AppConfig returns the default app config.
func AppConfig() depinject.Config {
	return depinject.Configs(
		appConfig,
		depinject.Supply(
			// supply custom module basics
			map[string]module.AppModuleBasic{
				genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
			},
		),
	)
}

// New returns a reference to an initialized App.
func New(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *App {
	var (
		app        = &App{}
		appBuilder *runtime.AppBuilder

		// merge the AppConfig and other configuration in one config
		appConfig = depinject.Configs(
			AppConfig(),
			depinject.Supply(
				appOpts, // supply app options
				logger,  // supply logger
				// here alternative options can be supplied to the DI container.
				// those options can be used f.e to override the default behavior of some modules.
				// for instance supplying a custom address codec for not using bech32 addresses.
				// read the depinject documentation and depinject module wiring for more information
				// on available options and how to use them.
			),
		)
	)

	var appModules map[string]appmodule.AppModule
	if err := depinject.Inject(appConfig,
		&appBuilder,
		&appModules,
		&app.appCodec,
		&app.legacyAmino,
		&app.txConfig,
		&app.interfaceRegistry,
		&app.AuthKeeper,
		&app.BankKeeper,
		&app.StakingKeeper,
		&app.SlashingKeeper,
		&app.MintKeeper,
		&app.DistrKeeper,
		&app.GovKeeper,
		&app.UpgradeKeeper,
		&app.ConsensusParamsKeeper,
		&app.ParamsKeeper,
		&app.SparkdreamKeeper,
		&app.BlogKeeper,
		&app.SplitKeeper,
		&app.EcosystemKeeper,
		&app.NameKeeper,
		&app.CommonsKeeper,
		&app.FutarchyKeeper,
		&app.RepKeeper,
		&app.ForumKeeper,
		&app.SeasonKeeper,
		&app.RevealKeeper,
		&app.CollectKeeper,
		&app.ShieldKeeper, &app.GnoVMKeeper,
		&app.SessionKeeper,
		&app.FederationKeeper,
	); err != nil {
		panic(err)
	}

	// Wire GovKeeper into Commons via adapter (concrete keeper → interface adapter).
	app.CommonsKeeper.SetGovKeeper(NewGovKeeperAdapter(app.GovKeeper))

	// Wire CommonsKeeper and RepKeeper into Futarchy after depinject to break cyclic dependency.
	app.FutarchyKeeper.SetCommonsKeeper(app.CommonsKeeper)
	app.FutarchyKeeper.SetRepKeeper(app.RepKeeper)

	// Wire cross-module keepers into Season after depinject.
	// Season has no optional depinject inputs to avoid cyclic deps.
	app.SeasonKeeper.SetRepKeeper(app.RepKeeper)
	app.SeasonKeeper.SetNameKeeper(app.NameKeeper)
	app.SeasonKeeper.SetCommonsKeeper(app.CommonsKeeper)
	app.SeasonKeeper.SetBlogKeeper(app.BlogKeeper)
	app.SeasonKeeper.SetForumKeeper(app.ForumKeeper)
	app.SeasonKeeper.SetCollectKeeper(app.CollectKeeper)

	// Wire cross-module keepers into Blog after depinject.
	app.BlogKeeper.SetRepKeeper(app.RepKeeper)

	// Wire RepKeeper into Collect after depinject.
	app.CollectKeeper.SetRepKeeper(app.RepKeeper)

	// Wire SeasonKeeper into Rep after depinject.
	app.RepKeeper.SetSeasonKeeper(app.SeasonKeeper)

	// Wire ForumKeeper into Rep so tag-moderation can prune stale references.
	// Retired when forum's sentinel state moves into x/rep (future commit).
	app.RepKeeper.SetForumKeeper(app.ForumKeeper)

	// Wire DistrKeeper into Split after depinject (adapter adds GetCommunityPool).
	app.SplitKeeper.SetDistrKeeper(NewDistrKeeperAdapter(app.DistrKeeper))

	// Wire cross-module keepers into Shield after depinject.
	app.ShieldKeeper.SetRepKeeper(app.RepKeeper)
	app.ShieldKeeper.SetDistrKeeper(NewDistrKeeperAdapter(app.DistrKeeper))
	app.ShieldKeeper.SetSlashingKeeper(app.SlashingKeeper)
	app.ShieldKeeper.SetStakingKeeper(app.StakingKeeper)

	// Wire cross-module keepers into Federation after depinject (leaf module).
	app.FederationKeeper.SetCommonsKeeper(app.CommonsKeeper)
	app.FederationKeeper.SetRepKeeper(app.RepKeeper)
	app.FederationKeeper.SetNameKeeper(app.NameKeeper)

	// We explicitly tell Futarchy to call Commons when markets resolve.
	app.FutarchyKeeper.SetHooks(
		futarchymoduletypes.NewMultiFutarchyHooks(
			app.CommonsKeeper,
		),
	)

	// add to default baseapp options
	// enable optimistic execution
	baseAppOptions = append(baseAppOptions, baseapp.SetOptimisticExecution())

	// build app
	app.App = appBuilder.Build(db, traceStore, baseAppOptions...)

	// Wire MsgServiceRouter into Commons for proposal message execution.
	app.CommonsKeeper.SetRouter(app.MsgServiceRouter())

	// Wire MsgServiceRouter into Shield for inner message dispatch.
	app.ShieldKeeper.SetRouter(app.MsgServiceRouter())

	// Wire MsgServiceRouter into Session for ExecSession inner message dispatch.
	app.SessionKeeper.SetRouter(app.MsgServiceRouter())

	// Wire CommonsKeeper into Session so Commons Operations Committee can update
	// operational params alongside governance authority.
	app.SessionKeeper.SetCommonsKeeper(app.CommonsKeeper)

	// Register ShieldAware modules for the double-gate security model.
	// Each module that accepts shielded operations must explicitly opt in.
	app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.blog.v1.", app.BlogKeeper)
	app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.forum.v1.", app.ForumKeeper)
	app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.collect.v1.", app.CollectKeeper)
	app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.rep.v1.", app.RepKeeper)
	app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.commons.v1.", app.CommonsKeeper)
	app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.federation.v1.", app.FederationKeeper)

	anteOptions := cosmos_ante.HandlerOptions{
		SignModeHandler: app.txConfig.SignModeHandler(),
		SigGasConsumer:  cosmos_ante.DefaultSigVerificationGasConsumer,
	}

	// Manually define the standard decorators (since NewModuleAnteDecorators doesn't exist)
	decorators := []sdk.AnteDecorator{
		cosmos_ante.NewSetUpContextDecorator(), // outermost
		cosmos_ante.NewExtensionOptionsDecorator(anteOptions.ExtensionOptionChecker),
		cosmos_ante.NewValidateBasicDecorator(),
		cosmos_ante.NewTxTimeoutHeightDecorator(),
		cosmos_ante.NewValidateMemoDecorator(app.AuthKeeper),
		cosmos_ante.NewConsumeGasForTxSizeDecorator(app.AuthKeeper),
		// Shield gas decorator: deducts fees from shield module for MsgShieldedExec txs.
		shieldante.NewShieldGasDecorator(app.ShieldKeeper, app.BankKeeper),
		// Session fee decorator: deducts fees from granter for MsgExecSession txs.
		sessionante.NewSessionFeeDecorator(app.SessionKeeper, app.BankKeeper),
		// Wrap DeductFeeDecorator to skip fee deduction when shield/session already paid.
		shieldante.NewSkipIfFeePaidDecorator(
			cosmos_ante.NewDeductFeeDecorator(app.AuthKeeper, app.BankKeeper, nil, anteOptions.TxFeeChecker),
		),
		cosmos_ante.NewSetPubKeyDecorator(app.AuthKeeper), // Set pub key
		cosmos_ante.NewValidateSigCountDecorator(app.AuthKeeper),
		cosmos_ante.NewSigGasConsumeDecorator(app.AuthKeeper, anteOptions.SigGasConsumer),
		cosmos_ante.NewSigVerificationDecorator(app.AuthKeeper, anteOptions.SignModeHandler),
		cosmos_ante.NewIncrementSequenceDecorator(app.AuthKeeper),
	}

	// 3. Insert the proposal fee decorator at the end
	// This ensures the transaction is valid and signed before checking fees
	decorators = append(decorators, commonsante.NewProposalFeeDecorator(app.CommonsKeeper))
	decorators = append(decorators, gnovmante.NewAnteHandler())

	// 4. Chain them together and set
	app.SetAnteHandler(sdk.ChainAnteDecorators(decorators...))

	// -------------------------------------------------------------------------
	// Wire ABCI++ Vote Extension handlers for DKG automation.
	// Validators automatically participate in DKG ceremonies via vote extensions
	// without needing to submit manual transactions.
	// -------------------------------------------------------------------------

	homeDir := cast.ToString(appOpts.Get(flags.FlagHome))
	if homeDir == "" {
		homeDir = DefaultNodeHome
	}

	dkgHandler := shieldabci.NewDKGVoteExtensionHandler(app.ShieldKeeper, homeDir)
	app.SetExtendVoteHandler(dkgHandler.ExtendVoteHandler())
	app.SetVerifyVoteExtensionHandler(dkgHandler.VerifyVoteExtensionHandler())
	app.SetPrepareProposal(shieldabci.PrepareProposalHandler(app.ShieldKeeper))
	app.SetProcessProposal(shieldabci.ProcessProposalHandler(app.ShieldKeeper))

	// Custom PreBlocker: process DKG vote extension injections (tx[0] with magic prefix),
	// then run module-level PreBlockers. The injection is stripped from req.Txs so it
	// doesn't go through normal tx delivery.
	app.SetPreBlocker(func(ctx sdk.Context, req *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
		shieldabci.ProcessDKGInjection(ctx, app.ShieldKeeper, req)
		return app.ModuleManager.PreBlock(ctx)
	})

	// -------------------------------------------------------------------------

	// register legacy modules
	if err := app.registerIBCModules(appOpts); err != nil {
		panic(err)
	}

	/****  Module Options ****/

	// create the simulation manager and define the order of the modules for deterministic simulations
	overrideModules := map[string]module.AppModuleSimulation{
		authtypes.ModuleName: auth.NewAppModule(app.appCodec, app.AuthKeeper, authsims.RandomGenesisAccounts, nil),
		"gnovm":              gnovmSimOverride{}, // upstream SimulateMsgRun returns fatal error; disable until fixed
	}
	app.sm = module.NewSimulationManagerFromAppModules(app.ModuleManager.Modules, overrideModules)

	app.sm.RegisterStoreDecoders()

	// A custom InitChainer sets if extra pre-init-genesis logic is required.
	// This is necessary for manually registered modules that do not support app wiring.
	// Manually set the module version map as shown below.
	// The upgrade module will automatically handle de-duplication of the module version map.
	app.SetInitChainer(func(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
		if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
			return nil, err
		}
		return app.App.InitChainer(ctx, req)
	})

	if err := app.Load(loadLatest); err != nil {
		panic(err)
	}

	return app
}

// GetSubspace returns a param subspace for a given module name.
func (app *App) GetSubspace(moduleName string) paramstypes.Subspace {
	subspace, _ := app.ParamsKeeper.GetSubspace(moduleName)
	return subspace
}

// LegacyAmino returns App's amino codec.
func (app *App) LegacyAmino() *codec.LegacyAmino {
	return app.legacyAmino
}

// AppCodec returns App's app codec.
func (app *App) AppCodec() codec.Codec {
	return app.appCodec
}

// InterfaceRegistry returns App's InterfaceRegistry.
func (app *App) InterfaceRegistry() codectypes.InterfaceRegistry {
	return app.interfaceRegistry
}

// TxConfig returns App's TxConfig
func (app *App) TxConfig() client.TxConfig {
	return app.txConfig
}

// GetKey returns the KVStoreKey for the provided store key.
func (app *App) GetKey(storeKey string) *storetypes.KVStoreKey {
	kvStoreKey, ok := app.UnsafeFindStoreKey(storeKey).(*storetypes.KVStoreKey)
	if !ok {
		return nil
	}
	return kvStoreKey
}

// SimulationManager implements the SimulationApp interface
func (app *App) SimulationManager() *module.SimulationManager {
	return app.sm
}

// RegisterAPIRoutes registers all application module routes with the provided
// API server.
func (app *App) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	app.App.RegisterAPIRoutes(apiSvr, apiConfig)

	// Re-register all gateway routes using a codec-safe gRPC connection.
	// The SDK's registration uses clientCtx which triggers proto v2 panics
	// on gogoproto custom types. Our codecConn wrapper forces the SDK codec.
	installGatewayFix(apiSvr)

	// register swagger API in app.go so that other applications can override easily
	if err := server.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}

	// register app's OpenAPI routes.
	docs.RegisterOpenAPIService(Name, apiSvr.Router)
}

// GetMaccPerms returns a copy of the module account permissions
//
// NOTE: This is solely to be used for testing purposes.
func GetMaccPerms() map[string][]string {
	dup := make(map[string][]string)
	for _, perms := range moduleAccPerms {
		dup[perms.GetAccount()] = perms.GetPermissions()
	}

	return dup
}

// BlockedAddresses returns all the app's blocked account addresses.
func BlockedAddresses() map[string]bool {
	result := make(map[string]bool)

	if len(blockAccAddrs) > 0 {
		for _, addr := range blockAccAddrs {
			result[addr] = true
		}
	} else {
		for addr := range GetMaccPerms() {
			result[addr] = true
		}
	}

	return result
}
