package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"kvm_console/logger"
	"kvm_console/service/libvirt_rpc"
	"kvm_console/service/vm_xml"
	"kvm_console/utils"
)

// diskSourceFileRe 匹配 VM XML 中所有磁盘 <source file='...'>（含 backingStore）。
var diskSourceFileRe = regexp.MustCompile(`<source[^>]*file='([^']+)'`)

// vmPowerLocks 串行化同一虚拟机的电源类操作（开机/关机/断电/重启），
// 避免并发触发两次 start/destroy 对同一 backing 镜像的 chattr 竞态与 libvirt 重复启动。
var vmPowerLocks sync.Map

// withVMPowerLock 在同一虚拟机的电源操作上串行执行 fn。
func withVMPowerLock(vmName string, fn func() error) error {
	key := strings.TrimSpace(vmName)
	if key == "" {
		return fn()
	}
	value, _ := vmPowerLocks.LoadOrStore(key, &sync.Mutex{})
	value.(*sync.Mutex).Lock()
	defer value.(*sync.Mutex).Unlock()
	return fn()
}

// ensureVMSELinuxImageLabels 在 SELinux Enforcing 下，对 VM 定义中所有磁盘镜像
// 文件（含 backing 模板）的父目录做 restorecon。libvirt 启动时若发现 backing
// 文件为 usr_t 等源上下文会尝试 relabel，无权限时报 "Operation not permitted"，
// 此处在启动前按 fcontext 幂等打标兜底。
func ensureVMSELinuxImageLabels(name string) {
	vmXML, err := libvirt_rpc.GetDomainXMLRPC(name, 0)
	if err != nil {
		xmlRes := utils.ExecCommand("virsh", "dumpxml", name)
		if xmlRes.Error != nil {
			return
		}
		vmXML = xmlRes.Stdout
	}
	var dirs []string
	seen := map[string]bool{}
	for _, m := range diskSourceFileRe.FindAllStringSubmatch(vmXML, -1) {
		p := filepath.Dir(m[1])
		if !seen[p] {
			seen[p] = true
			dirs = append(dirs, p)
		}
	}
	if len(dirs) > 0 {
		utils.EnsureSELinuxLabel(dirs...)
	}
}

// withVMDiskImagesUnlocked 临时解除虚拟机所有磁盘镜像文件（含 backing 模板）的
// immutable 属性，执行 fn，再恢复 immutable。
// libvirt 每次启动都会对共享 backing 镜像无条件执行 setfilecon 打标（virt_content_t），
// 而模板导入流程会把模板文件置为 immutable（chattr +i），immutable 文件连 root
// 也无法修改 xattr，SELinux Enforcing 下报 "Operation not permitted"。
// 本函数在启动期间临时解锁，保证 libvirt 可正常打标，启动结束后恢复保护。
// 非 Enforcing 或无可执行权限时静默跳过，不影响主流程。
func withVMDiskImagesUnlocked(name string, fn func() error) error {
	if !utils.SelinuxEnforcing() {
		return fn()
	}
	vmXML, err := libvirt_rpc.GetDomainXMLRPC(name, 0)
	if err != nil {
		xmlRes := utils.ExecCommand("virsh", "dumpxml", name)
		if xmlRes.Error != nil {
			return fn()
		}
		vmXML = xmlRes.Stdout
	}
	var unlocked []string
	// 在解锁循环之前注册恢复 defer：即使循环或 fn 中途 panic，已解锁的 immutable
	// 文件也能被恢复，避免保护被静默破坏。
	defer func() {
		for _, p := range unlocked {
			if err := utils.SetFileImmutable(p); err != nil {
				logger.App.Error("恢复磁盘 immutable 失败", "path", p, "error", err.Error())
			}
		}
	}()
	seen := map[string]bool{}
	for _, m := range diskSourceFileRe.FindAllStringSubmatch(vmXML, -1) {
		// XML 的 <source file> 仅列出覆盖层（overlay），而链式克隆的 backing 模板
		// 只存在于 qcow2 backing-file 元数据中，libvirt 启动时会按 backing 链解析并对
		// 每个层执行 setfilecon。模板层在导入时被置为 immutable（chattr +i），若仅解锁
		// 覆盖层，backing 模板仍 immutable → SELinux Enforcing 下启动依旧报
		// "Operation not permitted"。故此处用 qemu-img info 递归解析 backing 链，
		// 把每一层的文件路径都纳入解锁集合。
		p := strings.TrimSpace(m[1])
		if p == "" {
			continue
		}
		// resolveQcow2BackingChain(p) 首元素即 p 自身，直接由链循环统一纳入解锁集合。
		for _, chainPath := range resolveQcow2BackingChain(p) {
			if chainPath == "" || seen[chainPath] {
				continue
			}
			seen[chainPath] = true
			if utils.IsFileImmutable(chainPath) {
				if utils.RemoveFileImmutable(chainPath) == nil {
					unlocked = append(unlocked, chainPath)
				}
			}
		}
	}
	return fn()
}

