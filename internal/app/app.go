package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"

	"clovergo/internal/ark"
	"clovergo/internal/config"
	"clovergo/internal/httpapi"
	"clovergo/internal/logging"
	"clovergo/internal/repository"
	"clovergo/internal/service"
)

type App struct {
	server   *http.Server
	listener net.Listener
	store    *repository.SQLite
	service  *service.Service
}

func New(webFS fs.FS, version string) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	log := logging.New()
	// Bind the port before touching the database: a second copy started by
	// mistake must fail here instead of marking the running copy's in-flight
	// work as interrupted.
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("listen on %s (is CloverGo already running?): %w", cfg.Address, err)
	}
	store, err := repository.OpenSQLite(cfg.DatabasePath)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	recovered, err := store.RecoverInterruptedWork(context.Background())
	if err != nil {
		_ = listener.Close()
		_ = store.Close()
		return nil, fmt.Errorf("recover interrupted work: %w", err)
	}
	if recovered.Documents > 0 || recovered.Runs > 0 || recovered.Drafts > 0 {
		log.Warn("recovered work interrupted by a previous shutdown (documents back to ready or analysis_failed, runs interrupted, drafts failed)",
			"documents", recovered.Documents, "runs", recovered.Runs, "drafts", recovered.Drafts)
	}
	log.Info("starting CloverGo", "version", version, "base_dir", cfg.BaseDir, "database", cfg.DatabasePath,
		"address", cfg.Address, "embedding_model", cfg.ArkEmbeddingModel)
	arkClient := ark.New(cfg.ArkBaseURL, cfg.ArkAPIKey, cfg.ArkModel, cfg.ArkTimeout)
	embedder := ark.NewEmbedder(cfg.ArkBaseURL, cfg.ArkAPIKey, cfg.ArkEmbeddingModel, cfg.ArkEmbeddingDimensions, cfg.ArkTimeout)
	svc := service.New(
		store, arkClient, embedder, log, cfg.LibraryDir, cfg.MaxUploadBytes,
		cfg.MaxDocumentRunes, cfg.MinExtractConfidence,
	)
	svc.UseWechat(cfg.WechatAppID, cfg.WechatAppSecret, cfg.WechatAuthor, cfg.WechatThumbMediaID, cfg.WechatProxyURL)
	router, err := httpapi.NewRouter(svc, log, webFS, httpapi.Options{ListenAddress: cfg.Address, Version: version})
	if err != nil {
		_ = listener.Close()
		_ = store.Close()
		return nil, fmt.Errorf("initialize router: %w", err)
	}
	// Extraction and draft generation run in background workers. Nothing is
	// resumed at startup: extraction only starts when the owner asks for it.
	if err := svc.Start(context.Background()); err != nil {
		_ = listener.Close()
		_ = store.Close()
		return nil, fmt.Errorf("start extraction queue: %w", err)
	}
	return &App{
		server: &http.Server{
			Addr:              cfg.Address,
			Handler:           router,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       5 * time.Minute,
			MaxHeaderBytes:    64 << 10,
			IdleTimeout:       60 * time.Second,
		},
		listener: listener,
		store:    store,
		service:  svc,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- a.server.Serve(a.listener)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		return nil
	}
}

func (a *App) Close() error {
	a.service.Stop()
	return a.store.Close()
}
