# 国产超融合竞品差异分析与优化建议（QVMConsole vs HCI 6.11 / SMTXOS 6.2）

> **文档版本**：v1.0  
> **生成日期**：2026-08-01  
> **参考对象**：  
> - 深信服 HCI 6.11.1 R1 x86（20251023）：`/Volumes/cs/QVMConsole/HCI6.11.1_R1_X86(20251023)`  
> - SmartX SMTXOS 6.2.0-P2 el7（250613184653）：`/Volumes/cs/QVMConsole/SMTXOS-6.2.0-P2-el7-250613184653-x86_64`  
> **文档状态**：◻ 待开发确认　◻ 部分采纳　◻ 全部采纳　◻ 已排期实施
>
> **开发确认签字（按需填写）**：
>
> | 角色 | 姓名 | 日期 | 备注 |
> |---|---|---|---|
> | 后端 | | | |
> | 前端 | | | |
> | 运维/构建 | | | |
> | 产品 | | | |

---

## §0 前置：产品定位差异（背景）

| 维度 | SMTXOS 6.2（SmartX 超融合 OS） | HCI 6.11（深信服 VMP） | **QVMConsole** |
|---|---|---|---|
| 发布形态 | **自编译 CentOS 7 发行版 ISO**（全盘 OS，内核/包全部定制） | **自解压 shell 大包**（附加在现有 Linux 上） | **附加式 install.sh 面板**（同 HCI） |
| 内核 | 自编译 4.19.90 v97 + MLNX OFED 5.4 + ice 1.10.1 + kpatch 热补丁 + kmod hotpatch | rd.gz 加密未分析 | **复用宿主机内核**（零定制） |
| libvirt/qemu | libvirt 6.2.0 rc145 深度补丁；qemu 6.2.0 SmartX 定制 | 未知 | **复用宿主机发行版包**（apt/dnf/yum 装） |
| 存储 | elf-fs 自研文件系统 + MD RAID1 + ZBS 规格分层（Normal 128TiB / Large 256TiB） | /sf/data/local 回环映像（5GB） | Quota 文件系统 + 用户项目映射 |
| 网络 | OVS 2.13.3 SmartX .6 定制 + consul 1.12.9 服务发现 + envoy-xds 服务网格 | 未知 | OVS（发行版包） |
| 防火墙 | firewalld **0.4.4.4**（ks.cfg 显式 `firewall --disabled`，默认关闭） | 未知 | ufw/firewalld 双后端（启动/未启动均支持） |
| SELinux | ks.cfg 显式 `selinux --disabled`（默认关闭） | 未知 | 兼容 enforcing/permissive，自动 restorecon |
| 国产 CPU | **HygonGenuine 海光显式分支**（启动参数 C-state/P-state/no5lvl） | 未知 | 仅 avx2/fma 探测，无海光/飞腾/兆芯 显式分支 |
| 组件化 | 20+ 独立 RPM：elfvirt/aquarium/dolphin/crab/fisheye/harbor/hpctl/consul/envoy/fluent-bit/… | vmp.pkg 大包整体升级 | 单二进制 + web-dist |
| 升级 | hotfix-package 增量 RPM + **kpatch 内核热补丁** | vmp.pkg 多阶段（unpack→ing→outcfg）+ 密码保护 | install.sh update 覆盖安装 |
| 观测 | fluent-bit + prometheus 2.37 + aurora-monitor + **bash 审计**（chattr +a） | 未知 | Diagnostics 页 collector.go ZIP 导出 |

> 结论：SMTXOS/HCI 是**「自底向上造发行版」**，QVMConsole 是**「附加式面板」**——问题域不同，不追求做发行版。但以下 **§1~§12 的 12 条优化点**不涉及造 OS，可直接吸收。

---

## §1 可吸收优化建议（按 P0~P3 分级）

> 每条含：**现状 / 竞品做法 / 建议实施路径 / 关联文件与行号 / 待确认（勾选）**

---

### P0：国产化硬兼容（3 条，必须优先确认）

#### P0-1　CPU 厂商细分（海光 Hygon / 飞腾 Phytium / 鲲鹏 Kunpeng / 兆芯 Zhaoxin）

