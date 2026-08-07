package migrations

import (
	"gorm.io/gorm"
)

// 0001_scheduler_events_vm_status_index：为 scheduler_events 表增加 (vm_name, status)
// 复合索引。前端「定时任务-事件记录」列表高频按 VMName + Status 过滤（model/scheduler_event.go
// ListSchedulerEvents），升级前该表仅有单列索引，多条件过滤全表扫描。纯增量 DDL，
// 不触碰任何现有逻辑，新库/旧库均可安全执行。
func init() {
	Register(Migration{
		Version: "0001_scheduler_events_vm_status_index",
		Up: func(tx *gorm.DB) error {
			// 迁移在 AutoMigrate 之前执行：新库表尚未创建时跳过（AutoMigrate 随后建表）
			if !tx.Migrator().HasTable("scheduler_events") {
				return nil
			}
			return tx.Exec("CREATE INDEX IF NOT EXISTS idx_scheduler_events_vm_status ON scheduler_events(vm_name, status)").Error
		},
		Down: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable("scheduler_events") {
				return nil
			}
			return tx.Exec("DROP INDEX IF EXISTS idx_scheduler_events_vm_status").Error
		},
	})
}
