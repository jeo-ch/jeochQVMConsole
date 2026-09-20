# VM 整机迁移 — 完整测试手册

## 1. 架构概览

```
控制台 / curl ──HTTP──> 网关(gateway) ──WS 长连接──> 源主机 Agent(agent)
                       │                                │
                       │    ┌────────────────────────────┘
                       │    │  virsh / cat | ssh dd / sha256sum
                       │    │  回传 progress / result / error
                       ▼    ▼
                  taskqueue ──> MigrationService.RunMigration
```

### 迁移阶段

| 阶段 | Agent 动作 | 说明 |
|------|-----------|------|
| **discover** | `virsh list --all --name` + `virsh dumpxml` | 枚举源主机所有 VM 及磁盘 |
| **snapshot** | `virsh snapshot-create-as --disk-only --atomic --live` | 创建在线快照（一致性检查点） |
| **pull** | `cat 磁盘 \| ssh 目标 dd of=...` + `sha256sum` 校验 | SSH 流式直传 + 一致性校验 |
| **cutover** | `virsh shutdown` → 30s 超时 → `virsh destroy` | 关闭源 VM |
| **cleanup** | `virsh snapshot-delete --current` | 删除临时快照，恢复磁盘链 |

### 关键设计

- **令牌一次性**：网关签发 SHA256 哈希令牌，agent 连接后即消费，不可重用
- **快照由网关统一管理**：网关创建 → agent 用 → 网关清理；agent 仅在无快照名且 VM 运行时自建（并 defer 清理）
- **流式传输**：`cat | ssh dd`，内存占用恒定（不落本地临时文件）
- **backing file 回溯**：快照后磁盘源指向 overlay，agent 自动解析 backing file 链定位真实磁盘

---

## 2. 环境要求

### 2.1 源主机（运行 Agent）

| 工具 | 用途 | 安装命令 |
|------|------|---------|
| libvirt / virsh | VM 管理、快照 | `apt install libvirt-daemon-system libvirt-clients` |
| qemu-img | 磁盘信息查询 | `apt install qemu-utils` |
| openssh-client | SSH 到目标节点 | `apt install openssh-client` |
| sha256sum | 传输校验 | `apt install coreutils` |
| dd | 目标端写入 | `apt install coreutils` |
| sshpass（可选） | 密码鉴权 | `apt install sshpass` |

**权限要求**：Agent 需要 root 或 libvirt 组权限执行 virsh 命令。

### 2.2 目标主机（接收磁盘）

- SSH 服务可达（默认端口 22）
- 目标目录存在且有足够磁盘空间（`df -h` 确认）
- 已安装 `sha256sum`、`dd`（通常 coreutils 已包含）

### 2.3 网关

- Go 1.21+
- SQLite（内嵌，无需额外安装）
- 依赖：`gin-gonic/gin`、`gorilla/websocket`、`gorm.io/gorm`、`gorm.io/driver/sqlite`

---

## 3. 编译

### 3.1 交叉编译 Agent（在开发机编译 Linux 二进制）

```bash
cd QVMtools/agent

# amd64
GOOS=linux GOARCH=amd64 go build -o kvm-agent-amd64 .

# arm64（可选）
GOOS=linux GOARCH=arm64 go build -o kvm-agent-arm64 .
```

### 3.2 编译 Gateway（本机或服务器编译）

```bash
cd QVMtools/gateway

# 本地编译
go build -o kvm-gateway .

# 或交叉编译
GOOS=linux GOARCH=amd64 go build -o kvm-gateway-amd64 .
```

### 4.3 无网络编译

```bash
GOFLAGS=-mod=mod GOPROXY=off GOSUMDB=off go build ./...
```

---

## 4. 打包方案

### 4.1 Agent 发行包

```bash
# 创建发布目录
mkdir -p release/agent
cp kvm-agent-amd64 release/agent/kvm-agent
chmod +x release/agent/kvm-agent

# 编写 systemd 服务文件
cat > release/agent/kvm-agent.service << 'EOF'
[Unit]
Description=QVMConsole Migration Agent
After=network.target libvirtd.service
Wants=libvirtd.service

[Service]
Type=simple
ExecStart=/opt/kvm-agent/kvm-agent --gateway ws://GATEWAY_IP:8090/api/gateway/agent/connect --token YOUR_TOKEN
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# 打包
tar czf kvm-agent-v1.0.0-linux-amd64.tar.gz -C release/agent .
```

### 4.2 Gateway 发行包

