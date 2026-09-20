package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WS 消息类型：agent 回连握手、服务端下发命令、agent 上报进度与结果。
const (
	msgTypeRegister    = "agent.register"
	msgTypeRegisterRes = "agent.register.result"
	msgTypeCommand     = "command"
	msgTypeProgress    = "command.progress"
	msgTypeResult      = "command.result"
	msgTypeHeartbeat   = "agent.heartbeat"
)

// WSMessage 是 agent 与网关之间双向通信的统一信封。
type WSMessage struct {
	Type     string          `json:"type"`
	ID       string          `json:"id,omitempty"`
	Status   string          `json:"status,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Progress int             `json:"progress,omitempty"`
	Message  string          `json:"message,omitempty"`
}

// Connection 表示一个已认证并注册的 agent 长连接。
type Connection struct {
	conn      *websocket.Conn
	tokenHash string
	hostID    uint

	mu      sync.Mutex
	quit    chan struct{}
	msgCh   chan WSMessage
}

// request 表示一次派发中的命令：结果从 ch 读取，进度经 onProgress 回调上报。
type request struct {
	ch         chan WSMessage
	onProgress func(int, string)
}

// Manager 管理所有已连接的 agent，并支持按主机派发命令并等待结果。
type Manager struct {
	mu      sync.RWMutex
	conns   map[uint]*Connection
	tokens  *TokenService
	requests map[string]*request
}

// NewManager 初始化连接管理器。
func NewManager(tokens *TokenService) *Manager {
	return &Manager{
		conns:    make(map[uint]*Connection),
		tokens:   tokens,
		requests: make(map[string]*request),
	}
}

// HandleConnection 处理一次 agent 回连：握手认证、注册、持续收发。
func (m *Manager) HandleConnection(conn *websocket.Conn) {
	c := &Connection{
		conn: conn,
		quit: make(chan struct{}),
		msgCh: make(chan WSMessage, 64),
	}
	defer cleanupConnection(m, c)

	// 握手：校验一次性令牌并注册到源主机。
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return
	}
	var reg WSMessage
	if err := json.Unmarshal(raw, &reg); err != nil {
		return
	}
	if err := handshake(c, &reg, m); err != nil {
		return
	}

	m.mu.Lock()
	m.conns[c.hostID] = c
	m.mu.Unlock()

	// 读取循环。
	for {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case msgTypeProgress:
			// 进度仅回调上报，不关闭通道，避免结果消息丢失。
			if id := msg.ID; id != "" {
				m.mu.RLock()
				r := m.requests[id]
				m.mu.RUnlock()
				if r != nil && r.onProgress != nil {
					r.onProgress(msg.Progress, msg.Message)
				}
			}
		case msgTypeResult:
			if id := msg.ID; id != "" {
				m.mu.Lock()
				r := m.requests[id]
				if r != nil {
					delete(m.requests, id)
					select {
					case r.ch <- msg:
					default:
					}
					close(r.ch)
				}
				m.mu.Unlock()
			}
		case msgTypeHeartbeat:
			// 心跳仅维持连接，无需额外处理。
		}
	}
}

// handshake 校验令牌并建立连接与源主机的绑定。
// 已消费的令牌允许重连（Validate），未消费的令牌首次注册（Consume）。
func handshake(c *Connection, reg *WSMessage, m *Manager) error {
	var data struct {
		Token       string `json:"token"`
		AgentVersion string `json:"agent_version"`
		HostInfo    json.RawMessage `json:"host_info"`
	}
	if len(reg.Data) > 0 && json.Unmarshal(reg.Data, &data) != nil {
		return sendRegisterResult(c, false, "invalid payload")
	}

	// 先尝试 Consume（首次注册），若已消费则 Validate（重连）。
	tok, err := m.tokens.Consume(data.Token)
	if err == ErrTokenConsumed {
		// token 已被消费过，说明是重连，用 Validate 验证身份。
		tok, err = m.tokens.Validate(data.Token)
	}
	if err != nil {
		return sendRegisterResult(c, false, err.Error())
	}
	c.tokenHash = tok.TokenHash
	c.hostID = tok.HostID
	return sendRegisterResult(c, true, "ok")
}

// sendRegisterResult 回复 agent 注册结果。
func sendRegisterResult(c *Connection, ok bool, msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(WSMessage{
		Type:    msgTypeRegisterRes,
		Status:  boolStr(ok),
		Message: msg,
	})
}

// DispatchCommand 向指定源主机派发命令并等待其结果。onProgress 非空时，agent 上报的进度会回调该函数。
func (m *Manager) DispatchCommand(ctx context.Context, hostID uint, action string, params map[string]interface{}, onProgress func(int, string)) (*WSMessage, error) {
	m.mu.RLock()
	c, ok := m.conns[hostID]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrConnectionLost
	}

	reqID := generateRequestID()
	req := &request{
		ch:         make(chan WSMessage, 1),
		onProgress: onProgress,
	}
	m.mu.Lock()
	m.requests[reqID] = req
	m.mu.Unlock()

	cmd := WSMessage{
		Type: msgTypeCommand,
		ID:   reqID,
		Data: mustJSON(map[string]interface{}{
			"action": action,
			"params": params,
		}),
	}
	if err := c.send(cmd); err != nil {
		m.cleanupRequest(reqID)
		return nil, err
	}

	select {
	case msg := <-req.ch:
		if msg.Status != "ok" {
			return nil, fmt.Errorf("%s: %s", action, msg.Message)
		}
		return &msg, nil
	case <-ctx.Done():
		m.cleanupRequest(reqID)
		return nil, ctx.Err()
	case <-time.After(4 * time.Hour):
		m.cleanupRequest(reqID)
		return nil, ErrConnectionLost
	}
}

// SendProgress 主动向 agent 推送进度（服务端发起长任务时）。
func (m *Manager) SendProgress(hostID uint, reqID string, progress int, message string) error {
	m.mu.RLock()
	c, ok := m.conns[hostID]
	m.mu.RUnlock()
	if !ok {
		return ErrConnectionLost
	}
	return c.send(WSMessage{
		Type:     msgTypeProgress,
		ID:       reqID,
		Progress: progress,
		Message:  message,
	})
}

func (c *Connection) send(msg WSMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(msg)
}

func (m *Manager) cleanupRequest(reqID string) {
	m.mu.Lock()
	if r, ok := m.requests[reqID]; ok {
		delete(m.requests, reqID)
		close(r.ch)
	}
	m.mu.Unlock()
}

func cleanupConnection(m *Manager, c *Connection) {
	close(c.quit)
	m.mu.Lock()
	if cur, ok := m.conns[c.hostID]; ok && cur == c {
		delete(m.conns, c.hostID)
	}
	m.mu.Unlock()
	c.conn.Close()
}

func boolStr(b bool) string {
	if b {
		return "ok"
	}
	return "error"
}
