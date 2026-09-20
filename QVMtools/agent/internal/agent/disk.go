package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// qemuImgInfo 是 qemu-img info -o json 输出的子集。
type qemuImgInfo struct {
	Format        string `json:"format"`
	CapacityBytes int64  `json:"virtual-size"`
	ActualBytes   int64  `json:"actual-size"`
	Filename      string `json:"filename"`
	BackingFile   string `json:"backing-filename,omitempty"`
}

// diskSizeBytes 返回磁盘文件的虚拟容量字节数。
func diskSizeBytes(path string) (int64, error) {
	out, err := runOutput("qemu-img", "info", "-o", "json", path)
	if err != nil {
		return 0, fmt.Errorf("qemu-img info %s: %w", path, err)
	}
	var info qemuImgInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return 0, fmt.Errorf("parse qemu-img info: %w", err)
	}
	return info.CapacityBytes, nil
}

// resolveBackingFile 通过 qemu-img 解析 overlay 的底层真实磁盘路径（绝对路径）。
func resolveBackingFile(path string) string {
	if path == "" {
		return ""
	}
	out, err := runOutput("qemu-img", "info", "-o", "json", path)
	if err != nil {
		return path
	}
	var info qemuImgInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return path
	}
	if info.BackingFile != "" {
		// qemu-img 返回的 backing-filename 为相对路径，需拼接为绝对路径。
		if filepath.IsAbs(info.BackingFile) {
			return info.BackingFile
		}
		return filepath.Join(filepath.Dir(path), info.BackingFile)
	}
	return path
}
