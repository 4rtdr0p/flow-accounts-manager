package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	flowgorm "github.com/flow-hydraulics/flow-wallet-api/datastore/gorm"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	access "github.com/onflow/flow-go-sdk/access/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/flow-hydraulics/flow-wallet-api/accounts"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop"
	"github.com/flow-hydraulics/flow-wallet-api/auth/openapi"
	"github.com/flow-hydraulics/flow-wallet-api/chain_events"
	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/example"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/keys"
	"github.com/flow-hydraulics/flow-wallet-api/keys/basic"
	"github.com/flow-hydraulics/flow-wallet-api/ops"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/system"
	"github.com/flow-hydraulics/flow-wallet-api/templates"
	"github.com/flow-hydraulics/flow-wallet-api/tokens"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/gomodule/redigo/redis"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"go.uber.org/ratelimit"
)

type mongoPricingProbe interface {
	TestQuery(ctx context.Context) (int64, error)
}

const version = "0.9.0"

var (
	sha1ver   string // sha1 revision used to build the program
	buildTime string // when the executable was built
)

func main() {
	var (
		printVersion bool
		envFilePath  string // LEGACY: now used to check if user still is using envFilePath
	)

	// If we should just print the version number and exit
	flag.BoolVar(&printVersion, "version", false, "if true, print version and exit")
	flag.StringVar(&envFilePath, "envfile", "", "deprecated")
	flag.Parse()

	if envFilePath != "" {
		panic("'-envfile' is no longer supported, see readme")
	}

	if printVersion {
		fmt.Printf("v%s build on %s from sha1 %s\n", version, buildTime, sha1ver)
		os.Exit(0)
	}

	cfg, err := configs.Parse()
	if err != nil {
		panic(err)
	}

	runServer(cfg)

	os.Exit(0)
}

