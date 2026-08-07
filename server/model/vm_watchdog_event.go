package model

import "time"

// VMWatchdogEvent 看门狗审计事件（M8.9 / §14 P2-9）。
// 记录 guest agent 失联触发的硬重置、恢复、以及大页建议等事件，
// 供「操作审计」类查看与排查，与 VM 自身状态同步使用命令获取。
type VMWatchdogEvent struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	VMName        string    `gorm:"size:255;not null;index:idx_watchdog_vm" json:"vm_name"`
	Status        string    `gorm:"size:20;not null;index:idx_watchdog_status" json:"status"` // reset / warning / recovered
	Reason        string    `gorm:"type:text" json:"reason"`
	ResultMessage string    `gorm:"type:text" json:"result_message"`
	CreatedAt     time.Time `gorm:"index:idx_watchdog_created" json:"created_at"`
}

// VMWatchdogEventFilter 看门狗事件列表筛选参数。
type VMWatchdogEventFilter struct {
	Page     int
	PageSize int
	Status   string
	VMName   string
	Start    *time.Time
	End      *time.Time
}

// CreateVMWatchdogEvent 写入看门狗事件。
func CreateVMWatchdogEvent(event *VMWatchdogEvent) error {
	return DB.Create(event).Error
}

// ListVMWatchdogEvents 获取看门狗事件列表（按状态/虚拟机/时间范围筛选，创建时间倒序分页）。
func ListVMWatchdogEvents(filter VMWatchdogEventFilter) ([]VMWatchdogEvent, int64, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}

	query := DB.Model(&VMWatchdogEvent{})
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.VMName != "" {
		query = query.Where("vm_name LIKE ?", "%"+filter.VMName+"%")
	}
	if filter.Start != nil {
		query = query.Where("created_at >= ?", filter.Start)
	}
	if filter.End != nil {
		query = query.Where("created_at <= ?", filter.End)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []VMWatchdogEvent
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
