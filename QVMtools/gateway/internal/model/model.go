package model

import (
	"encoding/json"
	"time"
)

// Task 异步任务（仅保留 gateway 需要的字段）。
type Task struct {
	ID        uint            `gorm:"primaryKey" json:"id"`
	Type      string          `json:"type"`
	Status    string          `json:"status"`
	Params    string          `json:"params"`
	Result    string          `json:"result"`
	Progress  int             `json:"progress"`
	Message   string          `json:"message"`
	Detail    json.RawMessage `json:"detail,omitempty"` // 结构化进度详情（传输速度、ETA 等）
	CreatedBy string          `json:"created_by"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func (Task) TableName() string { return "async_tasks" }

// User 仅保留 gateway 做类型断言需要的字段。
type User struct {
	ID       uint   `json:"id" gorm:"primaryKey"`
	Username string `json:"username"`
	Role     string `json:"role"`
}
