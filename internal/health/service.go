package health

import (
	"context"
	"time"

	"github.com/kely-jian/ms-sar-dashboard/internal/domain"
)

type Checker interface {
	Name() string
	Check(ctx context.Context) (string, error)
}

type Service struct {
	name     string
	env      string
	version  string
	checkers []Checker
}

func NewService(name string, env string, version string, checkers ...Checker) *Service {
	return &Service{name: name, env: env, version: version, checkers: checkers}
}

func (s *Service) Check(ctx context.Context) domain.HealthResult {
	result := domain.HealthResult{
		Name:    s.name,
		Env:     s.env,
		Version: s.version,
		Status:  "up",
		Time:    time.Now().UTC(),
	}

	for _, checker := range s.checkers {
		status := "up"
		message := ""
		if checker != nil {
			if value, err := checker.Check(ctx); err != nil {
				status = "down"
				message = err.Error()
				result.Status = "degraded"
			} else if value != "" {
				status = value
			}
			result.Components = append(result.Components, domain.HealthComponent{
				Name:    checker.Name(),
				Status:  status,
				Message: message,
			})
		}
	}

	return result
}
