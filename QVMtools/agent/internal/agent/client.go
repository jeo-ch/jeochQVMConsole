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
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Version 是 agent 上报给网关的版本号。
const Version = "1.1.0"

// WS 消息类型常量，与网关 ws.go 保持一致。
const (
	msgRegister    = "agent.register"
	msgRegisterRes = "agent.register.result"
	msgCommand     = "command"
	msgProgress    = "command.progress"
	msgResult      = "command.result"
	msgHeartbeat   = "agent.heartbeat"
)

// 重连参数。
const (
	reconnectInitial = 2 * time.Second
	reconnectMax     = 60 * time.Second
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
	gatewayURL string
	token      string

	conn    *websocket.Conn
	mu      sync.Mutex
	done    chan struct{}
	stopped chan struct{}

	// 命令并发控制：允许有限并行，防止磁盘传输占满资源。
	sem     chan struct{}
	active  int64 // 当前正在执行的命令数
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
		gatewayURL: rawURL,
		conn:       conn,
		done:       make(chan struct{}),
		stopped:    make(chan struct{}),
		sem:        make(chan struct{}, 3), // 最多 3 个并发命令
	}, nil
}

// SetToken 保存注册令牌，用于重连时重新注册。
func (c *Client) SetToken(token string) {
	c.token = token
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
// 连接断开时自动以指数退避重连。
func (c *Client) Run() {
	defer close(c.done)

	backoff := reconnectInitial
	for {
		if err := c.runOnce(); err != nil {
			log.Printf("[agent] 连接中断: %v，%v 后重连...", err, backoff)
		} else {
			log.Printf("[agent] 读循环正常退出")
			return
		}

		// 等待退避时间或收到停止信号。
		select {
		case <-c.stopped:
			return
		case <-time.After(backoff):
		}

		// 指数退避，上限 reconnectMax。
		backoff = backoff * 2
		if backoff > reconnectMax {
			backoff = reconnectMax
		}

		// 重连。
		log.Printf("[agent] 正在重连...")
		if err := c.reconnect(); err != nil {
			log.Printf("[agent] 重连失败: %v", err)
			continue
		}
		log.Printf("[agent] 重连成功，重新注册...")
		backoff = reconnectInitial
	}
}

// reconnect 关闭旧连接并建立新连接、重新注册。
func (c *Client) reconnect() error {
	// 关闭旧连接。
	if c.conn != nil {
		c.conn.Close()
	}

	u, err := url.Parse(c.gatewayURL)
	if err != nil {
		return fmt.Errorf("invalid gateway url: %w", err)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}
	c.conn = conn

	if c.token != "" {
		if err := c.Register(c.token, HostInfo()); err != nil {
			return fmt.Errorf("register: %w", err)
		}
	}
	return nil
}

// runOnce 执行一次完整的读循环，返回错误时表示连接断开。
func (c *Client) runOnce() error {
	// 心跳：每30s上报一次，维持连接存活。
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
					log.Printf("[agent] heartbeat send failed: %v", err)
					return
				}
			}
		}
	}()

	for {
		c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		var e envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			log.Printf("[agent] unmarshal message failed: %v", err)
			continue
		}

		switch e.Type {
		case msgCommand:
			// 异步执行命令，不阻塞读循环，确保心跳和后续消息正常收发。
			go c.handleCommand(e)
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
	if len(e.Data) > 0 {
		if err := json.Unmarshal(e.Data, &payload); err != nil {
			log.Printf("[agent] unmarshal command payload failed: %v", err)
			c.reportResult(e.ID, "error", nil, "invalid command payload")
			return
		}
	}

	log.Printf("[agent] 收到命令: action=%s id=%s", payload.Action, e.ID)

	// 并发控制：获取令牌。
	c.sem <- struct{}{}
	atomic.AddInt64(&c.active, 1)
	defer func() {
		<-c.sem
		atomic.AddInt64(&c.active, -1)
	}()

	// 命令执行期间通过 progress 通道回调上报进度。
	data, err := c.dispatch(payload.Action, payload.Params, func(pct int, msg string) {
		c.reportProgress(e.ID, pct, msg)
	})
	if err != nil {
		log.Printf("[agent] 命令执行失败: action=%s err=%v", payload.Action, err)
		c.reportResult(e.ID, "error", nil, err.Error())
		return
	}
	log.Printf("[agent] 命令完成: action=%s", payload.Action)
	c.reportResult(e.ID, "ok", data, "")
}

// ActiveCommands 返回当前正在执行的命令数。
func (c *Client) ActiveCommands() int64 {
	return atomic.LoadInt64(&c.active)
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
		log.Printf("[agent] report progress failed: %v", err)
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
		log.Printf("[agent] report result failed: %v", err)
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
