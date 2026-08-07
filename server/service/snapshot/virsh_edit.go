package snapshot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"kvm_console/utils"
)

// virshEditWithSed 通过 virsh edit 以 sed 程序批量修改 VM XML。
//
// 安全说明：不走 `EDITOR="sed -i '...'" virsh edit` 的 shell 内联方式（磁盘路径可能包含
// 引号/反引号/分号等元字符导致命令注入）。这里把 sed 表达式写入临时文件，编辑脚本只执行
// `sed -i -f <表达式文件>`，磁盘路径内容永远不进入 shell 词法层。命令通过 exec 直接启动，
// 用 EDITOR 环境变量指向我们完全可控的脚本，避免任何用户可控内容拼接进 shell 命令串。
func virshEditWithSed(vmName string, expressions []string) error {
	expressions = cleanExpressions(expressions)
	if len(expressions) == 0 {
		return nil
	}

	tmp, err := os.MkdirTemp("", "qvmc-virsh-edit-")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmp)

	exprFile := filepath.Join(tmp, "sed-expr")
	if err := os.WriteFile(exprFile, []byte(strings.Join(expressions, "\n")+"\n"), 0600); err != nil {
		return fmt.Errorf("写入 sed 表达式失败: %w", err)
	}

	// 编辑脚本内容不包含任何用户可控字符串：只引用表达式文件的绝对路径。
	script := filepath.Join(tmp, "editor.sh")
	scriptBody := fmt.Sprintf("#!/bin/sh\nexec sed -i -f %s \"$1\"\n", utils.ShellSingleQuote(exprFile))
	if err := os.WriteFile(script, []byte(scriptBody), 0700); err != nil {
		return fmt.Errorf("写入编辑脚本失败: %w", err)
	}

	cmd := exec.Command("virsh", "edit", vmName)
	cmd.Env = append(nonLocalizedEnv(), "EDITOR="+script)
	done := make(chan *cmdResult, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		done <- &cmdResult{output: string(out), err: err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return fmt.Errorf("修改虚拟机配置失败: %s: %v", r.output, r.err)
		}
		return nil
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		return fmt.Errorf("修改虚拟机配置超时（60s）")
	}
}

// nonLocalizedEnv 构造继承自父进程但剔除本地化变量并强制 C 语言环的环境，
// 保证 virsh 输出英文便于解析（与 utils.buildCmdEnv 一致）。
func nonLocalizedEnv() []string {
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

type cmdResult struct {
	output string
	err    error
}

// cleanExpressions 去除空表达式。
func cleanExpressions(exprs []string) []string {
	out := make([]string, 0, len(exprs))
	for _, e := range exprs {
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out
}