// resolveQcow2BackingChain 用 qemu-img info -U 递归解析 qcow2 映像的 backing 链，
// 返回从顶层到最底层所有文件的路径（含自身）。中层级文件可能同时是上层 overlay 的
// backing 与自身的镜像，借助 lsattr/chattr 幂等处理；非 qcow2/不可读文件仅返回自身。
// 在 withVMDiskImagesUnlocked 中用于把 immutable 的共享模板一并纳入临时解锁集合。
// 单层解析带 10s 超时，整条链最多解析 16 层或累计耗时 30s，避免慢盘场景下
// 拖垮 VM 启动接口。
func resolveQcow2BackingChain(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	const (
		maxLayers  = 16
		maxTotal   = 30 * time.Second
		layerLimit = 10 * time.Second
	)
	result := []string{path}
	cur := path
	start := time.Now()
	for i := 0; i < maxLayers; i++ {
		if time.Since(start) > maxTotal {
			logger.App.Warn("解析 qcow2 backing 链超时，仅解锁已解析层", "path", path)
			break
		}
		res := utils.ExecShellQuietWithTimeout(
			fmt.Sprintf("qemu-img info -U %s 2>/dev/null | grep 'backing file:' | awk '{print $3}'", utils.ShellSingleQuote(cur)),
			layerLimit,
		)
		backing := strings.TrimSpace(res.Stdout)
		if backing == "" || backing == cur {
			break
		}
		if !filepath.IsAbs(backing) {
			// 相对 backing 路径相对其父文件所在目录解析
			backing = filepath.Join(filepath.Dir(cur), backing)
		}
		backing = filepath.Clean(backing)
		result = append(result, backing)
		cur = backing
	}
	return result
}

// ==================== 虚拟机生命周期操作 ====================

// StartVM 启动虚拟机
func StartVM(name string) error {
	return withVMPowerLock(name, func() error {
		return startVM(name, true)
	})
}

// StartVMPreserveRebootAction 启动虚拟机，但保留当前 on_reboot 策略。
func StartVMPreserveRebootAction(name string) error {
	return withVMPowerLock(name, func() error {
		return startVM(name, false)
	})
}

func applyVMRuntimeNetworkState(name string) error {
	if err := D.ApplyVPCBindingRuntime(name); err != nil {
		return fmt.Errorf("应用 VPC 网络失败: %w", err)
	}
	if D.IsLightweightCloudVM(name) {
		if err := D.ApplyLightweightVMBandwidth(name); err != nil {
			return fmt.Errorf("应用轻量云带宽失败: %w", err)
		}
	} else {
		if err := D.ReapplyConfiguredVMBandwidth(name); err != nil {
			return fmt.Errorf("刷新虚拟机带宽失败: %w", err)
		}
	}
	if D.IsPortSecurityEnabled != nil && D.IsPortSecurityEnabled() && D.ReconcileVMPortSecurity != nil {
		if err := D.ReconcileVMPortSecurity(name); err != nil {
			return fmt.Errorf("应用端口安全策略失败: %w", err)
		}
	}
	return nil
}

func syncVMInactiveVPCBindingBeforeStart(name string) error {
	if D == nil || D.ApplyVPCBindingToDomainXML == nil {
		return nil
	}
	xmlContent, err := GetVMInactiveDomainXML(name)
	if err != nil {
		return err
	}
	updatedXML, hasVPCBinding, err := D.ApplyVPCBindingToDomainXML(name, xmlContent)
	if err != nil {
		return fmt.Errorf("同步 VPC 网络配置失败: %w", err)
	}
	if !hasVPCBinding || strings.TrimSpace(updatedXML) == strings.TrimSpace(xmlContent) {
		return nil
	}
	if err := SetVMInactiveDomainXML(name, updatedXML); err != nil {
		return fmt.Errorf("保存 VPC 网络配置失败: %w", err)
	}
	logger.App.Info("启动前已同步虚拟机 VPC 网络配置", "vm", name)
	return nil
}

