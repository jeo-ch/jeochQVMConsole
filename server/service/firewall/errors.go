package firewall

import (
	"errors"
	"regexp"
)

// ── 结构化错误（v0.8/#R：error_code + 可操作 hint） ──

// FirewallError 后端错误，携带 error_code 与可操作 hint。
// BackendStatus.LastError 以 "message: hint" 形式兼容旧字段（§4.2 BackendStatus）。
type FirewallError struct {
	Code    string
	Message string
	Hint    string
}

func (e *FirewallError) Error() string {
	if e.Hint != "" {
		return e.Message + ": " + e.Hint
	}
	return e.Message
}

const (
	FirewalldNotRunning    = "FIREWALLD_NOT_RUNNING"
	FirewalldOldVersion    = "FIREWALLD_OLD_VERSION"
	FirewalldCommandFailed = "FIREWALLD_COMMAND_FAILED"
	ZoneNotBound           = "ZONE_NOT_BOUND"
	DBUSError              = "DBUS_ERROR"
	PermissionDenied       = "PERMISSION_DENIED"
)

// regexpMustCompile 预编译正则 helper（避免调用点重复 MustCompile）。
func regexpMustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

// errorCodeOf 从错误链中提取结构化错误码（#R）；非 FirewallError 返回空串。
func errorCodeOf(err error) string {
	if err == nil {
		return ""
	}
	var fe *FirewallError
	if errors.As(err, &fe) {
		return fe.Code
	}
	return ""
}
