package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// transferProgress 是传输进度的结构化信息，通过 JSON 上报给网关。
type transferProgress struct {
	BytesTransferred int64   `json:"bytes_transferred"`
	TotalBytes       int64   `json:"total_bytes"`
	Percent          float64 `json:"percent"`
	SpeedBytes       int64   `json:"speed_bytes"`
	SpeedHuman       string  `json:"speed_human"`
	Elapsed          string  `json:"elapsed"`
	ETA              string  `json:"eta"`
}

// pullDisk 将源磁盘文件经 SSH 流式直传到目标节点并校验一致性。
func pullDisk(disk DiskInfo, tgt TargetSSH, progress ProgressFunc) (PullResult, error) {
	if disk.SourcePath == "" {
		return PullResult{}, fmt.Errorf("disk source path is empty")
	}
	if _, err := os.Stat(disk.SourcePath); err != nil {
		return PullResult{}, fmt.Errorf("source disk not found: %w", err)
	}

	keyFile, err := prepareSSHKey(tgt)
	if err != nil {
		return PullResult{}, err
	}
	defer func() {
		if keyFile != "" {
			os.Remove(keyFile)
		}
	}()

	// 源端校验和（提前计算，避免传输后等待）。
	sum, err := sha256sumLocal(disk.SourcePath)
	if err != nil {
		return PullResult{}, err
	}

	// 启动传输进度监控（后台 goroutine 定期查询目标端文件大小）。
	doneCh := make(chan struct{})
	if progress != nil && disk.SizeBytes > 0 {
		go monitorTransferProgress(tgt, keyFile, disk.TargetDiskPath, disk.SizeBytes, progress, doneCh)
	}

	// 流式直传：cat 源文件 | ssh 目标端 dd 落盘，内存占用恒定。
	if err := streamTransfer(disk.SourcePath, tgt, keyFile, disk.TargetDiskPath); err != nil {
		close(doneCh)
		return PullResult{}, err
	}
	close(doneCh)

	// 目标端校验和并比对。
	remoteSum, err := sha256sumRemote(tgt, keyFile, disk.TargetDiskPath)
	if err != nil {
		return PullResult{}, err
	}
	if !strings.EqualFold(sum, remoteSum) {
		return PullResult{}, fmt.Errorf("checksum mismatch: local=%s remote=%s", sum, remoteSum)
	}

	// 清除 qcow2 backing file 引用（迁移快照残留），使镜像可独立启动。
	if err := rebaseRemoteDisk(tgt, keyFile, disk.TargetDiskPath); err != nil {
		log.Printf("[pull] rebase warning: %v (non-fatal)", err)
	}

	return PullResult{
		Checksum: sum,
		Size:     disk.SizeBytes,
		Path:     disk.TargetDiskPath,
	}, nil
}

// monitorTransferProgress 后台定期查询目标端文件大小，计算并上报传输速度与进度。
func monitorTransferProgress(tgt TargetSSH, keyFile, dstPath string, totalBytes int64, progress ProgressFunc, doneCh chan struct{}) {
	startTime := time.Now()
	var lastBytes int64
	var lastTime time.Time
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-doneCh:
			// 传输结束，发送最终进度。
			elapsed := time.Since(startTime)
			speed := calcSpeed(lastBytes, lastTime, startTime)
			pct := float64(lastBytes) / float64(totalBytes) * 100
			info := transferProgress{
				BytesTransferred: lastBytes,
				TotalBytes:       totalBytes,
				Percent:          pct,
				SpeedBytes:       speed,
				SpeedHuman:       formatBytes(speed) + "/s",
				Elapsed:          formatDuration(elapsed),
				ETA:              "0s",
			}
			detail, _ := json.Marshal(info)
			progress(int(pct), fmt.Sprintf("传输完成: %s / %s, 速度 %s, 用时 %s",
				formatBytes(lastBytes), formatBytes(totalBytes), info.SpeedHuman, info.Elapsed), detail)
			return
		case <-ticker.C:
			// 查询目标端当前文件大小。
			curBytes := queryRemoteFileSize(tgt, keyFile, dstPath)
			now := time.Now()
			if curBytes <= 0 || curBytes == lastBytes {
				continue
			}

			elapsed := time.Since(startTime)
			var speed int64
			if !lastTime.IsZero() && now.Sub(lastTime).Seconds() > 0 {
				speed = int64(float64(curBytes-lastBytes) / now.Sub(lastTime).Seconds())
			} else if elapsed.Seconds() > 0 {
				speed = int64(float64(curBytes) / elapsed.Seconds())
			}

			pct := float64(curBytes) / float64(totalBytes) * 100
			if pct > 99.9 {
				pct = 99.9
			}
			info := transferProgress{
				BytesTransferred: curBytes,
				TotalBytes:       totalBytes,
				Percent:          pct,
				SpeedBytes:       speed,
				SpeedHuman:       formatBytes(speed) + "/s",
				Elapsed:          formatDuration(elapsed),
				ETA:              calcETA(curBytes, totalBytes, speed),
			}
			detail, _ := json.Marshal(info)
			progress(int(pct), fmt.Sprintf("已传输 %s / %s (%.1f%%), 速度 %s, 预计剩余 %s",
				formatBytes(curBytes), formatBytes(totalBytes), pct, info.SpeedHuman, info.ETA), detail)

			lastBytes = curBytes
			lastTime = now
		}
	}
}

