package handler

// VM SSE 实时推送相关 handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"kvm_console/service"
)

// GetVmListSSE SSE 实时推送虚拟机列表及状态
func GetVmListSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	clientGone := c.Request.Context().Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	listOptions := buildVMListOptions(c)
	role, _ := c.Get("role")
	isAdmin := role == "admin"

	// 立即发送一次
	if isAdmin {
		service.TriggerAdminVMCacheRefreshIfNeeded()
	}
	if vms, err := loadVMListForRole(c, isAdmin, listOptions); err == nil {
		c.SSEvent("vm_list", vms)
		c.Writer.Flush()
	}

	for {
		select {
		case <-clientGone:
			return
		case <-ticker.C:
			if isAdmin {
				service.TriggerAdminVMCacheRefreshIfNeeded()
			}
			vms, err := loadVMListForRole(c, isAdmin, listOptions)
			if err != nil {
				if service.IsLibvirtUnavailableError(err) {
					c.SSEvent("vm_list", []service.VmInfo{})
					c.Writer.Flush()
				}
				continue
			}
			c.SSEvent("vm_list", vms)
			c.Writer.Flush()
		}
	}
}

// loadVMListForRole 按角色加载 VM 列表：admin 查看全部，普通用户仅查看自己拥有的 VM。
func loadVMListForRole(c *gin.Context, isAdmin bool, listOptions service.VMListOptions) ([]service.VmInfo, error) {
	if isAdmin {
		return service.ListCachedVMs(listOptions)
	}
	username, _ := c.Get("username")
	return service.ListCachedVMsByOwner(fmt.Sprintf("%v", username), listOptions)
}

// GetVmDetailSSE SSE 实时推送虚拟机详情
func GetVmDetailSSE(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "虚拟机名称不能为空",
		})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	clientGone := c.Request.Context().Done()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// 立即发送一次
	if vm, err := service.GetVM(name); err == nil {
		c.SSEvent("vm_detail", vm)
		c.Writer.Flush()
	}

	for {
		select {
		case <-clientGone:
			return
		case <-ticker.C:
			vm, err := service.GetVM(name)
			if err != nil {
				continue
			}
			c.SSEvent("vm_detail", vm)
			c.Writer.Flush()
		}
	}
}
