package main

import (
	_ "embed"
	"log"
	"os"
	"time"

	license "license_verifier/core/license"

	"github.com/gofiber/fiber/v3"
)

//go:embed public.pem
var publicKeyPEM string

var (
	deploymentID = "default_deployment"
	buildTimeRaw = "2026-06-01T00:00:00Z"
)

func main() {
	buildTime, err := time.Parse(time.RFC3339, buildTimeRaw)
	if err != nil {
		log.Fatal(err)
	}

	lic, err := license.New(license.LicenseConfig{
		PublicKeyPEM: publicKeyPEM,
		DeploymentId: deploymentID,
		BuildTime:    buildTime,
		TokenPath:    envOr("LICENSE_TOKEN_PATH", "/etc/license/license.token"),
		ClockFile:    envOr("LICENSE_CLOCK_FILE", "/var/lib/app/.license-hwm"),
	})
	if err != nil {
		log.Fatal(err)
	}

	lic.Start()
	defer lic.Stop()

	app := fiber.New()

	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
		})
	})

	app.Get("/license/status", lic.StatusHandler())

	app.Use(lic.FiberMiddleware())

	app.Get("/api/work", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"ok":   true,
			"data": "real service work",
		})
	})

	log.Fatal(app.Listen(":8080"))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}