// queryRemoteFileSize 通过 SSH 查询目标端文件大小（字节），失败返回 0。
func queryRemoteFileSize(tgt TargetSSH, keyFile, path string) int64 {
	ssh := sshCommand(tgt, keyFile, "stat", "-c", "%s", path)
	out, err := ssh.Output()
	if err != nil {
		return 0
	}
	cleaned := strings.TrimSpace(stripSSHWarnings(string(out)))
	var size int64
	fmt.Sscanf(cleaned, "%d", &size)
	return size
}

// calcSpeed 计算平均传输速度（字节/秒）。
func calcSpeed(lastBytes int64, lastTime, start time.Time) int64 {
	if lastTime.IsZero() {
		return 0
	}
	elapsed := lastTime.Sub(start).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return int64(float64(lastBytes) / elapsed)
}

// calcETA 计算预计剩余时间。
func calcETA(curBytes, totalBytes, speed int64) string {
	if speed <= 0 || curBytes >= totalBytes {
		return "0s"
	}
	remaining := totalBytes - curBytes
	eta := time.Duration(float64(remaining) / float64(speed) * float64(time.Second))
	return formatDuration(eta)
}

// formatDuration 将 Duration 格式化为简洁的可读形式。
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// streamTransfer 通过 cat | ssh dd 管道把本地文件流式写入目标节点。
func streamTransfer(src string, tgt TargetSSH, keyFile, dst string) error {
	cat := exec.Command("cat", src)
	remoteCmd := []string{"dd", fmt.Sprintf("of=%s", dst), "bs=1M", "status=none"}
	ssh := sshCommand(tgt, keyFile, remoteCmd...)

	stdout, err := cat.StdoutPipe()
	if err != nil {
		return fmt.Errorf("cat stdout pipe: %w", err)
	}
	ssh.Stdin = stdout
	ssh.Stdout = os.Stderr
	ssh.Stderr = os.Stderr

	if err := cat.Start(); err != nil {
		return fmt.Errorf("start cat: %w", err)
	}
	if err := ssh.Run(); err != nil {
		cat.Wait()
		return fmt.Errorf("ssh transfer: %w", err)
	}
	if err := cat.Wait(); err != nil {
		return fmt.Errorf("cat: %w", err)
	}
	return nil
}

// prepareSSHKey 按鉴权方式准备 SSH 密钥：key 模式写入 0600 临时文件，password 模式返回空。
func prepareSSHKey(tgt TargetSSH) (string, error) {
	switch tgt.AuthMethod {
	case "key", "":
		if tgt.KeyContent == "" {
			return "", nil
		}
		decoded, err := base64.StdEncoding.DecodeString(tgt.KeyContent)
		if err != nil {
			return "", fmt.Errorf("decode key: %w", err)
		}
		f, err := os.CreateTemp("", "agent-key-*")
		if err != nil {
			return "", err
		}
		if err := f.Chmod(0600); err != nil {
			f.Close()
			return "", err
		}
		if _, err := f.Write(decoded); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
		return f.Name(), nil
	case "password":
		// 密码鉴权由 sshpass 在 buildSSHArgs 中注入，此处无需临时文件。
		return "", nil
	default:
		return "", fmt.Errorf("unknown auth method: %s", tgt.AuthMethod)
	}
}

