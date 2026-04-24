package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

const (
	host = "0.0.0.0"
	port = 8080
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: func() slog.Level {
			switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
			case "debug":
				return slog.LevelDebug
			case "info":
				return slog.LevelInfo
			case "warn":
				return slog.LevelWarn
			case "error":
				return slog.LevelError
			default:
				return slog.LevelInfo
			}
		}(),
	}))
	slog.SetDefault(logger)

	ctx := context.Background()
	if err := run(ctx); err != nil {
		slog.Error("server failure", "error", err)
		os.Exit(1)
	}

	slog.Info("server shutdown complete")
}

func run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	page := fmt.Appendf(nil, `
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=0" />
		<script type="module" src="https://cdn.jsdelivr.net/gh/starfederation/datastar@v1.0.1/bundles/datastar.js"></script>
		<style>
			*, *::before, *::after {
  				box-sizing: border-box;
			}

			* {
				margin: 0;
			}

			:root {
				--bg-color: oklch(92.2%% 0 0);
			}

			@media (prefers-color-scheme: dark) {
				:root {
					--bg-color: oklch(25.3267%% 0.015896 252.417568);
				}	
			}

			body {
				align-items: center;
				background-color: var(--bg-color);
				display: flex;
				flex-direction: column;
				font-family: ui-sans-serif, system-ui, sans-serif, 'Apple Color Emoji', 'Segoe UI Emoji', 'Segoe UI Symbol', 'Noto Color Emoji';
				height: 100vh;
				justify-content: center;
			}
		</style>
	</head>
	<body data-init="%s">
		<span id="feed"></span>
	</body>
	</html>
	`,
		datastar.GetSSE("/stream"),
	)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write(page); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("GET /stream", func(w http.ResponseWriter, r *http.Request) {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		sse := datastar.NewSSE(w, r, datastar.WithCompression(datastar.WithBrotli()))

		for {
			select {

			case <-r.Context().Done():
				slog.Debug("client connection closed")
				return

			case <-ticker.C:
				bytes := make([]byte, 3)

				n, err := rand.Read(bytes)
				if err != nil || n != len(bytes) {
					slog.Error("error generating random bytes", slog.Int("read", n), slog.String("error", err.Error()))
					return
				}

				hexString := hex.EncodeToString(bytes)

				element := fmt.Sprintf(`<span id="feed" style="color:#%s;border:1px solid #%s;border-radius:0.25rem;padding:1rem;">%s</span>`, hexString, hexString, hexString)

				if err := sse.PatchElements(element); err != nil {
					slog.Error("failed to patch elements", "error", err)
					return
				}
			}
		}
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	srvErrChan := make(chan error, 1)

	go func() {
		slog.Info("server started", "addr", srv.Addr)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErrChan <- err
			return
		}

		srvErrChan <- nil
	}()

	select {
	case err := <-srvErrChan:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		slog.Debug("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown failed: %w", err)
		}
		return <-srvErrChan
	}
}
