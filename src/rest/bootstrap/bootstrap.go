package bootstrap

import (
	"fmt"
	"net/http"

	"chainora-api/adapter/repositories"
	"chainora-api/adapter/services"
	"chainora-api/core/properties"
	"chainora-api/core/usecases"
	restconfig "chainora-api/rest/config"
	"chainora-api/rest/controllers"
	"chainora-api/rest/handler"
	"chainora-api/rest/middlewares"
	restprops "chainora-api/rest/properties"
	"chainora-api/rest/routers"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// App contains initialized REST server dependencies.
type App struct {
	Engine *gin.Engine
	Config restprops.AppProperties
}

// Build wires repository, services, usecase and controller (DI root).
func Build() *App {
	cfg := restconfig.Load()

	authRepository := repositories.NewInMemoryAuthRepository()
	cryptoService := services.NewCryptoService()
	jwtService := services.NewJWTService(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTTTL)
	authUsecase := usecases.NewAuthUsecase(authRepository, cryptoService, properties.AuthProperties{
		AuthMessageTemplate: cfg.AuthMessageTemplate,
	})
	txUsecase := usecases.NewTxInteractor()

	hub := controllers.NewWSHub()
	authHandler := handler.NewAuthHandler(authUsecase, jwtService, hub)
	txController := controllers.NewTxController(txUsecase)

	r := gin.New()
	r.Use(middlewares.RecoveryWithStackTrace())
	r.Use(middlewares.RequestLogger())
	r.Use(cors.New(middlewares.CORS()))

	v1 := r.Group("/v1")
	routers.RegisterAuthRoutes(v1, authHandler)
	routers.RegisterTxRoutes(v1, txController)

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	return &App{Engine: r, Config: cfg}
}

func (a *App) Address() string {
	return fmt.Sprintf(":%s", a.Config.ServerPort)
}
