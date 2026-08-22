package vm_xml

import "path/filepath"

// DefaultNVRAMDir libvirt 默认 NVRAM 存储目录
const DefaultNVRAMDir = "/var/lib/libvirt/qemu/nvram"

// NVRAMVarsPath 返回虚拟机的 NVRAM VARS 文件路径
// 格式: /var/lib/libvirt/qemu/nvram/<vmName>_VARS.fd
func NVRAMVarsPath(vmName string) string {
	return filepath.Join(DefaultNVRAMDir, vmName+"_VARS.fd")
}