func runServer(cfg *configs.Config) {
	// Apply lightweight mode configuration if enabled
	if cfg.LightweightMode {
		log.Info("Running in lightweight mode with simplified dependencies")

		// Force SQLite as database
		cfg.DatabaseType = "sqlite"
		if cfg.DatabaseDSN == "" || cfg.DatabaseDSN == "wallet.db" {
			// Use a more explicit path for the SQLite database in lightweight mode
			cfg.DatabaseDSN = "./data/wallet-lightweight.db"
		}

		// Configure idempotency for lightweight mode
		if cfg.LightweightIdempotency {
			log.Info("Lightweight mode: Enabling idempotency with SQLite storage")
			cfg.DisableIdempotencyMiddleware = false
			cfg.IdempotencyMiddlewareDatabaseType = "shared" // Use same SQLite DB
		} else {
			log.Info("Lightweight mode: Disabling idempotency middleware")
			cfg.DisableIdempotencyMiddleware = true
		}

		// Optimize worker settings for lighter usage
		if cfg.WorkerCount > 4 {
			cfg.WorkerCount = 4
		}
		if cfg.WorkerQueueCapacity > 500 {
			cfg.WorkerQueueCapacity = 500
		}
	}

	configs.ConfigureLogger(cfg.LogLevel)

	log.Info("Starting server")

	// Flow client
	// TODO: WithInsecure()?
	fc, err := access.NewClient(
		cfg.AccessAPIHost,
		access.WithGRPCDialOptions(
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(cfg.GrpcMaxCallRecvMsgSize)),
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := fc.Close(); err != nil {
			log.Warn(err)
		}
		log.Info("Closed Flow Client")
	}()

	// Database. Plugin migrations are collected here (statically, ahead of
	// the plugin instances themselves) and appended to the core migration
	// list so they run in the same gormigrate table.
	db, err := flowgorm.New(cfg, artdrop.Migrations()...)
	if err != nil {
		log.Fatal(err)
	}
	defer flowgorm.Close(db)

	// Mongo (read-only pricing data from Payload). Optional: when MONGO_URI is
	// empty, mongoClient is nil and pricing features are disabled.
	mongoClient, err := datastoremongo.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer mongoClient.Close()
	if mongoClient != nil {
		// Passing a nil *PricingStore straight into the mongoPricingProbe
		// interface parameter would make probeMongoPricingStore's `store ==
		// nil` check pass a typed-nil interface value, which is not nil in
		// Go - the probe would then run against a nil receiver and fail
		// startup even when Mongo is deliberately left unconfigured.
		if err := probeMongoPricingStore(context.Background(), cfg.MongoConnectTimeout, datastoremongo.NewPricingStore(mongoClient, cfg)); err != nil {
			log.Fatal(err)
		}
	}

	systemService := system.NewService(
		system.NewGormStore(db),
		system.WithPauseDuration(cfg.PauseDuration),
	)

	// Create a worker pool
	wp := jobs.NewWorkerPool(
		jobs.NewGormStore(db),
		cfg.WorkerQueueCapacity,
		cfg.WorkerCount,
		jobs.WithJobStatusWebhook(cfg.JobStatusWebhookUrl, cfg.JobStatusWebhookTimeout),
		jobs.WithSystemService(systemService),
		jobs.WithMaxJobErrorCount(cfg.MaxJobErrorCount),
		jobs.WithDbJobPollInterval(cfg.DBJobPollInterval),
		jobs.WithAcceptedGracePeriod(cfg.AcceptedGracePeriod),
		jobs.WithReSchedulableGracePeriod(cfg.ReSchedulableGracePeriod),
	)

	defer func() {
		wp.Stop(true)
		log.Info("Stopped workerpool")
	}()

	txRatelimiter := ratelimit.New(cfg.TransactionMaxSendRate, ratelimit.WithoutSlack)

	// Key manager
	km := basic.NewKeyManager(cfg, keys.NewGormStore(db), fc)

	// Services
	templateService, err := templates.NewService(cfg, templates.NewGormStore(db))
	if err != nil {
		log.Fatal(err)
	}
	jobsService := jobs.NewService(jobs.NewGormStore(db))
	accountStore := accounts.NewGormStore(db)
	transactionService := transactions.NewService(cfg, transactions.NewGormStore(db), km, fc, wp,
		transactions.WithTxRatelimiter(txRatelimiter),
		transactions.WithCustodialSigningGuard(func(address string) error {
			return accounts.RequireCustodialForSigning(accountStore, address, cfg.ChainID)
		}),
	)
	accountService := accounts.NewService(cfg, accountStore, km, fc, wp, transactionService, templateService, accounts.WithTxRatelimiter(txRatelimiter))
	tokenService := tokens.NewService(cfg, tokens.NewGormStore(db), km, fc, wp, transactionService, templateService, accountService)
	opsService := ops.NewService(cfg, ops.NewGormStore(db), templateService, transactionService, tokenService)

	// Register a handler for account added events
	accounts.AccountAdded.Register(&tokens.AccountAddedHandler{
		TemplateService: templateService,
		TokenService:    tokenService,
	})

	err = accountService.InitAdminAccount(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	wp.Start()
	log.Info("Started workerpool")

	// HTTP handling
	systemHandler := handlers.NewSystem(systemService)
	templateHandler := handlers.NewTemplates(templateService)
	jobsHandler := handlers.NewJobs(jobsService)
	accountHandler := handlers.NewAccounts(accountService)
	transactionHandler := handlers.NewTransactions(transactionService, accountService)
	tokenHandler := handlers.NewTokens(tokenService)
	opsHandler := handlers.NewOps(opsService)

	routerOptions := routeOptions{
		DisableRawTransactions:   cfg.DisableRawTransactions,
		DisableFungibleTokens:    cfg.DisableFungibleTokens,
		DisableNonFungibleTokens: cfg.DisableNonFungibleTokens,
	}

	pluginDeps := plugins.PluginDeps{
		Accounts:     accountService,
		Tokens:       tokenService,
		Transactions: transactionService,
		Config:       cfg,
		WorkerPool:   wp,
		Mongo:        mongoClient,
		DB:           db,
	}

	artdropCfg, err := artdrop.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	registeredPlugins, err := registerPlugins(cfg, artdropCfg, pluginDeps)
	if err != nil {
		log.Fatal(err)
	}

	r := buildRouter(routerOptions, routeHandlers{
		System:         systemHandler,
		Templates:      templateHandler,
		Jobs:           jobsHandler,
		Accounts:       accountHandler,
		Transactions:   transactionHandler,
		Tokens:         tokenHandler,
		Ops:            opsHandler,
		DebugURL:       "https://github.com/flow-hydraulics/flow-wallet-api",
		DebugSHA:       sha1ver,
		DebugBuildTime: buildTime,
		WorkerPoolStatus: func() (interface{}, error) {
			return wp.Status()
		},
	}, registeredPlugins, pluginDeps)

	openAPISpecBytes, err := loadOpenAPISpec(cfg)
	if err != nil {
		log.Fatal(err)
	}
	scopeIndex, err := openapi.LoadScopeIndex(openAPISpecBytes)
	if err != nil {
		log.Fatal(err)
	}
	authRules, err := openapi.AuthRulesFromRouter(r, scopeIndex)
	if err != nil {
		log.Fatal(err)
	}

	h := http.TimeoutHandler(r, cfg.ServerRequestTimeout, "request timed out")
	h = handlers.UseCors(h)
	h = handlers.UseLogging(h)
	h = handlers.UseCompress(h)

	// Setup idempotency key middleware if it's enabled
	// redis for idempotency key handling
	if !cfg.DisableIdempotencyMiddleware {
		var is handlers.IdempotencyStore
		switch cfg.IdempotencyMiddlewareDatabaseType {
		// Shared SQL/Gorm store (same as for main app)
		case handlers.IdempotencyStoreTypeShared.String():
			is = handlers.NewIdempotencyStoreGorm(db)
		// Redis, separate from app db
		case handlers.IdempotencyStoreTypeRedis.String():
			if cfg.IdempotencyMiddlewareRedisURL == "" {
				log.Fatal("idempotency middleware db set to redis but Redis URL is empty")
			}
			pool := &redis.Pool{
				MaxIdle:   80,
				MaxActive: 12000,
				Dial: func() (redis.Conn, error) {
					c, err := redis.DialURL(cfg.IdempotencyMiddlewareRedisURL)
					if err != nil {
						panic(err.Error())
					}
					return c, err
				},
			}

			client := pool.Get()

			defer func() {
				log.Info("Closing Redis client..")
				if err := client.Close(); err != nil {
					log.Warn(err)
				}
			}()

			is = handlers.NewIdempotencyStoreRedis(client)
		case handlers.IdempotencyStoreTypeLocal.String():
			is = handlers.NewIdempotencyStoreLocal()
		}

		h = handlers.UseIdempotency(h, handlers.IdempotencyHandlerOptions{
			Expiry:      1 * time.Hour,
			IgnorePaths: []string{"/v1/scripts"}, // Scripts are read-only
		}, is)
	}

	h = handlers.UseAuth(h, handlers.AuthOptions{
		Enabled:  cfg.AuthEnabled,
		Secret:   cfg.AuthJWTSecret,
		Issuer:   cfg.AuthJWTIssuer,
		Audience: cfg.AuthJWTAudience,
		Rules:    authRules,
	})

	// Server boilerplate
	srv := &http.Server{
		Handler:      h,
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		WriteTimeout: 0, // Disabled, set cfg.ServerRequestTimeout instead
		ReadTimeout:  0, // Disabled, set cfg.ServerRequestTimeout instead
	}

	// Run our server in a goroutine so that it doesn't block.
	go func() {
		log.
			WithFields(log.Fields{
				"host": cfg.Host,
				"port": cfg.Port,
			}).
			Info("Server listening")
		if err := srv.ListenAndServe(); err != nil {
			log.Warn(err)
		}
	}()

	// Chain event listener
	if !cfg.DisableChainEvents {
		store := chain_events.NewGormStore(db)
		getTypes := func() ([]string, error) {
			// Get all enabled tokens
			tt, err := templateService.ListTokens(templates.NotSpecified)
			if err != nil {
				return nil, err
			}

			token_count := len(tt)
			event_types := make([]string, token_count)

			// Listen for enabled tokens deposit events
			for i, token := range tt {
				event_types[i] = templates.DepositEventTypeFromToken(token)
			}

			return event_types, nil
		}

		listener := chain_events.NewListener(
			fc, store, getTypes,
			cfg.ChainListenerMaxBlocks,
			cfg.ChainListenerInterval,
			cfg.ChainListenerStartingHeight,
			chain_events.WithSystemService(systemService),
		)

		defer func() {
			listener.Stop()
			log.Info("Stopped chain events listener")
		}()

		// Register a handler for chain events
		chain_events.ChainEvent.Register(&tokens.ChainEventHandler{
			AccountService:  accountService,
			ChainListener:   listener,
			TemplateService: templateService,
			TokenService:    tokenService,
		})

		listener.Start()

		log.Info("Started chain events listener")
	}

	// Trap interupt or sigterm and gracefully shutdown the server
	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	// SIGKILL, SIGQUIT or SIGTERM (Ctrl+/) will not be caught.
	signal.Notify(c, os.Interrupt)

	// Block until we receive our signal.
	sig := <-c

	log.Infof("Got signal: %s. Shutting down..", sig)

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Warnf("Error in server shutdown: %s", err)
	}
}

