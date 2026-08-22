# 虚拟机动态内存设计文档

> 本文档描述动态内存功能的完整架构、配置链路、运行时调度与兼容处理策略。
> 源码目录：`server/service/vm/memory/`
> 前端：`web/src/features/vm-form/`（创建 / 编辑表单）、`web/src/features/vm-form/dialogs/MemoryDynamicDialog.tsx`、`VirtioMemDetailDialog.tsx`
> 2026-08 更新：补充合并上游后端断链修复记录（详见下文「合并断链修复」章节）。

## 功能总览

动态内存功能提供两种后端模式：

| 模式 | 后端标识 | 说明 |
|------|---------|------|
| 气球调度 | `balloon` | 基于 virtio-balloon 调整运行中 VM 的当前内存；Linux 可配合 free page reporting（FPR）回收空闲页。默认模式。 |
| Windows 弹性内存 | `virtio_mem` | 实验功能。基于 virtio-mem 设备提供弹性内存，主表单内存作为规格内存，基础内存按 50% 自动计算，最大上限默认上浮 30%；运行后按 70% / 50% 使用率阈值自动伸缩。仅 Windows。 |

调度器按宿主机内存压力与虚拟机内实际使用率自动伸缩，支持手动暂停自动调度（10 分钟冷却窗口）与观测期（新配置应用后观察一定小时数再参与调度）。

## 数据模型

### 请求体（GB 为单位，`VMMemoryDynamicRequest`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `dynamic_enabled` | *bool | 是否启用动态内存 |
| `memory_backend` | string | `balloon` / `virtio_mem` |
| `memory_initial` | int | 初始内存（GB），即规格内存 |
| `memory_min` | int | 最小内存（GB） |
| `memory_max` | int | 最大内存（GB） |
| `memory_auto_balloon` | *bool | 是否启用自动调度 |
| `memory_current` | int | 编辑模式下手动调整的当前内存（GB），仅运行中生效 |

### 接口返回（MB 为单位，`VMMemoryDynamicInfo`）

| 字段 | 说明 |
|------|------|
| `memory_dynamic_enabled` | 是否启用 |
| `memory_backend` | 当前后端 |
| `memory_initial` / `memory_min` / `memory_max` | 初始 / 最小 / 最大（MB） |
| `memory_virtio_mem_current` | virtio-mem 当前大小（MB） |
| `memory_auto_balloon` | 是否自动调度 |
| `memory_pending_apply` | 是否存在待迁移配置（运行中修改，待关机后启动时应用） |
| `memory_compat_mode` | 兼容模式：`dynamic` / `pending_apply` |
| `memory_balloon_supported` | 是否支持 balloon |
| `memory_balloon_status` | balloon 状态 |
| `memory_observation_until` | 观测期截止时间戳（秒） |
| `memory_manual_pause_until` | 手动暂停自动调度截止时间戳（秒） |

### 持久化（`VMMemoryMetadata`，写入 libvirt domain metadata）

动态内存配置**不落数据库**（遵循「虚拟化层不依赖数据库」原则），持久化在 libvirt domain 的 `<metadata><memoryConfig>` 中，保证与运行中的 VM 同步，且迁移 / 克隆后仍保留：

```xml
<metadata>
  <memoryConfig xmlns="">
    version, dynamic_enabled, memory_backend, memory_initial_mb, memory_min_mb,
    memory_max_mb, auto_balloon, pending_apply, observation_until, manual_pause_until, updated_at
  </memoryConfig>
</metadata>
```

写入：`WriteVMMemoryMetadata`；读取：`ReadVMMemoryMetadata`。

## 配置链路

动态内存配置在三条链路中均以 `memoryMeta *VMMemoryMetadata` 贯穿（Build → Apply → Write），任一环节缺失即功能静默失效（JSON 反序列化忽略未知字段不报错）。

### 1. 新建虚拟机（ISO 安装）

- `server/handler/vm_create.go` / `user_storage.go`：映射请求 `MemoryDynamic` 到 `CreateVMParams`；非管理员调用 `sanitizeUserMemoryDynamicRequest` 限制取值。
- `server/service/vm/create.go:341`：`BuildVMMemoryMetadataForCreate(ram, params.MemoryDynamic)` 计算默认值并生成 metadata。
- `create.go:485`：`ApplyMemoryMetadataToDomainXML(vmXML, memoryMeta, enableFPR)` 将配置注入 VM XML（balloon 走 `ApplyDynamicMemoryConfigToDomainXML`，virtio_mem 走 `ApplyVirtioMemConfigToDomainXML`）。
- `create.go:693`：`WriteVMMemoryMetadata` 持久化 metadata。

### 2. 克隆（模板克隆 + 链接克隆）

- `server/service/clone/core.go:280` / `linked_clone.go:217`：`BuildVMMemoryMetadataForCreate(params.RAM, params.MemoryDynamic)`。
- `windows_init.go:409` / `xml.go:140` / `linked_clone.go:282`：`ApplyMemoryMetadataToDomainXML` 注入。
- `windows_init.go:497` / `xml.go:257` / `linked_clone.go:391`：`WriteVMMemoryMetadata` 持久化。
- 批量克隆与单克隆字段独立（`CloneVmRequest`/`BatchCloneRequest`），需同步补齐。

