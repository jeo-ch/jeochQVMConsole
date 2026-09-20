# VM 整机迁移测试手册

## 1. 架构概览

```
控制台 ──HTTP──> 网关(gateway) ──WS 长连接──> 源主机 Agent(agent)
                 │                                │
                 │    ┌────────────────────────────┘
                 │    │ 执行 virsh / ssh / dd / sha256sum
                 │    │ 回传 progress / result
                 ▼    ▼
            taskqueue ──> MigrationService.RunMigration
```

迁移流程：`discover` → `snapshot` → `pull`(SSH 直传) → `cutover`(关机) → `cleanup`(删快照)

## 2. 环境要求

### 源主机（运行 agent）
| 工具 | 用途 | 安装 |
|------|------|------|
| libvirt / virsh | 域管理、快照 | `apt install libvirt-client` |
| qemu-img | 磁盘信息查询 | `apt install qemu-img` |
| ssh | 到目标节点的直传 | `apt install openssh-client` |
| sha256sum | 校验 | `apt install coreutils` |
| dd | 目标端写入 | `apt install coreutils` |
| sshpass（可选） | 密码鉴权 | `apt install sshpass` |

### 目标主机（接收磁盘）
- SSH 服务可达
- 目标目录有足够磁盘空间
- 已安装 `sha256sum`、`dd`

### 网关
- Go 1.21+
- SQLite（内嵌，无需额外安装）
- 依赖：`gin-gonic/gin`、`gorilla/websocket`、`gorm.io/gorm`

## 3. 编译

```bash
cd QVMtools/agent
GOOS=linux GOARCH=amd64 go build -o kvm-agent .

cd ../gateway
go build -o kvm-gateway .
```

## 4. 启动网关

```bash
# 环境变量（均有默认值）
export GATEWAY_PORT=8090
export GATEWAY_TOKEN_TTL_SECONDS=1800    # 令牌有效期 30 分钟
export GATEWAY_DB_PATH=./data/gateway.db
export GATEWAY_LOG_LEVEL=debug

./kvm-gateway
```

网关启动后监听 `:8090`，提供以下 API：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/gateway/hosts/:id/token` | 为源主机签发一次性连接令牌 |
| GET | `/api/gateway/hosts/:id/status` | 查询源主机连接状态 |
| POST | `/api/gateway/migrations` | 发起整机迁移任务 |
| GET | `/api/gateway/agent/connect?token=xxx` | agent WS 回连端点 |

## 5. 部署 Agent

```bash
# 上传到源主机
scp kvm-agent user@源主机:/opt/kvm-agent

# 在源主机启动
ssh user@源主机
sudo /opt/kvm-agent \
  --gateway ws://网关IP:8090/api/gateway/agent/connect \
  --token <从网关获取的令牌>
```

### 获取令牌

```bash
curl http://网关IP:8090/api/gateway/hosts/1/token
# 返回: {"token":"xxxxx","url":"ws://网关IP:8090/api/gateway/agent/connect?token=xxxxx"}
```

## 6. 逐命令测试

### 6.1 discover — 枚举源主机 VM

```bash
# 通过网关 API 触发（需先有 agent 连接）
curl http://网关IP:8090/api/gateway/hosts/1/status
# 返回: {"host_id":1,"connected":true}
```

Agent 收到 discover 后返回：
```json
{
  "vms": [
    {
      "name": "test-vm",
      "id": "test-vm",
      "uuid": "xxxx-xxxx",
      "state": "running",
      "cpus": 2,
      "memory_mb": 2048,
      "disks": [
        {
          "target": "vda",
          "source_path": "/var/lib/libvirt/images/test-vm.qcow2",
          "size_bytes": 21474836480,
          "format": "qcow2"
        }
      ]
    }
  ]
}
```

### 6.2 snapshot — 创建快照

```bash
curl -X POST http://网关IP:8090/api/gateway/migrations \
  -H 'Content-Type: application/json' \
  -d '{
    "source_host_id": 1,
    "target_host_id": 2,
    "vm_name": "test-vm",
    "disk_target": "vda",
    "snapshot_name": "test-snap-001",
    "target_disk_path": "/data/test-vm.qcow2",
    "target_ssh": {
      "host": "目标IP",
      "port": "22",
      "user": "root",
      "auth_method": "key",
      "key_content": "<base64编码的私钥>"
    },
    "shutdown": false
  }'
