package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

const (
	host = "0.0.0.0"
	port = 9001
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: func() slog.Level {
			switch os.Getenv("LOG_LEVEL") {
			case "DEBUG":
				return slog.LevelDebug
			case "INFO":
				return slog.LevelInfo
			case "WARN":
				return slog.LevelWarn
			case "ERROR":
				return slog.LevelError
			default:
				return slog.LevelInfo
			}
		}(),
	}))
	slog.SetDefault(logger)

	slog.Info("server started", "host", host, "port", port)
	defer slog.Info("server shutdown complete")

	if err := run(ctx); err != nil {
		slog.Error("error running server", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	mux := http.NewServeMux()

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: mux,
	}

	cdn := "https://cdn.jsdelivr.net/gh/starfederation/datastar@main/bundles/datastar.js"

	page := fmt.Appendf(nil, `
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=0" />
		<script type="module" defer src="%s"></script>
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
	<body>
		<span id="feed" data-on-load="%s"></span>
	</body>
	</html>
	`,
		cdn,
		datastar.GetSSE("/stream"),
	)

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(page); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("GET /stream", func(w http.ResponseWriter, r *http.Request) {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		sse := datastar.NewSSE(w, r)

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
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}
		}
	})

	shutdownErrChannel := make(chan error, 1)
	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			slog.Error("error encountered during server shutdown", "error", err)
		}

		shutdownErrChannel <- err
	}()

	if err := srv.ListenAndServe(); err != nil {
		if err == http.ErrServerClosed {
			return <-shutdownErrChannel
		}
		return err
	}

	return nil
}
