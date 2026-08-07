package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"kvm_console/service"
)

// GetHealthProbeLatest 返回最近一次周期健康探针快照（M8.10 / §14 P2-10）。
// 公开只读接口（不要求认证），供前端 Dashboard 状态灯轮询：
//   - libvirt_ready=false → 黄灯（虚拟化栈异常）
//   - 面板离线（本接口不可达）→ 前端轮询超时 → 红灯
func GetHealthProbeLatest(c *gin.Context) {
	probe := service.GetHealthProbe()
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": probe})
}
