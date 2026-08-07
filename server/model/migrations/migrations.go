// Package migrations 提供轻量级版本化数据库迁移框架（M8.5 / §14 P1-5）。
//
// 设计目标：将「跨版本 schema/数据变更」从一次性幂等 migrate 函数升级为
// 版本化、可追溯、只执行一次的迁移记录，避免升级事故与重复迁移。
//
// 使用方式：
//   - 新增迁移：在 migrations/ 目录新建文件，init() 中调用 Register()
//     注册 {Version, Up, Down}；版本号建议格式 "0001_描述"。
//   - 执行时机：model.InitDB 在 AutoMigrate 之前调用 Run(db)，已应用的
//     版本通过 schema_migrations 表跳过，未应用的按注册顺序在一个事务内执行。
//   - 记录位置：schema_migrations 表（version 主键 + applied_at）。
package migrations

import (
	"time"

	"gorm.io/gorm"
)

// SchemaMigration 迁移记录表（跟踪已应用版本，避免重复迁移）
type SchemaMigration struct {
	Version   string    `gorm:"primaryKey;size:100"` // 迁移版本号，如 0001_vpc_switch_cidr
	AppliedAt time.Time `gorm:"autoCreateTime"`      // 应用时间
}

// TableName 指定迁移记录表名
func (SchemaMigration) TableName() string {
	return "schema_migrations"
}

// Migration 定义一次版本化迁移
type Migration struct {
	Version string // 版本号，建议 "0001_描述"，全局唯一
	Up      func(tx *gorm.DB) error
	Down    func(tx *gorm.DB) error // 回滚，可为 nil
}

var (
	migrations = make([]Migration, 0)
)

// Register 注册一个迁移（在 init() 中调用）
func Register(m Migration) {
	if m.Version == "" || m.Up == nil {
		panic("migrations: 迁移版本号不能为空且 Up 不能为 nil")
	}
	migrations = append(migrations, m)
}

// Run 执行所有未应用的迁移，返回本次应用的数量。
// 顺序：按注册顺序；每个迁移在一个事务中执行，失败则回滚该事务并返回错误。
// 已应用版本通过 schema_migrations 表跳过。
func Run(db *gorm.DB) (int, error) {
	if db == nil {
		return 0, nil
	}
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		return 0, err
	}

	var appliedRows []SchemaMigration
	if err := db.Find(&appliedRows).Error; err != nil {
		return 0, err
	}
	applied := make(map[string]bool, len(appliedRows))
	for _, row := range appliedRows {
		applied[row.Version] = true
	}

	appliedCount := 0
	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := m.Up(tx); err != nil {
				return err
			}
			// 迁移执行成功后记录版本
			return tx.Create(&SchemaMigration{Version: m.Version}).Error
		}); err != nil {
			return appliedCount, err
		}
		appliedCount++
	}
	return appliedCount, nil
}
