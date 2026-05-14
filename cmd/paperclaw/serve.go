package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"paper-claw/internal/config"
	"paper-claw/internal/document"
	"paper-claw/internal/oauth"
	"paper-claw/internal/serve"
	"paper-claw/internal/serverhttp"
	"paper-claw/internal/storage/drive"
	"paper-claw/internal/store"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfgPath := fs.String("config", "paperclaw.yaml", "path to paperclaw YAML config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, secrets, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	// Set up structured JSON logging.
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))

	// Open the SQLite state store.
	if err := os.MkdirAll(cfg.State.Dir, 0o750); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}
	st, err := store.Open(cfg.State.Dir + "/paperclaw.db")
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	// --- Build in dependency order -------------------------------------------

	// (1) Circular dependency break: driveStorage and oauthCfg reference each
	//     other. Use vars to capture their addresses before assignment so the
	//     closures below are valid at runtime (they execute after both are set).
	var (
		driveStorage *drive.Storage
		worker       *serve.Worker
	)

	enqueue := func() { worker.Enqueue() }

	oauthCfg := oauth.New(
		cfg.OAuth.ClientID,
		secrets.OAuthClientSecret,
		cfg.HTTP.PublicBaseURL,
		cfg.OAuth.RedirectPath,
		st,
		func(ctx context.Context, hc *http.Client) error {
			return driveStorage.SetHTTPClient(ctx, hc)
		},
		enqueue,
	)

	tp := &oauthTokenProvider{oauthCfg: oauthCfg}
	driveStorage = drive.New(
		cfg.Drive.InboxFolderID,
		cfg.Drive.LibraryFolderID,
		cfg.Drive.ProcessedFolderID,
		cfg.Poll.StableThreshold,
		tp,
		st,
	)

	// Try to restore a previously stored Drive client (token from store).
	if hc, err := oauthCfg.HTTPClient(context.Background()); err == nil && hc != nil {
		if err := driveStorage.SetHTTPClient(context.Background(), hc); err != nil {
			slog.Warn("restoring Drive client from stored token", "err", err)
		}
	}

	// (2) Anthropic classifier + Prober.
	_ = secrets.AnthropicAPIKey // consumed by the SDK from ANTHROPIC_API_KEY env var
	classifier := document.NewClaudeClassifier()
	anthropicClient := anthropic.NewClient()
	prober := serverhttp.NewProber(anthropicClient, time.Hour)

	// (3) Scan worker — owns the coalescing request channel.
	worker = serve.New(driveStorage, classifier, st)

	// (5) Readiness provider — delegates to store (Drive) and prober (Anthropic).
	rp := &serveReadyzProvider{st: st, prober: prober}

	// (6) HTTP server.
	httpSrv := serverhttp.New(cfg.HTTP.BindAddr, oauthCfg, st, rp)

	// --- Goroutines ----------------------------------------------------------

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	var wg sync.WaitGroup

	// HTTP server.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error", "err", err)
		}
	}()

	// Anthropic prober.
	wg.Add(1)
	go func() {
		defer wg.Done()
		prober.Run(ctx)
	}()

	// Scan worker.
	wg.Add(1)
	go func() {
		defer wg.Done()
		worker.Run(ctx)
	}()

	// Poll ticker — fires once at startup, then every Poll.Interval.
	wg.Add(1)
	go func() {
		defer wg.Done()
		enqueue() // initial scan
		ticker := time.NewTicker(cfg.Poll.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				enqueue()
			}
		}
	}()

	slog.Info("paperclaw serve started",
		"bind", cfg.HTTP.BindAddr,
		"public_url", cfg.HTTP.PublicBaseURL,
	)

	// --- Graceful shutdown ---------------------------------------------------

	<-ctx.Done()
	slog.Info("shutting down")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutCancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		slog.Error("HTTP server shutdown", "err", err)
	}

	wg.Wait()
	return nil
}

// oauthTokenProvider implements drive.TokenProvider via the OAuth config.
type oauthTokenProvider struct {
	oauthCfg *oauth.Config
}

func (p *oauthTokenProvider) Get() (*http.Client, error) {
	hc, err := p.oauthCfg.HTTPClient(context.Background())
	if err != nil {
		return nil, err
	}
	if hc == nil {
		return nil, drive.ErrUnauthenticated
	}
	return hc, nil
}

// serveReadyzProvider implements serverhttp.ReadyzProvider for the daemon.
type serveReadyzProvider struct {
	st     *store.DB
	prober *serverhttp.Prober
}

func (r *serveReadyzProvider) DriveAuthenticated(ctx context.Context) bool {
	raw, err := r.st.GetToken(ctx)
	return err == nil && raw != nil
}

func (r *serveReadyzProvider) AnthropicHealthy() bool {
	return r.prober.AnthropicHealthy()
}