### 3. 编辑（运行中 / 关机态）

- `server/handler/vm.go`：
  - `vm_memory.SetVMMemoryCurrent(name, req.MemoryDynamic.MemoryCurrent*1024, true)`：手动调整当前内存（运行中，10 分钟暂停自动调度）。
  - `vm_memory.SetVMMemoryDynamicConfig(name, req.MemoryDynamic)`：应用完整配置。
- `server/service/vm/memory/config.go`：
  - **关闭动态内存**：恢复静态内存（`ApplyStaticMemoryConfigToDomainXML`）+ 写关闭态 metadata。
  - **运行中启用 balloon**：校验存在可用 memballoon，仅写 `pending_apply=true` 的 metadata，**下次关机后启动时应用**（`ApplyPendingVMMemoryConfig` 在开机前自动应用）。
  - **关机态启用 balloon / virtio_mem**：直接改写 inactive XML + 写 metadata。
  - **virtio_mem 运行中**：禁止修改基础配置（须先关机）；基础配置未变更时幂等返回。

### 详情 / 列表回显

- `server/service/vm/detail.go:134`：`GetVMMemoryDynamicInfo(name, xmlStr, vm.Status)` 从 XML + metadata 推导动态内存信息；旧 VM 无 metadata 时推断为静态兼容。
- `server/service/vm/list.go:136`：`applyMemoryDynamicInfoToVMInfo` 将信息合入 `VmInfo`（批量列表展示）。

## 运行时调度

### 启动

`server/main.go:125` → `memory.StartMemoryBalloonScheduler()`：

1. `registerDynamicMemorySchedulers()` 注册两个调度器（balloon / virtio_mem），通过 `HookMemoryRegisterScheduler` 交给 scheduler 服务统一管理。
2. 每 `DynamicMemoryIntervalSeconds`（默认 60s）执行一次 `runMemoryBalloonScheduleOnce`：
   - 读取宿主机内存压力（`/proc/meminfo`，遵循「优先命令获取」原则）。
   - 遍历 VM 列表，对启用动态内存且非观测期 / 手动暂停期的 VM 执行 `scheduleVMMemory`（balloon）或 `scheduleVMVirtioMem`（virtio_mem）。

### balloon 调度算法

- 宿主机内存压力高（`HostReserveMB` / `HostReservePercent` 兜底）：按 `usedMB*1.35` 与 `actualMB*1.25` 取大并夹在 `[actualMB+256, MemoryMaxMB]`，触发扩容。
- 内存压力低：按 `usedMB*1.25` 与回收下限取大，夹在 `[MemoryMinMB, actualMB-256]`，触发回收。
- 观测期（`DynamicMemoryObservationHours`，默认 2 小时）与手动暂停期（10 分钟）内不调度。

### virtio_mem 调度算法

- 使用率 > `IncreaseThresholdPercent`（默认 70%）且低于最大值：扩容至目标（`calculateVirtioMemScheduleTarget`，基于使用量 + 余量）。
- 使用率 < `ReclaimThresholdPercent`（默认 50%）且高于初始值：回收。

### 设置项（系统设置，`server/config/config.go`）

| 设置项 | 默认 | 说明 |
|--------|------|------|
| `dynamic_memory_scheduler_enabled` | true | 调度器总开关 |
| `dynamic_memory_interval_seconds` | 60 | 调度周期 |
| `dynamic_memory_host_reserve_mb` / `_percent` | - | 宿主机内存预留 |
| `dynamic_memory_increase_threshold_percent` | 70 | virtio_mem 扩容阈值 |
| `dynamic_memory_reclaim_threshold_percent` | 50 | virtio_mem 回收阈值 |
| `dynamic_memory_cooldown_seconds` | - | 冷却时间 |
| `dynamic_memory_observation_hours` | 2 | 观测期 |

> 阈值类参数均已接入系统设置；调度算法内部的缓冲系数（1.35 / 1.25 / ±256MB）与 10 分钟手动暂停窗口属于策略细节，保持代码内常量。

## Balloon 调度算法内部常量（策略细节，代码内常量）

| 常量 | 数值 | 用途 | 位置 |
|------|------|------|------|
| `BalloonExpandMultUsed` | 1.35 | 扩容时：已用内存 × 1.35 作为目标下界 | `scheduler.go:259` |
| `BalloonExpandMultActual` | 1.25 | 扩容时：当前内存 × 1.25 作为目标下界 | `scheduler.go:259` |
| `BalloonExpandMinStepMB` | 256 | 扩容最小步进（MB），避免频繁微调 | `scheduler.go:259` |
| `BalloonReclaimMultUsed` | 1.25 | 回收时：已用内存 × 1.25 作为目标上界 | `scheduler.go:287` |
| `BalloonReclaimMinStepMB` | 256 | 回收最小步进（MB） | `scheduler.go:288` |
| `ManualPauseMinutes` | 10 | 手动暂停自动调度时长（分钟） | `config.go:160` |