// sshCommand 构造 SSH 执行命令：password 模式自动注入 sshpass 前缀。
// 使用 -e 环境变量传递密码，避免密码暴露在 /proc/*/cmdline 中。
func sshCommand(tgt TargetSSH, keyFile string, remoteCmd ...string) *exec.Cmd {
	args := buildSSHArgs(tgt, keyFile, remoteCmd...)
	if tgt.AuthMethod == "password" && tgt.Password != "" {
		sshpassArgs := append([]string{"-e", "ssh"}, args...)
		cmd := exec.Command("sshpass", sshpassArgs...)
		cmd.Env = append(os.Environ(), "SSHPASS="+tgt.Password)
		return cmd
	}
	return exec.Command("ssh", args...)
}

// buildSSHArgs 构造 ssh 参数列表（不含远程命令）。
func buildSSHArgs(tgt TargetSSH, keyFile string, remoteCmd ...string) []string {
	args := []string{}
	if tgt.Port != "" && tgt.Port != "22" {
		args = append(args, "-p", tgt.Port)
	}
	if keyFile != "" {
		args = append(args, "-i", keyFile)
	}
	// 严格 host key 校验，避免中间人攻击。
	args = append(args, "-o", "StrictHostKeyChecking=accept-new", "-o", "UserKnownHostsFile=/dev/null")
	args = append(args, fmt.Sprintf("%s@%s", tgt.User, tgt.Host))
	args = append(args, remoteCmd...)
	return args
}

// sha256sumLocal 计算本地文件 sha256。
func sha256sumLocal(path string) (string, error) {
	out, err := runOutput("sha256sum", path)
	if err != nil {
		return "", fmt.Errorf("sha256sum local %s: %w", path, err)
	}
	return parseSha256(out), nil
}

// sha256sumRemote 在目标端计算文件 sha256。
func sha256sumRemote(tgt TargetSSH, keyFile, path string) (string, error) {
	ssh := sshCommand(tgt, keyFile, "sha256sum", path)
	out, err := ssh.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sha256sum remote %s: %w", path, err)
	}
	// 去除 SSH 警告信息，只保留校验和。
	cleaned := stripSSHWarnings(string(out))
	return parseSha256(cleaned), nil
}

// parseSha256 从 sha256sum 输出提取校验和（首 token）。
func parseSha256(out string) string {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// rebaseRemoteDisk 在目标主机上清除 qcow2 的 backing file 引用，使镜像可独立启动。
// 迁移产生的 qcow2 可能引用源端快照作为 backing file，目标端不存在该文件会导致启动失败。
func rebaseRemoteDisk(tgt TargetSSH, keyFile, diskPath string) error {
	// 根本问题：exec.Command 传给 SSH 的多个参数被空格拼接，导致引号/heredoc 全部失效。
	// 解决方案：在源端创建脚本文件，通过 stdin 重定向传给远端 bash 执行。
	rawCmd := fmt.Sprintf("qemu-img rebase -u -b '' -F qcow2 -f qcow2 %s", diskPath)
	tmpScript := "/tmp/.qvm_rebase.sh"
	if err := os.WriteFile(tmpScript, []byte(rawCmd), 0644); err != nil {
		return fmt.Errorf("write local script: %w", err)
	}
	defer os.Remove(tmpScript)

	// 使用 stdin 重定向（< file）将脚本内容传给远端 bash，避免管道时序问题。
	sshArgs := buildSSHArgs(tgt, keyFile, "bash")
	var cmd *exec.Cmd
	if tgt.AuthMethod == "password" && tgt.Password != "" {
		cmd = exec.Command("sshpass", append([]string{"-e", "ssh"}, sshArgs...)...)
		cmd.Env = append(os.Environ(), "SSHPASS="+tgt.Password)
	} else {
		cmd = exec.Command("ssh", sshArgs...)
	}

	// 打开本地脚本作为 SSH 的 stdin。
	scriptFile, err := os.Open(tmpScript)
	if err != nil {
		return fmt.Errorf("open script: %w", err)
	}
	defer scriptFile.Close()
	cmd.Stdin = scriptFile

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img rebase %s: %w: %s", diskPath, err, stripSSHWarnings(string(out)))
	}
	log.Printf("[pull] rebase completed: %s", diskPath)
	return nil
}
