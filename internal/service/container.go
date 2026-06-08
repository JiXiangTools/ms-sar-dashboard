package service

import (
	"errors"
	"log"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/auth"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	platformcache "github.com/JiXiangTools/ms-sar-dashboard/internal/platform/cache"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/database"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/elasticsearch"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
)

type Container struct {
	Repo  *repository.Repository
	Audit *audit.Service
	Auth  *AdminAuthService
	SSO   *AdminSSOService
	Apps  *AppService
	Logs  *LogService
	Debug *DebugService
}

func NewContainer(cfg config.Config, dbClient *database.Client, cacheClient *platformcache.Client, esClient *elasticsearch.Client, logger *log.Logger) (*Container, error) {
	ormDB, err := requiredDatabase(dbClient)
	if err != nil {
		return nil, err
	}
	redisClient, err := requiredRedis(cacheClient)
	if err != nil {
		return nil, err
	}
	repo := repository.New(ormDB)
	auditService := audit.NewService(repo)
	tokenService := auth.NewService(cfg.Auth)

	container := &Container{
		Repo:  repo,
		Audit: auditService,
		Auth:  NewAdminAuthService(repo, tokenService, auditService, logger),
		SSO:   NewAdminSSOService(cfg.SSO, tokenService, auditService, logger),
		Apps:  NewAppService(repo, redisClient, auditService, logger),
		Logs:  NewLogService(repo),
		Debug: NewDebugService(cfg, redisClient, esClient, auditService, logger),
	}
	return container, nil
}

func requiredDatabase(client *database.Client) (*gorm.DB, error) {
	if client == nil {
		return nil, errors.New("database client is required")
	}
	if client.ORM == nil {
		return nil, errors.New("database gorm client is required")
	}
	return client.ORM, nil
}

func requiredRedis(client *platformcache.Client) (redis.UniversalClient, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	if client.Client == nil {
		return nil, errors.New("redis universal client is required")
	}
	return client.Client, nil
}
