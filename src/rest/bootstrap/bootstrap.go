package bootstrap

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"chainora-api/adapter/repositories"
	"chainora-api/adapter/services"
	"chainora-api/core/dbpool"
	"chainora-api/core/properties"
	"chainora-api/core/usecases"
	restconfig "chainora-api/rest/config"
	"chainora-api/rest/controllers"
	"chainora-api/rest/middlewares"
	restprops "chainora-api/rest/properties"
	"chainora-api/rest/routers"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

// App contains initialized REST server dependencies.
type App struct {
	Engine *gin.Engine
	Config restprops.AppProperties
}

// Build wires repository, services, usecase and controller (DI root).
func Build() *App {
	cfg := restconfig.Load()

	var dbConn *sql.DB
	var authRepository usecases.AuthRepository = repositories.NewInMemoryAuthRepository()
	if dbURL := strings.TrimSpace(cfg.DBURL); dbURL != "" {
		db, err := sql.Open("postgres", dbURL)
		if err != nil {
			log.Printf("[bootstrap] failed to open postgres, fallback to in-memory auth repository: %v", err)
		} else if pingErr := db.Ping(); pingErr != nil {
			log.Printf("[bootstrap] failed to ping postgres, fallback to in-memory auth repository: %v", pingErr)
			_ = db.Close()
		} else {
			poolSettings := dbpool.ConfigureFromEnv(db, dbpool.Settings{
				MaxOpenConns:    24,
				MaxIdleConns:    8,
				ConnMaxLifetime: 30 * time.Minute,
				ConnMaxIdleTime: 5 * time.Minute,
			})
			log.Printf(
				"[bootstrap] postgres pool configured max_open=%d max_idle=%d max_lifetime=%s max_idle_time=%s",
				poolSettings.MaxOpenConns,
				poolSettings.MaxIdleConns,
				poolSettings.ConnMaxLifetime,
				poolSettings.ConnMaxIdleTime,
			)
			dbConn = db
			authRepository = repositories.NewPostgresAuthRepository(db)
			log.Printf("[bootstrap] using postgres auth repository")
		}
	}

	cryptoService := services.NewCryptoService()
	initiaUsernameService := services.NewInitiaUsernameService(cfg.InitiaAPIURL)
	jwtService := services.NewJWTService(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTTTL, cfg.JWTRefreshTTL)
	authUsecase := usecases.NewAuthUsecase(authRepository, cryptoService, properties.AuthProperties{
		AuthMessageTemplate: cfg.AuthMessageTemplate,
	})
	relayerService := services.NewRelayerService(
		cfg.RelayerMasterPrivateKey,
		cfg.RelayerMasterAddress,
		cfg.RelayerInitiadBinary,
		cfg.RelayerChainID,
		cfg.RelayerNodeURL,
		cfg.RelayerKeyName,
		cfg.RelayerKeyringBackend,
		cfg.RelayerHome,
		cfg.RelayerGasPrices,
		cfg.RelayerMoveModuleAddr,
		cfg.RelayerMoveModuleName,
		cfg.RelayerMoveFunctionName,
		cfg.RelayerMoveArgsJSON,
		cfg.RelayerMoveTypeArgsJSON,
		cfg.RelayerMovePrimaryFunctionName,
		cfg.RelayerMovePrimaryArgsJSON,
		cfg.RelayerMovePrimaryTypeArgsJSON,
		cfg.RelayerDryRun,
	)
	relayerUsecase := usecases.NewRelayerUsecase(
		authRepository,
		cryptoService,
		relayerService,
		cfg.RelayerMessageTemplate,
		cfg.RelayerPrimaryMessageTemplate,
	)
	txUsecase := usecases.NewTxInteractor()

	hub := usecases.NewWSHub()
	wsOriginChecker := middlewares.WSOriginChecker(cfg.AllowedOrigins, cfg.AllowEmptyOriginForWS)
	authController := controllers.NewAuthController(authUsecase, jwtService, dbConn, hub, initiaUsernameService, wsOriginChecker)
	relayerController := controllers.NewRelayerController(relayerUsecase, hub, wsOriginChecker)
	cardController, cardHandlerErr := controllers.NewCardController(
		authRepository,
		cfg.CardFactoryRootPublicKey,
		cfg.ChainoraRPCURL,
		cfg.CardDeviceVerifierPrivateKey,
	)
	if cardHandlerErr != nil {
		log.Fatalf("[bootstrap] invalid card factory root public key: %v", cardHandlerErr)
	}
	groupController := controllers.NewGroupControllerWithOptions(
		dbConn,
		jwtService,
		cfg.ChainoraRPCURL,
		usecases.GroupHandlerOptions{
			ReputationSyncConfig: buildReputationSyncConfig(cfg),
		},
	)
	mediaController := controllers.NewMediaController(
		jwtService,
		cfg.CloudinaryCloudName,
		cfg.CloudinaryAPIKey,
		cfg.CloudinaryAPISecret,
		cfg.CloudinaryUploadPreset,
	)
	notificationController := controllers.NewNotificationController(dbConn, jwtService)
	txController := controllers.NewTxController(txUsecase)

	r := gin.New()
	r.Use(middlewares.RecoveryWithStackTrace())
	r.Use(middlewares.RequestLogger())
	r.Use(middlewares.SecurityHeaders())
	r.Use(middlewares.RequestBodyLimit(cfg.MaxRequestBodyBytes))
	r.Use(cors.New(middlewares.CORS(cfg.AllowedOrigins)))

	v1 := r.Group("/v1")
	routers.RegisterRoutes(v1, authController, txController, relayerController, cardController, groupController, mediaController, notificationController)

	apiV1 := r.Group("/api/v1")
	routers.RegisterRoutes(apiV1, authController, txController, relayerController, cardController, groupController, mediaController, notificationController)

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	return &App{Engine: r, Config: cfg}
}

func (a *App) Address() string {
	return fmt.Sprintf(":%s", a.Config.ServerPort)
}

func buildReputationSyncConfig(cfg restprops.AppProperties) usecases.ReputationSyncConfig {
	return usecases.ReputationSyncConfig{
		RPCURL:             strings.TrimSpace(cfg.ChainoraRPCURL),
		VerifierPrivateKey: strings.TrimSpace(cfg.ReputationVerifierPrivateKey),
		TxSenderPrivateKey: strings.TrimSpace(cfg.ReputationTxSenderPrivateKey),
		DeadlineSeconds:    cfg.ReputationSyncDeadlineSeconds,
		RetryMax:           cfg.ReputationSyncRetryMax,
		CooldownSeconds:    cfg.ReputationSyncCooldownSeconds,
		BatchSize:          cfg.ReputationSyncBatchSize,
	}
}
