package main

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/malmonte/go-coder-service/internal/api"
	"github.com/malmonte/go-coder-service/internal/config"
	"github.com/malmonte/go-coder-service/internal/web"
)

func main() {
	settings := config.Load()
	app := api.NewApp(settings)

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Use(middleware.Logger())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     settings.AllowedCORSOrigins(),
		AllowCredentials: true,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"*"},
	}))
	// Same security headers as the FastAPI middleware.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := c.Response().Header()
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			return next(c)
		}
	})

	api.Register(e.Group("/api"), app)
	// Ops probes at the root as well, matching the Python service.
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "coder_url": settings.CoderAPIBase()})
	})
	e.GET("/ready", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	})

	if err := web.Register(e, app); err != nil {
		log.Fatalf("failed to load templates: %v", err)
	}

	log.Printf("go-coder-service listening on %s (coder: %s)", settings.ListenAddr, settings.CoderAPIBase())
	if err := e.Start(settings.ListenAddr); err != nil {
		log.Fatal(err)
	}
}