func probeMongoPricingStore(ctx context.Context, timeout time.Duration, store mongoPricingProbe) error {
	if store == nil {
		return nil
	}

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	count, err := store.TestQuery(ctx)
	if err != nil {
		return fmt.Errorf("mongo pricing probe failed: %w", err)
	}

	log.WithField("count", count).Info("Mongo pricing probe succeeded")
	return nil
}

type routeOptions struct {
	DisableRawTransactions   bool
	DisableFungibleTokens    bool
	DisableNonFungibleTokens bool
}

type routeHandlers struct {
	System           *handlers.System
	Templates        *handlers.Templates
	Jobs             *handlers.Jobs
	Accounts         *handlers.Accounts
	Transactions     *handlers.Transactions
	Tokens           *handlers.Tokens
	Ops              *handlers.Ops
	DebugURL         string
	DebugSHA         string
	DebugBuildTime   string
	WorkerPoolStatus func() (interface{}, error)
}

// registerPlugins returns the list of active plugins.
func registerPlugins(cfg *configs.Config, artdropCfg *artdrop.Config, deps plugins.PluginDeps) ([]plugins.Plugin, error) {
	artdropPlugin, err := artdrop.NewPlugin(deps, artdropCfg)
	if err != nil {
		return nil, fmt.Errorf("register artdrop plugin: %w", err)
	}

	return []plugins.Plugin{
		example.NewPlugin(deps),
		artdropPlugin,
	}, nil
}

