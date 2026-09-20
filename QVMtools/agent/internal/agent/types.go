package agent

// VMInfo 是 discover 命令返回的单个虚拟机元信息。
type VMInfo struct {
	Name   string    `json:"name"`
	ID     string    `json:"id"`
	UUID   string    `json:"uuid"`
	State  string    `json:"state"`
	CPUs   int       `json:"cpus"`
	Memory int64     `json:"memory_mb"`
	Disks  []DiskInfo `json:"disks"`
}

// DiskInfo 是 VM 单个磁盘的元信息。
type DiskInfo struct {
	Target         string `json:"target"`
	SourcePath     string `json:"source_path"`
	TargetDiskPath string `json:"target_disk_path,omitempty"`
	SizeBytes      int64  `json:"size_bytes"`
	Format         string `json:"format"`
}

// TargetSSH 是目标主机的 SSH 接入信息。
type TargetSSH struct {
	Host       string `json:"host"`
	Port       string `json:"port"`
	User       string `json:"user"`
	AuthMethod string `json:"auth_method"`
	KeyContent string `json:"key_content,omitempty"`
	Password   string `json:"password,omitempty"`
}

// PullResult 是 pull 命令返回的传输校验结果。
type PullResult struct {
	Checksum string `json:"checksum"`
	Size     int64  `json:"size"`
	Path     string `json:"path"`
}
