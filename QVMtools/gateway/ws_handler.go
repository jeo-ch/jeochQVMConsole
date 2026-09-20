package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// wsUpgrader 沿用 console 既有 WS 风格：binary 子协议，放行同源/代理回连来源。
var wsUpgrader = websocket.Upgrader{
	Subprotocols:    []string{"binary"},
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
}

// WebSocketHandler 处理 agent 回连握手。
// GET /api/gateway/agent/connect?token=xxx
func (h *Handler) WebSocketHandler(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	h.manager.HandleConnection(conn)
}
