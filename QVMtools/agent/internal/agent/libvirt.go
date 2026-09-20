package agent

import (
	"encoding/xml"
	"fmt"
	"log"
	"strings"
	"time"
)

// domainXML 是 libvirt 域定义的子集，仅解析迁移所需字段。
type domainXML struct {
	XMLName xml.Name `xml:"domain"`
	Name    string   `xml:"name"`
	UUID    string   `xml:"uuid"`
	VCPU    struct {
		Value int `xml:",chardata"`
	} `xml:"vcpu"`
	Memory  memoryXML  `xml:"memory"`
	Devices devicesXML `xml:"devices"`
}

// memoryXML 解析 <memory> 元素，Value 默认以 KiB 为单位。
type memoryXML struct {
	Value int    `xml:",chardata"`
	Unit  string `xml:"unit,attr"`
}

// devicesXML 解析 <devices> 容器。
type devicesXML struct {
	Disks []diskXML `xml:"disk"`
}

// diskXML 解析 <disk> 元素。
type diskXML struct {
	Driver driverXML  `xml:"driver"`
	Source sourceXML  `xml:"source"`
	Target targetXML  `xml:"target"`
}

type driverXML struct {
	Type string `xml:"type,attr"`
}

type sourceXML struct {
	File string `xml:"file,attr"`
}

type targetXML struct {
	Dev string `xml:"dev,attr"`
}

// virshListNames 返回本机所有已注册 VM 域名。
func virshListNames() ([]string, error) {
	out, err := runOutput("virsh", "list", "--all", "--name")
	if err != nil {
		return nil, fmt.Errorf("virsh list: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if n := strings.TrimSpace(line); n != "" {
			names = append(names, n)
		}
	}
	return names, nil
}

// dumpXML 导出指定 VM 的域定义 XML。
func dumpXML(vmName string) (string, error) {
	out, err := runOutput("virsh", "dumpxml", vmName)
	if err != nil {
		return "", fmt.Errorf("virsh dumpxml %s: %w", vmName, err)
	}
	return out, nil
}

// domState 返回 VM 运行状态（running/halted/shut off 等）。
func domState(vmName string) (string, error) {
	out, err := runOutput("virsh", "dominfo", vmName)
	if err != nil {
		return "", fmt.Errorf("virsh dominfo %s: %w", vmName, err)
	}
	return parseDomState(out), nil
}

// parseDomState 从 virsh dominfo 输出提取第 1 行 State 字段。
func parseDomState(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "State:") {
			return normalizeState(strings.TrimSpace(strings.TrimPrefix(line, "State:")))
		}
	}
	return "unknown"
}

// normalizeState 将 libvirt 状态归一化为枚举值。
func normalizeState(s string) string {
	switch s {
	case "running":
		return "running"
	case "shut off":
		return "shut_off"
	case "paused":
		return "paused"
	case "blocked":
		return "blocked"
	default:
		return "halted"
	}
}

// toMiB 将内存值按 unit 换算为 MiB。
func (m memoryXML) toMiB() int64 {
	v := int64(m.Value)
	switch strings.ToLower(m.Unit) {
	case "mib", "mb":
		return v
	case "gib", "gb":
		return v * 1024
	default:
		// libvirt <memory> 默认单位 KiB（含空 unit 属性）。
		return v / 1024
	}
}

// discoverVMs 枚举本机全部 VM 及其磁盘信息。
func discoverVMs() ([]VMInfo, error) {
	names, err := virshListNames()
	if err != nil {
		return nil, err
	}
	vms := make([]VMInfo, 0, len(names))
	for _, name := range names {
		info, err := discoverOne(name)
		if err != nil {
			log.Printf("discover vm %s failed: %v", name, err)
			continue
		}
		vms = append(vms, info)
	}
	return vms, nil
}

// discoverOne 解析单个 VM 的元信息与磁盘。
func discoverOne(vmName string) (VMInfo, error) {
	raw, err := dumpXML(vmName)
	if err != nil {
		return VMInfo{}, err
	}
	var d domainXML
	if err := xml.Unmarshal([]byte(raw), &d); err != nil {
		return VMInfo{}, fmt.Errorf("parse domain xml: %w", err)
	}

	state, err := domState(vmName)
	if err != nil {
		return VMInfo{}, err
	}

	vms := VMInfo{
		Name:   firstNonEmpty(d.Name, vmName),
		ID:     vmName,
		UUID:   d.UUID,
		State:  state,
		CPUs:   d.VCPU.Value,
		Memory: d.Memory.toMiB(),
		Disks:  parseDisks(d.Devices.Disks),
	}
	return vms, nil
}

// parseDisks 将 XML 磁盘节点映射为磁盘元信息。
func parseDisks(nodes []diskXML) []DiskInfo {
	if len(nodes) == 0 {
		return nil
	}
	disks := make([]DiskInfo, 0, len(nodes))
	for _, n := range nodes {
		disks = append(disks, DiskInfo{
			Target:     n.Target.Dev,
			SourcePath: n.Source.File,
			Format:     firstNonEmpty(n.Driver.Type, "qcow2"),
		})
	}
	return disks
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// createSnapshot 为 VM 创建 disk-only 原子快照，作为迁移的一致检查点。
func createSnapshot(vmName, snapshotName string, online bool) error {
	args := []string{"snapshot-create-as", vmName, snapshotName, "--disk-only", "--atomic", "--no-metadata", "--diskname", snapshotName}
	if online {
		args = append(args, "--live")
	}
	if _, err := runOutput("virsh", args...); err != nil {
		return fmt.Errorf("virsh snapshot-create-as %s: %w", vmName, err)
	}
	return nil
}

// shutdownVM 关闭 VM：先 ACPI 优雅关机，超时则强杀。
func shutdownVM(vmName string) error {
	if _, err := runOutput("virsh", "shutdown", vmName); err != nil {
		return fmt.Errorf("virsh shutdown %s: %w", vmName, err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		state, err := domState(vmName)
		if err == nil && state != "running" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	if _, err := runOutput("virsh", "destroy", vmName); err != nil {
		return fmt.Errorf("virsh destroy %s: %w", vmName, err)
	}
	return nil
}

// deleteSnapshot 删除迁移用的临时快照，恢复磁盘链。
func deleteSnapshot(vmName, snapshotName string) error {
	if _, err := runOutput("virsh", "snapshot-delete", vmName, snapshotName, "--current"); err != nil {
		return fmt.Errorf("virsh snapshot-delete %s: %w", vmName, err)
	}
	return nil
}

// uniqueSnapshotName 生成带时间戳的迁移快照名，避免并发冲突。
func uniqueSnapshotName(vmName string) string {
	return fmt.Sprintf("agent-migrate-%s-%d", vmName, time.Now().UnixNano())
}