```

### 6.3 pull — 传输磁盘（手动测试）

Agent 会自动完成：创建快照 → 解析 backing file → SSH 直传 → sha256 校验

传输完成后返回：
```json
{
  "checksum": "a1b2c3d4...",
  "size": 21474836480,
  "path": "/data/test-vm.qcow2"
}
```

### 6.4 cutover — 关闭源 VM

```bash
# 通过迁移任务的 shutdown: true 参数触发
# 或手动调用 cutover action
```

Agent 执行：`virsh shutdown test-vm` → 等待30s → 超时则 `virsh destroy test-vm`

### 6.5 cleanup — 清理快照

迁移完成后自动清理，或手动触发：
```bash
# 网关 RunMigration 末尾自动调用
```

Agent 执行：`virsh snapshot-delete test-vm test-snap-001 --current`

## 7. 完整迁移流程测试

### 场景 A：在线 VM 迁移（推荐）

```bash
# 1. 确认源主机有运行中的 VM
virsh list --running

# 2. 确认目标主机 SSH 可达
ssh root@目标IP "echo ok"

# 3. 确认目标目录有足够空间
ssh root@目标IP "df -h /data"

# 4. 通过网关发起迁移（shutdown=true 会在传输后关机）
curl -X POST http://网关IP:8090/api/gateway/migrations \
  -H 'Content-Type: application/json' \
  -d '{
    "source_host_id": 1,
    "target_host_id": 2,
    "vm_name": "test-vm",
    "disk_target": "vda",
    "target_disk_path": "/data/test-vm.qcow2",
    "target_ssh": {
      "host": "目标IP",
      "port": "22",
      "user": "root",
      "auth_method": "key",
      "key_content": "<base64>"
    },
    "shutdown": true
  }'

# 5. 观察网关日志中的进度回调
# 6. 传输完成后验证目标主机上的磁盘
ssh root@目标IP "qemu-img info /data/test-vm.qcow2"
```

### 场景 B：离线 VM 迁移

```bash
# 1. 先关闭源 VM
virsh shutdown test-vm

# 2. 发起迁移（无快照，直接传输）
curl -X POST http://网关IP:8090/api/gateway/migrations \
  -H 'Content-Type: application/json' \
  -d '{
    "source_host_id": 1,
    "target_host_id": 2,
    "vm_name": "test-vm",
    "disk_target": "vda",
    "target_disk_path": "/data/test-vm.qcow2",
    "target_ssh": {
      "host": "目标IP",
      "port": "22",
      "user": "root",
      "auth_method": "key",
      "key_content": "<base64>"
    },
    "shutdown": false
  }'
```

## 8. 快速单机验证（无第二台机器）

```bash
# 终端 1：启动网关
cd QVMtools/gateway
go run . --port 8090

# 终端 2：启动 agent（指向本机网关）
cd QVMtools/agent
# 先从网关获取令牌
TOKEN=$(curl -s http://127.0.0.1:8090/api/gateway/hosts/1/token | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
go run . --gateway ws://127.0.0.1:8090/api/gateway/agent/connect --token "$TOKEN"

# 终端 3：测试 discover
curl http://127.0.0.1:8090/api/gateway/hosts/1/status

# 注：pull 需要目标 SSH，单机可自循环测试（需两块盘或环回设备）
```

## 9. 故障排查

| 现象 | 排查 |
|------|------|
| agent 启动报 `dial gateway` | 检查网关地址、端口、防火墙 |
| agent 启动报 `register failed` | 检查令牌是否过期（TTL 默认 30 分钟） |
| discover 返回空 VM 列表 | 检查 virsh 权限（需 root 或 libvirt 组） |
| pull 报 `source disk not found` | 检查 VM 磁盘路径是否正确 |
| pull 报 `ssh transfer` 失败 | 检查目标 SSH 连通性、密钥、端口 |
| pull 报 `checksum mismatch` | 传输中断或磁盘损坏，重试 |
| pull 报 `sshpass: command not found` | 安装 sshpass 或改用 key 鉴权 |
| snapshot 报错 | 检查 VM 是否在运行、磁盘空间是否充足 |
| cutover 超时 | 30s 后自动强杀（virsh destroy） |

## 10. 配置参考

### 网关环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `GATEWAY_PORT` | 8090 | 监听端口 |
| `GATEWAY_TOKEN_TTL_SECONDS` | 1800 | 令牌有效期（秒） |
| `GATEWAY_TOKEN_MAX_LENGTH` | 256 | 令牌最大长度 |
| `GATEWAY_TASK_WORKERS` | 3 | 任务队列并发数 |
| `GATEWAY_DB_PATH` | `./data/gateway.db` | SQLite 数据库路径 |
| `GATEWAY_LOG_LEVEL` | `info` | 日志级别（debug/info/warn） |

### Agent 命令行参数

| 参数 | 环境变量 | 说明 |
|------|----------|------|
| `--gateway` | `AGENT_GATEWAY_URL` | 网关 WS 端点地址 |
| `--token` | `AGENT_TOKEN` | 注册令牌 |
