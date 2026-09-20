package agent

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// pullDisk 将源磁盘文件经 SSH 流式直传到目标节点并校验一致性。
func pullDisk(disk DiskInfo, tgt TargetSSH) (PullResult, error) {
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

	// 源端校验和。
	sum, err := sha256sumLocal(disk.SourcePath)
	if err != nil {
		return PullResult{}, err
	}

	// 流式直传：cat 源文件 | ssh 目标端 dd 落盘，内存占用恒定。
	if err := streamTransfer(disk.SourcePath, tgt, keyFile, disk.TargetDiskPath); err != nil {
		return PullResult{}, err
	}

	// 目标端校验和并比对。
	remoteSum, err := sha256sumRemote(tgt, keyFile, disk.TargetDiskPath)
	if err != nil {
		return PullResult{}, err
	}
	if !strings.EqualFold(sum, remoteSum) {
		return PullResult{}, fmt.Errorf("checksum mismatch: local=%s remote=%s", sum, remoteSum)
	}

	return PullResult{
		Checksum: sum,
		Size:     disk.SizeBytes,
		Path:     disk.TargetDiskPath,
	}, nil
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
func sshCommand(tgt TargetSSH, keyFile string, remoteCmd ...string) *exec.Cmd {
	args := buildSSHArgs(tgt, keyFile, remoteCmd...)
	if tgt.AuthMethod == "password" && tgt.Password != "" {
		sshpassArgs := append([]string{"-p", tgt.Password, "ssh"}, args...)
		return exec.Command("sshpass", sshpassArgs...)
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
	return parseSha256(string(out)), nil
}

// parseSha256 从 sha256sum 输出提取校验和（首 token）。
func parseSha256(out string) string {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
