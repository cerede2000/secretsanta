// Commande secretsanta : serveur HTTP et sous-commande healthcheck.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"secretsanta/internal/app"
	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/health"
	"secretsanta/internal/i18n/catalog"
	"secretsanta/internal/i18n/cli"
	"secretsanta/internal/shared/clock"
	"secretsanta/web"
)

// version est injectée à la compilation (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		addr := os.Getenv(config.VarListenAddr)
		if addr == "" {
			addr = config.DefaultListenAddr
		}
		os.Exit(health.Run(addr))
	}
	if len(os.Args) > 1 && os.Args[1] == "i18n" {
		os.Exit(runI18n(os.Args[2:], os.Stdout, os.Stderr))
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runI18n exécute « secretsanta i18n export|check » sur le catalogue embarqué,
// sans exiger les secrets d'exécution : seules APP_DEFAULT_LANG et I18N_DIR
// (variables déclarées de internal/config) sont lues.
func runI18n(args []string, stdout, stderr io.Writer) int {
	lang := os.Getenv(config.VarDefaultLang)
	if lang == "" {
		lang = config.DefaultLang
	}
	cat, err := catalog.Load(web.I18n, catalog.Options{DefaultLang: lang, ExternalDir: os.Getenv(config.VarI18nDir)})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return cli.Run(args, stdout, stderr, cat)
}

func run() error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	database, err := db.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("ouverture de la base : %w", err)
	}
	defer database.Close()

	application, err := app.New(*cfg, database, clock.Real{}, version)
	if err != nil {
		return fmt.Errorf("assemblage : %w", err)
	}
	defer application.Close()

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           application.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	application.Start(ctx)

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("serveur démarré", "addr", cfg.ListenAddr, "version", version)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