> 这些常量属于调度策略内部实现细节，当前不暴露为系统设置。如需调整，需修改代码并重新编译。virtio_mem 阈值（扩容/回收使用率）已接入系统设置（`IncreaseThresholdPercent` / `ReclaimThresholdPercent`）。

## 兼容处理策略

部分 libvirt / QEMU 环境在 balloon 动态内存与 virtio-mem 弹性内存切换后，可能保留旧的 `<devices><memory model='virtio-mem'>` 设备。旧版本在应用 balloon 配置时会全局替换 `<memory>` 节点，可能把设备区内存节点改成缺少 `model` 属性的普通 `<memory>`，导致虚拟机开机前定义域失败。

典型错误：

```text
XML error: Missing required attribute 'model' in element 'memory'
```

处理策略（`server/service/vm/memory/xml.go`）：

- 应用 balloon 动态内存配置时，只修改 `<vcpu>` 之前的顶层 `<memory>` 与 `<currentMemory>`（`replaceTopLevelMemoryElement`）。
- 从 virtio-mem 切换到 balloon 或静态内存时，自动移除旧的 virtio-mem 设备（`removeVirtioMemDomainDevices`）。
- 从 virtio-mem 切换回 balloon 时，自动清理 virtio-mem 专用 `maxMemory` 与 NUMA 配置（`removeVirtioMemNumaConfig` / `removeDomainMaxMemoryElement`），避免 libvirt 继续按内存热插拔校验。
- 如果旧配置中已经残留缺少 `model` 的设备区 `<memory>` 节点，应用动态内存配置时会自动清理（`removeMemoryDevicesWithoutModel`）。
- 应用 virtio-mem 配置时，同样先清理旧 virtio-mem 设备与坏的设备区内存节点（`ensureVirtioMemNumaCell` / `rewriteDevicesMemoryElements`），再重新注入合法的 `<memory model='virtio-mem'>`。

## 运维建议

遇到该错误时，优先让虚拟机关机后重新应用动态内存配置或再次开机。开机前的待迁移配置会自动修复 XML；不需要手工编辑数据库。

若仍失败，可在宿主机上检查持久化 XML：

```bash
virsh -c qemu:///system dumpxml <虚拟机名>
```

确认 `<devices>` 内不存在缺少 `model` 属性的 `<memory>` 节点。

## 合并断链修复（2026-08）

将上游 `main` 合并进本地分支时，动态内存整条链路被静默切断（JSON 反序列化忽略未知字段不报错）。以下断链点已在合并后逐一修复，回归基准如下：

| 断链点 | 修复 |
|--------|------|
| `main.go` 未启动调度器 | 恢复 `vmmemory.StartMemoryBalloonScheduler()`（`StartStatsCollector` 之后） |
| `hooks_init.go` 未注入 4 个 scheduler hook | 恢复 `HookMemoryRegisterScheduler` / `HookMemoryStartSchedulerEvent` / `HookMemoryFinishSchedulerEventOk` / `HookMemoryFinishSchedulerEventFail`，因 memory 包自有 `SchedulerDefinition` / `SchedulerEventStartInput` 类型，需逐字段转换 |
| `CreateVMParams` 缺失 `MemoryDynamic` | 恢复字段 + create.go 三段消费（Build → Apply → Write）+ handler 映射 + 非管理员 sanitize |
| `VmEditRequest` 缺失 `MemoryDynamic` | 恢复字段 + EditVm 的 `SetVMMemoryCurrent` / `SetVMMemoryDynamicConfig` 完整块 + `buildVirtioMemRequestFromSpec` / `getVirtioMemSpecMemoryGB` 辅助函数 |
| `VmInfo` 缺失动态内存字段 | 恢复 11 个字段 + `VmDetail` 的 `ObservationUntil` / `ManualPauseUntil` + `detail.go` / `list.go` 调用 `GetVMMemoryDynamicInfo` |
| 克隆链路不消费 `MemoryDynamic` | 恢复 `core.go` / `windows_init.go` / `xml.go` / `linked_clone.go` 的 Build → Apply → Write + handler sanitize（单克隆 + 批量克隆） |
| 前端 detailConfig 缺失动态内存字段 | EditVmForm detailConfig 补 15 个字段（修复 SSE 签名去重失效导致的回显缺失）+ `api/vm.ts` 补 2 个时间戳字段 |
| 推荐逻辑口径漂移 | `ensureMemoryDynamicDefaults` 统一复用 `recommendedMemoryDynamicValues`（recommend.ts），消除三处独立计算的差异 |
| 死代码残留 | 删除 `applyEditVmDetail` / `applyRecommendedMemoryDynamicValues` / `getRecommendedWindowsBootType` / `CloneTaskHandler` / `RegisterCloneHandlers` / `LinuxCloneIPWaitSeconds` |

> 回归验证：`server` 下 `go build ./...` + `go vet ./...`，`web` 下 `npx tsc -b` 均通过；`web/scripts/generate-api-endpoints.mjs` 已重新生成接口文档（342 个端点）。