package main

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"kvm_console_gateway/internal/taskqueue"
)

// Handler 封装网关对外 HTTP 接口。
type Handler struct {
	manager   *Manager
	tokens    *TokenService
	migrations *MigrationService
}

// NewHandler 构造网关 Handler。
func NewHandler(m *Manager, t *TokenService, mig *MigrationService) *Handler {
	return &Handler{
		manager:   m,
		tokens:    t,
		migrations: mig,
	}
}

// IssueTokenResponse 签发令牌响应。
type IssueTokenResponse struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

// IssueToken 为指定源主机签发一次性迁移令牌。
// POST /api/gateway/hosts/:id/token
func (h *Handler) IssueToken(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host id"})
		return
	}
	plain, _, err := h.tokens.Issue(uint(id), "register")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	url := buildAgentURL(c, plain)
	c.JSON(http.StatusOK, IssueTokenResponse{Token: plain, URL: url})
}

// HostStatus 返回源主机当前连接状态。
// GET /api/gateway/hosts/:id/status
func (h *Handler) HostStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host id"})
		return
	}
	h.manager.mu.RLock()
	_, connected := h.manager.conns[uint(id)]
	h.manager.mu.RUnlock()
	c.JSON(http.StatusOK, gin.H{"host_id": id, "connected": connected})
}

// MigrationResponse 迁移任务响应。
type MigrationResponse struct {
	ID     uint   `json:"id"`
	Status string `json:"status"`
}

// StartMigration 发起一次整机迁移编排。
// POST /api/gateway/migrations
func (h *Handler) StartMigration(c *gin.Context) {
	var req MigrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SourceHostID == 0 || req.TargetHostID == 0 || req.VMName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source_host_id, target_host_id, vm_name are required"})
		return
	}
	if req.TargetSSH == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_ssh is required"})
		return
	}

	createdBy := userIDFromContext(c)
	task, err := taskqueue.SubmitWithStruct("gateway_migration", req, createdBy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, MigrationResponse{ID: task.ID, Status: task.Status})
}

// GetMigrationStatus 查询迁移任务状态（含传输进度详情）。
// GET /api/gateway/migrations/:id
func (h *Handler) GetMigrationStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task id"})
		return
	}
	task, ok := taskqueue.GetTask(uint(id))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":       task.ID,
		"type":     task.Type,
		"status":   task.Status,
		"progress": task.Progress,
		"message":  task.Message,
		"detail":   task.Detail,
		"result":   task.Result,
	})
}

// ListMigrationTasks 列出指定类型的迁移任务。
// GET /api/gateway/migrations?type=xxx
func (h *Handler) ListMigrationTasks(c *gin.Context) {
	taskType := c.DefaultQuery("type", "gateway_migration")
	tasks := taskqueue.ListTasksByType(taskType)
	result := make([]gin.H, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, gin.H{
			"id":       t.ID,
			"type":     t.Type,
			"status":   t.Status,
			"progress": t.Progress,
			"message":  t.Message,
			"detail":   t.Detail,
		})
	}
	c.JSON(http.StatusOK, result)
}