```bash
mkdir -p release/gateway
cp kvm-gateway-amd64 release/gateway/kvm-gateway
chmod +x release/gateway/kvm-gateway

cat > release/gateway/kvm-gateway.service << 'EOF'
[Unit]
Description=QVMConsole Migration Gateway
After=network.target

[Service]
Type=simple
Environment=GATEWAY_PORT=8090
Environment=GATEWAY_DB_PATH=/opt/kvm-gateway/data/gateway.db
Environment=GATEWAY_LOG_LEVEL=info
ExecStart=/opt/kvm-gateway/kvm-gateway
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

tar czf kvm-gateway-v1.0.0-linux-amd64.tar.gz -C release/gateway .
```

---

## 5. 部署步骤

### 5.1 部署网关（在网关服务器）

```bash
# 1. 上传并解压
tar xzf kvm-gateway-v1.0.0-linux-amd64.tar.gz -C /opt/kvm-gateway/

# 2. 创建数据目录
mkdir -p /opt/kvm-gateway/data

# 3. 安装 systemd 服务
cp /opt/kvm-gateway/kvm-gateway.service /etc/systemd/system/
systemctl daemon-reload

# 4. 启动网关
systemctl enable kvm-gateway
systemctl start kvm-gateway

# 5. 验证网关启动
curl http://127.0.0.1:8090/api/gateway/hosts/1/status
# 预期返回: {"host_id":1,"connected":false}
```

### 5.2 部署 Agent（在源主机）

```bash
# 1. 从网关获取令牌
TOKEN=$(curl -s http://网关IP:8090/api/gateway/hosts/1/token | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
echo "Token: $TOKEN"

# 2. 上传 Agent
tar xzf kvm-agent-v1.0.0-linux-amd64.tar.gz -C /opt/kvm-agent/
chmod +x /opt/kvm-agent/kvm-agent

# 3. 编写 systemd 服务（替换 GATEWAY_IP 和 TOKEN）
cat > /etc/systemd/system/kvm-agent.service << EOF
[Unit]
Description=QVMConsole Migration Agent
After=network.target libvirtd.service
Wants=libvirtd.service

[Service]
Type=simple
ExecStart=/opt/kvm-agent/kvm-agent --gateway ws://网关IP:8090/api/gateway/agent/connect --token $TOKEN
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# 4. 启动 Agent
systemctl daemon-reload
systemctl enable kvm-agent
systemctl start kvm-agent

# 5. 验证 Agent 连接
curl http://网关IP:8090/api/gateway/hosts/1/status
# 预期返回: {"host_id":1,"connected":true}
```

### 5.3 验证连通性

```bash
# 检查网关日志
journalctl -u kvm-gateway -f

# 检查 Agent 日志
journalctl -u kvm-agent -f

# 测试 discover
curl http://网关IP:8090/api/gateway/hosts/1/discover
# 返回源主机 VM 列表
```

---

## 6. 逐命令详细测试

### 6.1 discover — 枚举源主机 VM

**目的**：确认 Agent 能正确读取源主机的 VM 和磁盘信息。

```bash
# 通过网关触发 discover（网关转发给 agent）
curl -s http://网关IP:8090/api/gateway/hosts/1/discover | python3 -m json.tool
```

**预期返回**：
```json
{
  "vms": [
    {
      "name": "test-vm",
      "id": "test-vm",
      "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
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

**验证点**：
- [ ] VM 列表非空
- [ ] 每个 VM 有 name、state、cpus、memory_mb
- [ ] 每个 VM 的 disks 包含 target、source_path、size_bytes、format
- [ ] source_path 文件路径在源主机存在

**故障排查**：
| 问题 | 原因 | 解决 |
|------|------|------|
| 返回空列表 | virsh 权限不足 | Agent 以 root 运行或加入 libvirt 组 |
| 连接超时 | Agent 未启动或网络不通 | 检查 Agent 日志和防火墙 |

### 6.2 snapshot — 创建快照

**目的**：验证在线 VM 能成功创建 disk-only 快照。

```bash
# 通过网关创建快照（snapshot_name 可自定义）
curl -s -X POST http://网关IP:8090/api/gateway/migrations \
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
      "key_content": "BASE64_PRIVATE_KEY"
    },
    "shutdown": false
  }'
