package main

import (
	"context"
	"embed"
	"log"
	"os/signal"
	"strings"
	"syscall"

	"clovergo/internal/app"
)

// The web workspace is built from frontend/ with Vite into web/dist and
// committed, so the Go binary stays self-contained and needs no Node runtime.
//
//go:embed all:web/dist
var webAssets embed.FS

// releaseVersion also works in source archives without Git metadata.
//
//go:embed VERSION
var releaseVersion string

// version is set by scripts/build.ps1 through -ldflags "-X main.version=...".
var version string

func main() {
	application, err := app.New(webAssets, buildVersion())
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := application.Close(); err != nil {
			log.Printf("close application: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := application.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func buildVersion() string {
	if version != "" {
		return version
	}
	return strings.TrimSpace(releaseVersion)
}
