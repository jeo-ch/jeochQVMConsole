package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"kvm_console_gateway/internal/model"
	"kvm_console_gateway/internal/taskqueue"
)

// RegisterRoutes 注册网关所有 HTTP 与 WS 路由。
// 由 console router 在 /api 组内调用，保持与既有路由风格一致。
func RegisterRoutes(r *gin.Engine, h *Handler) {
	api := r.Group("/api/gateway")
	{
		api.GET("/hosts/:id/token", h.IssueToken)
		api.GET("/hosts/:id/status", h.HostStatus)
		api.POST("/migrations", h.StartMigration)
		api.GET("/migrations", h.ListMigrationTasks)
		api.GET("/migrations/:id", h.GetMigrationStatus)
	}
	// agent 回连端点（一次性令牌走 query）。
	r.GET("/api/gateway/agent/connect", h.WebSocketHandler)
}

// RegisterTaskHandler 注册迁移任务处理器到任务队列。
func RegisterTaskHandler(mig *MigrationService) {
	taskqueue.RegisterHandler("gateway_migration", func(ctx context.Context, task *model.Task, progress func(int, string, ...json.RawMessage)) (result string, retErr error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[migration] PANIC: %v", r)
				result = "failed"
				retErr = fmt.Errorf("panic: %v", r)
			}
		}()
		log.Printf("[migration] handler invoked: task_id=%d params_len=%d", task.ID, len(task.Params))
		var req MigrationRequest
		if err := json.Unmarshal([]byte(task.Params), &req); err != nil {
			log.Printf("[migration] JSON parse error: %v", err)
			return "failed", err
		}
		log.Printf("[migration] starting migration: vm=%s source=%d target=%d", req.VMName, req.SourceHostID, req.TargetHostID)
		return mig.RunMigration(ctx, req, progress)
	})
}