```

**验证点**：
- [ ] 返回 `{"id": 1, "status": "pending"}`
- [ ] Agent 日志显示 "creating snapshot"
- [ ] 在源主机验证快照存在：
  ```bash
  virsh snapshot-list test-vm
  ```

**快照细节**：
- 使用 `--disk-only --atomic` 确保原子性
- `--live` 标志允许运行中 VM 创建快照
- 快照后磁盘源指向 overlay 文件，backing file 指向原始磁盘

### 6.3 pull — 传输磁盘

**目的**：验证 SSH 流式传输 + sha256 校验。

```bash
# pull 由 snapshot 自动触发，无需单独调用
# 观察 Agent 日志中的传输进度
journalctl -u kvm-agent -f
```

**传输流程**：
1. Agent 解析 VM XML 获取磁盘源路径
2. 如果有快照，回溯 backing file 找到真实磁盘
3. 计算源文件 sha256
4. 执行 `cat 源文件 | ssh 目标 dd of=目标路径 bs=1M`
5. 在目标端执行 `sha256sum` 并比对

**验证点**：
- [ ] 目标主机文件存在：`ssh root@目标IP "ls -la /data/test-vm.qcow2"`
- [ ] 文件大小一致：`ssh root@目标IP "stat -c %s /data/test-vm.qcow2"`
- [ ] 校验和一致：Agent 日志显示 checksum
- [ ] 磁盘格式正确：`ssh root@目标IP "qemu-img info /data/test-vm.qcow2"`

**故障排查**：
| 问题 | 原因 | 解决 |
|------|------|------|
| `source disk not found` | VM 磁盘路径错误 | 检查 `virsh dumpxml` 中的 source 路径 |
| `ssh transfer` 失败 | SSH 连通性问题 | 测试 `ssh root@目标IP "echo ok"` |
| `checksum mismatch` | 传输中断或磁盘损坏 | 重试传输 |
| `sshpass: command not found` | 未安装 sshpass | `apt install sshpass` 或改用 key 鉴权 |

### 6.4 cutover — 关闭源 VM

**目的**：验证 VM 关闭流程（ACPI → 超时强杀）。

```bash
# cutover 由 shutdown=true 参数触发
# Agent 先执行 virsh shutdown，等待 30s，超时则 virsh destroy
```

**关闭流程**：
1. `virsh shutdown test-vm`（ACPI 信号）
2. 每 2s 检查状态，最多等待 30s
3. 超时则 `virsh destroy test-vm`（强杀）

**验证点**：
- [ ] VM 状态变为 `shut off`
- [ ] 源主机 `virsh list --all` 显示 VM 为 shut off

### 6.5 cleanup — 清理快照

**目的**：验证快照清理恢复磁盘链。

```bash
# cleanup 在迁移完成后自动触发
# 验证快照已删除
ssh root@源主机 "virsh snapshot-list test-vm"
```

**验证点**：
- [ ] 快照列表为空
- [ ] 磁盘文件 backing file 链恢复正常

---

## 7. 完整迁移流程测试

### 场景 A：在线 VM 迁移（推荐）

```bash
# ========== 准备阶段 ==========
# 1. 确认源主机有运行中的 VM
ssh root@源主机 "virsh list --running"
# 预期: test-vm running

# 2. 确认目标主机 SSH 可达
ssh root@目标IP "echo ok"
# 预期: ok

# 3. 确认目标目录有足够空间（至少为 VM 磁盘大小的 1.2 倍）
ssh root@目标IP "df -h /data"
# 预期: 可用空间 > VM 磁盘大小

# 4. 记录源 VM 信息用于后续对比
ssh root@源主机 "virsh dumpxml test-vm" > /tmp/source-vm.xml
```

```bash
# ========== 执行迁移 ==========
# 5. 通过网关发起迁移
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
      "key_content": "BASE64_PRIVATE_KEY"
    },
    "shutdown": true
  }'

# 6. 观察网关日志
journalctl -u kvm-gateway -f
# 预期日志:
# [STEP 1/5] 创建快照
# [STEP 2/5] 拉取磁盘 (显示百分比进度)
# [STEP 3/5] 切换至目标主机
# [STEP 4/5] 清理快照
```

```bash
# ========== 验证阶段 ==========
# 7. 验证源 VM 已关闭
ssh root@源主机 "virsh list --all | grep test-vm"
# 预期: test-vm    shut off

# 8. 验证目标磁盘完整
ssh root@目标IP "qemu-img info /data/test-vm.qcow2"
# 预期: file format=qcow2, virtual size 与源一致

# 9. 验证校验和
ssh root@目标IP "sha256sum /data/test-vm.qcow2"

