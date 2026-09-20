# 整机迁移 Agent 可行性报告

## 1. 结论

可行。迁移的「发现 → 快照 → 拉取 → 切流 → 清理」全流程均可由源主机上的 agent 通过 libvirt（`virsh`）与标准 SSH 工具链完成，无需在源主机安装额外守护进程。agent 作为一次性 WS 客户端回连网关，按网关下发的 `action` 执行本地命令并回传进度与结果。

## 2. 架构

```
控制台 ──HTTP──> 网关(gateway) ──WS 长连接──> 源主机 Agent(agent)
                 │                    执行 virsh / ssh / dd / sha256sum
                 │                    回传 command.progress / command.result
                 ▼
            taskqueue(gateway_migration) ──> MigrationService.RunMigration
```

- 网关侧已实现 `MigrationService`（Discover → Snapshot → Pull → Cutover → Cleanup → RunMigration），本次优化已让其全链路透传 `context.Context`，并在 `DispatchCommand` 增加 `ctx.Done()` 超时/取消分支，修复了「agent 报错被丢弃」的 nil-error 问题。
- Agent 侧实现：WS 客户端回连、按 `action` 分发的命令表、每个命令对应的本地执行逻辑。

## 3. 命令接口（agent ← 网关）

网关 `DispatchCommand` 下发 envelope `{"type":"command","id":...,"data":{"action","params"}}`，agent 回传 `command.progress`（进度）与 `command.result`（结果，含 `status`/`data`/`message`）。

| action | params | 返回 data | 说明 |
|--------|--------|-----------|------|
| `discover` | — | `{"vms":[{name,id,uuid,state,cpus,memory_mb,disks:[{target,source_path,size_bytes,format}]}]}` | 列出源主机已注册 VM 与磁盘 |
| `snapshot` | `vm_name,snapshot_name,online=true` | — | 为在线 VM 创建 disk-only 原子快照 |
| `pull` | `vm_name,snapshot_name,target_host_id,target_ssh,target_disk_path,format,checksum=sha256` | `{checksum,size,path}` | 磁盘经 SSH 直传到目标节点并校验 |
| `cutover` | `vm_name` | — | 关闭源 VM 并标记迁移完成 |
| `cleanup` | `vm_name,snapshot_name` | — | 删除临时快照 |

目标节点 SSH 接入信息 `TargetSSH{host,port,user,auth_method,key_path,key_content,password}` 由控制台解析目标主机后注入，agent 在 `pull` 时使用。

## 4. libvirt 能力评估

### 4.1 discover
- `virsh list --all` 获取全部域；对每个域 `virsh dominfo` / `virsh dumpxml` 解析磁盘。
- 磁盘来源从 domain XML 的 `<disk><source file='...'/><target dev='...'/><driver .../>` 读取：`source_path` = file，`target` = dev，`format` = driver format，`size_bytes` = 由 `qemu-img info` 或 XML `<capacity>` 得到。
- 状态 `state` 由 `virsh dominfo` 的 State 得到（running/halted/shut off 等）。

### 4.2 snapshot（在线热快照）
- 命令：`virsh snapshot-create-as <vm> <name> --disk-only --atomic --no-metadata --diskname <name>`
- `--disk-only`：仅创建磁盘叠加层（thin qcow2 overlay），不保存域状态，适合整盘迁移。
- `--atomic`：临时文件 + 原子重命名，避免半截快照。
- `--no-metadata`：不保存 XML 元数据，仅磁盘快照，便于后续清理。
- 该步骤为 VM 提供一个一致的检查点与回滚安全网；快照创建期间 VM 继续运行（在线）。
- 风险：快照创建到磁盘传输完成之间，VM 对磁盘的写入落在叠加层上，基础盘（待传输文件）存在微小时间窗口的不一致。对于「拉取后切流关机」的一次性迁移模型可接受；若需严格一致，可后续增加二次同步（增量）步骤。

### 4.3 pull（磁盘直传 + sha256）
- 从 libvirt XML 定位目标磁盘的 `source_path`（ backing file）。
- 传输方式：`cat <source_path> | ssh -i <key> <user>@<host> [-p <port>] "dd of=<target_disk_path> bs=..."`，或先用 `scp`。流式传输，内存占用恒定，适合大文件。
- 校验：源端 `sha256sum <source_path>`，目标端对 `dd` 输出 `sha256sum`，两端比对；`checksum=sha256` 由网关固定传入。
- 进度：通过 `pv` 或 `dd` 的周期性状态估算百分比回传（若未安装 pv，按传输字节数与文件大小估算）。
- 目标磁盘格式：按 `format` 在目标端用 `qemu-img convert` 转为目标格式（默认 qcow2）。

### 4.4 cutover
- `virsh shutdown <vm>`（ACPI 优雅关机），超时则 `virsh destroy <vm>`（强杀）。
- 关闭后迁移切流完成，源 VM 进入 halted 状态。

### 4.5 cleanup
- `virsh snapshot-delete <vm> <name> --current` 删除临时快照，恢复磁盘链。

## 5. 依赖（源主机）

| 工具 | 用途 | 来源 |
|------|------|------|
| libvirt / virsh | 域管理、快照、XML 解析 | 系统包 `libvirt-client` |
| qemu-img | 磁盘格式/信息查询 | 系统包 `qemu-img` |
| ssh / scp | 到目标节点的直传 | OpenSSH 客户端 |
| sha256sum | 校验 | coreutils |
| dd | 目标端写入 | coreutils |
| pv（可选） | 传输进度估算 | coreutils 替代 |

详细依赖见 `docs/dependencies.md`。未安装时对应命令返回错误，agent 回传明确 message，网关任务失败可观测。

## 6. 安全与健壮性

- SSH 鉴权支持 key（`key_content` base64 解码写入临时文件，权限 0600）与 password（需 `sshpass`，未安装时退回 key）。
- 临时密钥文件用完即删，不落盘明文长期存储。
- 所有外部命令通过 `exec.Command` 显式参数调用，不做 shell 拼接，避免注入。
- SSH 告警（known_hosts、权限提示）经 `stripSSHWarnings` 过滤，避免污染输出。
- 大文件 IO 传输不设置超时（遵循异步 IO 无超时原则）。

## 7. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 在线快照与传输窗口不一致 | 一次性迁移模型可接受；后续可加增量同步 |
| 目标节点磁盘空间不足 | pull 前可在目标端 `df` / `qemu-img info` 校验容量（可选前置校验） |
| SSH 不可达目标节点 | `pull` 先做 SSH 连通性探测，失败提前回传 error |
| virsh 权限不足 | agent 需 root 或 libvirt 组权限；启动时探测并回传 |
| 传输中断 | 断点暂不支持；失败后网关可重跑，目标端先清理残盘 |

## 8. 落地顺序

可行性报告 → libvirt（discover/snapshot/cutover/cleanup）→ disk（磁盘元信息）→ transfer（SSH 直传 + sha256）→ commands.go（分发表）→ main.go（WS 客户端 + exec）。
