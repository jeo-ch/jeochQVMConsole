package agent

import (
	"bytes"
	"os/exec"
	"strings"
)

// execCmd 是对 exec.Cmd 的轻量封装，可同时拿到标准输出与错误输出。
type execCmd struct {
	cmd    *exec.Cmd
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// newExecCmd 构造命令并绑定输出缓冲。
func newExecCmd(name string, args ...string) *execCmd {
	c := &execCmd{cmd: exec.Command(name, args...)}
	c.cmd.Stdout = &c.stdout
	c.cmd.Stderr = &c.stderr
	return c
}

// Output 执行命令并返回去噪后的标准输出。
func (c *execCmd) Output() ([]byte, error) {
	err := c.cmd.Run()
	if err != nil {
		return bytes.TrimSpace(c.stdout.Bytes()), err
	}
	return bytes.TrimSpace(c.stdout.Bytes()), nil
}

// Stderr 返回已收集的标准错误。
func (c *execCmd) Stderr() string {
	return strings.TrimSpace(c.stderr.String())
}

// sshWarningPatterns 是 SSH 连接时常见的非致命告警片段，会从输出中剥离。
var sshWarningPatterns = []string{
	"Warning: Permanently added",
	"Please add the key to the persistent key store",
	"Warning: the ECDSA host key",
	"Warning: adding to no known hosts",
	"Pseudo-terminal will not be allocated because stdin is not a terminal",
}

// stripSSHWarnings 从命令输出中移除常见的 SSH 告警行，避免污染结构化数据。
func stripSSHWarnings(s string) string {
	lines := strings.Split(s, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		skip := false
		for _, p := range sshWarningPatterns {
			if strings.Contains(trimmed, p) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, line)
		}
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}
