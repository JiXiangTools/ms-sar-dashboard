package service

import (
	"context"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/auth"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
)

func issueLoginOutput(tokens *auth.Service, admin domain.Admin) (LoginOutput, error) {
	accessToken, err := tokens.IssueAccessToken(admin)
	if err != nil {
		return LoginOutput{}, err
	}
	return LoginOutput{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int64(tokens.AccessTokenTTL().Seconds()),
	}, nil
}

func recordLoginFailed(ctx context.Context, auditSvc *audit.Service, adminID int64, content map[string]any) {
	_ = auditSvc.Record(ctx, adminID, "AUTH", "LOGIN_FAILED", content)
}

func recordLoginSuccess(ctx context.Context, auditSvc *audit.Service, adminID int64, content map[string]any) {
	_ = auditSvc.Record(ctx, adminID, "AUTH", "LOGIN_SUCCESS", content)
}