# 10. 验证快照已清理
ssh root@源主机 "virsh snapshot-list test-vm"
# 预期: 空列表
```

### 场景 B：离线 VM 迁移

```bash
# 1. 先关闭源 VM
ssh root@源主机 "virsh shutdown test-vm"
ssh root@源主机 "virsh list --all | grep test-vm"
# 确认状态: shut off

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
      "key_content": "BASE64_PRIVATE_KEY"
    },
    "shutdown": false
  }'

# 3. 验证同场景 A 的步骤 7-10
```

---

## 8. 单机自测（无第二台机器）

```bash
# ========== 终端 1：启动网关 ==========
cd QVMtools/gateway
go run . --port 8090

# ========== 终端 2：启动 Agent ==========
cd QVMtools/agent
TOKEN=$(curl -s http://127.0.0.1:8090/api/gateway/hosts/1/token | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
echo "Token: $TOKEN"
go run . --gateway ws://127.0.0.1:8090/api/gateway/agent/connect --token "$TOKEN"

# ========== 终端 3：测试各阶段 ==========
# 测试 discover
curl -s http://127.0.0.1:8090/api/gateway/hosts/1/discover | python3 -m json.tool

# 测试完整迁移（单机模式：源和目标都是本机）
# 注意：单机测试需确保目标路径有足够空间且与源磁盘不在同一物理位置
curl -X POST http://127.0.0.1:8090/api/gateway/migrations \
  -H 'Content-Type: application/json' \
  -d '{
    "source_host_id": 1,
    "target_host_id": 1,
    "vm_name": "test-vm",
    "disk_target": "vda",
    "target_disk_path": "/tmp/test-vm-migrated.qcow2",
    "target_ssh": {
      "host": "127.0.0.1",
      "port": "22",
      "user": "root",
      "auth_method": "key",
      "key_content": "BASE64_PRIVATE_KEY"
    },
    "shutdown": false
  }'
```

**单机测试限制**：
- 源和目标使用同一个 SSH，传输的是本机文件到本机
- 适合验证流程完整性，不适合性能测试
- 需要本机 SSH 服务可达（`ssh localhost` 能连通）

---

## 9. API 参考

### 9.1 签发令牌

```
GET /api/gateway/hosts/:id/token
```

**参数**：
- `id`：源主机 ID（HostNode 表的 ID）

**响应**：
```json
{
  "token": "base64url_token",
  "url": "ws://网关:8090/api/gateway/agent/connect?token=base64url_token"
}
```

### 9.2 查询主机状态

```
GET /api/gateway/hosts/:id/status
```

**响应**：
```json
{
  "host_id": 1,
  "connected": true
}
```

### 9.3 发起迁移

```
POST /api/gateway/migrations
Content-Type: application/json
```

**请求体**：
```json
{
  "source_host_id": 1,          // 源主机 ID（必填）
  "target_host_id": 2,          // 目标主机 ID（必填）
  "vm_name": "test-vm",         // VM 名称（必填）
  "disk_target": "vda",         // 磁盘设备名（默认第一个磁盘）
  "snapshot_name": "snap-001",  // 快照名（可选，自动生成）
  "target_disk_path": "/data/test-vm.qcow2", // 目标磁盘路径（必填）
  "format": "qcow2",           // 磁盘格式（默认 qcow2）
  "shutdown": true,             // 传输后是否关闭源 VM
  "target_ssh": {               // 目标 SSH 信息（必填）
    "host": "192.168.1.100",
    "port": "22",
    "user": "root",
    "auth_method": "key",       // key 或 password
    "key_content": "base64_private_key",
    "password": "optional_password"
  }
}
```

**响应**：
```json
{
  "id": 1,
  "status": "pending"
}
```

### 9.4 Agent WS 端点

```
GET /api/gateway/agent/connect?token=xxx
```

WebSocket 握手后，Agent 与网关通过 JSON 消息通信：
- **command**：网关下发命令（discover/snapshot/pull/cutover/cleanup）
- **result**：Agent 返回执行结果
- **progress**：Agent 上报进度（0-100 + 描述）
- **error**：Agent 返回错误

---

## 10. 配置参考

### 10.1 网关环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `GATEWAY_PORT` | `8090` | 监听端口 |
| `GATEWAY_TOKEN_TTL_SECONDS` | `1800` | 令牌有效期（秒），默认 30 分钟 |
| `GATEWAY_TOKEN_MAX_LENGTH` | `256` | 令牌最大长度 |
| `GATEWAY_TASK_WORKERS` | `3` | 任务队列并发数 |
| `GATEWAY_DB_PATH` | `./data/gateway.db` | SQLite 数据库路径 |
| `GATEWAY_LOG_LEVEL` | `info` | 日志级别：debug / info / warn |

### 10.2 Agent 命令行参数

| 参数 | 环境变量 | 说明 |
|------|----------|------|
| `--gateway` | `AGENT_GATEWAY_URL` | 网关 WebSocket 端点 |
| `--token` | `AGENT_TOKEN` | 注册令牌 |

### 10.3 Agent 内部参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| 心跳间隔 | 30s | Agent 向网关发送心跳 |
| cutover 超时 | 30s | virsh shutdown 等待时间 |
| SSH 连接超时 | 10s | SSH 握手超时 |
| 传输块大小 | 1M | dd bs=1M |

---

## 11. 故障排查

### 11.1 连接问题

| 现象 | 排查命令 | 解决方案 |
|------|---------|---------|
| Agent 启动报 `dial gateway` | `curl http://网关:8090/api/gateway/hosts/1/status` | 检查网关地址、端口、防火墙 |
| Agent 启动报 `register failed` | 检查令牌是否过期 | 重新获取令牌（TTL 默认 30 分钟） |
| Agent 连接后断开 | `journalctl -u kvm-agent -f` | 检查网络稳定性，Agent 会自动重连 |

