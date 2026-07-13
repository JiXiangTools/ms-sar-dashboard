package service

import (
	"context"
	"log"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/auth"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
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

func recordLoginFailed(ctx context.Context, auditSvc *audit.Service, logger *log.Logger, adminID int64, content map[string]any) {
	recordLoginAudit(ctx, auditSvc, logger, adminID, "LOGIN_FAILED", content)
}

func recordLoginSuccess(ctx context.Context, auditSvc *audit.Service, logger *log.Logger, adminID int64, content map[string]any) {
	recordLoginAudit(ctx, auditSvc, logger, adminID, "LOGIN_SUCCESS", content)
}

func recordLoginAudit(ctx context.Context, auditSvc *audit.Service, logger *log.Logger, adminID int64, action string, content map[string]any) {
	if auditSvc == nil {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := auditSvc.Record(auditCtx, adminID, "AUTH", action, content); err != nil {
		logx.Error(logger, auditCtx, time.Now(), "audit.record.failed", "login audit record failed",
			logx.Int64("admin_id", adminID), logx.String("action", action), logx.Err(err))
	}
}