func startVM(name string, fixOnReboot bool) error {
	if err := D.HookEnsureVMNotMigrating(name, "开机"); err != nil {
		return err
	}
	if owner := D.FindVMOwner(name); owner != "" && owner != "admin" {
		if D.IsLightweightCloudUser(owner) {
			if err := D.CheckLightweightVMRuntimeQuotaAvailable(name); err != nil {
				return err
			}
		} else {
			if err := D.CheckQuotaForStart(owner, name); err != nil {
				return err
			}
		}
	}
	if err := D.EnsureMaintenanceModeDisabled("启动虚拟机"); err != nil {
		return err
	}
	if err := D.EnsureOVSNetworkReady(); err != nil {
		return err
	}

	if fixOnReboot {
		// 启动前自动修复 on_reboot 配置
		FixOnReboot(name)
	}

	// 检测当前状态
	stateResult := utils.ExecCommand("virsh", "domstate", name)
	if stateResult.Error == nil {
		state := strings.TrimSpace(stateResult.Stdout)
		switch state {
		case "running":
			return fmt.Errorf("虚拟机 %s 已在运行中", name)
		case "paused":
			if isQEMUInternalErrorPaused(name) {
				return fmt.Errorf("虚拟机处于 QEMU 内部错误暂停，当前状态不能继续启动；请先执行重置或强制断电后重新开机。如果重置后仍反复进入该状态，请检查宿主机 KVM/嵌套虚拟化能力和 QEMU 日志")
			}
			// 防护开启时先在暂停状态完成策略安装，避免恢复瞬间出现未保护端口。
			if D.IsPortSecurityEnabled != nil && D.IsPortSecurityEnabled() {
				if err := applyVMRuntimeNetworkState(name); err != nil {
					return fmt.Errorf("虚拟机保持暂停，%w", err)
				}
			}
			if libvirt_rpc.IsLibvirtRPCAvailable() {
				err := libvirt_rpc.ResumeDomainRPC(name)
				if err == nil {
					UpdateVMRuntimeState(name, "running", time.Now())
					if err := applyVMRuntimeNetworkState(name); err != nil {
						return fmt.Errorf("恢复运行成功，但%w", err)
					}
					return nil
				}
				logger.Libvirt.Warn("恢复虚拟机失败，降级为 virsh", "domain", name, "error", err)
			}
			result := utils.ExecCommand("virsh", "resume", name)
			if result.Error != nil {
				return formatResumeError(name, result.Stderr)
			}
			UpdateVMRuntimeState(name, "running", time.Now())
			if err := applyVMRuntimeNetworkState(name); err != nil {
				return fmt.Errorf("恢复运行成功，但%w", err)
			}
			return nil
		case "crashed", "pmsuspended":
			logger.App.Warn("虚拟机处于异常状态，尝试强制关闭后重启", "vm", name, "state", state)
			utils.ExecCommand("virsh", "destroy", name)
		}
	}

	// 启动前清理不完整的 backingStore XML（防止 AppArmor 拦截 backing chain 访问）
	fixBackingStoreXML(name)
	// SELinux Enforcing 下对磁盘/backing 镜像打标兜底（模板文件可能为 usr_t）
	ensureVMSELinuxImageLabels(name)
	if err := syncVMInactiveVPCBindingBeforeStart(name); err != nil {
		return err
	}

	// 检查UEFI NVRAM文件是否存在（如果虚拟机配置了UEFI启动）
	if libvirt_rpc.IsLibvirtRPCAvailable() {
		vmXML, getErr := libvirt_rpc.GetDomainXMLRPC(name, 0)
		if getErr == nil {
			bootType := vm_xml.ParseVMBootTypeFromDomainXML(vmXML)
			if bootType == vm_xml.VMBootTypeUEFI || bootType == vm_xml.VMBootTypeUEFISecure {
				nvramPath := vm_xml.GetVMNVRAMPath(name)
				if _, err := os.Stat(nvramPath); os.IsNotExist(err) {
					return fmt.Errorf("UEFI NVRAM文件不存在: %s, 请检查虚拟机UEFI配置", nvramPath)
				}
			}
		}
	}

	freeze, err := GetVMFreeze(name)
	if err != nil {
		return err
	}

	protectedStart := D.IsPortSecurityEnabled != nil && D.IsPortSecurityEnabled()
	startPaused := freeze || protectedStart
	startArgs := []string{"start", name}
	statusAfterStart := "running"
	if startPaused {
		startArgs = append(startArgs, "--paused")
		statusAfterStart = "paused"
	}

// 链接克隆的 backing 模板在导入时被置为 immutable（chattr +i）。libvirt 每次
	// 启动都会对共享 backing 镜像无条件执行 setfilecon 打标（virt_content_t），
	// immutable 文件连 root 也无法改 xattr，SELinux Enforcing 下报
	// "Operation not permitted" 导致启动失败。此处临时解除磁盘镜像的 immutable，
	// 启动成功后再恢复，保护语义不变。
	startErr := withVMDiskImagesUnlocked(name, func() error {
		started := false
		if libvirt_rpc.IsLibvirtRPCAvailable() {
			var startErr error
			if startPaused {
				startErr = libvirt_rpc.StartDomainPausedRPC(name)
			} else {
				startErr = libvirt_rpc.StartDomainRPC(name)
			}
			if startErr == nil {
				started = true
			} else {
				logger.Libvirt.Warn("启动虚拟机失败，降级为 virsh", "domain", name, "error", startErr)
			}
		}
		if !started {
			result := utils.ExecCommand("virsh", startArgs...)
			if result.Error != nil {
				// 检查是否是权限问题，自动修复后重试一次
				if strings.Contains(result.Stderr, "Permission denied") {
					D.FixSnapshotDiskPermissions(name)
					retryResult := utils.ExecCommand("virsh", startArgs...)
					if retryResult.Error != nil {
						return fmt.Errorf("启动虚拟机失败: %s", retryResult.Stderr)
					}
				} else {
					return fmt.Errorf("启动虚拟机失败: %s", result.Stderr)
				}
			}
		}
		return nil
	})
	if startErr != nil {
		return startErr
	}
	UpdateVMRuntimeState(name, statusAfterStart, time.Now())
	if err := applyVMRuntimeNetworkState(name); err != nil {
		if protectedStart {
			return fmt.Errorf("虚拟机已暂停启动，%w", err)
		}
		return fmt.Errorf("启动成功，但%w", err)
	}
	if protectedStart && !freeze {
		if libvirt_rpc.IsLibvirtRPCAvailable() {
			if err := libvirt_rpc.ResumeDomainRPC(name); err != nil {
				return fmt.Errorf("端口安全策略已安装，但恢复虚拟机运行失败: %w", err)
			}
		} else {
			result := utils.ExecCommand("virsh", "resume", name)
			if result.Error != nil {
				return formatResumeError(name, result.Stderr)
			}
		}
		UpdateVMRuntimeState(name, "running", time.Now())
	}
	return nil
}

