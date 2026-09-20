package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	sqllogger "gorm.io/gorm/logger"

	"kvm_console_gateway/internal/taskqueue"
)

// GatewayConfig 网关模块本地配置，全部经环境变量注入，避免硬编码。
type GatewayConfig struct {
	Port           int
	TokenTTL       int
	TokenMaxLength int
	TaskWorkers    int
	DBPath         string
	LogLevel       string
}

// loadGatewayConfig 从环境变量读取网关配置，未设置则回退合理默认值。
func loadGatewayConfig() *GatewayConfig {
	return &GatewayConfig{
		Port:           getEnvInt("GATEWAY_PORT", 8090),
		TokenTTL:       getEnvInt("GATEWAY_TOKEN_TTL_SECONDS", 1800),
		TokenMaxLength: getEnvInt("GATEWAY_TOKEN_MAX_LENGTH", 256),
		TaskWorkers:    getEnvInt("GATEWAY_TASK_WORKERS", 3),
		DBPath:         getEnv("GATEWAY_DB_PATH", "./data/gateway.db"),
		LogLevel:       getEnv("GATEWAY_LOG_LEVEL", "info"),
	}
}

func main() {
	cfg := loadGatewayConfig()

	db, err := gorm.Open(sqlite.Open(cfg.DBPath), &gorm.Config{
		Logger: logLevelToMode(cfg.LogLevel),
	})
	if err != nil {
		log.Fatalf("gateway 数据库连接失败: %v", err)
	}
	if err := db.AutoMigrate(&GatewayToken{}); err != nil {
		log.Fatalf("gateway 数据库迁移失败: %v", err)
	}

	tokenSvc := NewTokenService(db, TokenConfig{
		TTL:       time.Duration(cfg.TokenTTL) * time.Second,
		MaxLength: cfg.TokenMaxLength,
	})
	mgr := NewManager(tokenSvc)
	mig := NewMigrationService(mgr)
	h := NewHandler(mgr, tokenSvc, mig, db)

	RegisterTaskHandler(mig)
	taskqueue.Start(cfg.TaskWorkers)

	r := gin.New()
	r.Use(gin.Recovery())
	RegisterRoutes(r, h)

	addr := ":" + strconv.Itoa(cfg.Port)
	log.Printf("gateway 启动于 %s（端口 %d，令牌 TTL %ds，任务队列 worker=%d）", addr, cfg.Port, cfg.TokenTTL, cfg.TaskWorkers)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("gateway 服务停止: %v", err)
	}
}

// logLevelToMode 将网关日志级别映射为 gorm logger 级别。
func logLevelToMode(level string) sqllogger.Interface {
	switch level {
	case "debug":
		return sqllogger.Default.LogMode(sqllogger.Info)
	case "warn":
		return sqllogger.Default.LogMode(sqllogger.Warn)
	default:
		return sqllogger.Default.LogMode(sqllogger.Silent)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
