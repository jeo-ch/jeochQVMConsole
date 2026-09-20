package main

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"kvm_console/taskqueue"
)

// Handler 封装网关对外 HTTP 接口。
type Handler struct {
	manager  *Manager
	tokens   *TokenService
	migrations *MigrationService
	db       *gorm.DB
}

// NewHandler 构造网关 Handler。
func NewHandler(m *Manager, t *TokenService, mig *MigrationService, db *gorm.DB) *Handler {
	return &Handler{
		manager:  m,
		tokens:   t,
		migrations: mig,
		db:       db,
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
	// 令牌绑定源主机 ID（控制台 HostNode ID），由控制台侧传入，网关侧不查 HostNode 表。
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
