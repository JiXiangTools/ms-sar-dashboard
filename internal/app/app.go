package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/kely-jian/ms-sar-dashboard/internal/config"
	"github.com/kely-jian/ms-sar-dashboard/internal/health"
	"github.com/kely-jian/ms-sar-dashboard/internal/http/router"
	platformcache "github.com/kely-jian/ms-sar-dashboard/internal/platform/cache"
	"github.com/kely-jian/ms-sar-dashboard/internal/platform/database"
	"github.com/kely-jian/ms-sar-dashboard/internal/platform/elasticsearch"
	"github.com/kely-jian/ms-sar-dashboard/internal/platform/logx"
	"github.com/kely-jian/ms-sar-dashboard/internal/service"
)

type Options struct {
	ConfigPath  string
	Environment string
}

type App struct {
	cfg              config.Config
	logger           *log.Logger
	logWriter        io.Closer
	httpServer       *http.Server
	database         *database.Client
	cache            *platformcache.Client
	elasticsearch    *elasticsearch.Client
	backgroundCancel context.CancelFunc
}

func New(opts Options) (*App, error) {
	cfg, err := config.Load(opts.ConfigPath, opts.Environment)
	if err != nil {
		return nil, err
	}

	logWriter, err := newRotatingFileWriter(filepath.Join("logs", "ms-sar-dashboard.log"), 500*1024*1024, 3)
	if err != nil {
		return nil, fmt.Errorf("init file logger: %w", err)
	}
	logger := log.New(io.MultiWriter(os.Stdout, logWriter), "", log.LstdFlags|log.LUTC|log.Lmicroseconds)

	databaseClient, err := database.New(cfg.Database)
	if err != nil {
		_ = logWriter.Close()
		return nil, fmt.Errorf("init database: %w", err)
	}

	cacheClient, err := platformcache.New(cfg.Redis)
	if err != nil {
		_ = logWriter.Close()
		_ = databaseClient.Close()
		return nil, fmt.Errorf("init redis: %w", err)
	}

	esClient, err := elasticsearch.New(cfg.Elasticsearch)
	if err != nil {
		_ = logWriter.Close()
		_ = cacheClient.Close()
		_ = databaseClient.Close()
		return nil, fmt.Errorf("init elasticsearch: %w", err)
	}

	healthService := health.NewService(cfg.App.Name, cfg.App.Env, cfg.App.Version, databaseClient, cacheClient, esClient)
	services, err := service.NewContainer(cfg, databaseClient, cacheClient, esClient, logger)
	if err != nil {
		_ = logWriter.Close()
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
		logWriter:     logWriter,
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
	if a.logWriter != nil {
		if err := a.logWriter.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

type rotatingFileWriter struct {
	mu         sync.Mutex
	path       string
	maxBytes   int64
	maxFiles   int
	file       *os.File
	currentLen int64
}

func newRotatingFileWriter(path string, maxBytes int64, maxFiles int) (*rotatingFileWriter, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("max log size must be positive")
	}
	if maxFiles <= 0 {
		return nil, fmt.Errorf("max log files must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	writer := &rotatingFileWriter{
		path:     path,
		maxBytes: maxBytes,
		maxFiles: maxFiles,
	}
	if err := writer.open(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *rotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.currentLen > 0 && w.currentLen+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.currentLen += int64(n)
	return n, err
}

func (w *rotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingFileWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.currentLen = info.Size()
	return nil
}

func (w *rotatingFileWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}

	for index := w.maxFiles - 1; index >= 1; index-- {
		source := rotatedLogPath(w.path, index-1)
		target := rotatedLogPath(w.path, index)
		if index == w.maxFiles-1 {
			_ = os.Remove(target)
		}
		if _, err := os.Stat(source); err == nil {
			if err := os.Rename(source, target); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	return w.open()
}

func rotatedLogPath(path string, index int) string {
	if index <= 0 {
		return path
	}
	return fmt.Sprintf("%s.%d", path, index)
}
