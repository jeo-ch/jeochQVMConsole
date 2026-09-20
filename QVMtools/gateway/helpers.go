package main

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"kvm_console/model"
)

// userIDFromContext 从 gin context 取出当前登录用户 ID（API Key 路径下可能为空）。
func userIDFromContext(c *gin.Context) string {
	user, _ := c.Get("current_user")
	if u, ok := user.(*model.User); ok && u != nil {
		return strconv.FormatUint(uint64(u.ID), 10)
	}
	return ""
}

// buildAgentURL 构造 agent 回连地址：wss://<host>/api/gateway/agent/connect?token=xxx。
func buildAgentURL(c *gin.Context, token string) string {
	scheme := "http"
	if c.Request.TLS != nil || c.Request.URL.Scheme == "https" {
		scheme = "https"
	}
	if c.GetHeader("X-Forwarded-Proto") != "" {
		scheme = c.GetHeader("X-Forwarded-Proto")
	}
	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}
	return scheme + "://" + host + "/api/gateway/agent/connect?token=" + token
}
