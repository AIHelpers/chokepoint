// Command chokepoint runs the LLM call governance and observability
// gateway: a drop-in reverse proxy that sits between an application
// and its LLM providers, enforcing policy and recording an audit
// trail, alongside a dashboard for cost, usage, and latency.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chokepoint/chokepoint/internal/api"
	"github.com/chokepoint/chokepoint/internal/audit"
	"github.com/chokepoint/chokepoint/internal/config"
	"github.com/chokepoint/chokepoint/internal/cost"
	"github.com/chokepoint/chokepoint/internal/metrics"
	"github.com/chokepoint/chokepoint/internal/pii"
	"github.com/chokepoint/chokepoint/internal/policy"
	"github.com/chokepoint/chokepoint/internal/provider"
	"github.com/chokepoint/chokepoint/internal/proxy"
	"github.com/chokepoint/chokepoint/internal/store"
)

func main() {
	cfg := config.Load()

	st := store.NewMemoryStore()
	metricsRecorder := metrics.NewRecorder()
	policyEngine := policy.NewEngine(policy.DefaultRules())
	piiDetector := pii.New()
	costCalculator := cost.NewCalculator()

	gw := proxy.New(proxy.Gateway{
		Upstream: proxy.NewHTTPUpstream(&http.Client{Timeout: cfg.UpstreamTimeout}),
		PII:      piiDetector,
		Policy:   policyEngine,
		Cost:     costCalculator,
		Metrics:  metricsRecorder,
		Store:    st,
		BaseURLs: map[provider.Name]string{
			provider.OpenAI:    cfg.OpenAIBaseURL,
			provider.Anthropic: cfg.AnthropicBaseURL,
		},
		MaxExcerptChars: cfg.MaxExcerptChars,
	})

	mux := http.NewServeMux()
	mux.Handle("/proxy/", http.StripPrefix("", gw))

	dashboardAPI := api.New(st, metricsRecorder, policyEngine)
	dashboardAPI.Register(mux)
	dashboardAPI.RegisterDashboard(mux)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pruner := audit.NewPruner(st, cfg.RetentionDays)
	go pruner.Run(ctx)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("chokepoint: listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("chokepoint: server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("chokepoint: shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("chokepoint: graceful shutdown failed: %v", err)
	}
}
