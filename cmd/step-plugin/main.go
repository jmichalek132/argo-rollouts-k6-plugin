package main

import (
	"log/slog"
	"os"
	"strings"

	goPlugin "github.com/hashicorp/go-plugin"
)

// handshakeConfig must match the argo-rollouts controller's step plugin client.
// Source: github.com/argoproj/argo-rollouts/rollout/steps/plugin/client/client.go
var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "step",
}

func main() {
	// Configure logging to stderr ONLY -- stdout reserved for go-plugin handshake (DIST-04).
	setupLogging()

	// Serve() prints handshake to stdout, then redirects os.Stdout to a pipe. // stdout-ok
	// NOTHING must write to stdout before this line. // stdout-ok
	// Phase 3 will add: "RpcStepPlugin": &stepRpc.RpcStepPlugin{Impl: impl}
	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         map[string]goPlugin.Plugin{},
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
