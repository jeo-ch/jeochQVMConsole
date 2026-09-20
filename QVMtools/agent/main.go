package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"kvm_console_agent/internal/agent"
)

func main() {
	gatewayURL := flag.String("gateway", envOr("AGENT_GATEWAY_URL", ""), "网关 WS 端点")
	token := flag.String("token", envOr("AGENT_TOKEN", ""), "注册令牌")
	flag.Parse()

	if *gatewayURL == "" {
		log.Fatal("gateway url is required (flag --gateway or env AGENT_GATEWAY_URL)")
	}
	if *token == "" {
		log.Fatal("token is required (flag --token or env AGENT_TOKEN)")
	}

	c, err := agent.NewClient(*gatewayURL)
	if err != nil {
		log.Fatalf("connect gateway: %v", err)
	}
	defer c.Close()

	c.SetToken(*token)

	if err := c.Register(*token, agent.HostInfo()); err != nil {
		log.Fatalf("register failed: %v", err)
	}
	log.Printf("[agent] %s 已注册到网关 %s", agent.Version, *gatewayURL)

	// 优雅退出：收到终止信号时关闭 WS 连接。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Printf("[agent] 收到终止信号，正在退出...")
		c.Close()
		os.Exit(0)
	}()

	c.Run()
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
