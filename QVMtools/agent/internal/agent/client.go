package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Version 是 agent 上报给网关的版本号。
const Version = "1.0.0"

// WS 消息类型常量，与网关 ws.go 保持一致。
const (
	msgRegister    = "agent.register"
	msgRegisterRes = "agent.register.result"
	msgCommand     = "command"
	msgProgress    = "command.progress"
	msgResult      = "command.result"
	msgHeartbeat   = "agent.heartbeat"
)

// envelope 是 agent 与网关之间双向通信的统一信封。
type envelope struct {
	Type     string          `json:"type"`
	ID       string          `json:"id,omitempty"`
	Status   string          `json:"status,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Progress int             `json:"progress,omitempty"`
	Message  string          `json:"message,omitempty"`
}

// Client 维护与网关的 WS 长连接，接收并回源主机命令。
type Client struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	done    chan struct{}
	stopped chan struct{}
}

// NewClient 连接网关 WS 端点并返回客户端实例。
func NewClient(rawURL string) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway url: %w", err)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("dial gateway: %w", err)
	}
	return &Client{
		conn:    conn,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}, nil
}

// Close 关闭底层连接。
func (c *Client) Close() error {
	select {
	case <-c.stopped:
	default:
		close(c.stopped)
	}
	return c.conn.Close()
}

// send 以互斥方式写入一条消息。
func (c *Client) send(e envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(e)
}

// Register 向网关上报一次性令牌与本机信息，完成注册握手。
func (c *Client) Register(token, hostInfo string) error {
	data, _ := json.Marshal(map[string]string{
		"token":        token,
		"agent_version": Version,
		"host_info":    hostInfo,
	})
	if err := c.send(envelope{Type: msgRegister, Data: data}); err != nil {
		return fmt.Errorf("send register: %w", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	c.conn.SetReadDeadline(deadline)
	defer c.conn.SetReadDeadline(time.Time{})
	_, raw, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read register result: %w", err)
	}
	var res envelope
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("unmarshal register result: %w", err)
	}
	if res.Type != msgRegisterRes || res.Status != "ok" {
		return fmt.Errorf("register failed: %s", res.Message)
	}
	return nil
}

// Run 进入读循环：处理命令、心跳与进度回执。
func (c *Client) Run() {
	defer close(c.done)
	// 心跳：每30s上报一次，维持连接存活；通过独立 goroutine 并发发送，避免阻塞 ReadMessage。
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		for {
			select {
			case <-quit:
				return
			case <-heartbeat.C:
				if err := c.send(envelope{Type: msgHeartbeat}); err != nil {
					log.Printf("heartbeat send failed: %v", err)
					return
				}
			}
		}
	}()

	for {
		c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("read loop terminated: %v", err)
			return
		}
		var e envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			log.Printf("unmarshal message failed: %v", err)
			continue
		}

		switch e.Type {
		case msgCommand:
			c.handleCommand(e)
		case msgHeartbeat:
			// 服务端心跳，忽略。
		}
	}
}

// handleCommand 解析并派发一条命令，执行后上报进度与结果。
func (c *Client) handleCommand(e envelope) {
	var payload struct {
		Action string                 `json:"action"`
		Params map[string]interface{} `json:"params"`
	}
	if len(e.Data) > 0 && json.Unmarshal(e.Data, &payload) != nil {
		c.reportResult(e.ID, "error", nil, "invalid command payload")
		return
	}

	// 命令执行期间通过 progress 通道回调上报进度。
	data, err := c.dispatch(payload.Action, payload.Params, func(pct int, msg string) {
		c.reportProgress(e.ID, pct, msg)
	})
	if err != nil {
		c.reportResult(e.ID, "error", nil, err.Error())
		return
	}
	c.reportResult(e.ID, "ok", data, "")
}

// reportProgress 向网关上报某条命令的进度。
func (c *Client) reportProgress(id string, pct int, msg string) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	if err := c.send(envelope{Type: msgProgress, ID: id, Progress: pct, Message: msg}); err != nil {
		log.Printf("report progress failed: %v", err)
	}
}

// reportResult 向网关上报某条命令的最终结果。
func (c *Client) reportResult(id, status string, data interface{}, errMsg string) {
	var raw json.RawMessage
	if data != nil {
		raw, _ = json.Marshal(data)
	}
	if err := c.send(envelope{
		Type:    msgResult,
		ID:      id,
		Status:  status,
		Data:    raw,
		Message: errMsg,
	}); err != nil {
		log.Printf("report result failed: %v", err)
	}
}

// HostInfo 收集本机基础信息，作为注册的一部分上报。
func HostInfo() string {
	out, _ := runOutput("uname", "-s", "-r", "-m")
	mem := HostMemory()
	return strings.Join([]string{
		"hostname=" + hostname(),
		"os=" + out,
		"cpu=" + fmt.Sprintf("%d", runtime.NumCPU()),
		"memory=" + mem,
	}, ",")
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown"
}

// runOutput 执行命令并返回去噪后的标准输出。
func runOutput(name string, args ...string) (string, error) {
	cmd := newCommand(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return strings.TrimSpace(cmd.Stderr()), err
	}
	return stripSSHWarnings(string(out)), nil
}

// newCommand 构造一个带标准输出与错误输出缓冲的命令。
func newCommand(name string, args ...string) *execCmd {
	return newExecCmd(name, args...)
}
