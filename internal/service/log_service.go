package service

import (
	"context"

	"github.com/kely-jian/ms-sar-dashboard/internal/domain"
	"github.com/kely-jian/ms-sar-dashboard/internal/repository"
)

type LogService struct {
	repo *repository.Repository
}

func NewLogService(repo *repository.Repository) *LogService {
	return &LogService{repo: repo}
}

func (s *LogService) List(ctx context.Context, query AdminLogQuery) (domain.Page[domain.AdminLog], error) {
	return s.repo.ListAdminLogs(ctx, query.Cate, query.Type, query.Page, query.PageSize)
}