func isQEMUInternalErrorPaused(name string) bool {
	status := getQEMUMonitorStatus(name)
	return strings.Contains(strings.ToLower(status), "internal-error")
}

func getQEMUMonitorStatus(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	result := utils.ExecCommand("virsh", "qemu-monitor-command", name, "--hmp", "info status")
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
}

func formatResumeError(name, stderr string) error {
	message := strings.TrimSpace(stderr)
	if message == "" {
		message = "未知错误"
	}
	lower := strings.ToLower(message + "\n" + getQEMUMonitorStatus(name))
	if strings.Contains(lower, "resetting the virtual machine is required") || strings.Contains(lower, "internal-error") {
		return fmt.Errorf("恢复运行失败: 虚拟机处于 QEMU 内部错误暂停，当前状态不能继续启动；请先执行重置或强制断电后重新开机。如果重置后仍反复进入该状态，请检查宿主机 KVM/嵌套虚拟化能力和 QEMU 日志。原始错误: %s", message)
	}
	return fmt.Errorf("恢复运行失败: %s", message)
}

// fixBackingStoreXML 清理 VM XML 中不完整的 backingStore 标签
// 外部快照创建后 libvirt 会写入部分 backingStore 信息，但 backing chain 可能更深，
// 导致 virt-aa-helper 无法为完整 backing chain 生成 AppArmor 权限，开机时报 Permission denied
func fixBackingStoreXML(vmName string) {
	dumpResult := utils.ExecCommand("virsh", "dumpxml", vmName, "--inactive")
	if dumpResult.Error != nil {
		return
	}
	if !strings.Contains(dumpResult.Stdout, "<backingStore") {
		return
	}
	shellCmd := fmt.Sprintf("EDITOR=\"sed -i '/<backingStore type/,/<\\/backingStore>/d'\" virsh edit %s", utils.ShellSingleQuote(vmName))
	utils.ExecShell(shellCmd)
}

