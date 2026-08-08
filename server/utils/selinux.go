package utils

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SELinux 打标辅助：openEuler/麒麟等 SELinux Enforcing 环境下，libvirt/qemu 需读取
// 正确上下文的镜像文件。模板/克隆目录的 fcontext 规则通常已由 install.sh 写入，
// 但模板文件若经 mv/替换放入会保留源上下文（如 usr_t），且模板导入流程会对模板文件
// 设置 immutable（chattr +i）防误删，immutable 文件连 root 也无法改 xattr，导致
// 启动 VM 时 libvirt relabel 报 "Operation not permitted"。此处先解除 immutable
// 打标后再恢复，保护语义不变。

var (
	selinuxModeOnce    sync.Once
	selinuxModeCached  = ""
	selinuxModeInvalid = false

	// fcontextDone 缓存已补写 fcontext 规则的目录，避免每次打标都 exec semanage。
	// 进程重启后重新探测，缺失时按需重建，幂等。
	fcontextDone sync.Map
)

// ensureSVirtFileContext 确保目录及其子路径在 semanage 中存在 svirt_image_t 规则。
// restorecon 仅在 fcontext 有匹配规则时才会把文件恢复成 svirt_image_t；对用户
// 后期新建的存储路径（如面板存储池挂载的大盘 /dataX、/3T-xxx 等）默认无规则，
// restoreconvol 会把它reset成 default_t，导致 qemu 写镜像时被 SELinux 拦截
//（快照 "Input/output error / Operation not supported"）。
// 该函数在任意新路径写入镜像前幂等补上规则，与 install.sh 的 apply_storage_selinux_label
// 行为一致，确保"换机器/换路径/后建存储池"三种场景下快照与写入均可正常。
// semanage 不可用时跳过（restorecon 规则缺失时打标会失效，但仍尽力而为）。
func EnsureSVirtImageFcontext(dir string) {
	if !SelinuxEnforcing() {
		return
	}
	if dir == "" {
		return
	}
	if _, ok := fcontextDone.Load(dir); ok {
		return
	}
	fcontextDone.Store(dir, struct{}{})
	if LookupCmdPath("semanage") == "" || LookupCmdPath("restorecon") == "" {
		return
	}
	// 与 install.sh 使用相同的正则约定：已存在的规则 semanage 报错，忽略即可（幂等）。
	_ = ExecCommand("semanage", "fcontext", "-a", "-t", "svirt_image_t", dir+"(/.*)?")
}

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

// EnsureSELinuxLabel 在 SELinux Enforcing 下对指定路径执行 fcontext 规则补写与
// restorecon（幂等）。目录递归处理，文件按 fcontext 单条打标；对 immutable
// （chattr +i）目标临时解除后打标再恢复，确保模板保护不丢。失败静默忽略
// （权限不足/非 Enforcing 不影响主流程）。
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
		// 自定义/后建存储路径默认没有 svirt_image_t 规则，先补写保证 restorecon 生效
		EnsureSVirtImageFcontext(p)
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			ensureSELinuxLabelDir(p)
		} else {
			ensureSELinuxLabelFile(p)
		}
	}
}

// ensureSELinuxLabelDir 目录整体 restorecon，再对 immutable 子文件逐个临时解除后打标恢复。
func ensureSELinuxLabelDir(dir string) {
	_ = ExecCommand("restorecon", "-RF", dir)
	_ = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		if isFileImmutable(path) {
			_ = ExecCommand("chattr", "-i", path)
			_ = ExecCommand("restorecon", path)
			_ = ExecCommand("chattr", "+i", path)
		}
		return nil
	})
}

// ensureSELinuxLabelFile 单文件打标，immutable 时临时解除后恢复。
func ensureSELinuxLabelFile(p string) {
	if isFileImmutable(p) {
		_ = ExecCommand("chattr", "-i", p)
		defer func() { _ = ExecCommand("chattr", "+i", p) }()
	}
	_ = ExecCommand("restorecon", p)
}

// isFileImmutable 判断文件是否带 immutable（chattr +i）属性。
func isFileImmutable(p string) bool {
	res := ExecCommandQuiet("lsattr", "-d", p)
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return false
	}
	fields := strings.SplitN(out, " ", 2)
	if len(fields) == 0 {
		return false
	}
	return strings.Contains(fields[0], "i")
}