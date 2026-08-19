package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/handler"
	"github.com/seifghazi/claude-code-monitor/internal/middleware"
	"github.com/seifghazi/claude-code-monitor/internal/provider"
	"github.com/seifghazi/claude-code-monitor/internal/service"
	"github.com/seifghazi/claude-code-monitor/internal/webui"
)

type Options struct {
	Assets fs.FS
}

type App struct {
	logger    *log.Logger
	server    *http.Server
	storage   service.StorageService
	closeOnce sync.Once
	closeErr  error
}

func New(cfg *config.Config, logger *log.Logger, options Options) (*App, error) {
	assets := options.Assets
	if assets == nil {
		embedded, err := webui.Embedded()
		if err != nil {
			return nil, err
		}
		assets = embedded
	}
	dashboard, err := webui.NewHandler(assets)
	if err != nil {
		return nil, err
	}

	storage, migration, err := service.OpenSQLiteStorageService(&cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("initialize SQLite storage: %w", err)
	}
	if migration.BackupPath != "" {
		logger.Printf("SQLite schema upgraded from v%d to v%d; backup: %s", migration.PreviousVersion, migration.CurrentVersion, migration.BackupPath)
	}

	providers := map[string]provider.Provider{
		"anthropic": provider.NewAnthropicProvider(&cfg.Providers.Anthropic),
	}
	openAIProvider := provider.NewOpenAIProvider(&cfg.Providers.OpenAI)
	providers["openai"] = openAIProvider
	modelRouter := service.NewModelRouter(cfg, providers, logger)
	h := handler.New(
		storage,
		logger,
		modelRouter,
		openAIProvider,
		handler.Options{
			MaxCaptureBytes:            cfg.Storage.MaxCaptureBytes,
			ShowRawStreamEvents:        cfg.Web.ShowRawStreamEvents,
			RawRequestMaxDisplayChars:  cfg.Web.RawRequestMaxDisplayChars,
			RawResponseMaxDisplayChars: cfg.Web.RawResponseMaxDisplayChars,
		},
	)

	router := mux.NewRouter()
	router.Use(middleware.Logging)
	router.HandleFunc("/v1/chat/completions", h.ChatCompletions).Methods(http.MethodPost)
	router.HandleFunc("/v1/responses", h.Responses).Methods(http.MethodPost)
	router.HandleFunc("/v1/messages", h.Messages).Methods(http.MethodPost)
	router.HandleFunc("/v1/models", h.Models).Methods(http.MethodGet)
	router.HandleFunc("/health", h.Health).Methods(http.MethodGet)
	router.HandleFunc("/api/requests", h.GetRequests).Methods(http.MethodGet)
	router.HandleFunc("/api/requests", h.DeleteRequests).Methods(http.MethodDelete)
	router.HandleFunc("/api/sessions", h.GetSessions).Methods(http.MethodGet)
	router.HandleFunc("/api/ui-config", h.UIConfig).Methods(http.MethodGet)
	router.NotFoundHandler = dashboard
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
	})

	httpHandler := middleware.AccessControl(cfg.Server.AccessToken, cfg.Server.RequiresAccessToken())(router)
	return &App{
		logger:  logger,
		storage: storage,
		server: &http.Server{
			Addr:              net.JoinHostPort(cfg.Server.Host, cfg.Server.Port),
			Handler:           httpHandler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       cfg.Server.ReadTimeout,
			WriteTimeout:      cfg.Server.WriteTimeout,
			IdleTimeout:       cfg.Server.IdleTimeout,
		},
	}, nil
}

func (a *App) Address() string {
	return a.server.Addr
}

func (a *App) Handler() http.Handler {
	return a.server.Handler
}

func (a *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		_ = a.Close()
		return fmt.Errorf("listen on %s: %w", a.server.Addr, err)
	}

	a.logger.Printf("Agent Context Probe running on http://%s", a.server.Addr)
	errCh := make(chan error, 1)
	go func() {
		errCh <- a.server.Serve(listener)
	}()

	select {
	case serveErr := <-errCh:
		closeErr := a.Close()
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return closeErr
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		shutdownErr := a.server.Shutdown(shutdownContext)
		serveErr := <-errCh
		closeErr := a.Close()
		if shutdownErr != nil {
			return shutdownErr
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return closeErr
	}
}

func (a *App) Close() error {
	a.closeOnce.Do(func() {
		a.closeErr = a.storage.Close()
	})
	return a.closeErr
}
