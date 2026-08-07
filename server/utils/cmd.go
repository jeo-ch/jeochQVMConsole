package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"kvm_console/logger"
)

// SafeGo 启动带 panic recovery 的 goroutine
func SafeGo(fn func()) {
	go func() {
		defer RecoverAndLog("goroutine")
		fn()
	}()
}

// RecoverAndLog 在 defer 中调用，捕获 panic 并记录错误日志
func RecoverAndLog(scope string) {
	if r := recover(); r != nil {
		logger.App.Error("panic recovered",
			"scope", scope,
			"panic", fmt.Sprintf("%v", r),
			"stack", string(debug.Stack()),
		)
	}
}

// CmdResult 命令执行结果
type CmdResult struct {
	Stdout   string // 标准输出
	Stderr   string // 标准错误
	ExitCode int    // 退出码
	Error    error  // 错误信息
}

// ── 命令路径缓存（单一来源，§2.3 评审：firewall/diagnostics 共用，避免每次探测重复 LookPath） ──

var (
	cmdPathMu    sync.RWMutex
	cmdPathCache = map[string]string{}
)

// LookupCmdPath 解析并缓存命令绝对路径；解析失败返回原名（由执行结果判定可用性）。
// 缓存命中后不再走 PATH 查找，也避免路径解析结果被外部 PATH 变动影响（防 #N 类劫持）。
func LookupCmdPath(name string) string {
	if name == "" {
		return name
	}
	cmdPathMu.RLock()
	path, ok := cmdPathCache[name]
	cmdPathMu.RUnlock()
	if ok {
		return path
	}
	resolved, err := exec.LookPath(name)
	if err != nil {
		return name
	}
	cmdPathMu.Lock()
	cmdPathCache[name] = resolved
	cmdPathMu.Unlock()
	return resolved
}

// DetectGlibcVersion 探测宿主机 glibc 版本（单一来源，firewall/advice.go 与
// diagnostics/component_health.go 共用，与 install.sh 同口径）：
//  1. ldd --version 首行最后一个 token
//  2. 回退 getconf GNU_LIBC_VERSION 第二个字段
//  3. 失败返回空串
func DetectGlibcVersion() string {
	if result := ExecCommandQuietWithTimeout("ldd", 5*time.Second, "--version"); result.Error == nil {
		first := strings.TrimSpace(strings.SplitN(result.Stdout, "\n", 2)[0])
		fields := strings.Fields(first)
		if len(fields) > 0 {
			if v := strings.TrimSpace(fields[len(fields)-1]); validGlibcToken(v) {
				return v
			}
		}
	}
	if result := ExecCommandQuietWithTimeout("getconf", 5*time.Second, "GNU_LIBC_VERSION"); result.Error == nil {
		fields := strings.Fields(result.Stdout)
		if len(fields) >= 2 {
			if v := strings.TrimSpace(fields[1]); validGlibcToken(v) {
				return v
			}
		}
	}
	return ""
}

func validGlibcToken(token string) bool {
	if token == "" {
		return false
	}
	for _, ch := range token {
		if !strings.ContainsRune("0123456789.", ch) {
			return false
		}
	}
	return strings.Contains(token, ".")
}

// ValidGlibcToken 判断 token 是否为合法 glibc 版本串（仅数字与点、含至少一个点）。
// 供 native-glibc.txt 等配置读取处复用（与 DetectGlibcVersion 同口径）。
func ValidGlibcToken(token string) bool {
	return validGlibcToken(token)
}

// ExecCommand 执行系统命令
func ExecCommand(name string, args ...string) *CmdResult {
	return ExecCommandWithTimeout(name, 30*time.Second, args...)
}

// ExecCommandWithTimeout 执行系统命令（带超时）
func ExecCommandWithTimeout(name string, timeout time.Duration, args ...string) *CmdResult {
	return ExecCommandContextWithTimeout(context.Background(), name, timeout, args...)
}

// ExecCommandContextWithTimeout 执行系统命令（支持取消和超时）
func ExecCommandContextWithTimeout(ctx context.Context, name string, timeout time.Duration, args ...string) *CmdResult {
	return execCommandContextWithTimeout(ctx, name, timeout, false, args...)
}

// buildCmdEnv 构造命令执行环境：剔除父进程继承的本地化变量后强制 C 语言环境。
// exec 直接传递 envp（不经 shell），子进程 getenv 取"首个"匹配项，若仅 append 覆盖值，
// 会被父进程原有的 LANG/LC_* 变量遮蔽，导致 virsh 等命令仍输出本地化错误文本
// （实测 openEuler zh_CN 下 `virsh metadata` 返回"未找到元数据：所需元数据元素未出现"，
// 使只匹配英文 "metadata not found" 的调用方把"无元数据"误判为硬错误）。
func buildCmdEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if strings.HasPrefix(key, "LC_") || key == "LANG" || key == "LANGUAGE" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "LANG=C", "LC_ALL=C")
}

