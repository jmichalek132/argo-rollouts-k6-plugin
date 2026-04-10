package main

import (
	"log/slog"
	"os"
	"strings"

	rolloutsPlugin "github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
	goPlugin "github.com/hashicorp/go-plugin"

	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/metric"
	"github.com/jmichalek132/argo-rollouts-k6-plugin/internal/provider/cloud"
)

// version is set at build time via LDFLAGS: -X main.version={{.Version}}
var version = "dev"

// handshakeConfig must match the argo-rollouts controller's metric plugin client.
// Source: github.com/argoproj/argo-rollouts/metricproviders/plugin/client/client.go
var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "metricprovider",
}

func main() {
	// Configure logging to stderr ONLY -- stdout reserved for go-plugin handshake (DIST-04).
	setupLogging()

	// Create provider and metric plugin implementation.
	var opts []cloud.Option
	if baseURL := os.Getenv("K6_BASE_URL"); baseURL != "" {
		opts = append(opts, cloud.WithBaseURL(baseURL))
	}
	p := cloud.NewGrafanaCloudProvider(opts...)
	impl := metric.New(p)

	// Serve() prints handshake to stdout, then redirects os.Stdout to a pipe. // stdout-ok
	// NOTHING must write to stdout before this line. // stdout-ok
	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins: map[string]goPlugin.Plugin{
			"RpcMetricProviderPlugin": &rolloutsPlugin.RpcMetricProviderPlugin{Impl: impl},
		},
	})
}

func setupLogging() {
	levelStr := os.Getenv("LOG_LEVEL")
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}