- **QVMConsole 现状**：[handler/version.go:112](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/handler/version.go#L112) 仅探测 `avx2/fma`；install.sh `ensure_kvm_runtime` 不区分 CPU 厂商、不处理 kvm_amd 参数。
- **竞品做法（SMTXOS ks_automatic.cfg:461-483）**：
  - 对 `HygonGenuine`（海光）+ `AuthenticAMD`：追加 `intel_idle.max_cstate=0 processor.max_cstate=1 intel_pstate=disable transparent_hugepage=never slab_nomerge tsx=on no5lvl megaraid_sas.scmd_timeout=20 nvme_core.multipath=0`
  - 对 SMTXELF 产品：Intel CPU 也关 cstate/pstate
  - VMware 虚拟化 VM：减半参数
- **建议实施路径**：
  1. `server/service/arch/` 新增 `domestic_cpu.go`（与 `ArchProfile` 同注册模式），暴露 `DetectCPUVendor()` 返回 `Intel | AMD | Hygon | Phytium | Zhaoxin | Kunpeng | Unknown`
  2. install.sh `precheck_domestic` 调用检测 → 写 `.env DOMESTIC_CPU_VENDOR=xxx`；对海光 7000/5000 提示 `kvm_amd.npt=0`（第一世代海光需关嵌套页表）；对飞腾/鲲鹏 ARM64 检查 KVM 模块加载顺序（kvm→kvm_arm→hyp/vhe）
  3. `compat-manifest.json` 的 `system_requirements` 扩展 `cpu_vendor.whitelist`（已实施，manifest 键），component_health 检测到未支持厂商报 warning
  4. 文档 §5.11.2 组件阈值表增加一行 `cpu_vendor`
- **关联文件**：[build.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh) COMPONENT_REQ_*；[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) precheck_domestic + ensure_kvm_runtime；[server/handler/version.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/handler/version.go) /system-info；[service/diagnostics/component_health.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/diagnostics/component_health.go)

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

#### P0-2　firewalld 最低版本从 0.8.0 下调到 0.4.0，并对 0.4.x~0.8.x 显式降级

- **QVMConsole 现状**：[build.sh:74](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh#L74) `COMPONENT_REQ_FIREWALLD="0.8.0|0.9.0"`，min=0.8；CentOS 7.4 基线（SMTXOS 基）firewalld=**0.4.4.4**，`check_component_versions` 会把这些发行版误报 **critical**。
- **竞品做法**：SMTXOS ks_automatic.cfg:20 `firewall --disabled`（默认关），HCI 基也是老系统。实际面板即使 firewalld 0.4.x 也只需要 rule 增删——policy（0.9+）是优化功能。
- **建议实施路径**：
  1. `COMPONENT_REQ_FIREWALLD` → `"0.4.0|0.9.0"`
  2. `service/firewall/backend_firewalld.go` `Enable()` 增加 `<0.6` 版本分支：直接 `return &FirewallError{Code: FirewalldOldVersion, Message: "firewalld 版本过低，面板不启用宿主机防火墙统一管理", Hint: "请升级 firewalld ≥ 0.6 或使用发行版 iptables-service"}`
  3. component_health 的 firewalld 状态 warning 文案区分：`< 0.6 = 不完整支持（仅 warning）` vs `0.6~0.9 = 缺 policy 能力（warning）` vs `≥ 0.9 = healthy`
  4. 文档 §5.11.2 表格 firewalld 行更新 min=0.4.0
- **关联文件**：[build.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh) L74；[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) versions.conf 解析；[service/firewall/backend_firewalld.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/firewall/backend_firewalld.go) Enable()；[service/firewall/advice.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/firewall/advice.go) FirewalldOld 判定

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

#### P0-3　glibc 2.17 真兼容实测（CI + 冒烟扩展）

- **QVMConsole 现状**：[build.sh:48](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh#L48) `amd64 compat 默认上限 2.2.5`，Zig `x86_64-linux-gnu.2.2.5` 目标；但「理论兼容 ≠ 实测通过」——某些 CGO 符号（pthread_cond_clockwait 等 glibc 2.30+）Zig 处理不一致，在 glibc 2.17（CentOS 7）上可能 `Symbol not found` 崩溃。
- **竞品做法**：SMTXOS 基 glibc=2.17；所有 RPM 都按 el7 基线重新编译，无"从高 glibc 降档"风险。
- **建议实施路径**：
  1. `.github/workflows/build.yml` 新增 job `verify-centos7-glibc217`：`docker run centos:7` → 挂载 build 输出 compat 二进制 → 执行 `kvm-console --version` + `kvm-console --db-probe`（打开一次 SQLite + 空连 libvirt），确保 CGO 符号可解析
  2. install.sh `select_binary_smoke_test()`（L2127）除 `--version` 外新增 `--smoke-selfcheck` 子命令（后端实现一个简单的 self-check：db.AutoMigrate 空结构体 + libvirt Connect 超时 2s）
  3. 若 CI 不挂，正式在文档 §4.3 声明"兼容 glibc ≥ 2.17"；否则定位符号，手动在 CGO LDFLAGS 里 -Wl,--wrap 解决
- **关联文件**：[build.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh) verify_compat_glibc；[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) select_binary_smoke_test；[server/main.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/main.go) 新增 --smoke-selfcheck；.github/workflows/build.yml

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

### P1：安装与升级可靠性（4 条）

#### P1-4　安装阶段持久化文件 + 权限加固（.env 600 / 敏感目录 700）

- **QVMConsole 现状**：[install.sh:2587](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L2587) `run_install_or_update` 靠 STEP_NUM/exit code 传递状态，无持久化 stage 文件；`.env`、`FIREWALL_DIR /etc/kvm-console/firewall`、`VPC_CONFIG_DIR /etc/kvm-console/vpc` 创建后未显式 chmod。
- **竞品做法（HCI vmp.pkg 头部 strings 可见）**：`$update_dir/ing`（进行中标记）、`$outcfg`（输出配置不删，供下次取）、`$die_file`（失败写错误信息）、`$cleanpath`（是否清理）、`diewithclean` 按 is_success 决定是否清 tmpdir。
- **建议实施路径**：
  1. 新增 `${INSTALL_DIR}/.install_state/` 目录：写 `stage=<STEP_NUM>`、`last_error=`、`degraded_notes=`、`binary_tier=`、`component_summary=`、`release_sha256=`
  2. install.sh 新增 `--resume` 参数：读 stage 文件从失败步骤继续
  3. `write_env` 末尾 `chmod 600 "$ENV_FILE"`；`ensure_directories` 对 `/etc/kvm-console/**/*.json / rules / policies` `chmod 600`，对 `/etc/kvm-console`、`/etc/kvm-portforward`、`/etc/libvirt/vm-access` 等目录 `chmod 700`
  4. `extract_tarball` 增加 sha256sum 校验（发行包内放 SHA256SUMS；没有则 CI 构建注入）
- **关联文件**：[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) run_install_or_update / write_env / ensure_directories / extract_tarball

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

#### P1-5　数据库 schema 版本化迁移（弃用 AutoMigrate 无版本化）

- **QVMConsole 现状**：`server/main.go` 启动时 `db.AutoMigrate(model...)`——无版本号、无向下迁移、跨版本 schema 改名/删列会丢数据或静默失败。
- **竞品做法**：SMTXOS `elfvirt` RPM 在 `%post` 里按版本号跑 SQL 迁移脚本；hotfix-package 内含增量 SQL。
- **建议实施路径**：
  1. 发行包 `migrations/` 目录：`0000_init.go`（当前模型全量）、`0001_xxx.go`（未来增量，Up/Down 成对）
  2. 引入 `github.com/go-gormigrate/gormigrate/v2`（或自写简单 migrate 表），main.go 启动先迁移再 AutoMigrate
  3. install.sh `install_files` 失败且 db 已迁移时不回滚数据库（可在 install_state.db_migrated 标记），下次 update 按版本继续
- **关联文件**：server/main.go；`server/migrations/` 新建目录；[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) install_files

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

#### P1-6　发行版文件保留上一份可回滚（INSTALL_DIR.bak-YYYYMMDD-HHMMSS）

- **QVMConsole 现状**：update 模式下 install_files 直接 `mv -f` 覆盖 INSTALL_DIR，无回滚点。
- **竞品做法**：SMTXOS hotfix-package RPM `%pretrans` 自动做 `cp -a /usr/share/smartx /var/lib/smartx/backup-$(date)`；失败时 `%postun` 恢复。
- **建议实施路径**：
  1. install.sh `install_files` 前：`cp -a "$INSTALL_DIR" "$INSTALL_DIR.bak-$(date +%Y%m%d-%H%M%S)"`，保留最近 3 份，超过自动删除最早
  2. 新增 `qvmc-manage.sh rollback <bak-dir>` 一键回滚：停服务 → mv bak → 回滚 db（schema 版本号 ≥ 当前时提示"可能需手动下迁"）→ 起服务
  3. install.sh `repair_config` 扩展 `--rollback` 子模式
- **关联文件**：[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) install_files；qvmc-manage.sh（若有）

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

#### P1-7　安装包完整性校验 + 可选签名

- **QVMConsole 现状**：install.sh `get_release` 直接 `wget` URL，不校验哈希；本地发行包也不校验。
- **竞品做法**：HCI vmp.pkg 自解压脚本带 `--password` 解密；SMTXOS ISO 带 `isomd5sum` + RPM GPG 签名（RPM-GPG-KEY-CentOS-7 两份）。
- **建议实施路径**：
  1. build.sh 在 release 目录内生成 `SHA256SUMS`；同步生成 `SHA256SUMS.minisig`（minisign 签名，公私钥文档化）
  2. install.sh `extract_tarball` 前：`sha256sum -c SHA256SUMS 2>&1 | grep FAILED` → 发现失败则 error 退出；签名文件存在时校验 minisign（缺失不阻断，仅 warn，CI=0 交互确认、CI=1 阻断）
- **关联文件**：[build.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh) 末尾；[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) extract_tarball

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

### P2：观测性与运维（3 条）

#### P2-8　bash 审计日志（PROMPT_COMMAND）+ kdump 建议提示

- **QVMConsole 现状**：install.sh 未做任何 shell 审计配置；系统 kdump 配置不检测。
- **竞品做法（SMTXOS ks.cfg:923-926）**：
  - `PROMPT_COMMAND` 记录每条命令时间+whoami+who -u+RC 到 `/var/log/bash.log`，`chmod a+w` + `chattr +a`（仅追加，防删）
  - kdump：裸金属 crashkernel=2048M,high；虚拟化 512M；grub.cfg 强制生效
- **建议实施路径**：
  1. install.sh 新增辅助步骤 `setup_bash_audit`（失败 warn）：对 `/root/.bashrc` + `/etc/skel/.bashrc` + admin 用户追加 PROMPT_COMMAND；chattr +a 失败则仅 chmod 622
  2. install.sh `precheck_domestic` 检测 `systemd-detect-virt == none` 且 `crashkernel` 未配置 → print_install_report warn"建议启用 kdump 2G crashkernel"
  3. Diagnostics ZIP 导出新增类别 `shell-history`：收集 `~/.bash_history` + `/var/log/bash.log`（存在时）
- **关联文件**：[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) run_install_or_update 辅助列表；[service/diagnostics/collector.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/diagnostics/collector.go)

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

#### P2-9　VM 看门狗 + 大页配置建议

- **QVMConsole 现状**：依赖 libvirt `on_crash restart`；对大页（HugePage）无任何提示。
- **竞品做法**：SMTXOS 独立 RPM：`elf-vm-watchdog-1.1.0-rc6`（VM 守护）、`hugepage-manager-1.0.0-rc9`（动态大页）、`hpctl-0.2.0-rc15`（硬件功耗？）
- **建议实施路径**：
  1. `server/service/` 新增 `vmwatchdog/` 子包：开 goroutine，周期 30s 通过 `guest-agent ping` 检测所有 running VM；连续 3 次无响应 → `virsh reset` + 审计事件入库；systemd 不单独起服务，作为面板的后台 goroutine（taskqueue 机制可复用）
  2. Diagnostics / system-info 增加 `hugepages` 字段：若 `vm.total_memory_allocated >= 128GB && /proc/meminfo HugePages_Total == 0`，报 warning"建议开启大页提升性能"，给出 sysctl + 启动参数示例
- **关联文件**：server/service/vmwatchdog/（新建）；[service/diagnostics/component_health.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/diagnostics/component_health.go) 追加 hugepages 条目

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

#### P2-10　周期健康探针 + Dashboard 健康度灯

- **QVMConsole 现状**：诊断导出是"手动触发"；无后台健康探针。
- **竞品做法**：SMTXOS `consul-server` 做全集群健康检查 + 服务注册（单机版也跑）；`prometheus 2.37` 采集指标。
- **建议实施路径**（单机轻量版，不引入 consul）：
  1. `service/diagnostics/` 新增 `periodic_probe.go`：`StartPeriodicProbe(interval = env.HEALTH_PROBE_INTERVAL, 默认 1min)`，探针项目：systemd status of {kvm-console, libvirtd, ovs-vswitchd, ovsdb-server, dnsmasq, firewalld/ufw} + disk free of {/, /opt, /var, STORAGE_MOUNT}
  2. 结果写 `/opt/kvm-console/.health/latest.json`，同时在内存缓存；前端 Dashboard 首页顶部新增「系统健康度」灯：healthy(绿)/degraded(黄)/critical(红)，点击跳 Diagnostics
  3. health 灯规则：面板 alive 且关键服务（libvirtd/ovs）全 alive = green；关键服务任一 dead 但面板 alive = yellow；面板 dead（systemd Restart=always 兜底）由前端轮询超时报红
- **关联文件**：[service/diagnostics/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/diagnostics) 新建 periodic_probe.go；前端 Dashboard 页

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

### P3：文档与生态（2 条）

#### P3-11　发行版支持等级 S/A/B/C + 认证硬件清单

- **QVMConsole 现状**：[build.sh:214-222](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh#L214) `os_compat` 仅 7 行 + recommend_tier，未分支持等级；无硬件认证矩阵。
- **竞品做法**：SMTXOS/HCI 官网公开发布"硬件兼容性列表 HCL"，分认证通过 / 自测通过 / 理论兼容三档。
- **建议实施路径**：
  1. `compat-manifest.json` 的 `os_compat` 每一项扩展字段：`support_level ∈ {S, A, B, C}`（S=官方全量回归、A=核心功能回归、B=社区自测、C=理论兼容）和 `certified_hardware: string[]`
  2. install.sh `precheck_domestic` 检测到 support_level=C → warn"本发行版为理论兼容，生产请升级到认证基线"
  3. 文档 §5.11.2 表末尾加两列 support_level / certified_hardware（示例：Dell PowerEdge、H3C UniServer、华为 TaiShan、飞腾腾云 S2500、海光 7000/5000）
- **关联文件**：[build.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh) write_compat_manifest；[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) precheck_domestic；[design-domestic-component-compat.md](file:///Volumes/cs/QVMConsole/jeoQVMConsole/docs/design-domestic-component-compat.md)

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

#### P3-12　国内镜像源自测 + offline 安装模式

- **QVMConsole 现状**：`check_and_install_deps` 直接 `apt/dnf` 命中系统默认源；专网环境常失败无回退。
- **竞品做法**：HCI/SMTXOS 把所有依赖 RPM 打包进 ISO，纯离线；网络版安装器也有镜像测速。
- **建议实施路径**：
  1. install.sh `precheck_domestic` 新增 `test_mirror_speed`：
     - 对 apt：在 `/etc/apt/sources.list` 追加清华/阿里/163 临时注释源；每个 `apt-get update` 跑一遍计时（或 curl 100KB 包），取最快
     - 对 dnf：`/etc/yum.repos.d` 同样做法
  2. 结果写入 `.env DEPS_MIRROR=tsinghua|aliyun|163|system|offline`
  3. `check_and_install_deps` 支持 `DEPS_MIRROR=offline`：跳过 apt/dnf install，仅 `command -v xxx` 扫缺包并汇总清单，print_install_report 提示"缺 X/Y/Z，需从内网源手动安装"
- **关联文件**：[install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) precheck_domestic / check_and_install_deps

☐ 开发确认：本条采纳（　）；部分采纳，仅做（　）；不采纳，原因（　）。

---

## §2 对 design-domestic-component-compat.md 的结构性补正

| 章节 | 文档当前写法 | 建议补正后写法 | 对应优化项 |
|---|---|---|---|
| **§4.3 构建档位** | compat / native / compat-2.28 三档 + zig 目标 | 三档基础上**新增「centos7-glibc217 真兼容 CI 验证」**要求：compat 档必须通过 docker centos:7 的 `--smoke-selfcheck`；不通过 → 构建失败 | P0-3 |
| **§5.1 防火墙后端抽象** | firewalld < 0.9 报 FirewalldOld（仅 policy 不可用） | 细分为**三档**：< 0.4 → 不在阈值内；0.4.x~0.5.x → Enable 直接报 FirewalldOldVersion（不启用面板管理，warning）；0.6.x~0.8.x → 缺 policy 能力（warning）；≥ 0.9 → healthy。build.sh min 下调为 0.4.0 | P0-2 |
| **§5.8 install.sh 阶段清单** | precheck_domestic 描述笼统；无 权限/stage 文件/镜像源/CPU/kdump | 在「10. 安装前预检 precheck_domestic」下分 4 子项：① CPU 厂商探测与写 env（P0-1）② 存储/分区布局检测（P1-5 背景）③ kdump crashkernel 建议（P2-8）④ 国内镜像源测速（P3-12）。「15. 配置写入 write_env」新增 chmod 600/700（P1-4） | P0-1 P1-4 P2-8 P3-12 |
| **§5.11.2 组件阈值表** | firewalld min=0.8.0；无 cpu_vendor 行 | ① firewalld min=0.4.0（P0-2）② 新增行 `cpu_vendor`（阈值 Hygon/Phytium/Kunpeng/Zhaoxin/Intel/AMD，warning 级未在白名单）（P0-1）③ 新增两列 `support_level` / `certified_hardware`（P3-11） | P0-1 P0-2 P3-11 |
| **§8 回退与降级策略** | manifest 缺失 / 版本解析失败降级 | 新增：**① 升级失败回滚**：INSTALL_DIR.bak-N 保留（P1-6）；② **DB schema 版本化迁移**：gormigrate + migrations/ 目录（P1-5）；③ **安装包完整性校验**：SHA256/minisign（P1-7）；④ **安装 stage 持久化文件**：.install_state/  + --resume（P1-4） | P1-4 P1-5 P1-6 P1-7 |
| **§13.10 待实施表（补充 M8 系列里程碑）** | （原表只有 M7.x） | 新增 12 行 M8.0~M8.11，每条对应 §1 的 P0~P3；同时 M7.1 check_component_versions、M7.2 component_health 从待实施→已实施（之前评估结论） | 全部 |

---

## §3 实施优先级与 ROI（9 条，不含 P3）

| 优先级 | 编号 | 优化项 | 预估工时 | 预期收益（国产场景） |
|---|---|---|---|---|
| **P0** | P0-2 | firewalld_min 0.8→0.4 + 0.4.x Enable 显式报错 | 0.5d | 消除 CentOS 7 基线 critical 误报，老系统可直接安装 |
| **P0** | P0-1 | CPU 厂商分支（海光/飞腾/鲲鹏/兆芯）+ .env 持久化 | 2d | 国产服务器部署前摄性兼容，现场坑位减少 50%+ |
| **P0** | P0-3 | glibc 2.17 CI 验证 + 冒烟 selfcheck 扩展 | 1d | 麒麟 V10/openEuler 20.03/CentOS 7 上部署零"Symbol not found" |
| **P1** | P1-4 | 安装阶段持久化 + .env 600 + 目录 700 | 1d | 安全性（敏感配置不外泄）+ 可恢复性（失败断点续装） |
| **P1** | P1-5 | schema migrations 版本化目录 | 1.5d | 跨版本 DB 升级零事故，杜绝 AutoMigrate 丢字段 |
| **P2** | P2-8 | bash 审计 + kdump 提示 | 0.5d | 等保 2.0 中「操作审计」合规项满足，客户验收通过 |
| **P2** | P2-10 | 周期健康探针 + Dashboard 灯 | 1d | 运维发现故障前移（不再等用户报 VM 失联） |
| **P3** | P3-11 | 支持等级 S/A/B/C + 认证硬件 | 0.5d | 售前/交付沟通成本降 30%，客户对支持范围清晰 |
| **P3** | P3-12 | 国内镜像源测速 + offline | 1d | 专网/内网环境安装成功率提升 80% |
| | | **合计** | **约 9 人日** | |

> 建议按 P0-2 → P0-1 → P1-4 → P1-5 顺序开工，前 4 条（共 5 人日）即可把国产服务器上最常见的坑全部堵住。

---

## §4 开发逐项确认总表（请打勾 + 备注）

| 编号 | 标题 | ◻ 采纳 / ◻ 部分采纳 / ◻ 不采纳 | 备注 / 实施人 / 预计完成日 |
|---|---|---|---|
| P0-1 | CPU 厂商细分（海光/飞腾/鲲鹏/兆芯） | | |
| P0-2 | firewalld min 0.8→0.4 + 0.4.x 降级 | | |
| P0-3 | glibc 2.17 CI + smoke selfcheck | | |
| P1-4 | stage 持久化 + 权限 chmod 600/700 | | |
| P1-5 | DB schema 版本化 migrations | | |
| P1-6 | INSTALL_DIR.bak-N 回滚保留 | | |
| P1-7 | SHA256 + minisign 安装包校验 | | |
| P2-8 | bash 审计 + kdump 提示 | | |
| P2-9 | VM 看门狗 + 大页提示 | | |
| P2-10 | 周期健康探针 + Dashboard 灯 | | |
| P3-11 | support_level S/A/B/C + 硬件认证矩阵 | | |
| P3-12 | 国内镜像源自测 + offline | | |
| §2 补正 | design-domestic-component-compat.md 结构性补正（6 处） | | |
