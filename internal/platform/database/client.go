package database

import (
	"context"
	"database/sql"
	"errors"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
)

type Client struct {
	DB  *sql.DB
	ORM *gorm.DB
	cfg config.DatabaseConfig
}

func New(cfg config.DatabaseConfig) (*Client, error) {
	client := &Client{cfg: cfg}

	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, err
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}

	client.DB = db
	ormDB, err := openGORM(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	client.ORM = ormDB

	if err := client.ping(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return client, nil
}

func (c *Client) Name() string {
	return "database"
}

func (c *Client) Check(ctx context.Context) (string, error) {
	if c.DB == nil {
		return "down", errors.New("database client is nil")
	}
	if err := c.ping(ctx); err != nil {
		return "down", err
	}
	return "up", nil
}

func (c *Client) Close() error {
	if c == nil || c.DB == nil {
		return nil
	}
	return c.DB.Close()
}

func (c *Client) ping(ctx context.Context) error {
	if c.DB == nil {
		return errors.New("database client is nil")
	}
	if c.cfg.HealthCheckTimeout <= 0 {
		return c.DB.PingContext(ctx)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, c.cfg.HealthCheckTimeout)
	defer cancel()
	return c.DB.PingContext(timeoutCtx)
}

func openGORM(db *sql.DB) (*gorm.DB, error) {
	return gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: db}), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
		TranslateError: true,
	})
}