func buildRouter(opts routeOptions, hs routeHandlers, registeredPlugins []plugins.Plugin, deps plugins.PluginDeps) *mux.Router {
	r := mux.NewRouter()
	rv := r.PathPrefix("/{apiVersion}").Subrouter()

	for _, p := range registeredPlugins {
		log.Infof("Registering plugin routes: %s", p.Name())
		p.RegisterRoutes(rv, deps)
	}

	rv.Handle("/debug", handlers.Debug(hs.DebugURL, hs.DebugSHA, hs.DebugBuildTime)).Methods(http.MethodGet)
	rv.HandleFunc("/health/ready", handlers.HandleHealthReady).Methods(http.MethodGet)
	rv.Handle("/health/liveness", handlers.Liveness(hs.WorkerPoolStatus)).Methods(http.MethodGet)

	rv.Handle("/system/settings", hs.System.GetSettings()).Methods(http.MethodGet)
	rv.Handle("/system/settings", hs.System.SetSettings()).Methods(http.MethodPost)
	rv.Handle("/system/sync-account-key-count", hs.Accounts.SyncAccountKeyCount()).Methods(http.MethodPost)

	rv.Handle("/jobs", hs.Jobs.List()).Methods(http.MethodGet)
	rv.Handle("/jobs/{jobId}", hs.Jobs.Details()).Methods(http.MethodGet)

	rv.Handle("/tokens", hs.Templates.ListTokens(templates.NotSpecified)).Methods(http.MethodGet)
	rv.Handle("/tokens", hs.Templates.AddToken()).Methods(http.MethodPost)
	rv.Handle("/tokens/{id_or_name}", hs.Templates.GetToken()).Methods(http.MethodGet)
	rv.Handle("/tokens/{id}", hs.Templates.RemoveToken()).Methods(http.MethodDelete)

	rv.Handle("/fungible-tokens", hs.Templates.ListTokens(templates.FT)).Methods(http.MethodGet)
	rv.Handle("/non-fungible-tokens", hs.Templates.ListTokens(templates.NFT)).Methods(http.MethodGet)

	rv.Handle("/transactions", hs.Transactions.List()).Methods(http.MethodGet)
	rv.Handle("/transactions/{transactionId}", hs.Transactions.Details()).Methods(http.MethodGet)

	rv.Handle("/accounts", hs.Accounts.List()).Methods(http.MethodGet)
	rv.Handle("/accounts", hs.Accounts.Create()).Methods(http.MethodPost)
	rv.Handle("/accounts/{address}", hs.Accounts.Details()).Methods(http.MethodGet)
	// Legacy wrapper that keeps the old /setup route working while the example
	// plugin owns the new /setup-example route.
	rv.Handle("/accounts/{address}/setup", example.NewSetupHandler(deps)).Methods(http.MethodPost)
	rv.Handle("/accounts/{address}/graduate-to-self-custody", hs.Accounts.GraduateToSelfCustody()).Methods(http.MethodPost)
	rv.Handle("/admin/reconcile/{address}", hs.Accounts.ReconcileAccount()).Methods(http.MethodGet)

	if !opts.DisableRawTransactions {
		rv.Handle("/accounts/{address}/sign", hs.Transactions.Sign()).Methods(http.MethodPost)
		rv.Handle("/accounts/{address}/transactions", hs.Transactions.List()).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/transactions", hs.Transactions.Create()).Methods(http.MethodPost)
		rv.Handle("/accounts/{address}/transactions/{transactionId}", hs.Transactions.Details()).Methods(http.MethodGet)
	} else {
		log.Info("raw transactions disabled")
	}

	rv.Handle("/watchlist/accounts", hs.Accounts.AddNonCustodialAccount()).Methods(http.MethodPost)
	rv.Handle("/watchlist/accounts/{address}", hs.Accounts.DeleteNonCustodialAccount()).Methods(http.MethodDelete)
	rv.Handle("/scripts", hs.Transactions.ExecuteScript()).Methods(http.MethodPost)

	if !opts.DisableFungibleTokens {
		rv.Handle("/accounts/{address}/fungible-tokens", hs.Tokens.AccountTokens(templates.FT)).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/fungible-tokens/{tokenName}", hs.Tokens.Details()).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/fungible-tokens/{tokenName}", hs.Tokens.Setup()).Methods(http.MethodPost)
		rv.Handle("/accounts/{address}/fungible-tokens/{tokenName}/withdrawals", hs.Tokens.ListWithdrawals()).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/fungible-tokens/{tokenName}/withdrawals", hs.Tokens.CreateWithdrawal()).Methods(http.MethodPost)
		rv.Handle("/accounts/{address}/fungible-tokens/{tokenName}/withdrawals/{transactionId}", hs.Tokens.GetWithdrawal()).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/fungible-tokens/{tokenName}/deposits", hs.Tokens.ListDeposits()).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/fungible-tokens/{tokenName}/deposits/{transactionId}", hs.Tokens.GetDeposit()).Methods(http.MethodGet)
	} else {
		log.Info("fungible tokens disabled")
	}

	if !opts.DisableNonFungibleTokens {
		rv.Handle("/accounts/{address}/non-fungible-tokens", hs.Tokens.AccountTokens(templates.NFT)).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/non-fungible-tokens/{tokenName}", hs.Tokens.Details()).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/non-fungible-tokens/{tokenName}", hs.Tokens.Setup()).Methods(http.MethodPost)
		rv.Handle("/accounts/{address}/non-fungible-tokens/{tokenName}/withdrawals", hs.Tokens.ListWithdrawals()).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/non-fungible-tokens/{tokenName}/withdrawals", hs.Tokens.CreateWithdrawal()).Methods(http.MethodPost)
		rv.Handle("/accounts/{address}/non-fungible-tokens/{tokenName}/withdrawals/{transactionId}", hs.Tokens.GetWithdrawal()).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/non-fungible-tokens/{tokenName}/deposits", hs.Tokens.ListDeposits()).Methods(http.MethodGet)
		rv.Handle("/accounts/{address}/non-fungible-tokens/{tokenName}/deposits/{transactionId}", hs.Tokens.GetDeposit()).Methods(http.MethodGet)
	} else {
		log.Info("non-fungible tokens disabled")
	}

	rv.Handle("/ops/missing-fungible-token-vaults/start", hs.Ops.InitMissingFungibleVaults()).Methods(http.MethodGet)
	rv.Handle("/ops/missing-fungible-token-vaults/stats", hs.Ops.GetMissingFungibleVaults()).Methods(http.MethodGet)

	return r
}

func loadOpenAPISpec(cfg *configs.Config) ([]byte, error) {
	if cfg.AuthOpenAPISpecPath != "" {
		return os.ReadFile(cfg.AuthOpenAPISpecPath)
	}
	if len(openAPISpec) == 0 {
		return nil, fmt.Errorf("embedded openapi spec is empty")
	}
	return openAPISpec, nil
}
