package repository

import (
	"encoding/json"
	"time"
)

type adminModel struct {
	ID             int64     `gorm:"column:id;primaryKey"`
	Name           string    `gorm:"column:name"`
	Nickname       string    `gorm:"column:nickname"`
	Password       string    `gorm:"column:password"`
	Disabled       bool      `gorm:"column:disabled"`
	CreateTime     time.Time `gorm:"column:create_time"`
	LastUpdateTime time.Time `gorm:"column:last_update_time"`
}

func (adminModel) TableName() string { return "t_admin" }

type appModel struct {
	ID             int64     `gorm:"column:id;primaryKey"`
	Name           string    `gorm:"column:name"`
	Secret         string    `gorm:"column:secret"`
	Remark         string    `gorm:"column:remark"`
	Disabled       bool      `gorm:"column:disabled"`
	CreateTime     time.Time `gorm:"column:create_time"`
	LastUpdateTime time.Time `gorm:"column:last_update_time"`
}

func (appModel) TableName() string { return "t_app" }

type adminLogModel struct {
	ID         int64           `gorm:"column:id;primaryKey"`
	AdminID    int64           `gorm:"column:admin_id"`
	Cate       string          `gorm:"column:cate"`
	Type       string          `gorm:"column:type"`
	Content    json.RawMessage `gorm:"column:content;type:jsonb"`
	CreateTime time.Time       `gorm:"column:create_time"`
}

func (adminLogModel) TableName() string { return "t_admin_log" }