// ShutdownVM 正常关机
func ShutdownVM(name string) error {
	return withVMPowerLock(name, func() error {
		if err := D.HookEnsureVMNotMigrating(name, "关机"); err != nil {
			return err
		}
		if err := libvirt_rpc.ShutdownDomainRPC(name); err != nil {
			return fmt.Errorf("关机失败: %w", err)
		}
		return nil
	})
}

// DestroyVM 强制断电
func DestroyVM(name string) error {
	return withVMPowerLock(name, func() error {
		if err := D.HookEnsureVMNotMigrating(name, "强制断电"); err != nil {
			return err
		}
		if err := libvirt_rpc.DestroyDomainRPC(name); err != nil {
			return fmt.Errorf("强制断电失败: %w", err)
		}
		UpdateVMRuntimeState(name, "shut off", time.Now())
		return nil
	})
}

// RebootVM 重启虚拟机
func RebootVM(name string) error {
	return withVMPowerLock(name, func() error {
		if err := D.HookEnsureVMNotMigrating(name, "重启"); err != nil {
			return err
		}
		if err := D.EnsureMaintenanceModeDisabled("重启虚拟机"); err != nil {
			return err
		}

		// 先修复 on_reboot 配置（Cockpit/virt-install 默认 destroy 导致重启变关机）
		FixOnReboot(name)

		if err := libvirt_rpc.RebootDomainRPC(name); err != nil {
			return fmt.Errorf("重启失败: %w", err)
		}
		ResetVMContinuousRuntime(name, time.Now())
		if err := applyVMRuntimeNetworkState(name); err != nil {
			return fmt.Errorf("重启成功，但%w", err)
		}
		return nil
	})
}

// ResetVM 硬重置虚拟机，适用于 QEMU internal-error 暂停等无法 resume 的状态。
func ResetVM(name string) error {
	return withVMPowerLock(name, func() error {
		if err := D.HookEnsureVMNotMigrating(name, "重置"); err != nil {
			return err
		}
		if err := D.EnsureMaintenanceModeDisabled("重置虚拟机"); err != nil {
			return err
		}
		FixOnReboot(name)
		if err := libvirt_rpc.ResetDomainRPC(name); err != nil {
			return fmt.Errorf("重置失败: %w", err)
		}
		ResetVMContinuousRuntime(name, time.Now())
		if err := applyVMRuntimeNetworkState(name); err != nil {
			return fmt.Errorf("重置成功，但%w", err)
		}
		return nil
	})
}

// FixOnReboot 修复虚拟机的 on_reboot 配置（destroy → restart）
func FixOnReboot(name string) {
	xmlPath := fmt.Sprintf("/etc/libvirt/qemu/%s.xml", name)
	// 检查是否需要修复
	content, err := os.ReadFile(xmlPath)
	if err != nil {
		return // 文件不存在或无法读取，不需要修复
	}
	if !strings.Contains(string(content), "<on_reboot>destroy</on_reboot>") {
		return // 不需要修复
	}
	// 直接替换 XML 文件内容
	newContent := strings.Replace(string(content), "<on_reboot>destroy</on_reboot>", "<on_reboot>restart</on_reboot>", 1)
	if err := os.WriteFile(xmlPath, []byte(newContent), 0644); err != nil {
		logger.App.Warn("修复 on_reboot 配置失败", "vm", name, "error", err)
		return
	}
	// 重载 libvirtd 使配置生效
	utils.ExecCommand("systemctl", "reload", "libvirtd")
}
