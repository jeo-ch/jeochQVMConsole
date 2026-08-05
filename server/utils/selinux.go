package utils

import (
	"strings"
	"sync"
)

// SELinux 打标辅助：openEuler/麒麟等 SELinux Enforcing 环境下，libvirt/qemu 需读取
// 正确上下文的镜像文件。模板/克隆目录的 fcontext 规则通常已由 install.sh 写入，
// 但模板文件若经 mv/替换放入会保留源上下文（如 usr_t），启动 VM 时 libvirt 尝试
// relabel 报 "Operation not permitted"。这里在后端使用路径前按 fcontext 自动 restorecon。

var (
	selinuxModeOnce    sync.Once
	selinuxModeCached  = ""
	selinuxModeInvalid = false
)

// SelinuxMode 返回 SELinux 状态：Enforcing / Permissive / Disabled（含 getenforce 不可用等异常）。
// 结果缓存，避免每次 VM 操作都 exec。
func SelinuxMode() string {
	selinuxModeOnce.Do(func() {
		if LookupCmdPath("getenforce") == "" {
			selinuxModeCached = "Disabled"
			return
		}
		res := ExecCommand("getenforce")
		if res.Error != nil || strings.TrimSpace(res.Stdout) == "" {
			selinuxModeCached = "Unknown"
			return
		}
		selinuxModeCached = strings.TrimSpace(res.Stdout)
	})
	return selinuxModeCached
}

// SelinuxEnforcing 判断当前是否 SELinux Enforcing。
func SelinuxEnforcing() bool {
	_ = selinuxModeInvalid
	return SelinuxMode() == "Enforcing"
}

// EnsureSELinuxLabel 在 SELinux Enforcing 下对指定路径执行 restorecon（幂等）。
// 目录用 -RF 递归，普通文件单条打标；失败静默忽略（权限不足/非 Enforcing 不影响主流程）。
// 注意：后端需以 root 运行才有权限打标（面板默认 root）。
func EnsureSELinuxLabel(paths ...string) {
	if !SelinuxEnforcing() {
		return
	}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_ = ExecCommand("restorecon", "-RF", p)
	}
}