func execCommandContextWithTimeout(ctx context.Context, name string, timeout time.Duration, sensitive bool, args ...string) *CmdResult {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.Command(name, args...)
	// 强制使用 C 语言环境，确保 virsh 等命令输出英文便于解析（剔除父进程本地化变量后追加）
	cmd.Env = buildCmdEnv()
	prepareProcessGroup(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	argsStr := strings.Join(args, " ")
	if sensitive {
		argsStr = "[敏感参数已隐藏]"
	}
	logger.CMD.Info("执行命令", "cmd", name, "args", argsStr)

	start := time.Now()

	// 启动命令
	if err := cmd.Start(); err != nil {
		logger.CMD.Error("命令启动失败", "cmd", name, "args", argsStr, "error", err)
		return &CmdResult{
			Stderr:   err.Error(),
			ExitCode: -1,
			Error:    fmt.Errorf("启动命令失败: %w", err),
		}
	}

	// 超时控制。timeout 小于等于 0 时仅响应上下文取消，不自动终止命令。
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	var timeoutCh <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case err := <-done:
		elapsed := time.Since(start)
		result := &CmdResult{
			Stdout: strings.TrimSpace(stdout.String()),
			Stderr: strings.TrimSpace(stderr.String()),
		}
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitErr.ExitCode()
			} else {
				result.ExitCode = -1
			}
			result.Error = fmt.Errorf("命令执行失败: %w, stderr: %s", err, result.Stderr)
			logger.CMD.Error("命令执行失败", "cmd", name, "args", argsStr, "exit_code", result.ExitCode, "error", result.Error, "stderr", truncate(result.Stderr, 500), "duration", elapsed.String())
		} else {
			logger.CMD.Info("命令执行完成", "cmd", name, "args", argsStr, "exit_code", result.ExitCode, "duration", elapsed.String())
		}
		return result

	case <-timeoutCh:
		killProcessTree(cmd)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		logger.CMD.Error("命令执行超时", "cmd", name, "args", argsStr, "timeout", timeout.String())
		return &CmdResult{
			Stderr:   "命令执行超时",
			ExitCode: -1,
			Error:    fmt.Errorf("命令执行超时: %s", name),
		}

	case <-ctx.Done():
		killProcessTree(cmd)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		logger.CMD.Warn("命令已取消", "cmd", name, "args", argsStr, "reason", ctx.Err())
		return &CmdResult{
			Stderr:   "命令已取消",
			ExitCode: -1,
			Error:    fmt.Errorf("命令已取消: %s: %w", name, ctx.Err()),
		}
	}
}

// ExecCommandLongRunning 执行长时间运行的命令（超时 10 分钟）
func ExecCommandLongRunning(name string, args ...string) *CmdResult {
	return ExecCommandWithTimeout(name, 10*time.Minute, args...)
}

// ExecCommandSensitiveLongRunning 执行包含密码、令牌等敏感参数的长任务，日志不记录参数正文。
func ExecCommandSensitiveLongRunning(name string, args ...string) *CmdResult {
	return execCommandContextWithTimeout(context.Background(), name, 10*time.Minute, true, args...)
}

// ExecShell 执行 Shell 命令（通过 bash -c）
func ExecShell(command string) *CmdResult {
	return ExecCommand("bash", "-c", command)
}

// ExecShellWithTimeout 执行 Shell 命令（带超时）
func ExecShellWithTimeout(command string, timeout time.Duration) *CmdResult {
	return ExecCommandWithTimeout("bash", timeout, "-c", command)
}

// ExecShellContext 执行 Shell 命令，仅响应上下文取消，不设置自动超时。
func ExecShellContext(ctx context.Context, command string) *CmdResult {
	return ExecCommandContextWithTimeout(ctx, "bash", 0, "-c", command)
}

// ExecShellContextWithTimeout 执行 Shell 命令（支持取消和超时）
func ExecShellContextWithTimeout(ctx context.Context, command string, timeout time.Duration) *CmdResult {
	return ExecCommandContextWithTimeout(ctx, "bash", timeout, "-c", command)
}

// ── Quiet 变体：非零退出码仅记录 DEBUG 日志（适用于预期可能失败的查询/清理命令）──

// ExecCommandQuiet 与 ExecCommand 相同，但非零退出码仅记录 DEBUG
func ExecCommandQuiet(name string, args ...string) *CmdResult {
	return execCommandWithLogLevel(name, logger.CMD.Debug, 30*time.Second, args...)
}

// ExecCommandQuietWithTimeout 与 ExecCommandWithTimeout 相同，但非零退出码仅记录 DEBUG
func ExecCommandQuietWithTimeout(name string, timeout time.Duration, args ...string) *CmdResult {
	return execCommandWithLogLevel(name, logger.CMD.Debug, timeout, args...)
}

