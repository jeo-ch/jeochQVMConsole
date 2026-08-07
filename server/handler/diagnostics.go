package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"kvm_console/service"
	"kvm_console/service/diagnostics"
)

// DiagnosisExportRequest 诊断导出请求
type DiagnosisExportRequest struct {
	Categories []string `json:"categories" binding:"required"`
}

// GetDiagnosticCategories 返回可用诊断类别
func GetDiagnosticCategories(c *gin.Context) {
	categories := diagnostics.GetAvailableCategories()
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "ok",
		"data":    categories,
	})
}

// GetOsSupport 返回各发行版的支持等级（S/A/B/C）与认证硬件矩阵（M8.11 / §14 P3-11）
// C1：附带当前系统识别（current_os），前端据此高亮当前发行版支持等级徽标。
func GetOsSupport(c *gin.Context) {
	osSupport := diagnostics.GetOsSupport()
	cur := diagnostics.DetectCurrentOsSupport()
	var currentOS *diagnostics.OsSupportEntry
	if cur != nil {
		entry := *cur
		currentOS = &entry
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "ok",
		"data": gin.H{
			"os_compat":  osSupport,
			"meta":       diagnostics.SupportLevelMetas,
			"current_os": currentOS,
			"os_release": diagnostics.CurrentOsReleaseName(),
		},
	})
}

// RefreshDiagnostics 重新探测组件版本健康度并返回最新结果（M7.2 / §5.11.5 前端「刷新」按钮，
// H3：冷却期内复用缓存，避免高频点击触发多次全量探测）
func RefreshDiagnostics(c *gin.Context) {
	health := service.RefreshComponentHealth()
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "组件版本健康度已刷新", "data": health})
}

// ExportDiagnostics 收集诊断信息并返回 ZIP
func ExportDiagnostics(c *gin.Context) {
	var req DiagnosisExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误：请选择要收集的诊断类别"})
		return
	}

	if len(req.Categories) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "至少需要选择一个诊断类别"})
		return
	}

	// 收集诊断信息
	buf, err := diagnostics.CollectDiagnostics(req.Categories)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "收集诊断信息失败: " + err.Error()})
		return
	}

	// 生成导出文件名
	exportName := fmt.Sprintf("qvmconsole-diagnostics-%s.zip", time.Now().Format("20060102-150405"))

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, exportName))
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}
