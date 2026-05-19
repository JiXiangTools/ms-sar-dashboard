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
	Key     string   `json:"key"`
	Size    int      `json:"size"`
	Exclude []string `json:"exclude"`
}

type RecDebugResult struct {
	Type            string         `json:"type"`
	AppID           string         `json:"appid"`
	Key             string         `json:"key"`
	Exists          bool           `json:"exists"`
	KeyType         string         `json:"key_type,omitempty"`
	RawCount        int            `json:"raw_count"`
	ParsedCount     int            `json:"parsed_count"`
	FilteredCount   int            `json:"filtered_count"`
	FinalCount      int            `json:"final_count"`
	FilteredReasons map[string]int `json:"filtered_reasons,omitempty"`
	Items           []RecItem      `json:"items"`
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