// ExecShellQuiet 与 ExecShell 相同，但非零退出码仅记录 DEBUG
func ExecShellQuiet(command string) *CmdResult {
	return ExecCommandQuiet("bash", "-c", command)
}

// ExecShellQuietWithTimeout 与 ExecShellQuiet 相同，但带命令超时。
func ExecShellQuietWithTimeout(command string, timeout time.Duration) *CmdResult {
	return ExecCommandQuietWithTimeout("bash", timeout, "-c", command)
}

// execCommandWithLogLevel 执行命令，使用指定日志级别记录非零退出码
func execCommandWithLogLevel(name string, logFn func(string, ...any), timeout time.Duration, args ...string) *CmdResult {
	cmd := exec.Command(name, args...)
	cmd.Env = buildCmdEnv()
	prepareProcessGroup(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	argsStr := strings.Join(args, " ")
	logger.CMD.Info("执行命令", "cmd", name, "args", argsStr)

	start := time.Now()

	if err := cmd.Start(); err != nil {
		logFn("命令启动失败", "cmd", name, "args", argsStr, "error", err)
		return &CmdResult{
			Stderr:   err.Error(),
			ExitCode: -1,
			Error:    fmt.Errorf("启动命令失败: %w", err),
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// 与 execCommandContextWithTimeout 对齐（#A5）：timeout <= 0 时不自动终止命令，
	// 避免零/负超时导致命令立即被杀（此变体无 context，timeout<=0 视为不限时）。
	var timeoutCh <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case err := <-done:
		elapsed := time.Since(start)
		result := &CmdResult{
			Stdout: strings.TrimSpace(stdout.String()),
			Stderr: strings.TrimSpace(stderr.String()),
		}
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitErr.ExitCode()
			} else {
				result.ExitCode = -1
			}
			result.Error = fmt.Errorf("命令执行失败: %w, stderr: %s", err, result.Stderr)
			// 使用调用方指定的日志级别（DEBUG 而非 ERROR）
			logFn("命令执行失败", "cmd", name, "args", argsStr, "exit_code", result.ExitCode, "error", result.Error, "stderr", truncate(result.Stderr, 500), "duration", elapsed.String())
		} else {
			logger.CMD.Info("命令执行完成", "cmd", name, "args", argsStr, "exit_code", result.ExitCode, "duration", elapsed.String())
		}
		return result

	case <-timeoutCh:
		killProcessTree(cmd)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		logFn("命令执行超时", "cmd", name, "args", argsStr, "timeout", timeout.String())
		return &CmdResult{
			Stderr:   "命令执行超时",
			ExitCode: -1,
			Error:    fmt.Errorf("命令执行超时: %s %s", name, strings.Join(args, " ")),
		}
	}
}

// ShellSingleQuote 对 shell 参数做单引号转义，防止命令注入。
// 将单引号替换为 '"'"'（结束引号、转义单引号、开始引号），
// 使参数在 shell 单引号上下文中安全使用。
func ShellSingleQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

var lsblkMountpointsColumnVal = ""

// LsblkMountpointsColumn 返回 lsblk 挂载点列名。
// util-linux >= 2.37 提供 MOUNTPOINTS（数组型），旧版（麒麟 V10 util-linux 2.33 等）仅支持 MOUNTPOINT（单数），
// 直接用 MOUNTPOINTS 会报 "lsblk: unknown column: MOUNTPOINTS"。按版本动态选择，结果缓存。
func LsblkMountpointsColumn() string {
	if lsblkMountpointsColumnVal != "" {
		return lsblkMountpointsColumnVal
	}
	col := "MOUNTPOINT"
	res := ExecCommand("lsblk", "--version")
	if res.Error == nil {
		ver := strings.TrimSpace(res.Stdout)
		// 形如: lsblk from util-linux 2.37.2
		if i := strings.Index(ver, "util-linux"); i >= 0 {
			fields := strings.Fields(ver[i+len("util-linux"):])
			if len(fields) > 0 && versionAtLeast(fields[0], "2.37") {
				col = "MOUNTPOINTS"
			}
		}
	}
	lsblkMountpointsColumnVal = col
	return col
}

// versionAtLeast 比较 "a.b.c" 版本是否 >= "min"，点分数字逐段比较。
func versionAtLeast(v, min string) bool {
	vParts := strings.Split(strings.TrimSpace(v), ".")
	minParts := strings.Split(strings.TrimSpace(min), ".")
	for i := 0; i < len(minParts); i++ {
		vp, mp := 0, 0
		if i < len(vParts) {
			vp, _ = strconv.Atoi(vParts[i])
		}
		mp, _ = strconv.Atoi(minParts[i])
		if vp != mp {
			return vp > mp
		}
	}
	return true
}

// truncate 截断字符串到指定长度，超过部分用 "..." 替代
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
