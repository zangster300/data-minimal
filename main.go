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

const port = 9001

func main() {
	ctx := context.Background()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	sctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	mux := http.NewServeMux()

	srv := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", port),
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
		datastar.GetSSE("/stream"))

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Write(page)
	})

	mux.HandleFunc("GET /stream", func(w http.ResponseWriter, r *http.Request) {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		sse := datastar.NewSSE(w, r)

		for {
			select {

			case <-r.Context().Done():
				logger.Debug("Client connection closed")
				return

			case <-ticker.C:
				bytes := make([]byte, 3)

				_, err := rand.Read(bytes)
				if err != nil {
					logger.Error("Error generating random bytes: ", slog.String("error", err.Error()))
					return
				}

				hexString := hex.EncodeToString(bytes)

				element := fmt.Sprintf(`<span id="feed" style="color:#%s;border:1px solid #%s;border-radius:0.25rem;padding:1rem;">%s</span>`, hexString, hexString, hexString)

				sse.PatchElements(element)
			}
		}
	})

	logger.Info(fmt.Sprintf("Server starting at 0.0.0.0:%d", port))
	defer logger.Info("Server closed")

	go func() {
		<-sctx.Done()
		srv.Shutdown(ctx)
	}()

	err := srv.ListenAndServe()
	if err != nil {
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}

	return nil
}
