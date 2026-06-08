package auth

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
)

const TokenTypeAccess = "access"

type Claims struct {
	AdminID        int64  `json:"admin_id"`
	TokenType      string `json:"token_type"`
	AdminUpdatedAt int64  `json:"admin_updated_at"`
	AdminNickname  string `json:"admin_nickname,omitempty"`
	AuthSource     string `json:"auth_source,omitempty"`
	jwt.RegisteredClaims
}

type Service struct {
	secret         []byte
	issuer         string
	accessTokenTTL time.Duration
}

func NewService(cfg config.AuthConfig) *Service {
	secret := strings.TrimSpace(cfg.JWTSecret)
	if secret == "" {
		secret = "ms-sar-dashboard-dev-secret"
	}
	issuer := strings.TrimSpace(cfg.Issuer)
	if issuer == "" {
		issuer = "ms-sar-dashboard"
	}
	ttl := cfg.AccessTokenTTL
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	return &Service{
		secret:         []byte(secret),
		issuer:         issuer,
		accessTokenTTL: ttl,
	}
}

func (s *Service) HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func (s *Service) ComparePassword(hashedPassword string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

func (s *Service) IssueAccessToken(admin domain.Admin) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		AdminID:        admin.ID,
		TokenType:      TokenTypeAccess,
		AdminUpdatedAt: admin.LastUpdateTime.UTC().UnixNano(),
		AdminNickname:  strings.TrimSpace(admin.Nickname),
		AuthSource:     "local",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   strings.TrimSpace(admin.Name),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTokenTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *Service) IssueSSOAccessToken(adminID int64, account string, nickname string) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		AdminID:        adminID,
		TokenType:      TokenTypeAccess,
		AdminUpdatedAt: 0,
		AdminNickname:  strings.TrimSpace(nickname),
		AuthSource:     "sso",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   strings.TrimSpace(account),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTokenTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *Service) ParseAccessToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	}, jwt.WithIssuer(s.issuer))
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.TokenType != TokenTypeAccess {
		return nil, errors.New("invalid token type")
	}
	return claims, nil
}

func (s *Service) AccessTokenTTL() time.Duration {
	return s.accessTokenTTL
}