### 11.2 VM 操作问题

| 现象 | 排查命令 | 解决方案 |
|------|---------|---------|
| discover 返回空列表 | `virsh list --all` | 检查 libvirtd 服务和权限 |
| snapshot 失败 | `virsh snapshot-list vm-name` | 检查 VM 是否运行、磁盘空间 |
| snapshot 报 `unable to execute` | `virsh version` | 升级 libvirt 到支持 --atomic 的版本 |

### 11.3 传输问题

| 现象 | 排查命令 | 解决方案 |
|------|---------|---------|
| pull 报 `source disk not found` | `virsh dumpxml vm-name \| grep source` | 检查磁盘路径 |
| pull 报 `ssh transfer` 失败 | `ssh root@目标 "echo ok"` | 检查 SSH 连通性、密钥、端口 |
| pull 报 `checksum mismatch` | `sha256sum 源文件` + `ssh 目标 sha256sum 目标文件` | 传输中断，重试 |
| pull 报 `sshpass: command not found` | `which sshpass` | `apt install sshpass` 或改用 key 鉴权 |
| 传输速度慢 | `iperf3 -s` / `iperf3 -c 目标IP` | 检查网络带宽 |

### 11.4 Cutover 问题

| 现象 | 排查命令 | 解决方案 |
|------|---------|---------|
| cutover 超时 | `virsh list --all` | 30s 后自动强杀（virsh destroy） |
| VM 无法关闭 | `virsh dominfo vm-name` | 检查 Guest Agent 是否响应 |

---

## 12. 安全注意事项

1. **令牌安全**：令牌为一次性使用，泄露后无法重用（SHA256 哈希存储）
2. **SSH 密钥**：Agent 将 base64 密钥解码写入临时文件（0600），使用后立即删除
3. **密码传输**：sshpass 密码通过 SSH 参数传递（非最佳实践，建议使用 key 鉴权）
4. **网络加密**：SSH 传输本身加密，无需额外 TLS
5. **快照一致性**：使用 `--atomic` 确保快照原子性，避免数据损坏

---

## 13. 文件清单

```
QVMtools/
├── agent/
│   ├── main.go                    # Agent 入口
│   ├── go.mod / go.sum
│   └── internal/agent/
│       ├── client.go              # WS 客户端 + 心跳
│       ├── commands.go            # 命令分发
│       ├── libvirt.go             # virsh 封装
│       ├── transfer.go            # SSH 传输 + 校验
│       ├── disk.go                # 磁盘信息查询
│       ├── exec.go                # 命令执行
│       ├── types.go               # 数据类型
│       └── util.go                # 工具函数
├── gateway/
│   ├── main.go                    # Gateway 入口
│   ├── go.mod / go.sum
│   ├── handler.go                 # HTTP API
│   ├── migration.go               # 迁移编排
│   ├── ws.go                      # WS Manager
│   ├── ws_handler.go              # WS 升级
│   ├── token.go                   # 令牌签发/消费
│   ├── routes.go                  # 路由注册
│   ├── helpers.go                 # 辅助函数
│   ├── utils.go                   # 工具函数
│   └── errors.go                  # 错误定义
└── MIGRATION-TEST-GUIDE.md        # 本手册
```
