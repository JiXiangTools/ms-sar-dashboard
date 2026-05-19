package domain

import "time"

type Admin struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Nickname       string    `json:"nickname"`
	Password       string    `json:"-"`
	Disabled       bool      `json:"disabled"`
	CreateTime     time.Time `json:"create_time"`
	LastUpdateTime time.Time `json:"last_update_time"`
}

type App struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Secret         string    `json:"secret"`
	Remark         string    `json:"remark"`
	Disabled       bool      `json:"disabled"`
	CreateTime     time.Time `json:"create_time"`
	LastUpdateTime time.Time `json:"last_update_time"`
}

type AdminLog struct {
	ID         int64     `json:"id"`
	AdminID    int64     `json:"admin_id"`
	Cate       string    `json:"cate"`
	Type       string    `json:"type"`
	Content    any       `json:"content"`
	CreateTime time.Time `json:"create_time"`
}

type Page[T any] struct {
	Items    []T   `json:"items"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

type TokenResult struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

type LoginResult struct {
	TokenResult
}

type HealthComponent struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type HealthResult struct {
	Name       string            `json:"name"`
	Env        string            `json:"env"`
	Version    string            `json:"version"`
	Status     string            `json:"status"`
	Time       time.Time         `json:"time"`
	Components []HealthComponent `json:"components"`
}
