# 修复：跨节点迁移网络适配 + qcow2 backing file 清理

## 问题现象

1. 迁移后的 VM 在目标主机无法启动，报错 `网络 'default' 未激活`；
2. 迁移后的 qcow2 镜像包含无效的 backing file 引用（指向源端快照路径），导致 `virsh start` 失败。

## 根因分析

### 问题 1：网络适配逻辑缺陷

Agent 的 `define` 命令在目标主机定义 VM 时，**盲目将 OVS 桥接网卡转换为 `default` NAT 网卡**（`adaptNetwork` 函数），未检测目标主机是否存在 `default` 网络。

- 源主机 VM 有两个网卡：`default` (NAT) + `br-ovs` (OVS)
- 目标主机可能没有 `default` 网络（如 OVS-only 环境）
- `define` 命令强制将 OVS 网卡改为 `default`，但目标主机 `default` 网络不存在或无法启动（与 `br-ovs` 冲突）
- 结果：VM 定义成功但无法启动

### 问题 2：迁移快照 backing file 残留

`pull` 命令通过 `dd` 流式传输磁盘，传输完成后 qcow2 文件保留了源端快照的 backing file 引用：

```
backing file: /vm-disks/vmdrmrj6v2.qvmconsole_migration_20260921144257
```

该路径在目标主机不存在，导致 QEMU 无法启动 VM。

## 修复内容

### Agent 修改（`QVMtools/agent/internal/agent/`）

#### 1. 网络适配智能化（`commands.go`）

| 函数 | 修改 |
|------|------|
| `runDefine` | 迁移前通过 SSH 检测目标主机是否有 `default` 网络，据此决定网络适配策略 |
| `checkNetworkExists` | **新增**：通过 `virsh net-info` 检查目标网络是否存在 |
| `adaptNetworkForTarget` | **新增**：替代原 `adaptNetwork`，根据目标网络情况适配 XML |
| `removeNetworkInterface` | **新增**：移除指定网络的 `<interface>` 块，保留其他网卡 |
| `removeVirtualport` | **新增**：移除 OVS `<virtualport>` 块 |
| `adaptXMLForTarget` | 移除 `adaptNetwork` 调用，网络适配延迟到 `runDefine` |

逻辑：
- 目标有 `default` 网络 → 激活 + OVS 转 NAT
- 目标无 `default` 网络 → 移除 `default` 网卡，保留 OVS

#### 2. qcow2 backing file 清理（`transfer.go`）

| 函数 | 修改 |
|------|------|
| `pullDisk` | 校验和通过后调用 `rebaseRemoteDisk` |
| `rebaseRemoteDisk` | **新增**：在目标主机执行 `qemu-img rebase -u -b ''` 清除 backing file |

**SSH 参数拆分问题**：`exec.Command("ssh", args...)` 将每个参数独立传递，SSH 收到后用空格拼接发给远端 shell。`bash -c "qemu-img rebase -u -b '' ..."` 中的 `''` 空引号被拆分丢失，导致 rebase 命令静默失败。

解决方案：在源端创建脚本文件，通过 `cmd.Stdin = scriptFile`（stdin 重定向）将内容传给远端 `bash` 执行，绕过参数拼接问题。

## 验证方法

1. 准备源主机（有 `default` + OVS 网卡）和目标主机（仅有 OVS）
2. 执行迁移，检查目标 VM XML：
   - 应仅保留 OVS `br-ovs` 网卡
   - 无 `<interface type='network'>` 块
3. 检查 qcow2 backing file：
   ```bash
   qemu-img info /path/to/disk.qcow2 | grep "backing file"
   # 应无输出
   ```
4. 启动 VM 应成功

## 影响范围

- 仅影响跨节点迁移的 `define` 和 `pull` 阶段
- 不影响单节点克隆、快照等其他功能
- 向后兼容：目标主机有 `default` 网络时行为不变
