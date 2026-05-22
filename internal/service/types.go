package service

import "time"

type LoginInput struct {
	Name     string
	Password string
}

type LoginOutput struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

type AppListQuery struct {
	AppID    *int64
	Name     string
	Page     int
	PageSize int
}

type AppCreateInput struct {
	Name   string
	Secret string
	Remark string
}

type AppUpdateInput struct {
	Name   *string
	Secret *string
	Remark *string
}

type AdminLogQuery struct {
	Cate     string
	Type     string
	Page     int
	PageSize int
}

type ESIndexInfo struct {
	Index    string `json:"index"`
	Exists   bool   `json:"exists"`
	Mapping  any    `json:"mapping,omitempty"`
	Settings any    `json:"settings,omitempty"`
	Count    any    `json:"count,omitempty"`
	Health   any    `json:"health,omitempty"`
}

type ESDebugSearchResult struct {
	Index  string `json:"index"`
	Result any    `json:"result"`
}

type RecDebugRequest struct {
	Type    string   `json:"type"`
	AppID   string   `json:"appid"`
	ItemID  string   `json:"item_id"`
	UserID  string   `json:"user_id"`
	Period  string   `json:"period"`
	Size    int      `json:"size"`
	Exclude []string `json:"exclude"`
}

type RecDebugResult struct {
	Type     string         `json:"type"`
	AppID    string         `json:"appid"`
	Endpoint string         `json:"endpoint"`
	Params   map[string]any `json:"params"`
	Status   int            `json:"status"`
	Message  string         `json:"message"`
	ItemIDs  []string       `json:"item_ids"`
	Size     int            `json:"size"`
	Raw      any            `json:"raw,omitempty"`
}

type RecItem struct {
	ItemID string  `json:"item_id"`
	Score  float64 `json:"score,omitempty"`
	Raw    string  `json:"raw,omitempty"`
}

type AppAuthRecord struct {
	ID        int64
	Secret    string
	Disabled  bool
	UpdatedAt time.Time
}
