package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* on http.DefaultServeMux
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/handlers"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/cron"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/config"
	database "github/lakshyakumar/production-ready-url-shortner-server/internal/platform/databse"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/leader"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/logger"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/observability"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/redisclient"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/service"
)

const shutdownTimeout = 10 * time.Second

// @title           URL Shortener API
// @version         1.0
// @description     Production-ready URL shortener service.
// @host            localhost:8080
// @BasePath        /
// @schemes         http https
func main() {
	// All real work happens in run(). main() exists only to translate run's
	// exit code to the process exit, which is the canonical way to keep
	// `defer`s honest in Go: `os.Exit` would skip them, so we never call
	// it from inside a function that owns deferred cleanup.
	os.Exit(run())
}

// run is the actual entrypoint. It owns every defer for the lifetime of the
// process and returns 0 on a clean shutdown, non-zero on any startup or
// runtime error. Errors are logged at the call site that detected them.
func run() int {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}

	logger.Init(cfg.LogLevel)

	// pprof block/mutex profiles are off by default — enable sampling so
	// /debug/pprof/block and /debug/pprof/mutex return data. Rate 1 samples
	// every event; lower under real load.
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)

	// Root context canceled on SIGINT/SIGTERM. Every long-lived component
	// (DB pool init, cron, in-flight requests) derives from this context, so
	// a single Ctrl+C unwinds the whole process cleanly.
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Tracing first, before any component that wants to emit spans. With no
	// endpoint configured this installs a no-op provider so otelpgx /
	// redisotel / otelgin all become free.
	tracerShutdown, err := observability.InitTracing(rootCtx, observability.TracingConfig{
		ExporterEndpoint: cfg.OtelExporterEndpoint,
		Insecure:         cfg.OtelExporterInsecure,
		ServiceName:      cfg.ServiceName,
		ServiceVersion:   cfg.ServiceVersion,
		SampleRatio:      cfg.OtelSampleRatio,
	})
	if err != nil {
		slog.Error("tracing init failed", slog.String("err", err.Error()))
		return 1
	}
	if cfg.OtelExporterEndpoint != "" {
		slog.Info("tracing enabled",
			slog.String("endpoint", cfg.OtelExporterEndpoint),
			slog.String("service", cfg.ServiceName))
	}

	// Metrics registry — owns Go runtime + process collectors plus the
	// AppMetrics bundle (HTTP RED, rate-limit decisions). Wired into the
	// router middleware and exposed at /metrics on the debug server below.
	metricsReg, appMetrics := observability.NewRegistry()

	router, primaryPool, closeRouter, err := database.NewRouter(rootCtx, cfg.DatabaseURL, cfg.ReadDatabaseURL)
	if err != nil {
		slog.Error("database router init failed", slog.String("err", err.Error()))
		return 1
	}
	defer closeRouter()

	redisClient, err := redisclient.NewClient(rootCtx, cfg.RedisURL)
	if err != nil {
		slog.Error("redis init failed", slog.String("err", err.Error()))
		return 1
	}
	defer func() { _ = redisClient.Close() }()

	svcs, err := service.New(service.Deps{Router: router, Redis: redisClient, Config: cfg})
	if err != nil {
		slog.Error("service wiring failed", slog.String("err", err.Error()))
		return 1
	}

	urlHandler := &handlers.URLHandler{Service: svcs.URL}
	healthHandler := &handlers.HealthHandler{DB: primaryPool}

	// Single-leader gate for the cleanup cron. Across N replicas, only one
	// per tick acquires the advisory lock and runs cleanup; the rest skip.
	cleanupLocker := leader.New(primaryPool)
	cleanupCron := cron.NewCleanupCron(svcs.Cleanup, cfg, cleanupLocker)
	go cleanupCron.Start(rootCtx)

	// Separate debug server hosts both pprof and Prometheus /metrics. Both
	// surfaces expose internals (stack traces, query latencies, request
	// patterns) so they share the same private port. Never expose this
	// publicly — firewall :PprofPort to internal scrapers / operators.
	debugMux := http.NewServeMux()
	debugMux.Handle("/debug/pprof/", http.DefaultServeMux)
	debugMux.Handle("/metrics", observability.Handler(metricsReg))
	debugSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.PprofPort),
		Handler:           debugMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		slog.Info("debug server starting",
			slog.String("pprof", "http://localhost"+debugSrv.Addr+"/debug/pprof/"),
			slog.String("metrics", "http://localhost"+debugSrv.Addr+"/metrics"))
		if err := debugSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("debug server exited", slog.String("err", err.Error()))
		}
	}()

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.ServerPort),
		Handler: api.SetupRouter(api.RouterDeps{
			URL:         urlHandler,
			Health:      healthHandler,
			RateLimiter: svcs.RateLimiter,
			Metrics:     appMetrics,
			ServiceName: cfg.ServiceName,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server starting",
			slog.String("addr", "http://localhost"+srv.Addr),
			slog.String("swagger", "http://localhost"+srv.Addr+"/swagger/index.html"),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Fall through to the shutdown sequence in both cases. On a server crash
	// (serverErr non-nil) we still want to flush the tracer and close the
	// debug server cleanly; we just return 1 at the end instead of 0.
	exitCode := 0
	select {
	case err := <-serverErr:
		if err != nil {
			slog.Error("server exited with error", slog.String("err", err.Error()))
			exitCode = 1
		}
	case <-rootCtx.Done():
		slog.Info("shutdown signal received")
	}

	// Shutdown ordering, all under one bounded context so a stuck component
	// can't pin the process forever:
	//   1. Stop accepting HTTP requests; drain in-flight ones. In-flight
	//      handlers complete spans which land in the BSP queue.
	//   2. Flush the tracer — exports anything still buffered. Doing this
	//      before closing Redis means Redis-backed limiter spans for the
	//      last requests still get exported.
	//   3. Stop the debug server (closes /debug/pprof and /metrics).
	//   4. Deferred closes (Redis, DB) run on function return.
	//
	// shutdownCtx is detached from rootCtx so a second Ctrl+C doesn't
	// abruptly cancel the drain.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("api server shutdown failed", slog.String("err", err.Error()))
	}
	if err := tracerShutdown(shutdownCtx); err != nil {
		slog.Error("tracer shutdown failed", slog.String("err", err.Error()))
	}
	if err := debugSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("debug server shutdown failed", slog.String("err", err.Error()))
	}
	slog.Info("shutdown complete")
	return exitCode
}
