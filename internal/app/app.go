package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/health"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/router"
	platformcache "github.com/JiXiangTools/ms-sar-dashboard/internal/platform/cache"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/database"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/elasticsearch"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

type Options struct {
	ConfigPath  string
	Environment string
}

type App struct {
	cfg           config.Config
	logger        *log.Logger
	httpServer    *http.Server
	database      *database.Client
	cache         *platformcache.Client
	elasticsearch *elasticsearch.Client

	backgroundCancel context.CancelFunc
}

func New(opts Options) (*App, error) {
	cfg, err := config.Load(opts.ConfigPath, opts.Environment)
	if err != nil {
		return nil, err
	}

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC|log.Lmicroseconds)

	databaseClient, err := database.New(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("init database: %w", err)
	}

	cacheClient, err := platformcache.New(cfg.Redis)
	if err != nil {
		_ = databaseClient.Close()
		return nil, fmt.Errorf("init redis: %w", err)
	}

	esClient, err := elasticsearch.New(cfg.Elasticsearch)
	if err != nil {
		_ = cacheClient.Close()
		_ = databaseClient.Close()
		return nil, fmt.Errorf("init elasticsearch: %w", err)
	}

	healthCheckers := []health.Checker{databaseClient, cacheClient}
	if len(cfg.Elasticsearch.Addrs) > 0 {
		healthCheckers = append(healthCheckers, esClient)
	}
	healthService := health.NewService(cfg.App.Name, cfg.App.Env, cfg.App.Version, healthCheckers...)
	services, err := service.NewContainer(cfg, databaseClient, cacheClient, esClient, logger)
	if err != nil {
		_ = cacheClient.Close()
		_ = databaseClient.Close()
		return nil, fmt.Errorf("init services: %w", err)
	}
	engine := router.New(cfg, logger, router.Dependencies{
		HealthService: healthService,
		Services:      services,
	})

	httpServer := &http.Server{
		Addr:         net.JoinHostPort(cfg.App.Host, strconv.Itoa(cfg.App.Port)),
		Handler:      engine,
		ReadTimeout:  cfg.App.ReadTimeout,
		WriteTimeout: cfg.App.WriteTimeout,
	}

	return &App{
		cfg:           cfg,
		logger:        logger,
		httpServer:    httpServer,
		database:      databaseClient,
		cache:         cacheClient,
		elasticsearch: esClient,
	}, nil
}

func (a *App) Run() error {
	backgroundCtx, cancel := context.WithCancel(context.Background())
	a.backgroundCancel = cancel

	errCh := make(chan error, 1)
	go func() {
		logx.Info(a.logger, backgroundCtx, time.Now(), "app.start", "http server starting",
			logx.String("addr", a.httpServer.Addr),
			logx.String("env", a.cfg.App.Env),
		)
		if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	select {
	case err := <-errCh:
		if a.backgroundCancel != nil {
			a.backgroundCancel()
		}
		return err
	case sig := <-signalCh:
		logx.Info(a.logger, backgroundCtx, time.Now(), "app.shutdown_signal", "shutdown signal received",
			logx.String("signal", sig.String()),
		)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.App.ShutdownTimeout)
	defer cancel()

	return a.Shutdown(shutdownCtx)
}

func (a *App) Shutdown(ctx context.Context) error {
	var errs []error

	if a.backgroundCancel != nil {
		a.backgroundCancel()
	}
	if err := a.httpServer.Shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := a.cache.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := a.database.Close(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
