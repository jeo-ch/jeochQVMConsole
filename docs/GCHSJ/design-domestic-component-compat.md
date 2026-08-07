# 国产系统组件版本改善设计文档 v0.18

> 目标：解决 QVMConsole 在国产系统（麒麟 Kylin V10 / openEuler / UOS 等）上因 GLIBC 与防火墙组件（ufw）版本差异导致的性能折损与功能阻断，并沉淀一套可复用、可交互、与现有代码风格一致的国产化适配机制。
> 状态：设计已评审（v0.6 综合评估 + v0.7 实施评审补充 + v0.8 安装包最佳实践综合 + v0.9 项目架构总览附录 + v0.9.1 §13 附录补强 + v0.9.2 阅读导航补强 + v0.9.3 组件升级提示与接口收敛 + v0.9.4 组件版本检测闭环 + v0.9.5 §13 架构附录同步 + v0.9.6 §13 深度补强 + v0.9.7 事实核对修正 + v0.9.8 实施后文档同步 + v0.9.9 评审修复同步 + v0.9.10 产品评审批量修复 + v0.9.11 审计修复同步 + v0.10 竞品差异吸收 + v0.10.1 代码对照核实 + v0.11 M8 首批实施 + v0.12 M8 全量实施 + origin/main 分叉合并 + v0.12.1 综合校正 + v0.12.2 openEuler 实测修复 + v0.12.3 架构归一化下沉 + v0.12.4 发行版归一化+apt锁防护+源备份回滚 + v0.13 安装期健壮性（set -e 静默退出脚枪）+安装性能（慢源前置/交互倒计时/计时汇总）+ v0.14 当前仓库实施状态校正 + v0.15 前端整体迁入 + v0.16 CI 缺口补齐 + v0.17 安装语言环境检查增强 + v0.18 官方仓库核对比对 + 测试机孤儿清理），5 项待确认问题已决策（§11），运维/后端补充项 47 项已固化（§12，其中 #A 为优先实施项 M0.5，#T 为 v0.9.4 新增 M7 闭环，#U-#AA 为 v0.9.9 评审修复新增，#AB-#AM 为 v0.9.10 产品评审批量修复新增，#AN-#AP 为 v0.9.11 审计修复新增，#AQ-#AT 为 v0.13 安装期健壮性/性能修复新增）。**实施进度：M0-M7 已全部落地（✅）；M8 竞品差异吸收（P0-P3 共 12 条，§14）后端已全部落地（✅，v0.14 起以当前仓库 `jeochQVMConsole` 为准——M8.1/M8.5/M8.9/M8.10/M8.11 后端此前仅存在于旧仓库 `jeoQVMConsole`，v0.14 已整体迁移并接线验证）；M8 相关前端展示已全部迁入当前仓库 `web/`（✅，v0.15：health.ts / HealthLight / watchdog.ts / vm-watchdog 页 / os-support 卡 / 看门狗配置分区，见 v0.15 修订要点）；CI 侧缺口自 v0.16 补齐（✅：`verify-centos7-glibc217` / `verify-arm64-glibc-low` / 产物 .sha256/.minisig/minisign.pub 上传 / release minisign 签名步骤 / amd64 glibc 2.35 固定档，见 v0.16 修订要点）；评审修复（C1/H1-H3/M1/M3/M4/M5/L2/M2）、产品评审批量修复与审计修复（#AN-#AP）已落地，详见 §7 实施进度汇总与 §13.10**。
>
> **v0.12.1 修订要点（综合各文档与代码现状校正，无功能新增）**：
> 1. **§14.3/§14.5#6 分叉状态更新**：`origin/main` 功能提交已通过 `d534974` 并入本地（分叉消除，当前 origin/main=`a2e9473`，本地领先 origin 14 提交）；`git merge-base HEAD upstream/main == 51330e5` 即 HEAD 已包含 upstream/main 全部提交。§14.5 候选⑥标注已完成，仅需按 merge-from-upstream.md 定期拉取。
> 2. **表数口径统一**：AutoMigrate 新增 `VMWatchdogEvent`（M8.9）后共 **29 张表**（§13.2 架构图 / §13.3.1 step 4 / §13.3.5 / §13.5.3 启动流程图 / §13.7.5），原「28 张表」表述全部同步。
> 3. **handler 文件数更新**：新增 `health_probe.go`（M8.10）后 `server/handler/` 为 **45 个 .go 文件**（43 处理器 + types.go/helpers.go 支撑），原「44 个处理器文件」同步。
> 4. **§13.3.1 后台调度器 7→10**：新增 `StartFirewallDriftMonitor` / `StartVMWatchdog`（M8.9）/ `StartHealthProbe`（M8.10）+ 异步 `GetComponentHealth()` 预热。
> 5. **§13.7.2 同步**：CI job 表补 `verify-centos7-glibc217`（M8.3）与 `release`；`build`（amd64）runner 修正为 `ubuntu-22.04`（非 ubuntu-latest）；辅助配置 6→7（新增 `setup_bash_audit`，M8.8），共 25 次函数调用；产物表补 `SHA256SUMS`/`*.tar.gz.sha256`（M8.7）与 `versions.conf`（含 `SUPPORT_LEVEL_<os>`，M8.11）。
> 6. **§13.7.5 测试环境路径更新**：`/opt/project/new-web` → `/opt/project/QVMConsole`（与 AGENTS.md、qvmc-manage.sh、固件路径一致）。
> 7. **§5.11.2 组件清单口径统一**：表格为 19 行（18 项系统级组件 + cpu_vendor 厂商检测项），分类为「核心必装/磁盘镜像初始化/诊断扩展」三类（原文漏列 cpu_vendor 分类、类别写「网络」与实际表头不符）。
> 8. **§13.3.3/§13.4.2/§13.3.2/§13.5 架构附录补新条目**：service 子包矩阵补 `vmwatchdog` 子包 + diagnostics 周期健康探针行；前端路由表补 `/vm-watchdog`（看门狗事件页）；路由分组表补公开 `/system/health/latest`、系统设置 `os-support`、`/vm-watchdog/events` 组；关键接口表补 `vmwatchdog.StartWatchdog`/`VMWatchdogEvent`/`StartHealthProbe`/`migrations`；前端组件表补 `HealthLight`/`getHealthProbeLatest`/`watchdog.ts`/`VmWatchdogPage`。
> 9. **qvmc-manage.sh 菜单补第 6 项**：回滚上一版本发行版（M8.6）。
> 10. **§7 下一步更新**：§14.5 候选①③⑤已实施，剩余可选为 ④minisign（无密钥基建）与定期同步上游。
> 11. **未完成/优化清单登记（§14.5 末尾）**：唯一实质性未落地 = **minisign 离线签名（候选④，登记实施中）**；麒麟桌面版 V10 明确排除、以后不再提示；优化项登记 = CI arm64 glibc 验证（已实现 `verify-arm64-glibc-low`，见修订要点 13）、`release` job runner 说明（保持现状）、minisign 公钥发布机制（随候选④）。
> 12. **minisign 实施（§14.5 候选④）**：build.sh 签名步骤（`MINISIGN_KEY` 环境变量/`--minisign-key` 参数，缺失降级跳过）+ install.sh 安装期 `minisign -V -m` 验证（公钥优先级：内嵌 `MINISIGN_PUBLIC_KEY` > INSTALL_DIR > 包内/同目录，无命令/签名/公钥降级 SHA256）+ `docs/minisign.pub` 公钥占位入库 + `docs/minisign-publishing.md` 签发流程 + `docs/dependencies.md` 依赖登记。密钥由发行方生成后填入。
> 13. **麒麟桌面版排除（v0.12.1 最终口径）**：桌面版 V10（Debian 11 系/apt+ufw）走既有路径，不纳入国产化适配验收；`install.sh:280` 的 apt 回退保留（顺带兼容），后续文档/评审不再就桌面版提示或扩展。
> 14. **CI 落实（v0.12.1 优化项）**：新增 `verify-arm64-glibc-low` job（`ubuntu-24.04-arm` + `arm64v8/ubuntu:20.04` 容器，arm64 compat 档低 glibc 2.31 冒烟——`centos:7` 无 arm64 变体，无法严格测 2.17）；`release` job 新增可选 minisign 签名步骤（`secrets.MINISIGN_KEY` base64 私钥，未配置则跳过仅 SHA256）+ 必做 SHA256 校验生成 + Release 上传 `.tar.gz`/`.sha256`/`.minisig`。
> 15. **§14.5 登记表状态更新**：CI arm64 glibc 验证「待启用」→「已实现（v0.12.1）」，见 §13.7.2/修订要点 14。
> 16. **minisign 协同修正（v0.12.1）**：真实密钥生成（公钥入库 `docs/minisign.pub` + install.sh 内嵌 `MINISIGN_PUBLIC_KEY`，三处一致）；验签命令 `-Vm`→`-V -m`（实测 `-Vm` 解析异常致验签失败）；**公钥分发链路补全**——build.sh 打包**前**拷公钥（进包 + `release/` 同目录，新增 `--minisign-pub-file` 参数），install.sh verify 明确「包内公钥因解压前验证不可用、仅用同目录/内嵌/INSTALL_DIR」；CI build artifact、Release、OSS 上传均补齐 `.sha256`/`.minisig`/`minisign.pub`。端到端实测：签名→解压前用同目录公钥验签通过、被篡改包 exit 1。
> 17. **verify 误删公钥 bug 修复 + 文档行号校正（v0.12.1）**：`verify_minisign_signature()` 原用单变量 `tmp_pub` 兼作「内嵌公钥临时文件」与「同目录公钥路径」，返回时 `trap rm -f` 会误删发行包同目录真实 `minisign.pub` 用户文件；拆分为 `tmp_key`（临时文件，返回清理）+ `found_key`（用户文件，不删除），实测同目录公钥验证后文件仍存活。同步校正 §14.2 实施表与架构描述中过时的 install.sh 行号引用（select_binary_smoke_test 2209→2284、write_env .env chmod 1229→1285、install_files 2294→2493、extract_tarball 2347→2355、verify 公钥 2303→2306、precheck_domestic 2605→2814、run_install_or_update 3012→3080、main 1934→3143）。
> 18. **安装路径调整（v0.12.2）**：`INSTALL_DIR` `/opt/kvm-console` → `/opt/QVMConsole`。同步更新 install.sh（INSTALL_DIR/ENV_FILE/STATE_DIR）、qvmc-manage.sh（detect_project_dir 候选加 `/opt/QVMConsole`、INSTALL_DIR 默认、db_path 候选）、server 硬编码默认（periodic_probe defaultHealthDir、config.go 注释、zram.go 回退路径、arch/aarch64.go 固件路径）。`/opt/kvm-console` 保留为兼容迁移识别候选，旧安装仍可识别。
> 19. **B7 系列协同优化（v0.12.2）**：**B5** `runSmokeSelfcheck` 修正假阴性——dial 失败分支改 `libvirt.New(nil)` 保留实例化（先前 `return nil` 使 go-libvirt 符号面未纳入 smoke 真实运行面）；**A1** `build.sh` 提取 `set_build_variant_flags()` 统一 BUILD_COMPAT/BUILD_NATIVE 判定（消除两处漂移）；**A2** 兼容档构建改独立 GOCACHE（弃 `go clean -cache` 全清，native 档复用缓存提速）；**A3** 捆绑 RPM URL 变量化（`BUNDLE_EPEL_BASE_*`/`BUNDLE_ALMA_APPSTREAM_*`）；**B3** `InitLibvirtRPC` 失败不再 `log.Fatal` 阻止启动，改 `StartLibvirtReconnectLoop` 后台 15s 重连（期间相关操作走原 libvirt 或 virsh 路径，恢复后自动恢复 RPC）；**C1** 诊断接口新增当前发行版高亮（`DetectCurrentOsSupport` 按 /etc/os-release ID/ID_LIKE/PRETTY_NAME 匹配 manifest os_compat；handler 返回 `current_os`/`os_release`；前端「发行版支持等级」卡片顶部绿色 Tag 标注当前系统与等级）；**C2** install.sh 修复菜单新增第 4 项「回滚到历史发行版」（`rollback_release` 对齐 qvmc-manage.sh 功能 6）。
>
> **v0.12.2 修订要点（基于 openEuler 24.03 实测安装日志，B7 修复）**：
> 1. **edk2-ovmf 误报"缺失"修复（openEuler 固件路径）**：openEuler/RHEL9 的 edk2-ovmf 固件位于 `/usr/share/edk2/ovmf/`（非 `/usr/share/OVMF/`，Debian 才是后者）。此前 install.sh STEP 12、后端 `component_health.go`、`arch/x86_64.go`、`vm_xml/boot_type.go`、clone/vmimport 的 NVRAM 硬编码全部只查 `/usr/share/OVMF/`，导致 openEuler 已装 edk2-ovmf 却误报"缺失"，且 UEFI 引导 VM 创建选不到固件。现全部路径探测/NVRAM 模板统一纳入 `/usr/share/edk2/ovmf/OVMF_CODE*.fd`、`OVMF_VARS*.fd` 候选；clone `xml.go`/`windows_init.go`/`vmimport/windows_guest.go` 的硬编码 `OVMF_VARS_4M.ms.fd` 改为 `vm_xml.ResolveOVMFVarsTemplatePath(true)`。
> 2. **捆绑 RPM 提取物可运行性校验（B2-1）**：`extract_rpm_cmd` 原仅按 `/usr/local/bin/<cmd>` 文件存在判定成功，导致 libguestfs-tools-c 提取的二进制缺动态库（libguestfs.so.0 等）无法运行时仍报"提取完成"，但 STEP 12 的 `--version` 探测失败→误报"未安装"。现提取后 `timeout 5s "$cmd" --version` 实测运行面，失败则移除该二进制并 warn（交 dnf provides 回退），避免"已提取却未安装"矛盾。
> 3. **测试机环境**：openEuler 24.03 (LTS-SP4) dnf 系，后端 firewalld / glibc 2.38，编译兼容档 GLIBC 2.2.5，面板服务启动正常。growpart/cloud-utils-growpart 在 openEuler 源不可用属环境差异（已有 dnf provides 回退）。
> 4. **openEuler libguestfs 系列误报"未安装"修复（B7-2）**：捆绑 RPM（CentOS 系）提取到 `/usr/local/bin` 的 virt-customize/guestfish 依赖 `libpcre.so.1`/`libconfig.so.9`，在 openEuler（pcre2/libconfig.so.11）无法运行，且 `/usr/local/bin` 在 PATH 优先于原生 `/usr/bin` 版本 → 检测读到坏副本误报。新增 `has_runnable_cmd()`（检查 `/usr/bin` 等系统目录真实可运行、容忍无 `--version` 的工具）+ 提取前无条件清理 `/usr/local/bin` 坏副本 + growpart 版本探测回退 `rpm -qf`（growpart 无 `--version`）。
> 5. **单文件自解压安装器（v0.12.2）**：build.sh 在签名后生成 `kvm-console-linux-<arch>.run`，将 tar.gz + `.minisig` + `.sha256` 全量内嵌（payload 单行 base64，`;` 分隔）；引导头自校验 SHA256（失败 exit 1）→ 解压 → `exec bash install.sh "$@"`（`set --` 前备份 `CALL_ARGS` 避免污染原参）。用户只需下载一个 `.run` 文件，杜绝漏下载旁文件。同时生成 `.run.sha256` 供下载后核验。
>
> **v0.12.3 修订要点（架构归一化工具下沉收敛，无协议/行为变更）**：
> 1. **新增 `server/service/arch/normalize.go` 归一化 API**：`NormalizeArch()` 将 `uname -m`、`runtime.GOARCH`、XML `<type arch>`、前端入参的架构别名统一归一到模块规范常量——`x86_64`（`amd64`/`x64`/`i386`/`i486`/`i586`/`i686`/`x86`）、`aarch64`（`arm64`/`armv8`/`armv8-a`）、`riscv64`（`riscv`）；配套语义化谓词 `IsX86Arch()`/`IsAarch64Arch()`/`IsRiscv64Arch()`。这是 §4.1 系统能力探测层的统一入口，一处定义、多处复用，消除跨文件的裸字符串架构比较漂移。
> 2. **收敛散落的裸架构字符串比较到模块 API**：`vm_xml/{pae,apic,boot_type,display,kvm_features}.go` 的 x86/ARM/RISC-V 判断、`diagnostics/component_health.go` 的 `arm64` 分支、`appliance/parse.go` 的 `normalizeArchitecture`、`host/igpu_passthrough.go` 的 `iommuParamHint`、`handler/version.go` 的 `resolveQEMUCmd` 全部改用 `arch.*` 谓词（保留各处原有空字符串降级语义，如 PAE/APIC 空架构默认视作 x86）。`appliance/parse.go` 保留宽松子串兜底以兼容 OVF 非规范描述串。
> 3. **非此轮范围**：`igpu_passthrough.go` 的 `uname -m` 用于展示原始内核架构字符串（非判断）不收敛；`domestic_cpu.go` 仅 gofmt 对齐、逻辑不变。所有改动经 `go build`/`go vet`/`gofmt`/前端 `oxlint` 验证通过。
>
> **v0.12.4 修订要点（发行版归一化 + apt 锁防护 + 系统源备份回滚，无协议/行为变更）**：
> 1. **新增 `server/service/arch/os.go` 发行版归一化模块**：`ReadOSRelease()` 统一读取并解析 `/etc/os-release`（ID/ID_LIKE/PRETTY_NAME/VERSION_ID），返回结构化 `OSRelease`；`DistroFamilyOf()` 按 ID 精确归组 → ID_LIKE 继承链 → PRETTY_NAME 子串兜底三段式归一到 `DistroFamily` 常量（`Debian`/`Ubuntu`/`RHEL`/`CentOS`/`Fedora`/`OpenEuler`/`Kylin`/`NeoKylin`/`Amazon`/`Unknown`）；配套 `PackageFamily`（`PkgDeb`/`PkgRpm`/`PkgAny`）由家族派生包管理类型；`OSRelease.Family()`/`IsDeb()`/`IsRpm()` 便捷谓词。这是 §4.1 系统能力探测层的发行版归一化入口，与 v0.12.3 的 `normalize.go`（架构归一化）对称成体系，消除散落的裸 ID 字符串比较（`handler/version.go` 的 `detectPackageManager`、`diagnostics/component_health.go` 的 `readOSRelease`、`readOSRelease` 重复三处）。
> 2. **收敛散落的裸发行版解析到模块 API**：`handler/version.go` 的 `getOSReleaseInfo()`/`detectPackageManager()`/`getDistroName()` 改用 `arch.ReadOSRelease()`；`diagnostics/component_health.go` 的 `readOSRelease()` 改用 `arch.ReadOSRelease()`（保留原有 `osReleaseBasic` 结构体与匹配逻辑，仅数据来源统一）。`handler/version.go` 的 `os` 包 import 保留（`os.Hostname`/`os.ReadFile` 其他用途未动）。所有改动经 `go build`/`go vet`/`gofmt` 验证通过。
> 3. **install.sh apt/dpkg 锁防护**：新增 `wait_apt_dpkg_lock()` 函数（参考宝塔 `Fix_Apt_Lock`），检查 `/var/lib/dpkg/lock`、`/var/lib/apt/lists/lock`、`/var/cache/apt/archives/lock` 三个锁文件，最多等待 60s，超时 `fuser -k` 强制释放 + `dpkg --configure -a` + `apt-get install -f -y` 修复；`pkg_update_index()` 和 `pkg_install()` 的 apt 分支调用 `wait_apt_dpkg_lock`（dnf/yum 分支无锁机制，不动）。避免首次安装时 GUI 包管理器或并发 apt 持锁导致 `E: Unable to acquire the dpkg frontend lock`。
> 4. **install.sh 系统源备份与回滚**：新增 `backup_system_sources()`/`restore_system_sources()`/`apply_apt_mirror()`/`apply_rpm_mirror()`/`apply_system_mirror()` 函数组（参考宝塔 `Set_Repo_Url`/`Check_And_Fix_Debian_Ubuntu_Source` 的备份→修改→验证→失败回滚模式）；`check_and_install_deps()` 在 `test_mirror_speed` 后调用 `apply_system_mirror()`，按 `DEPS_MIRROR`（tsinghua/aliyun/163）生成 `/etc/apt/sources.list.d/kvm-console-mirror.sources`（apt 系）或 `/etc/yum.repos.d/kvm-console-local-mirror.repo`（RPM 系）并用 `apt-get update`/`dnf repolist` 验证，失败自动回滚到备份；`backup_system_sources()` 按时间戳隔离备份目录（`${TMPDIR:-/tmp}/kvm_console_mirror_backup/<timestamp>/`），apt 系备份 `sources.list` + `sources.list.d/*`，RPM 系备份 `yum.repos.d/*kvm*`；`system`/`offline` 模式不修改系统源。所有改动经 `bash -n install.sh` 语法检查通过。
> 5. **旧版安装路径自动迁移（v0.12.4）**：`detect_existing_install()` 新增检测旧版路径 `/opt/kvm-console`；`choose_install_dir()` 在 install 模式下检测到旧版安装后提示用户是否自动迁移到新路径（默认/自定义均可）；`migrate_from_old_path()` 实现完整迁移流程：停止旧服务 → 拷贝二进制/前端/数据/配置/签名公钥 → 更新 `.env` 路径 → 更新 systemd 服务文件 → 保留全局共享配置（`/etc/kvm-console`、`/var/lib/kvm-console`）。迁移后旧版安装保留在原路径，用户确认成功后可手动删除。所有改动经 `bash -n install.sh` 语法检查通过。
> 6. **macOS 交叉编译自动降级（v0.12.4）**：`build.sh` 的 `set_build_variant_flags()` 新增逻辑——macOS/非 Linux 宿主交叉编译时自动降级仅构建 compat 档（`BUILD_NATIVE=false`），跳过原生版构建（原生版需 Linux 头文件，macOS 无法构建）。避免原生版构建失败导致脚本中断、`.run` 单文件安装器未生成。所有改动经 `bash -n build.sh` 语法检查通过。
>
> **v0.13 修订要点（安装期健壮性与性能修复，基于 0.3.0.5 平滑基线 diff 现场定位）**：
> 1. **set -e 静默退出脚枪修复（#AQ，现场 P1）**：install.sh:6 全局 `set -Eeuo pipefail` 下，`var=$(grep ... | cut | tr)` 在 grep 无匹配退出 1 时经 pipefail 传播 → `set -e` 静默结束脚本且**无任何输出**。现场表现：update 模式 `choose_install_dir` 读已有 `.env`，当 `.env` 存在但无 `INSTALL_DIR=` 行时（grep 退出 1），选完交互默认 1（更新）后**静默退回 shell**，用户误以为「倒计时超时后退出」。已对 10 个代码位置共 14 条命令替换统一加 `|| true`（install.sh:568-570、604-605、1086、2423、2623、2943、3278、3768、3929、3931），空值交给下游 `[ -z ]`/默认值处理，行为不变仅不再被 `set -e` 误杀。**工程约束（写死）**：install.sh 中所有 `$(...)` 管道命令替换必须带 `|| true` 或置于 `if`/`&&` 条件中，禁止裸 `var=$(grep|ls|find|... | ...)`。
> 2. **openEuler 仓库校验前置慢源卡顿修复（#AS，配合 M8.12）**：`enable_openeuler_repos()`（`check_os` 阶段）原对官方 `repo.openeuler.org`（metalink 未禁用、未切镜像）执行 `dnf makecache` + 5× `dnf list available`，早于镜像切换 → 安装前期（STEP 2 前）可达数分钟卡顿（两份现场日志均表现为「发现缺失依赖」后的长时间停顿）。**修复**：从 `enable_openeuler_repos` 移除 makecache/可用性探测，新增 `probe_critical_rpm_packages()`（install.sh:665）在 `check_and_install_deps` 的 `apply_system_mirror` **之后**调用（此时 baseurl 已切 nju/清华/阿里且 metalink 已注释，命中快源）；`system`/`offline` 直接跳过。`enable_openeuler_repos` 仅保留仓库文件清理 + EPOL/everything Section 补写（curl `-m 10` 有界探测）。
> 3. **`dnf provides` 逐命令回退收敛（#AT）**：`install_bundled_packages` Phase 1 原对 16 条命令逐个 `timeout 30s dnf provides`（最坏 ~8 分钟，命中慢源时呈「卡死」），且与 Phase 2 捆绑包提取（`lgft_bins` 覆盖全部 virt-* 工具）职责重复。**修复**：收敛回 5 条核心命令（`virt-filesystems virt-customize guestfish virt-win-reg growpart`，对齐 0.3.0.5 平滑基线）、`timeout` 30s→10s、`DEPS_MIRROR=offline` 整段跳过；其余 virt-* 工具由 Phase 2 捆绑包提取兜底。
> 4. **交互读取 3 秒读秒倒计时（#AR）**：`read_user_input`/`read_tty` 由阻塞式 `read -rp` 改为 `countdown_read_line`（`QVM_READ_TIMEOUT=3`，每秒刷新剩余秒数，`/dev/tty` 隔离读取防 stdout 回灌污染；超时返回 1 由调用方 `${var:-默认}` 落默认值）；`step()` 每步计时（`date +%s.%N`）+ 结尾 `print_step_timing_summary` 按耗时倒序汇总 + 本次流程总耗时（配合 #AS 定位「安装卡在某步」）。非 TTY/CI=1 直接走默认逻辑（#M）不倒计时；**v0.13 核对（#AR 补丁）**：`read_tty` 此前漏了与 `read_user_input` 同款的非交互守卫，网页终端/管道场景每个提示硬等满 5 秒（实测 STEP 7×3 + STEP 8 + STEP 14 = 5 次「等待超时」浪费 25s）；现已对齐：`CI=1` 或 `! -t 0` 时立即返回、由调用方 `${var:-默认}` 落默认值，`QVM_READ_TIMEOUT` 5→3s。
> 5. **镜像源测速补 nju**（v0.13，§5.8）：openEuler 优先推荐南京大学源 `mirrors.nju.edu.cn`（linuxmirrors.cn 高优先级教育网镜像，可达即直接选用，避免阿里云偶发限流/404），不可达再对清华/阿里计时选最快。
> 6. **STEP_TOTAL 计数修正（v0.13 核对）**：`run_install_or_update` 实际含 **19 个 `step()` 主步骤**（此前漏计 `open_frontend_port` 与 `setup_ovs_foundation`），日志曾显示 `[STEP 19/18]` 的错位计数；`STEP_TOTAL` 18→19，§13.7.2 主步骤清单同步补 `open_frontend_port` 并重排编号（主步骤 19 + 辅助 7 = 26 次函数调用）。
> 7. **麒麟镜像源排除（v0.13，Kylin 专项）**：麒麟服务器版（`OS_ID=kylin|neokylin`）基于 CentOS 8 系但使用自有 `archive.kylinos.cn` 官方源、无公开国内镜像。修复两处：`test_mirror_speed` 非 apt 非 openEuler 的 RPM 系原一律探 centos/centos-vault（麒麟探了也无意义）→ 麒麟直接 `DEPS_MIRROR=system` 返回；`apply_rpm_mirror` 原仅处理 `OS_ID=openeuler`，麒麟会落入 `centos-vault` 分支写入 CentOS 源污染麒麟 → 麒麟分支直接返回走系统源。依赖安装靠 dnf `--setopt=timeout/minrate/retries` 兜底。麒麟桌面版（apt 系）不受影响。
> 8. **麒麟 KYSEC 探测（v0.13，Kylin 专项）**：KYSEC（麒麟内核安全机制）可能限制内核模块加载与 `/dev/kvm` 访问。install.sh `check_kysec()`（`ensure_kvm_runtime` STEP 内）防御性多重回退探测（`kysec_ctl` → `/sys/kernel/security/kysec` → `/proc/kysec` → `/etc/kysec`），命中 → info 提示放行建议 + `KYSEC_STATE` 报告复述；面板侧新增 `server/service/arch/kysec.go`（`DetectKYSEC()`/`KysecStatus()` 同口径）接入 component_health `kysec` 条目（仅麒麟命中上报，`diag`+`warning`）+ `/system-info` admin `cpu.kysec` 字段（`enabled`/空）。
> 9. **certified_hardware 口径对齐（v0.13，Kylin 专项）**：build.sh:252 `kylin-v10-server` 原 `["Kunpeng","Hygon"]`，设计文档原写中文型号（海光 7000/5000、飞腾腾云 S2500）且 support_level 写 B——统一对齐到与 `cpu_vendor` 白名单同源的英文 CPU 厂商 token `["Kunpeng","Phytium","Hygon","Zhaoxin","Intel","AMD"]`（麒麟同源支持 飞腾/鲲鹏/龙芯/申威/兆芯/海光 + x86），support_level 统一 S（与 build.sh 已实施口径一致）。
>
> **v0.14 修订要点（当前仓库实施状态校正：后端整体迁移接入 + 前端状态修正 + 键名核对）**：
>
> **背景**：M8.1 / M8.5 / M8.9 / M8.10 / M8.11 的后端实现此前仅存在于旧仓库 `jeoQVMConsole`，当前仓库 `jeochQVMConsole` 长期缺失（文档 v0.11/v0.12 的「已实施」标注与实际代码仓库不一致）。v0.14 从旧仓库将该 5 项后端整体迁入当前 `jeochQVMConsole/server` 并完成接线验证，同时按当前仓库真实代码修正前端状态与 manifest 键名：
>
> 1. **迁移的后端模块（✅ 已接入当前仓库 **`jeochQVMConsole/server`**，`go build ./...` / `go vet ./...` 通过）**：
>    - `service/arch/{os.go,normalize.go,domestic_cpu.go,kysec.go}`——发行版归一化（`ReadOSRelease`/`DistroFamilyOf`）、架构归一化（`NormalizeArch`/`IsX86Arch`/`IsAarch64Arch`/`IsRiscv64Arch`）、国产 CPU 厂商探测（`DetectCPUVendor`）、麒麟 KYSEC 探测（`DetectKYSEC`/`KysecStatus`）。
>    - `model/migrations/`：`schema_migrations` 表 + `Register`/`Run` 单事务执行；`model/db.go` InitDB 在 AutoMigrate 前调用；首个迁移 `0001_scheduler_events_vm_status_index`。
>    - `model/vm_watchdog_event.go` + AutoMigrate 注册（表数 28→29）。
>    - `service/vmwatchdog/watchdog.go` + `vmwatchdog_wire.go`（HookResetVM / IsMaintenanceModeEnabled 注入）：`StartWatchdog()` / `CheckHugePagesAdvice()`。
>    - `service/diagnostics/{component_health.go,periodic_probe.go}` + `diagnostics_wire.go`：`GetComponentHealth` / `ResetComponentHealthCache` / `RefreshComponentHealth` / `GetOsSupport` / `DetectCurrentOsSupport` / `CurrentOsReleaseName` / `StartHealthProbe` / `GetHealthProbe`。
>    - `handler/health_probe.go` + `handler/version.go` 重构（admin 专属 `component_health`/`cpu`/`glibc`/`selinux` 字段 + TTL 缓存 + aarch64 QEMU 解析）；`handler/diagnostics.go` 补 `GetOsSupport`/`RefreshDiagnostics`；`handler/scheduler.go` 补 `GetVMWatchdogEventList`。
>    - `config` 增 `VMWatchdogEnabled/IntervalSeconds/MaxMisses`、`HealthDir`、`AppVersion`（全链路 env/DB/序列化）；`main.go` 启动 `StartVMWatchdog`/`StartHealthProbe`；`router.go` 挂 `/system/health/latest`（公开）、`/settings/diagnostics/os-support`、`POST /settings/diagnostics/refresh`、`/vm-watchdog/events`（admin）。
>
> 2. **前端状态修正（关键）**：v0.12 标注的 M8.9/M8.10/M8.11 前端组件（`web/src/api/health.ts`、`web/src/components/HealthLight.tsx`、`web/src/api/watchdog.ts`、`web/src/views/vm-watchdog/index.tsx`、AdminDashboard 顶部灯位、诊断页「发行版支持等级」卡片 /「面板运行状态」卡片、系统设置「VM 看门狗」分区）**当前仓库 `web/` 中均不存在**。参考实现位于旧仓库 `jeoQVMConsole/web/`（上述文件齐全）。**待迁移**：按旧仓库实现将健康探针灯、看门狗事件页、os-support/健康度诊断卡片、看门狗配置分区迁入当前仓库 `web/`，并同步 `endpointDescriptions.ts`/`fieldDictionary.ts`/路由与导航。优先级 P2。
>
> 3. **manifest 键名修正**：§5.11.2/§5.11.3/§14 多处误写 `cpu_vendor_whitelist`，实际代码使用 `system_requirements.cpu_vendor.whitelist`（见 `compat-manifest.json` 实文件与 `component_health.go` `compatRequirement.Whitelist`）。本版统一更正为 `cpu_vendor.whitelist`（含 `domestic-hci-gap-analysis.md` / `差异分析.md` 的相关表述），并回退了此前对 `cpu_vendor` 条目的误清理（该条目被 component_health 消费，非死数据）。
>
> 4. **表数/文件数口径复核**：AutoMigrate 加入 `VMWatchdogEvent` 后 29 张表、`server/handler/` 45 个 .go 文件、「后台调度器 10 项」等 v0.12 附录口径**仅对迁移后的后端成立**；前端完成后将再复核前端路由/组件表。
>
> **v0.15 修订要点（M8 相关前端整体迁入当前仓库，前后端对齐完成）**：
>
> 1. **迁移的前端模块（✅ 已接入当前仓库 `jeochQVMConsole/web`，`tsc -b` 通过）**：`api/health.ts`（`HealthProbe`/`getHealthProbeLatest`）、`api/watchdog.ts`（`VMWatchdogEvent` 列表）、`views/dashboard/components/HealthLight.tsx`（Dashboard 顶部 30s 轮询健康灯，绿色/黄色/红色三态 + 维护模式）、`views/vm-watchdog/index.tsx`（「看门狗事件」页：列表 + status/vm_name/时间筛选，复用 `sch-*` 调度页样式）、`views/settings/components/DiagnosticsTab.tsx`（**全量替换**为参考实现：面板运行状态卡 + 组件版本健康度卡 + 发行版支持等级矩阵卡 + 诊断类别收集导出，图标库 `@douyinfe/semi-icons`）、`AdvancedTab.tsx`（系统设置「VM 看门狗」分区：启用开关 + 探测间隔 + 失联次数阈值，带校验）。
> 2. **接线同步**：`config/nav.tsx`（新增 `vm-watchdog` 菜单项 + `IconPulse` + `NAV_COLORS`）、`router/pages.tsx`/`router/index.tsx`（`/vm-watchdog` 懒加载路由）、`AdminDashboard.tsx`（引入 `<HealthLight />`）、`api/settings.ts`（`getOsSupport`/`refreshDiagnostics`/`ComponentHealth` 类型 + `cpu`/`component_health` 字段）、`views/settings/types.ts`（`vm_watchdog_enabled/interval/max_misses` 设置项 + 校验 + payload）、`endpointDescriptions.ts`（补 `/vm-watchdog/events`、`/settings/diagnostics/os-support`、扩展 `/system-info` 描述 + 分组）。
> 3. **样式补充**：`settings/settings.css`（`stg-comp-*`/`stg-os-*`/`stg-probe-*` 健康度与支持等级卡片样式 + 深色模式低对比扩展 `stg-comp-title/cat-label/name`）、`dashboard/dashboard.css`（`qvm-health-light` 灯 + 红点脉冲动画 + 深色降对比）。vm-watchdog 页复用既有 `sch-*` 调度页样式与 `qvm-mono`/`qvm-fade-up` 通用 token，无需新增 CSS。
> 4. **口径更新**：§14.2/§14.4/§14.5 的 M8.9/M8.10/M8.11 前端「待迁移」标注全部改为「已迁移（v0.15）」；前端路由/组件表、表数/文件数口径无需再变更。当前仓库 `jeochQVMConsole` 前后端已与设计文档 v0.12 起的「已实施」标注完全一致。
>
> **v0.16 修订要点（CI 缺口补齐：glibc 实测 / arm64 低档 / 产物签名上传，与参考仓库对齐）**：
>
> 1. **背景**：v0.15 只迁移了前后端源码与文档；对照参考仓库 `jeoQVMConsole` 的 `.github/workflows/build.yml` 后发现当前仓库 CI 与本设计「已实施」标注不符——多个 job 与服务步骤缺失。本版一次性补齐 CI 侧缺口，使构建/验证/发布流水线与参考仓库完全一致。
> 2. **补 `verify-centos7-glibc217` job（M8.3 / §4.3 / §8 P0-3）**：等 amd64 构建完成后下载发行包，解压取默认 compat 档 `kvm-console` 二进制，在 docker `centos:7`（glibc 2.17 基线）内实测 `ldd --version` + `kvm-console --version` + `kvm-console --smoke-selfcheck`，将 glibc 2.17「理论兼容」升级为「实测通过」。（说明：实现基于后端 `main.go` 既有的 `--smoke-selfcheck` 自检子命令，本次仅补 CI 编排。）
> 3. **新增 `verify-arm64-glibc-low` job（§13.7.2 优化登记，v0.12.1）**：等 arm64 构建完成后在 arm64 runner 上下载产物，于 `arm64v8/ubuntu:20.04`（glibc 2.31）容器内实测 `--version` + `--smoke-selfcheck`；`centos:7` 无 arm64 变体镜像，故用 2.31 作最低档冒烟（默认 compat 档 GLIBC 上限即 2.17，运行时只依赖更低符号，属预期）。
> 4. **产物上传补 `.sha256`/`.minisig`/`minisign.pub`**：`build`/`build-arm64` 的 GitHub artifact 均含 `release/...tar.gz`、`...tar.gz.sha256`、`...tar.gz.minisig`、`release/minisign.pub`（`if-no-files-found: warn`，未签名时缺 `.minisig` 不阻断）；OSS 上传同步补上述三旁文件。
> 5. **`release` job 补 minisign 离线签名（候选④/M8.7 增强）**：新增「生成 SHA256 校验（必做）」+「minisign 签名发行包（可选）」两步——私钥经 Actions secret `MINISIGN_KEY`（minisign 私钥文件内容 base64）传入，未配置则跳过签名仅 SHA256，不阻断发布；Release 上传文件由仅 `*.tar.gz` 扩为 `*.tar.gz`/`*.sha256`/`*.minisig`/`minisign.pub`。
> 6. **amd64 构建档位固定（#A/M0.5）**：`build` job 由 `ubuntu-latest` 改为 `ubuntu-22.04`（glibc 2.35 固定档），避免 `native-glibc.txt` 随 runner 漂移到 2.39；`build-arm64` 因 GitHub 无 `ubuntu-22.04-arm` 公共 runner 保留 `ubuntu-24.04-arm`（glibc 2.39，arm64 用户回落 compat 属预期，见 §4.3）。
> 7. **补充公钥资产**：新增 `docs/GCHSJ/minisign.pub`（与 install.sh 内嵌 `MINISIGN_PUBLIC_KEY` 一致，Ed25519 公钥注释名 `F605F4243FA08760`）。**唯一仍待发行方外部动作**：CI 启用 minisign 需在 GitHub 配置 `secrets.MINISIGN_KEY`（未配置自动降级为仅 SHA256，不阻断）；私钥 `.minisign-sec/` 由发行方离线保管。
> 8. **一致性**：`.github/workflows/build.yml` 经 `diff` 与参考仓库逐字节一致；YAML 语法校验通过。
>
> **v0.17 修订要点（安装语言环境检查增强：推荐英文 + 明确不支持日语/韩语）**：
>
> 1. **背景**：安装期预检 `check_locale`（install.sh:752）原仅按「英文/中文 UTF-8」放行、其余告警；且正则大小写敏感（`zh_CN.utf8` 小写后缀会误警）。本版在 v0.16 之后按运维诉求做两处增强——**安装检测优先推荐英文**，且**明确声明不支持日语/韩语**。
> 2. **英文优先推荐**：放行分支与告警文案均改为「建议优先使用英文 `en_US.UTF-8`」；`en*`（任意子区域）配 UTF-8 编码纳入放行区间。理由：QVMConsole 依赖 `virsh`/`qemu-img` 等命令输出进行正确识别，英文环境输出最稳定。
> 3. **明确排除日语/韩语（ja_/ko_）**：`check_locale` 新增优先级最高（先于编码放行判定）的 `ja_*`/`ko_*` 命中分支——无论编码是否为 UTF-8，检测到即明确 warning「不支持日语/韩语」，提示改用英文或中文；`ja_JP.UTF-8`/`ja_JP.EUC-JP`/`ko_KR.*` 均不误放行。理由：日/韩命令输出解析不可靠，且不在本次国产化适配范围内。
> 4. **放行区间校正**：统一转小写后判定（zh_CN.utf8 与 zh_CN.UTF-8 等价，消除大小写误报）；放行区间①英文/en/C/POSIX 配 UTF-8、②中文（zh_CN/zh_SG/zh_TW/zh_HK 等）UTF-8、③任意 `.utf8/.utf-8` 结尾；GBK/GB2312/GB18030/latin1/EUC-JP 等非 UTF-8 编码仍拦截提示。
> 5. **实现**：仅改动 `install.sh::check_locale`（install.sh:752-791），无后端/前端改动。改动经 `bash -n` 语法检查通过 + 22 组语言场景实测（日/韩 → 明确不支持，英/中/其他 UTF-8 → 放行并推荐英文，非 UTF-8 → 拦截）。
>
> **v0.18 修订要点（官方 GitHub 仓库核对比对 + 测试机孤儿 VM 清理，代码零回归确认）**：
>
> 1. **与官方仓库核对**：克隆 `https://github.com/QVMConsole/QVMConsole.git`（HEAD `c32c865`）全仓比对。结论：**当前仓库 `jeochQVMConsole` 是官方仓库的严格超集**——官方 HEAD 为本地 HEAD（`a4d3461`）的祖先，本地领先 8 提交、落后 0 提交，官方无任何本地缺失内容。重点核对用户关注的五块（虚拟机创建 / 模版导入 / 模版创建虚拟机 / 存储池 / 系统设置存储与网络）：`template/*`、`vmimport/*`（除 OVMF 路径）、`storage/pool/*`（除 lsblk 兼容）、`storage/disk/*`、`network` 核心、前端 `StorageNetworkTab` 均与官方完全一致。本地与官方的全部差异均为**国产化/增强**（SELinux 打标、OVMF 路径动态解析、osinfo-query 兼容、lsblk 兼容、防火墙可插拔后端、看门狗/健康探针/诊断、元数据中英文识别、`--smoke-selfcheck`），无功能回退。
> 2. **此前排查的「镜像回归」澄清**：上一轮对比的是参考副本 `jeoQVMConsole`（HEAD `782c015`，比官方更新），之见到的 `template/linux_deps.go` 国内镜像差异与 `PassthroughSection` 删减，是**参考副本相对 GitHub 官方的演进**，不是本地相对官方的回归——本地与 GitHub 官方在模板链路完全一致。
> 3. **测试机孤儿清理（192.168.10.170 / openEuler 2403，面板 `/opt/QVMConsole`）**：发现一台用「模版创建」产生的坏虚拟机 `vmdnqtvq2e`（`template-source`：Ubuntu26.04-LTS 链式克隆，`clone_mode=linked`），其磁盘文件缺失、缺 `domain-config` 元数据、`images` 池无模板，导致面板持续报「磁盘文件缺失 + metadata 未找到」。已清理：`virsh undefine --nvram`（含 NVRAM）移除域 → `pool-refresh vm-disks` 清孤儿卷 → 删除 `vm_caches` 缓存行 → 全表清扫（`vpc_vm_bindings`/`vm_credentials`/`vm_schedules`/`vm_watchdog_events`/`public_ip_bindings`）确认无残留；删除前已备份 XML 至 `/opt/QVMConsole/data/backup_vmdnqtvq2e.xml`。另清理了 `vm_caches` 中 20+ 条既无域定义也无卷的幽灵缓存（`guestfs-*` 等），仅保留真实运行的 `vm8qtqoqp4`。
> 4. **结论**：代码层面本地无缺口，测试机模版创建报错为**运行态数据残留**（孤儿 VM 缓存 + 缺失磁盘）所致，现已在 libvirt / 卷 / 缓存 / 元数据全链路清理干净。**代码零新增，仅文档修订**。

> ---
>
> **v0.13.1 修订要点（openEuler/Kylin 实机回归修复 + 后端 SELinux 自动打标）**：
> 1. **openEuler 系统主源 glob 补漏（v0.13.1，openEuler 专项）**：openEuler 系统主源文件名为 `openEuler.repo`（无连字符），原 glob 仅匹配 `openEuler-*.repo`/`openeuler-*.repo`，metalink 未注释、baseurl 未切换 → 全量包下载打官方慢源、`makecache` 卡 600s+。修复：glob 扩为四模式 `openEuler.repo`/`OpenEuler.repo`/`openEuler-*.repo`/`openeuler-*.repo`，统一注释 metalink、写入镜像站 baseurl。
> 2. **makecache 冗余合并 + minrate 提速（v0.13.1，openEuler 专项）**：`apply_rpm_mirror` 与 `probe_critical_rpm_packages` 两处全量 `makecache` 重复（实测 106s+45s），合并为一次统一执行；`--minrate` 1KB/s→10KB/s 令慢源快速放弃切源。STEP2 总耗时 186s 显著下降。
> 3. **swtpm 依赖与 SELinux 放行（v0.13.1，openEuler 专项）**：openEuler 模版创建 TPM 时报 `无法执行二进制文件 /usr/bin/swtpm: Permission denied`——根因 install.sh 从未安装 swtpm + SELinux Enforcing 未打标。修复：APT_DEPS 补 `swtpm`、RPM_PKG_MAP 映射 `swtpm`；`setup_selinux` 增加 `restorecon -R /usr/bin/swtpm`。见 §2.3.1 依赖补充与 docs/dependencies.md。
> 4. **lsblk `MOUNTPOINTS` 列兼容（v0.13.1，Kylin 专项）**：新 `utils.LsblkMountpointsColumn()` 按 util-linux 版本（≥2.37 输出复数 `MOUNTPOINTS`，旧版输出单数 `MOUNTPOINT`）动态取列，调用点 `storage/pool/list.go` 与 `diagnostics/collector.go` 同步；麒麟 V10 util-linux 2.3x 不再报 `unknown column: MOUNTPOINTS`。
> 5. **osinfo 变体列表回退（v0.13.1，Kylin 专项）**：`vm/create.go` 的 `ListOSVariants` 优先 `osinfo-query os`（libosinfo 标准输出），失败回退 `virt-install --osinfo list`（麒麟 virt-install 2.2.1 不支持长列表），新增跳过 `Short-ID` 表头 + `categorizeOSVariant` 归类。麒麟 V10 不再报 `获取系统变体列表失败: exit status 2`。
> 6. **麒麟组件版本门槛基线覆盖（v0.13.1，Kylin 专项）**：麒麟 V10 系统源锁定 qemu 4.1/libvirt 6.2/ovs 2.12/virt-install 2.2 无法升级。`check_component_versions` 按 `OS_ID=kylin|neokylin` + `VERSION_ID=V10*` 覆盖门槛（qemu≥4.0/libvirt≥6.0/ovs≥2.10/virt-install≥2.0，均为关键组件健康值），杜绝 STEP13 版本检测硬中止安装。
> 7. **os-release 引号剥离（v0.13.1，Kylin 专项）**：麒麟 `/etc/os-release` 为 `ID="kylin"`/`VERSION_ID="V10"` 带引号，`sed` 提取后未去引号导致 `case "$ID"` 匹配失败（首轮修复未生效）。统一在提取后 `tr -d '"'` 再参与 case 判定。
> 8. **后端 SELinux 自动打标（v0.13.1，SELinux Enforcing 专项）**：openEuler 模版克隆启动报 `无法在 'system_u:object_r:virt_content_t:s0' 中设定安全上下文 …/Ubuntu26.04-LTS.qcow2: Operation not permitted`——模板目录内单个文件经 mv/替换保留 `usr_t` 源上下文，fcontext 规则虽存在但 restorecon 未执行。新增 `utils/selinux.go`（`SelinuxMode()` 探测 / `EnsureSELinuxLabel()` 在 Enforcing 下对模板+克隆目录幂等 `restorecon -RF`），挂接克隆主入口 `clone/core.go` 与 `clone/linked_clone.go`。面板进程需 root 运行方可生效。
> 9. **AAVMF aarch64 补漏（v0.13.1，openEuler/麒麟 arm）**：`server/service/arch/aarch64.go` 补 openEuler `/usr/share/edk2/aarch64/AAVMF_{CODE,VARS}.fd` 布局（此前 install.sh 检测已含，后端 profile 缺失），与 x86_64 的 `/usr/share/edk2/ovmf/` 候选路径（v0.12.4）对齐；clone/vmimport 硬编码路径已统一走 `vm_xml.ResolveOVMFVarsTemplatePath(true)`。
>
> **v0.12 修订要点（M8 剩余 7 条实施交付 + 设计文档同步）**：
> 1. **M8.3（P0-3）glibc 2.17 真兼容实测已实施**：`server/main.go` 新增 `--smoke-selfcheck`（别名 `--db-probe`）子命令 + `runSmokeSelfcheck()`（SQLite 内存库 AutoMigrate 空结构体 + libvirt 空连 2s 超时，socket 不存在也返回成功）；`libvirt_rpc.LibvirtSocketPath()` 导出；install.sh `select_binary_smoke_test` 追加 `--smoke-selfcheck`；build.yml 新增 `verify-centos7-glibc217` job（docker `centos:7` 下跑 `--version` + `--smoke-selfcheck`）。
> 2. **M8.5（P1-5）DB schema 版本化迁移已实施**：新建 `server/model/migrations/` 子包（`schema_migrations` 表 + `Register`/`Run`，单事务执行）；`model/db.go` InitDB 在 AutoMigrate 前调用 `migrations.Run(DB)`；首个迁移 `0001_scheduler_events_vm_status_index`（scheduler_events `(vm_name,status)` 复合索引；原 0001_vpc_switch_cidr_index 方案因与遗留 `preFixVPCSwitchCIDRIndex`/`migrateVPCSwitchCIDRColumn` 逻辑冲突被删除，VPC cidr 索引继续由遗留迁移管理）。
> 3. **M8.6（P1-6）发行版回滚已实施**：install.sh `backup_previous_release()`（`.release_backup/{01|02|03}` 循环保留 3 份，备份 kvm-console/native/compat 二进制 + web-dist + meta）；`install_files()` update 分支停服后调用；部署末尾写 `INSTALL_DIR/.version`；`qvmc-manage.sh` 新增「回滚到上一版本」菜单项与 `rollback_release()`（列备份槽位 → 停服 → 还原二进制/web-dist → 重启，数据/配置不动）。
> 4. **M8.7（P1-7）安装包完整性已实施**：build.sh 打包段生成 `SHA256SUMS`（tarball 的 `.tar.gz.sha256` + 包内二进制 `SHA256SUMS`）；install.sh `extract_tarball()` 解压前校验 `.sha256`（失败 exit 1 中止），下载分支同步拉取 `${url}.sha256`。
> 5. **M8.9（P2-9）VM 看门狗 + 大页建议已实施**：新建 `server/service/vmwatchdog/` 子包（`StartWatchdog()` 周期探测运行中 VM 的 Guest Agent，连续 3 次失联 → HookResetVM 硬重置 + `VMWatchdogEvent` 入库；维护模式/无 libvirt 跳过）；`config` 增 `VMWatchdogEnabled/IntervalSeconds/MaxMisses`（.env `KVM_VM_WATCHDOG_*`）；component_health 增 `hugepages` 项（内存 ≥128GB 且 HugePages_Total=0 → warning「建议开启大页」）；`server/model/vm_watchdog_event.go` + AutoMigrate 注册。
> 6. **M8.10（P2-10）周期健康探针 + Dashboard 灯已实施**：`server/service/diagnostics/periodic_probe.go`（`StartHealthProbe()` 每分钟原子写 `${KVM_HEALTH_DIR:-/opt/QVMConsole/.health}/latest.json`，含 libvirt 就绪/daemon/维护模式/版本/运行时长）；公开接口 `GET /api/system/health/latest`；前端 `web/src/api/health.ts` + `HealthLight` 组件（30s 轮询：libvirt 不可用→黄灯、接口超时/不可达→红灯、正常→绿灯，深色模式适配）；AdminDashboard 顶部灯位。
> 7. **M8.11（P3-11）支持等级 S/A/B/C 已实施**：compat-manifest.json `os_compat` 各条目新增 `support_level`（S=官方全量回归/A=核心功能回归/B=社区自测/C=理论兼容）与 `certified_hardware` 认证硬件清单；build.sh 打包与嵌入 manifest 同步；versions.conf 增 `SUPPORT_LEVEL_<os>` 行；install.sh `check_support_level()` 按发行版匹配，`support_level=C`（如 CentOS 7）→ warn「理论兼容，生产请升级到认证基线」+ 报告复述。
> 8. **§14.5 候选 4 条已实施（v0.12 追加）**：①看门狗灵敏度配置界面（系统设置「调度与高级」Tab 新增「VM 看门狗」分区，config 持久化 + 每轮重读配置即生效）；②支持等级矩阵前端展示（`GET /settings/diagnostics/os-support` + 诊断页「发行版支持等级」卡片）；③看门狗事件前端查看（`GET /vm-watchdog/events` + 系统菜单「看门狗事件」页，类型/虚拟机/时间筛选）；④诊断面板接入健康探针实时数据（诊断页新增「面板运行状态」卡片，拉取 `GET /system/health/latest`，展示 libvirt 就绪/daemon/维护模式/运行时长/面板版本）。
>
> **v0.11 修订要点（M8 首批 5 条实施交付 + 设计文档同步）**：
> 1. **M8.2（P0-2）firewalld 三档分级已实施**：`COMPONENT_REQ_FIREWALLD` 0.4.0（build.sh + 嵌入 compat-manifest.json + install.sh 回退默认）；`Enable()` `<0.6` 早退返回 `FirewalldOldVersion`；advice.go 新增 `firewalld_unsupported` 字段（`<0.6` 不完整支持 > `firewalld_old` 0.6~0.9 > glibc > selinux）；前端 Banner 与 install.sh print_install_report 同步三档文案。
> 2. **M8.1（P0-1）CPU 厂商细分已实施**：新增 `server/service/arch/domestic_cpu.go` `DetectCPUVendor()`（Intel/AMD/Hygon/Phytium/Zhaoxin/Kunpeng）；install.sh precheck_domestic 写 `.env DOMESTIC_CPU_VENDOR` + 海光 npt=0 / 飞腾鲲鹏 kvm 模块顺序提示；component_health 增 cpu_vendor 白名单项（`Whitelist` 字段，manifest 键 `system_requirements.cpu_vendor.whitelist`）；`/system-info` `cpu.cpu_vendor` 上报。
> 3. **M8.4（P1-4）stage 持久化 + 权限加固已实施**：`.install_state/`（stage/last_error/release_sha256/binary_tier/component_summary/degraded_notes）+ `--resume` 参数（step 包装器记录已完成步骤、失败时写入 last_error 并提示续跑）；ensure_directories 对 `/etc/kvm-console` 系目录 chmod 700/敏感文件 600。
> 4. **M8.8（P2-8）bash 审计 + kdump 已实施**：`setup_bash_audit`（PROMPT_COMMAND 注入 /root/.bashrc + /etc/skel/.bashrc，chattr +a 失败降级 chmod 622）+ `check_kdump_suggestion`（裸金属无 crashkernel → warn + 报告复述）。
> 5. **M8.12（P3-12）镜像源测速 + offline 已实施**：`test_mirror_speed`（tsinghua/aliyun/163 curl 计时取最快）写入 `.env DEPS_MIRROR`；offline 时 check_and_install_deps 跳过 install 仅扫缺包汇总报告。
>
> **v0.10.1 修订要点（M8 与已修改代码逐条对照核实，明确复用点与缺口）**：
> 1. **P0-2 firewalld 三档分级已具备 ~60% 代码基础**：`firewalldVersionAtLeast()`（backend_firewalld.go:810）已存在并用于 `<0.7` 区段写文件（`firewalldEnsureZoneExists` L403/`firewalldDeleteZone` L643 的 `!firewalldVersionAtLeast("0.7")` 分支）与 `≥0.9` policy 门控（L680）；`FirewalldOldVersion` 错误码（errors.go:27）已定义。**缺口仅剩**：`COMPONENT_REQ_FIREWALLD` 阈值 0.8→0.4（build.sh:74）+ `Enable()`（L196）`<0.6` 显式降级分支 + advice.go `<0.6` 三档判定——即 M8.2 范围收窄为「阈值 + Enable 分支 + advice 判定」三处，无需新建版本探测（复用 `firewalldVersionAtLeast`）。
> 2. **P0-1 CPU 厂商可复用现有架构**：`detectCPUFlags()`（version.go:202，`/proc/cpuinfo` Fields 分词）是现成探测入口，新增 `DetectCPUVendor()` 与其同文件扩展即可；install.sh `precheck_domestic`（L2605）为写入 `.env DOMESTIC_CPU_VENDOR` 的现成挂点。
> 3. **P1-4 权限加固已做一半**：`.env` `chmod 600`（install.sh:1229）已落地；缺口为 `ensure_directories`（L1348）对 `/etc/kvm-console/**` 的 chmod 700/600 与 `.install_state/` stage 持久化 + `--resume`。
> 4. **P0-3 可复用 `DetectGlibcVersion`**（utils/cmd.go:78）：`--smoke-selfcheck` 子命令与 CI centos:7 job 均以此为探测基线；`select_binary_smoke_test`（install.sh:2209）为冒烟扩展挂点。
> 5. **P1-5 落点确认**：AutoMigrate 在 `server/model/db.go:92`，migrations 需在其前置（原设计写 main.go，修正为 db.go 初始化链）。
> 6. **M8.2~M8.12 其余各项无已改代码基础，维持待实施**。逐条代码落点与复用点见 §14.4。

> **v0.10 修订要点（国产超融合竞品差异吸收，对照 HCI 6.11 / SMTXOS 6.2 分析）**：
> 1. **新增 §14 竞品差异吸收**：从深信服 HCI 6.11.1 与 SmartX SMTXOS 6.2 两个竞品 ISO 解压分析提炼 12 条可吸收优化点（P0-P3），逐条映射到设计文档落点（§4.3 / §5.1 / §5.8 / §5.11.2 / §5.11.3 / §8 / §13.7.2）并登记为 **M8.1~M8.12 待实施里程碑**。完整分析见 `docs/差异分析.md`。
> 2. **P0-2 firewalld 版本三档分级**：`COMPONENT_REQ_FIREWALLD` 最低版本 0.8.0 → **0.4.0**（CentOS 7 基线 0.4.4.4 不再误报 critical），`Enable()` 增加 `<0.6` 显式降级分支（§5.1 决策 3 扩展 + §5.11.2 组件表 + §5.11.3 manifest 阈值）。
> 3. **P0-3 glibc 2.17 真兼容实测**：§4.3 新增「centos7-glibc217 CI 验证 + `--smoke-selfcheck` 冒烟扩展」要求，将「理论兼容」升级为「实测通过」。
> 4. **P0-1 CPU 厂商细分**：§5.8 precheck_domestic 新增 CPU 厂商探测子项，§5.11.2 组件表新增 `cpu_vendor` 行（海光/飞腾/鲲鹏/兆芯阈值）。
> 5. **P1-P3 安装可靠性/观测性**：§5.8 新增 `.install_state` stage 持久化 + `--resume` + 目录 chmod 700/600（P1-4）、§8 新增回滚/migrations/包校验/stage 持久化验收用例（P1-4~P1-7）、bash 审计 + kdump 提示（P2-8）、VM 看门狗 + 大页提示（P2-9）、周期健康探针 + Dashboard 灯（P2-10）、支持等级 S/A/B/C + 硬件认证矩阵（P3-11）、国内镜像源自测 + offline 模式（P3-12）。
> 6. **远程仓库分叉说明**：本地 `main`（本设计 + M0-M8 国产化工作）与 `origin/main`（VPC 安全组联动、用户管理、轻量云 VM 删除、关于页折叠 4 个功能提交）已分叉；§14 记录合并口径（本地国产化工作为待推送基线，远程功能提交按 `docs/merge-from-upstream.md` 合并回本地时保留 §14 全部国产化文件）。
>
> **v0.9.11 修订要点（对照上游原生代码审计，兼容性错误与未完善项修复）**：
> 1. **firewalld deny 语义修复（H1→#AN）**：`firewalldAddRuleToZone` 无来源 deny 规则此前落 `--add-port`（放行），语义与 ufw deny 相反；现 deny 规则（无论有无来源）统一走 rich-rule（reject），`buildFirewalldRichRule` 来源字段可选；`parseFirewalldRichRuleLine` 动作 token 改为支持行尾无空格的 reject/drop/accept（此前漏判无 description 规则）。
> 2. **firewalld <0.7 优雅降级（H2→#AO）**：CentOS 7 / Kylin V10（0.6.3）无 `--new-zone`/`--delete-zone`，`firewalldEnsureZoneExists`/`firewalldDeleteZone` 降级为直接原子写/删 `qvm-host.xml`（复用 Enable 路径），daemon 运行中才 reload；`Disable()` 的 `--delete-policy`（≥0.9）加版本门控（H3），消除「zone 已删但 reload 未执行」半失败态。
> 3. **RPM 系 UEFI 固件包映射修复（H4）**：`RPM_PKG_MAP` 补 `edk2-ovmf`/`edk2-aarch64`/`qemu` 自映射，此前 `pkg_name` 无映射被 `continue` 静默跳过，RPM 发行版 UEFI 引导 VM 全不可建。
> 4. **高兼容档版本动态发现（build.sh H1→#AP）**：install.sh 不再硬编码 `compat-2.28`，`select_binary_tier` 从发行包扫描 `kvm-console-compat-{VER}` 取最高档得 `HIGH_COMPAT_VER`，`install_files` 按动态档落位、`write_env` 按动态档白名单校验，与 build.sh `--high-compat-glibc` 任意值对齐。
> 5. **宿主机连接管理旧 iproute2 兼容**：`ss -H` 移除（表头由解析自然跳过），`ss -K` 加能力探测（iproute2 <4.8 明确报错而非静默空转）。
> 6. **`.env` 持久化后端 update 复用（M2）**：`detect_firewall_backend` update 模式优先读 `.env` FW_BACKEND，防自动探测静默切后端；repair 模式不再清空 KVM_PORT（M3）；update 不再销毁 libvirt default 网络（M4）。
> 7. **其余修正**：`resolveQEMUCmd` arm64 加 qemu-kvm 回退；UEFI 探针对齐 arch profile（OVMF_CODE_4M.fd / qemu-efi-aarch64 QEMU_EFI.fd）；CPU 旗标改 Fields 分词（防末尾漏判）；firewalld Defaults 在 qvm-host 未创建时回退系统默认 zone 判定；none 后端不再每次重置探测缓存；`execCommandWithLogLevel` timeout≤0 语义与 context 变体对齐；OVS systemd 单元名按 PKG_MGR 区分；麒麟桌面（Debian 系空 ID_LIKE）回退 apt；AVX2 仅 x86 上报；prepare-bridge.sh 在 firewalld 后端下不再写 iptables INPUT 规则（#F 设计一致性）。
>
> **v0.9.8 修订要点（实施后文档与代码再核对，9 项修正）**：
> 1. **§13.7.2 install.sh 阶段清单全面重写**：原写「19 个阶段」顺序与内容均与实际代码（install.sh:2235-2261 `run_install_or_update`）严重错位——漏列 `detect_firewall_backend`（主步骤 9）/ `precheck_domestic`（主步骤 10）/ `select_binary_tier`（主步骤 12）/ `print_install_report`（辅助 23），且 `install_files` 描述仍写「改造目标」而实际已实施。重写为 **17 主步骤（STEP_TOTAL=17，step 包装失败 exit 1）+ 6 辅助配置（失败仅 warn 不阻断）= 23 次函数调用**，补全 `precheck_domestic` / `print_install_report`，修正 `install_files` 描述为「已实施」，修正代码行号引用。
> 2. **`POST /settings/diagnostics/refresh` 路由待实施标注**：文档 5 处引用该路由（§5.11.1 / §5.11.5 / §8 M7 用例 / v0.9.3 修订要点 / §12 #T），但 router.go:114-115 `/settings/diagnostics/*` 当前仅 `categories` + `export`，无 `refresh`。在 §5.11.1 / §5.11.5 / §8 各处补「**当前路由待实施 M7.2**」标注。
> 3. **§13.10 待实施项表补 `POST /settings/diagnostics/refresh` 行**：原表 5 行（manifest / check / component_health / go:embed / 前端卡片）漏列 refresh 路由，现补为 6 行，归属 M7.2。
> 4. **子包命名 `system` → `diagnostics`**：§13.3.3 / §13.5.1 / §13.8 / §13.9 / §7 M7.2 / §13.10 全文将计划新建的 `system` 子包改为**复用现有 `diagnostics` 子包**（已有 `collector.go`，语义聚合），减少子包碎片化。`system.ComponentHealthChecker` → `diagnostics.ComponentHealthChecker`，文件路径 `server/service/system/` → `server/service/diagnostics/`。**注：v0.9.4/0.9.5 历史修订块中残留的 `system.*` 命名（如 `system.ComponentHealthChecker`）属记录当时写法，不作为当前设计口径，以正文与 §13.10 为准**。
> 5. **§4.3 CI runner 实施要点更新为已实施状态**：原写「build.yml 当前 amd64 用 ubuntu-latest」，实际已固定为 `ubuntu-22.04`（build.yml:41，#A/M0.5 已落地）。更新为已实施描述 + 补 `release` job（build.yml:209）用 `ubuntu-latest` 的释疑（该 job 不编译 Go 二进制，仅下载 artifact + 创建 Release）。
> 6. **§4.4 安装流程补 `precheck_domestic` 步骤**：原流程图漏列该步骤（§5.8 #L 新增的端口占用/多防火墙/NM 环境预检），补在 `detect_firewall_backend` 之后、`get_release` 之前。
> 7. **§13.9 对应关系表与阅读路径指引「19 阶段」→「17 主步骤」**：§13.9 表 3 行 + 阅读路径指引 1 处，同步修正。
> 8. **AGENTS.md handler 文件数 43 → 44**：AGENTS.md L18 原写「43 个文件」，实际 `server/handler/*.go` 为 44 个（设计文档 §13.2 / §13.3.2 已写 44，AGENTS.md 自身偏差），已同步修正。
> 9. **本轮第二轮核对 5 项差异修复**：① **§4.2 #E 规则 1 落地**——原设计「任一后端命令失败自动 Reset 重测重试」未实现且与 §12 口径冲突（backend_detect.go:45 注释虚标），现新增 `resolveBackend()`（缓存后端 `Available()=false` 时重置重测一次）并接入 status/编排/规则/端口转发全部入口，§4.2 #E 规则 1 改写为已实施口径；② **`print_install_report` 补升级提示**——原 M5 宣称已输出 advice 但实际只输出基本信息，现补三行提示（firewalld <0.9 → 建议升级、glibc < native 需求 → 提示当前 compat 档、SELinux Enforcing → restorecon 已处理），并新增 `DETECTED_FW_VER`（detect_firewall_backend 探测 `firewall-cmd --version`）与 `NATIVE_GLIBC_REQUIRED` 全局（select_binary_tier 提前读取，update 模式同样可得）；③ **`DEGRADED_NOTES` 补齐赋值**——原引用但从未赋值（降级项永不显示），现由 detect_firewall_backend（后端 none）与 install_files（档位冒烟回退）累积写入；④ **探测缓存优化**——`DetectGlibcVersion` / `DetectSELinuxMode` 加 10min TTL 缓存（与 advice 同周期），version.go 的 `DetectSELinuxMode()` 由调用 2 次减为 1 次；⑤ **§4.1 / §13.10 M2 字段补全**——`firewall.docker_compatible` / `firewall.error_code` 代码早已实现但文档字段表未列，已补全。
>
> **v0.9.7 修订要点（实施前事实核对修正，与代码库逐项对齐）**：
> 1. **TaskType 注册数修正**：50 枚举 → 注册 **50 个**（原写「48 个」漏算了 `registerGuestDiskHandler` 闭包注册的 3 个磁盘处理器：TaskTypeVMDiskResize / TaskTypeVMDiskProvision / TaskTypeVMDiskGuestMount），§13.2 / §13.3.1 step 10 / §13.3.6 / §13.5.1 / §13.5.3 调用链全文统一。
> 2. **AutoMigrate 表数精确化**：「25+ 表」→ **28 张表**（§13.2 架构图 / §13.3.1 step 4 / §13.3.5 / §13.7.5），与 db.go:92-95 实际调用一致。
> 3. **migrate 函数数修正**：「10 个」→ **11 个 `migrateXxx` 函数定义**（§13.3.1 step 4 / §13.3.5 / §13.7.5），其中 10 个在 InitDB 中直接调用（db.go:99-108），另 1 个 `migrateVPCBindingUniqueIndex`（db.go:264）作为 `migrateVPCBindingInterfaceOrder` 的内部辅助函数被间接调用。
> 4. **install.sh 阶段数修正**：18 → **19 个**（§13.7.2 / §13.9 / §13 阅读路径指引），原写「18」误将 `configure_qemu_for_rpm` 与 `configure_libvirt_nonroot` 用「/」合并算作 1 个；实际 install.sh:1889-1909 是 19 个独立串行调用。
> 5. **§13.3.3 `system` 子包行明确标注「待实施」**：原描述「（v0.9.4 新建）」可能让读者误认为当前已存在，现补「**待实施**，当前文件不存在，见 §13.10」——与 §13.10 待实施项表保持闭环。
> 6. **本次核对未修改的项**（已核实正确）：22 个 `*_wire.go` 文件、19 个 `ArchProfile` 接口方法、7 个后台调度器、7 个网络运行态恢复函数、`GET /system-info` 路由位置（router.go:533 + version.go:32）、handler 44 个 .go 文件（设计文档正确，AGENTS.md「43 个」是 AGENTS.md 自身偏差）。
> 7. **结论**：经本轮逐项核对修正后，§13 项目架构总览与代码库现状完全对齐，§13.10 待实施项表清晰区分「目标设计」与「现状代码」，可作为实施基线启动 §7 实施计划。
>
> **v0.9.6 修订要点（§13 深度补强 + 全文事实核对，与代码库对齐）**：
> 1. **§13.5.1 后端关键接口与结构体追加 `scheduler.SchedulerDefinition`**：调度器注册表（`RegisterScheduler` / `ListSchedulers`）+ 事件中心（`StartSchedulerEvent` / `FinishSchedulerEventSuccess` / `FinishSchedulerEventFailed`）+ SSE 推送 + 168h 自动清理，并标注与 taskqueue 的区别（scheduler 持久化到 SQLite，taskqueue 纯内存）。
> 2. **§13.5.1 补强 `libvirt_rpc.libvirtConn`**：补充自动重连细节（3 次指数退避 1s/2s/4s）+ 降级 virsh 条件（`UseGoLibvirt=false` 或连接失败时）。
> 3. **§13.7.2 install.sh 阶段补全**：从原 14 阶段补全为 19 阶段（`configure_qemu_for_rpm` 与 `configure_libvirt_nonroot` 拆分为两个独立阶段），新增 `ensure_apparmor_storage_access`（AppArmor 访问规则）/ `ensure_sysctl_network`（IPv4 转发）/ `setup_sshd_foundation`（sshd 配置）/ `show_info`（安装摘要），并修正阶段编号。
> 4. **§13.7.6 CVE 缓解工具补强**：补充漏洞存在时长（16 年）+ `restore.sh` 安全检查机制 + 风险级别判定表（5 级：无风险/低/中/中/高）。
> 5. **§5.11.5/§5.11.1/§12 #T/§8 M7 用例/§13.10 的 capabilities 路由引用统一**：`component_health` 挂载点、手动刷新接口、验收用例、待实施项全部由「`GET /system/capabilities` / `POST /system/diagnostics/refresh`」改为「**复用 §4.1 的 `GET /system-info` 扩展 + 现有 `/settings/diagnostics/*` 组新增 `POST /settings/diagnostics/refresh`**」——消除 v0.9.4 新增章节与 v0.9.3「不再新建 `/system/*` 分组」决策的冲突。
> 6. **arch 接口方法数两处说法统一为 19 个**（§2.1 原「14 个」、§13.5.1 原「18 个」均与实际不符；`arch/types.go` 实为 19 个方法，已在 §13.5.1 全列）。
> 7. **TaskType 计数更新为现状**：50 种枚举、`registerTaskHandlers` 注册 50 个（含通过 `registerGuestDiskHandler` 闭包注册的 3 个磁盘处理器）（§13.2 架构图 / §13.3.1 启动步骤 10 / §13.3.6 / §13.5.1 / §13.5.3 调用链，原「30+/35+/48 个」均过时）。
> 8. **前端依赖包名与版本修正**：`react-router-dom` → `react-router` 8.3.0（package.json 实际包名）；go-libvirt 补全完整版本 `v0.0.0-20260217163227-273eaa321819`（§13.6）。
> 9. **诊断页现状描述修正**：现有 `DiagnosticsTab.tsx` 为「诊断类别选择 + 导出 ZIP」（`/settings/diagnostics/categories` + `/export`），非「服务状态/网络状态」展示（§5.11.1 决策 3）。
>
> **v0.9.5 修订要点（§13 项目架构附录同步完善）**：
> 1. **§13.3.3 service 子包职责矩阵追加 `system` 子包**：标注为「v0.9.4 新建」，对应 §5.11.5 的 `component_health.go`（18 项组件 `--version` 探测 + `go:embed compat-manifest.json` 阈值比对 + `sync.Once` 缓存 + `ResetComponentHealthCache()` 手动刷新）。
> 2. **§13.5.1 后端关键接口与结构体追加 3 项**：`system.ComponentHealthChecker`（18 项组件版本探测器）+ `system.ComponentHealthItem`（单项健康度结构）+ `//go:embed compat-manifest.json`（Go 1.16+ embed 指令）。
> 3. **§13.7.2 构建产物结构表追加 `compat-manifest.json`**：标注为「v0.9.4 新增」，含 `binaries`（三档 GLIBC）+ `system_requirements`（18 项组件阈值）+ `os_compat`（7 个发行版推荐档位），并说明缺失时的回退行为。
> 4. **§13.8 关键设计模式追加「go:embed + sync.Once 缓存」**：标注为「v0.9.4 新建」，复用 `arch.DetectHostArch` 的 sync.Once 模式，新增 `//go:embed` 构建期资产嵌入能力。
> 5. **目的**：让 §13 项目架构总览真正「自包含」——读者无需跨章节跳转即可在 §13 看到 §5.11（v0.9.4）新增的 `system` 子包、`ComponentHealthChecker` 结构、`compat-manifest.json` 产物、`go:embed` 模式，与 §13.9 对应关系表、§13.10 待实施项表中的 §5.11 条目形成完整闭环。
>
> **v0.9.4 修订要点（组件版本检测与升级提示闭环）**：
> 1. **新增 §5.11 组件版本检测与升级提示**（7 个子节，约 360 行）：构建期 `compat-manifest.json` + 部署期 `check_component_versions()` + 运行期 `component_health` 探测 + 前端诊断页卡片，覆盖 18 项系统级组件（核心 8 项 / 磁盘镜像 7 项 / 诊断扩展 3 项）。**仅检测 + 提示，不自动升级**（决策 1）；前端展示位于系统设置 → 诊断页（决策 3）。本节是 v0.9.3 advice 机制（3 项 upgrade_advice）的系统化扩展，从「3 项 advice」升级为「18 项全量组件版本健康度」。
> 2. **§12 追加 #T 补充项**：组件版本检测闭环，固化位置 §5.11 / §6.1 / §7 (M7) / §8 (M7 用例块)。补充项总数 23 → 24。
> 3. **§7 新增 M7 里程碑**：与 M0-M6 并行独立交付，分 M7.0-M7.4 五个子阶段（manifest 生成 / install.sh 检测 / 后端探测 / 前端卡片 / 文档同步），跨 M4/M5/M2/M3 扩展。
> 4. **§8 追加 M7 用例块**：15 条验收用例，覆盖构建期 manifest 生成、部署期 critical/warning/多工具回退/架构专属/探测超时、运行期探测缓存/版本解析失败降级、前端诊断页展示/深色模式、manifest 缺失回退、18 项组件探测覆盖。
> 5. **§13.9 对应关系表追加 §5.11 行**：manifest + check + component_health + 前端诊断页 → §13.7.2 build.sh + install.sh 19 阶段 + §13.3.3 system 子包（新建）+ §13.4.3 诊断页（现有）+ §13.8 go:embed + sync.Once 模式。
> 6. **§13.10 待实施项表追加 5 行**：`compat-manifest.json` / `check_component_versions()` / `component_health` 字段 / `go:embed` / 前端诊断页卡片，均为本设计待实施产物，现状代码中均不存在。
> 7. **组件清单来源**：经核查 install.sh `APT_DEPS`/`RPM_DEP_MAP`/`COMMAND_CHECKS` + DEPENDENCIES.md + server/service/ 实际命令调用，共 18 项系统级组件，每项标注包名（apt/dnf）、探测命令、最低版本、推荐版本、用途与影响。
>
> **v0.9.3 修订要点（能力探测接口收敛 + 组件升级提示 + 构建硬校验）**：
> 1. **能力探测收敛到现有 `GET /system-info`（§4.1/§5.6/§11）**：不再新建 `/system/capabilities` 接口与 `/system/*` 路由分组。复用现有 `authorized.GET /system-info`（router.go:533，handler/version.go:32）——该接口已返回 os/distro/pkg_manager/arch/cpu/libvirt/qemu/ovs 字段，且已有 `ovs_install_command`（version.go:59）「缺失组件安装提示」先例。本设计在其响应上**增量扩展** `glibc`、`cpu.avx2`、`firewall.{backend,available,active,version,ip_backend,nm_managed,upgrade_advice}`、`selinux.{mode}` 字段；API Key 兼容由现有 authorized 只读口径自然覆盖（§11 决策 3 保持「允许」）。同时删除原「后端新增 service/systeminfo 包 + settings 组扩展」设计（§13.10 待实施行同步删除）。
> 2. **新增组件升级提示 advice 机制（§4.1/#Q/§5.7/§6）**：基于探测结果输出结构化 `upgrade_advice`，对国产系统相关组件给出可操作升级/降级提示——`firewalld_old`（<0.9 无 policy → 升级 firewalld 或依赖 iptables 路径，对应 #R）、`glibc_low_for_native`（glibc < native-glibc.txt → 当前使用 compat 档，升级 glibc 可启用 native）、`selinux_enforcing`（→ restorecon 提示，对应 #S1）。提示分**安装期**（install.sh `print_install_report` 输出一次）与**运行期**（面板防火墙页 Banner）；**不改任何后端自动返回行为**（自动兼容返回沿用 §4.3/§6.4 既有设计，与 version.go 现有 `ovs_install_command` 提示模式一致）。
> 3. **构建期 readelf 硬校验（§5.9/#S/M6）**：`verify_compat_glibc` 无 readelf 现状「warn 降级跳过」（build.sh:68-71）升级为**硬失败并给出安装命令**（`apt install binutils` / `dnf install binutils`），消除 §5.9 指出的「静默漏检」风险——CI 固定镜像（#A ubuntu-22.04）自带 binutils，本地构建按提示安装即可。
>
>
> **v0.9.2 修订要点（阅读导航与跨章节索引补强）**：
> 1. **§13 章首新增「阅读路径指引」**：按 5 类角色（首次接触的实施者 / 防火墙改造开发者 / GLIBC 档位开发者 / 运维部署人员 / 代码评审回归测试）给出推荐选读章节顺序，避免读者在 10 个 §13 子节中迷路。
> 2. **§13.5.3 末尾新增「调用链与 §1-§12 改造点的对应位置」表**：10 行表格将调用链每一段映射到「现状代码 → 本设计改造点 → 设计章节」，读者看完微图后一眼可知本设计改在哪一段（如 `[firewall/host.go: Enable]` 现状硬编码 `ufw --force enable` → 改为 `DetectHostFirewallBackend().Enable()`，对应 §3.1/§5.3）。
> 3. **§8 章首新增「用例编号映射表」**：22 行表格列出每个用例类别对应的 §12 补充项编号（#A-#S）与关键验收点，让验收用例与运维补充项的双向引用关系一目了然（如「zone 绑定与转发全链路 → #F」「命令加固 → #N」）。
>
> **v0.9.1 修订要点（§13 项目架构总览附录补强）**：
> 1. **§13.4.3 末尾新增「`VmFormProvider` 嵌套边界」**：明确 Provider 必须直接包裹 `CreateVmWizard` 与 `EditVmForm` 根 JSX、绝不允许放进 Layout、sections 子组件可无差别消费 Context、TypeScript Symbol 注入做编译+运行双层校验——解答「新增字段为什么必须在 create/edit 两处同步」之根因。
> 2. **§13.5.3 新增「关键调用链（从用户点击到宿主机执行）」**：以「点击 Enable 宿主机防火墙」为例画出 10 段调用链（Semi Button → axios 拦截器 → Gin 中间件链 → 路由分组 → handler 薄层 → service delegate → 子包业务 → 新 backend 抽象 → `ExecCommandWithTimeout` → firewalld D-Bus → progress 回调 → taskqueue SSE 广播），并附「后端启动到服务可用」15 步 + init 顺序拓扑。让读者看到 §1-§12 的代码改动落在整条调用链的哪一段。
> 3. **§13.7.5 测试环境由 4 行扩为完整段落**：补全测试账号表、运行时信息（数据库/迁移/同步/进程/端口/备份）、运维入口（含本设计新增 `/etc/firewalld/zones/qvm-host.xml` 与 `qvmc-manage.sh` firewalld 分支的关联）、冒烟测试 6 步最小路径。
>
> **v0.9 修订要点（项目架构总览附录）**：
> 1. **新增 §13 项目架构总览**：作为本设计的「项目上下文附录」，自包含项目整体架构、主要模块职责、关键类与函数说明、依赖关系、项目运行方式、关键设计模式六大块，让读者在不阅读其他文档的情况下理解 QVMConsole 项目骨架，从而更好地理解 §1-§12 中国产化适配具体改造点所基于的代码结构。
> 2. **§13 与 §1-§12 的对应关系（§13.9）**：以表格形式列出本设计每章改造点依赖的现有架构（§13）位置，明确「长在现有骨架上」的具体含义——如 §2.1 ArchProfile 注册表复用 §13.3.3 arch 子包 + §13.8 注册表模式；§4.2 防火墙后端抽象复用 §13.3.3 firewall 子包 + §13.8 注册表/Deps/Hook 模式。
> 3. **澄清设计待实施项与现状代码的差异（§13.10）**：经核对，§4-§5 中描述的 `native-glibc.txt`、`detect_firewall_backend`、`select_binary_tier`、`KVM_BINARY_TIER`、`FW_BACKEND`、`--high-compat-glibc`、`kvm-console-compat-2.28` 等均为本设计的待实施产物，当前 build.sh/install.sh 中尚未存在；现状代码在 install.sh `install_files` 中以 GLIBC ≥ 2.34 + AVX2 硬编码判断切换 native，二进制命名仅 `kvm-console` / `kvm-console-native` / `kvm-console-compat`（无版本后缀）。本设计的改造目标正是替换这些硬编码与缺失项。
>
> **v0.8 修订要点（完整/可靠/安全/灵活/可交互/报错反馈综合，参考商用安装包实践）**：
> 1. **安装期完整性与报错反馈（§5.8/#K/#L/#M）**：install.sh 引入全局安装日志 + `step` 序号包装 + 失败定位（输出失败步骤、原因、日志路径、排查命令）+ 安装总结报告（后端/档位/glibc/CPU/SELinux/降级项/日志路径）；新增预检（面板端口占用、多防火墙共存、NM 环境）；支持非交互/无人值守（`FW_BACKEND`、`KVM_BINARY_TIER` 环境变量覆盖，非 TTY 或 `CI=1` 跳过 `read` 采用推荐值，`.env` 值白名单校验防路径注入）。
> 2. **命令执行加固与可靠性（§4.2/§5.1/#N/#O/#P）**：后端命令固定绝对路径（防 PATH 劫持）+ 全部带超时（防 dbus 挂死阻塞面板）；新增 **iptables/nftables 后端探测**（`iptables -V`）作为 §5.1 决策 10「面板 iptables 是否可依赖」的落地判据；`qvm-host.xml` 改用「临时文件 + rename」原子写入（崩溃安全）+ SELinux restorecon 时序后移。
> 3. **报错反馈结构化（§4.1/§5.3/§5.7/#Q/#R）**：`/system-info` 扩展诊断字段（`firewall.version` / `ip_backend` / `nm_managed` / `selinux.mode` / `upgrade_advice`）；后端错误统一 `error_code` + 可操作 `hint`（如 FIREWALLD_NOT_RUNNING → `systemctl start firewalld`），`LastError` 保持兼容；前端 Banner/Tooltip 展示 hint，Enable 任务后展示自检结果并可回滚。
> 4. **文档同步范围明确（§5.7/#S）**：M3 同步更新 `firewall-page.md`（UFW→后端措辞、none Banner、未管理 Tag）；M6 更新 `build-compatibility.md` 补充 `--high-compat-glibc` 及 `verify_compat_glibc` 在无 readelf 时 warn 降级现状（build.sh:68-71）。**v0.9.3 修订：warn 降级改为硬失败**（缺 binutils → 构建失败 + `apt install binutils` / `dnf install binutils`）。
>
> **v0.7 修订要点（实施评审补充）**：
> 1. **国产系统按服务器版分析（§3.2/§4.3/§8/§9）**：明确分析范围——国产系统（麒麟 V10 / openEuler / UOS）一律按**服务器版**评估（麒麟服务器版 V10 = CentOS 系，dnf + firewalld，glibc 2.28+）。**桌面版暂不分析**（麒麟桌面版 V10 = Debian 11 系/apt+ufw，与 Ubuntu/Debian 走同一既有路径，如需支持另行补充，不纳入本设计验收）。原文档「麒麟 V10 = glibc 2.28」统一为服务器版口径。
> 2. **#A 实施要点补 build.yml runner 固定细节（§4.3/§5.9/§9）**：核实 `.github/workflows/build.yml` 现状——amd64 `runs-on: ubuntu-latest`（build.yml:40，=ubuntu-24.04/glibc 2.39）、arm64 `runs-on: ubuntu-24.04-arm`（build.yml:122，glibc 2.39）。实施 #A 时必须**同步固定两处 runner 至 glibc 2.35 档**，否则 `native-glibc.txt` 记录 2.39，Ubuntu 22.04/Debian 12 用户被静默降级到 compat（原 §9 注 3 仅表述「必须固定」，未指出 build.yml 具体改动点）。
>
> **v0.6 修订要点（可实施性/可靠性/安全性/解耦性/兼容性综合评估）**：
> 1. **CI 双档事实修正（§5.9/§4.3/#I）**：核实 `.github/workflows/build.yml:72` 的 `bash build.sh -v` 在 `BUILD_VARIANT=""` 下**同时构建 compat + native 双档**（build.sh:252），install.sh:1645 也真实检查 `kvm-console-native`。文档原「CI 当前仅构建 compat」表述错误，修正为：CI 已产出 compat + native **双档二进制**（双档结构是现状而非新增）；`native-glibc.txt` 文件经 v0.9.1 §13.10 核实为**待实施产物**（build.sh 现有 `verify_compat_glibc` 仅校验不写文件）。新增项仅是 `--high-compat-glibc 2.28` 高兼容档（可选）。
> 2. **zig 版本结论澄清（§5.9/#I）**：zig 0.14.0（CI 现行版本，build.yml:65）已支持 `x86_64-linux-gnu.2.28` 目标（zig glibc 版本锁定自 0.7+ 即支持，0.14 默认档即 2.28），**高兼容档无需升级 zig**；文档原「zig 0.16.0 支持 2.28」表述过度保守。
> 3. **`ManageUFWRule` 命令注入面补入 #S3（§5.4/§12）**：核实 `network/helpers.go:21-39` 用 `fmt.Sprintf("ufw allow %s", rule)` 走 `ExecShell`，且 `handler/network.go:666` 接受**任意字符串 rule**——这是现存真实注入面，文档原 #S3 只覆盖 rich-rule description/CIDR。修正：新后端 `ManageHostFirewallRule` 必须用**结构化参数**（action + proto + port），禁止 shell 字符串拼接；迁移期对 rule 输入做白名单校验（`^[0-9a-zA-Z:/,. -]+$`）。
> 4. **hook 表述矛盾统一（§2.2/§5.4）**：§2.2「不需要新增 hook」与 §5.4「推荐新增 hook 变量」自相矛盾。统一为：**新增 `HookManageHostFirewallRule` 一个 hook 变量**（与 `HookEnsureHostFirewallPortForwardRule` 同模式，firewall_wire.go:75），`GetUFWStatus`/`ManageUFWRule` 均经它委托。
> 5. **NetworkManager 与 zone 绑定交互（§5.1 决策 10/#J）**：firewalld `--add-interface` 绑定在 NM 管理的连接上，NM 连接配置可携带自身 zone（`connection.zone`），NM 重启/连接重连时可能覆盖 firewalld 绑定。修正：绑定物理接口时同步处理 NM zone 属性（`nmcli connection modify <conn> connection.zone qvm-host`，无 NM 则跳过）；验收增加「NM 重启后 zone 绑定保持」用例。

> **v0.5 修订要点（可行性第二轮复审）**：
> 1. **firewalld zone 语义修正（关键，§5.1 决策 4/6/10）**：zone `--set-target` 并非只作用于 INPUT——man 页明确**转发包按 ingress zone 的 target 决定放行/丢弃**，标准 zone 在 `default` target 下转发流量默认被拒。单靠 `qvm-host` DROP zone + 面板直写 iptables 仅在 **iptables 后端**（RHEL7 系 firewalld 0.8.x）可靠（面板 `iptables -I FORWARD 1` 先于 firewalld 链求值）；**nftables 后端**（0.9+/1.x，openEuler 22.03）下 firewalld `--reload` 后链注册顺序不保证，zone DROP 可能先于面板 ACCEPT 规则 → **VPC 转发 / 端口转发到 VM / dnsmasq 入站有被丢弃风险**。
> 2. **zone 绑定缺失（§5.1 决策 10）**：原设计创建 `qvm-host` zone 但未定义绑定方式，未绑接口/源/默认 zone 的 zone **完全不生效**（DROP 惰性）。修正：VM 桥接口（`br-ovs`、`vpcsw*`）绑 `trusted`（ACCEPT）解决 VM 转发与 dnsmasq 入站；上行接口绑 `qvm-host`（DROP）管制宿主机入站；firewalld ≥0.9 用 **policy**（ingress=上行 zone，egress=trusted，target=ACCEPT）放行 uplink→VM 的转发（端口转发/SPICE 公网）。
> 3. **dnsmasq INPUT 放行纳入 firewalld 验收（§5.8/§8/#F）**：install.sh:1527 现有 iptables INPUT 放行（UDP 67/53、TCP 53）在 nftables 后端不保证先于 firewalld 链，需改为 firewalld zone/policy 等效放行，并在验收矩阵覆盖 VPC DHCP/DNS。
> 4. **capabilities glibc 探测口径补齐（§4.1/#G）**：后端与 install.sh 同口径（`ldd --version` 首行末 token，回退 `getconf GNU_LIBC_VERSION`），杜绝两处口径漂移。
> 5. **firewalld 服务未运行读行为（§6.4/#H）**：Available=true 但 Active=false；`ListRules/Defaults` 返回空不报错，`Enable()` 负责启动服务（当前设计已在 §5.1 写 `systemctl start`，补齐读取边界）。

> **v0.4 修订要点（可靠性复审）**：
> 1. **GLIBC 默认基线不再提升**（§4.3）：原方案把 compat 默认从 2.2.5 提到 2.28 会破坏存量 Debian 系（Ubuntu 18.04=2.27、16.04=2.23、Debian 9=2.24），与目标 6 冲突。改为默认档不变 + 新增 `--high-compat-glibc 2.28` 可选高档。
> 2. **锁粒度修正**（§4.2）：backendExec 只包裹单条子进程命令；严禁在锁回调内调用其他加锁方法（`Enable→EnsureRule` 二次加锁 = 死锁）。`Enable/Disable` 原子序列单次持锁。
> 3. **Docker 结论修正**（§5.1 决策 6）：`docker -p` 经 FORWARD/DOCKER 链不经 INPUT，qvm-host zone DROP 通常不拦 Docker 发布端口；重验焦点改为 FORWARD 策略与 `--reload` 对 DOCKER 链的影响。
> 4. **CI 镜像固定**（§4.3/#A/§9 注 3）：native 构建机 glibc 固定 2.35 档（如 ubuntu-22.04），避免 `ubuntu-latest` 漂移导致既有 Ubuntu 22.04 用户被静默降级。
> 5. **SELinux restorecon 收敛到单文件**（§5.1 决策 7）：不动系统自带 zone 的 context。
> 6. **install.sh 顺序与 update 边界**（§4.4/§5.8/§6.1）：`select_binary_tier` 移至 `get_release` 之后（需确认发布包内含哪些档位）；`detect_firewall_backend` 在依赖安装之后；update 模式非交互复用 `.env` KVM_BINARY_TIER、切换主程序前冒烟测试（失败保留旧档）。
> 7. **qvmc-manage.sh 自维护后端检测**（§5.10）：按 install.sh 口径增加 ufw/firewalld 分支，且与 ufw 同样「仅后端 active 时操作」。
> 8. **文档定位收敛**：本文件为国产化适配唯一事实来源，自包含 GLIBC 目标表/依赖清单/验证命令；上游合并保护本仓库独有后端文件（§10 新增风险行）。

---

## 1. 背景与目标

### 1.1 要解决的问题

| # | 问题 | 严重度 | 影响面 |
| --- | --- | --- | --- |
| P1 | 后端硬编码调用 `ufw`，而国产 RPM 系统只装 `firewalld` | 阻断 | 宿主机防火墙、端口转发持久放通、SPICE 公网暴露全部不可用 |
| P2 | GLIBC 档位策略过保守，国产系统（glibc 2.28）全部落回低性能兼容版 | 性能 | 二进制体积、运行性能、新 glibc 特性缺失 |
| P3 | 无统一「系统能力探测」出口，install.sh 与后端各自探测、口径不一 | 可维护性 | 未来新增国产化适配时重复建设 |

### 1.2 设计目标

1. **防火墙后端可插拔**：ufw / firewalld 语义对等，国产系统零配置自动适配。
2. **GLIBC 档位选优**：发布包内多档二进制，install.sh 按「glibc + CPU 指令集」交互式选择最优档。
3. **系统能力探测层**：扩展现有 `GET /system-info`（v0.9.3 收敛），install.sh / 前端 / 运维共用同一口径。
4. **完全遵循现有代码模式**：复用 `arch` 包「接口 + 注册表 + sync.Once 缓存」模式、`deps.go` hook 注入、taskqueue + progress 回调、委托模式。
5. **可交互**：安装期与面板运行期均提供探测结果展示、默认推荐、人工确认/覆盖的交互点，杜绝「静默降级」。
6. 不改变既有 Debian/Ubuntu 行为；不破坏现有前端交互语义。

---

## 2. 现有代码模式分析（本设计的依据）

设计必须「长在现有骨架上」，以下模式被复用。

### 2.1 ArchProfile 注册表模式（重点范例）

`server/service/arch/` 是现成的「可插拔后端」教科书：

- `types.go`：定义 `ArchProfile` 接口（**19 个**能力方法，见 §13.5.1）。
- `registry.go`：`profiles map[string]ArchProfile` + `RegisterProfile()` + `GetProfile()`（未知回退 x86_64）。
- `detect.go`：`DetectHostArch()` 用 `sync.Once` 缓存探测结果，探测失败降级默认值。
- `x86_64.go` / `aarch64.go`：各自 `init()` 中 `RegisterProfile` 自注册。

> **结论**：防火墙后端抽象直接复制此模式，即 `Backend` 接口 + `RegisterFirewallBackend` + `DetectHostFirewallBackend()`（实现用 RWMutex 双检，语义等同 sync.Once）。文件结构、命名、降级策略全部对齐。

### 2.2 deps.go / Hook 注入模式

`service/firewall/deps.go` 定义包级函数变量（`HookOvsBridgeName` 等），`service/firewall_wire.go` 的 `init()` 赋值，打破循环依赖。

> **结论（v0.6 统一）**：本设计新增 **一个 hook 变量** `HookManageHostFirewallRule`（与 `HookEnsureHostFirewallPortForwardRule` 同模式，firewall_wire.go:75 处追加），`network` / `spice` 均经既有 hook 或该新 hook 间接调用，注入点保持薄委托，不产生包间新依赖。

### 2.3 委托模式

`service/firewall_wire.go` 为 `service` 根包提供 `GetHostFirewallStatus()` 等薄委托函数，handler 只调用 `service.XXX`。

> **结论**：所有新增 delegate 遵循该文件现有风格追加，不动 handler 层 API 签名。

### 2.4 taskqueue + progress 回调

`main.go:928-943` 中 `TaskTypeEnableHostFirewall` / `TaskTypeDisableHostFirewall` 已注册，`EnableHostFirewall(req, progress func(int, string))` 支持进度推送。

> **结论**：firewalld 后端的 `Enable/Disable` 保持同一签名，前端任务中心进度展示零改动。

### 2.5 install.sh 交互式主流程

`main()`（install.sh:3143）先解析命令行参数（`--skip-version-check`/`--resume`，v0.11 起），再串行执行 `check_root → check_os → check_arch → check_locale → choose_mode → init_log_file`，`choose_mode` 已用 `read_user_input`（v0.13 起带 3 秒读秒倒计时）提供交互选择，最后按模式调 `run_install_or_update`/`repair_config`/`uninstall_app`。

> **结论**：新增「防火墙后端探测」「二进制档位选择」作为 install.sh 中的可交互步骤，复用 `info/warn/success/read_user_input`（v0.13 起带读秒倒计时，底层 `countdown_read_line`）风格。

### 2.6 前端「架构专属显示」约定

AGENTS.md「架构专属功能：前端仅在对应架构上显示」。现有 `arch.DetectHostArch()` → 前端通过系统信息接口区分（如 ARM UEFI 兼容固件）。

> **结论**：防火墙后端信息同理，仅在 `HostFirewallTab` 展示后端名 + 可用性，不新增独立页面。

---

## 3. 问题全景与影响面

### 3.1 ufw 硬编码调用点（P1）

| 文件 | 现状 `ufw` 用法 | 改造方式 |
| --- | --- | --- |
| `service/firewall/host.go` | `status verbose` / `show added` / `default ...` / `--force enable|disable` / 规则写入删除（host.go:23,50,98,114,128,432,441） | 全部改走 `DetectHostFirewallBackend()` 返回的后端 |
| `service/firewall/host_portfwd.go` | `ufw status`（host_portfwd.go:12） | `backend.Active()` |
| `service/firewall/host_rules.go` | 间接（走 ensureHostFirewallRule） | 无改动（底层已换） |
| `service/firewall/types.go` | `UFWAvailable bool`（types.go:119） | 新增 `Backend string` / `BackendName string`，保留旧字段兼容 |
| `service/network/helpers.go` | `ufw status numbered` / `ufw allow|deny|delete`（helpers.go:13,25-29） | 委托 `firewall` 包的后端接口 |
| `service/firewall_wire.go` | hook 注入 | 注入点不变 |
| `service/spice/expose.go` + `spice_wire.go` | `HookManageUFWRule → ManageUFWRule` | 不变（内部实现换） |
| `qvmc-manage.sh` | `ufw status/allow/delete`（qvmc-manage.sh:253-315） | 增加 firewalld 分支 |

### 3.2 GLIBC 档位问题（P2）

现状（build.sh:45-51, 176-179；install.sh:1650-1680）：

- `get_compat_glibc_default`：amd64=2.2.5、arm64=2.17。
- install.sh 仅当 `glibc >= 2.34 && AVX2` 才用 native，其余全用兼容版。
- 国产系统 glibc（v0.7 明确：按服务器版口径）：麒麟服务器版 V10 = 2.28+、openEuler 20.03 / UOS 1060 = 2.28、openEuler 22.03 = 2.34。

### 3.3 其余组件（无需改造，仅登记）

- ISO 生成：`clone/windows_configdrive.go:189-197` 已有 `genisoimage → xorriso → mkisofs` 回退链。
- nftables/iptables：后端已直连。
- `arp-scan` / `libguestfs-tools`：build.sh:372-416 已捆绑 RPM。

---

## 4. 总体设计

### 4.1 系统能力探测层（新增，统一口径）

**接口归属（v0.9.3 收敛）**：**复用现有 `GET /system-info`**（`authorized` 分组，router.go:535；handler `version.go:123`），在现有响应上**增量扩展**国产化相关字段——**不新建 `/system/capabilities` 接口与 `/system/*` 路由分组**。该接口已是「授权只读」，且现有实现已有 `ovs_install_command`（version.go:153）「缺失组件安装提示」先例，本设计的 advice 提示沿用同一模式。

```
GET /system-info        （授权只读，v0.9.3 扩展）
{
  // ...现有字段（version.go:40-59：go_version/os/distro/os_id/os_id_like/
  //   pkg_manager/arch/num_cpu/hostname/num_goroutine/kernel/uptime/
  //   libvirt/qemu/qemu_spice/ovs_package/ovs_service/ovs_installed/ovs_install_command）
  "glibc": "2.34",
  "cpu": { "avx2": true, "fma": true },
  "selinux": { "enforcing": false, "available": true, "mode": "permissive" },
  "firewall": {
    "backend": "firewalld", "available": true, "active": true,
    "version": "0.8.0", "ip_backend": "legacy", "nm_managed": true,
    "docker_compatible": true, "error_code": "FIREWALLD_NOT_RUNNING",
    "upgrade_advice": { "firewalld_old": true, "glibc_low_for_native": false, "selinux_enforcing": false }
  }
}
```

- 后端在现有 `handler/version.go` `GetPublicSystemInfo` 中**增量扩展**产出（不新增独立 service 包，字段计算复用 `service/firewall` 的 `DetectHostFirewallBackend` / `backend.Version()`）。
- 该接口供前端「系统信息」页与防火墙页共用；install.sh 保持 bash 自探测（脚本与后端不共享代码，但**口径文档化**于 `docs/dependencies.md`）。
- **glibc 探测口径（v0.5 补齐，#G）**：后端与 install.sh 必须同口径，避免两处探测漂移导致选优档位不一致。统一规则（与 install.sh:1652-1654 一致）：
  1. 主路径：执行 `ldd --version`，取首行最后一个 token（形如 `2.34`）；无输出或格式异常走回退。
  2. 回退路径：`getconf GNU_LIBC_VERSION` 的第二个字段。
  3. 仍失败：返回空串，由调用方按「未知 glibc」降级处理（install.sh 兜底默认档；面板展示「未知」不阻断）。
   实现上后端用 `utils.ExecShellQuiet`（代码库既有工具），禁止引入 cgo/系统调用探测。
- **探测缓存（v0.9.8 落地，防重复子进程）**：`DetectGlibcVersion` / `DetectSELinuxMode` / `DetectUpgradeAdvice` 均在 `service/firewall/advice.go` 带 **10min TTL 缓存**（RWMutex 双检，`adviceCacheTTL`），避免 `/system-info` 每次调用重复执行 `ldd --version` / `getenforce` / `firewall-cmd --version`；组件升级后超时自动重新探测。`GetPublicSystemInfo` 对 `DetectSELinuxMode()` 单次调用，同时产出 `enforcing` 与 `mode`。
- **诊断字段扩展（v0.8 新增，#Q，报错反馈）**：`firewall` 对象增加 `version`（`firewall-cmd --version`）、`ip_backend`（`iptables -V` 含 `(nf_tables)`/`(legacy)`，见 #O）、`nm_managed`（是否存在 NetworkManager 且上行物理接口归属 NM）；`selinux` 增加 `mode`（enforcing/permissive/disabled）。一次调用即可完成「后端 + 转发可靠性路径 + SELinux + NM」全套故障诊断（对齐商用安装器诊断报告做法）。
- **组件升级提示 advice（v0.9.3 新增，主动告知）**：`firewall.upgrade_advice` 为结构化提示对象，供 install.sh（安装期输出一次）与面板防火墙页（运行期 Banner）复用。基于探测结果对比「现状 vs 本设计期望」，**只告知不改变后端自动返回行为**（自动兼容返回沿用 §4.3/§6.4 既有设计）。字段与提示文案：
  - `firewalld_old`：`firewall.version` < 0.9（无 policy 能力）→ 提示「firewalld 版本过旧，端口转发/SPICE 公网可靠性受限（依赖 iptables 路径），建议升级至 0.9+」。与 #R `FIREWALLD_OLD_VERSION` 的 hint 一致。
  - `glibc_low_for_native`：`glibc` < `native-glibc.txt` 记录的需求版 → 提示「当前使用 compat 档，升级 glibc 后系统将满足 native 档启用条件」（仅提示，不自动切换，档位切换仍由 install.sh `select_binary_tier` 决定）。
  - `selinux_enforcing`：`selinux.mode` == enforcing → 提示「SELinux Enforcing 下 zone 文件已 restorecon 单文件处理（#S1），若读取被拒请核对 `etc_t` context」。
  - 提示优先级：**升级类提示（firewalld_old）> 优化类提示（glibc_low_for_native）> 配置类提示（selinux_enforcing）**，多命中时面板仅展示最高优先级一条，避免噪音。
- 目标：未来新增国产化项（SELinux 状态、AAVMF 探测等）只在此层登记一次。

### 4.2 防火墙后端抽象（核心，P1 解决）

**文件布局**（对齐 arch 包）：

```
server/service/firewall/
  backend.go          # Backend 接口 + BackendStatus + 注册表
  backend_detect.go   # DetectHostFirewallBackend()（RWMutex 双检缓存）+ resolveBackend()（#E 后端失效自动重测）
  backend_ufw.go      # ufw 后端（现 host.go 逻辑迁移）
  backend_firewalld.go# firewalld 后端（新增）
  host.go             # 仅保留编排逻辑，命令执行全走后端
  host_portfwd.go     # 改用 backend.Active()
  types.go            # HostFirewallStatus 增加 Backend 字段
```

```go
// backend.go
type Backend interface {
    Name() string                       // "ufw" | "firewalld" | "none"
    DisplayName() string                // "UFW" | "Firewalld" | "不可用"
    Available() bool                    // 命令/服务存在
    Active() (bool, error)              // 当前是否启用
    Defaults() (incoming, outgoing, routed string, err error)
    ListRules() ([]HostFirewallRule, error)
    EnsureRule(rule HostFirewallRule) error
    DeleteRule(rule HostFirewallRule) error
    Enable(progress func(int, string)) error
    Disable() error
}

type BackendStatus struct {
    Backend         string            `json:"backend"`
    BackendName     string            `json:"backend_name"`
    Available       bool              `json:"available"`
    Active          bool              `json:"active"`
    DefaultIncoming string            `json:"default_incoming"`
    DefaultOutgoing string            `json:"default_outgoing"`
    DefaultRouted   string            `json:"default_routed"`
    Rules           []HostFirewallRule `json:"rules"`
    IPBackend       string            `json:"ip_backend"`      // v0.8/#O：legacy | nf_tables | 空
    ErrorCode       string            `json:"error_code"`      // v0.8/#R：如 FIREWALLD_NOT_RUNNING
    LastError       string            `json:"last_error"`      // 兼容旧字段，格式 "message: hint"
}
```

**探测顺序**（backend_detect.go）：

```
0. 配置覆盖（v0.8，#M）：若配置（.env 的 FW_BACKEND=ufw|firewalld|none）显式指定，直接采用并跳过探测
   ——为无人值守/特殊部署提供强覆盖；配置值非法时告警并回退自动探测
1. ufw（Debian/Ubuntu）        → command -v ufw
2. firewalld（国产 RPM 默认）   → command -v firewall-cmd（**仅要求命令/服务存在，不要求当前 running**——是否运行由 `Active()` 单独判定；服务停止时仍探测为 firewalld 而非 none，`Enable()` 负责 `systemctl start`，见 #H/§6.4）
3. 均无 → noneBackend（Available=false，功能降级，与现 ufw_available=false 语义一致，不 panic）
```

**iptables/nftables 后端探测（v0.8 新增，#O，可靠性判据）**：探测防火墙后端时**同步探测 IP 防火墙后端**——执行 `iptables -V`，输出含 `(nf_tables)` 则为 nftables 后端、含 `(legacy)` 则为 iptables 后端。该结果经系统信息接口暴露为 `firewall.ip_backend`（§4.1 `/system-info` 扩展字段，v0.9.3 收敛），并作为 §5.1 决策 10 的落地判据：**legacy → 面板直写 `iptables -I FORWARD 1` 可靠（可作依赖）；nf_tables → 必须依赖 zone/policy 绑定（面板 iptables 仅兼容层）**。`BackendStatus` 同时暴露 `ip_backend` 字段，前端在「默认转发未管理」场景可据此给出准确提示。

注册机制与 arch 相同：`RegisterFirewallBackend(b Backend)` + `DetectHostFirewallBackend()` 在 `init()` 中注册三个实现，探测函数**以 RWMutex 双检锁定缓存**（语义等同 `sync.Once`，首次探测在写锁内执行一次，之后读路径 RLock 快速返回），提供 `ResetFirewallBackendCache()` 供手动刷新；编排层经 `resolveBackend()` 取后端，实现 #E 后端失效自动重测（§4.2 #E 规则 1）。

**命令执行加固（v0.8 新增，#N，可靠/安全）**：

- **命令路径固定**：后端对 `ufw` / `firewall-cmd` / `systemctl` / `iptables` / `nmcli` 在进程启动时用 `exec.LookPath` 解析一次并缓存绝对路径，后续一律调用缓存路径，防 PATH 劫持（与 AGENTS.md 安全基线一致）。
- **全部命令带超时**：`ExecCommand` 默认 30s 超时；`firewall-cmd --reload` / `systemctl start|stop firewalld` 用 `ExecCommandWithTimeout`（60s+），防 dbus/daemon 挂死导致面板阻塞。现有 ufw 后端 `--force enable/disable` 已用 2min 超时（host.go:114,128），迁移时保持。
- **禁止危险用法**：firewalld 后端不得使用 `--command=`/`--direct`（绕过 zone 模型且易注入）；rich-rule 参数经 `utils.ShellSingleQuote` 处理后作为**单个 argv 参数**传入（非 shell 拼接）。

**并发互斥（新增，运维补充项 #B，修正 v0.4 锁粒度）**：taskqueue 有 3 个 worker，`Enable/Disable` 走任务队列，而规则 CRUD 是即时操作，`ufw`/`firewall-cmd --reload` 可能并发执行互相踩踏。在 `backend_detect.go` 提供 **backend 级互斥锁**：

```go
var backendMu sync.Mutex

// 锁粒度 = 单条防火墙命令（一次子进程执行）。
// 严禁把锁包住「调用其他加锁公共方法」的编排流程，
// 否则 Enable→EnsureRule→backendExec 同一 goroutine 二次加锁 = 死锁。
func backendExec(fn func() error) error {
    backendMu.Lock()
    defer backendMu.Unlock()
    return fn()
}
```

**锁粒度与死锁规避（v0.4 修正，关键）**：

1. `backendExec` 只包裹**单条子进程命令**（一次 `ufw ...` / `firewall-cmd ...`）。读操作（`Active`/`ListRules`/`Defaults`）与写操作共用同一把锁，简单可靠（防火墙命令均亚秒级，不影响吞吐）。
2. **严禁**在 `backendExec` 的回调内调用其他经 `backendExec` 加锁的公共方法（如 `Enable()` 内循环 `EnsureRule()`）——Go 的 `sync.Mutex` 不可重入，必然死锁。这是 v0.3 原设计把锁「包裹整个 `Enable/Disable` 编排流程」的缺陷。
3. 因此 `Enable()`/`Disable()` 的**原子序列**（firewalld：渲染完整 XML → `--check-config` → `--reload`）必须在**一次**锁持有内完成（单次 `backendExec` 包裹整段），序列内部只做内存/文件操作，不调用任何加锁公共方法；规则 CRUD 各持锁一次，互不嵌套。
4. 审查点：M0 合入时用 `go vet` + 并发回归（§8）覆盖「Enable 并发规则 CRUD」用例，防止后续维护引入嵌套加锁。

**探测缓存失效策略（新增，运维补充项 #E）**：探测结果被 `RWMutex 双检` 缓存，但运行期运维可能手动安装 ufw 或启停 firewalld。规则：

1. **后端失效自动重测（v0.9.8 落地，v0.9.11 修正）**：编排层统一经 `resolveBackend()`（backend_detect.go）解析后端——当缓存后端命令已不可用（如运行期被卸载）时，自动 `ResetFirewallBackendCache()` 并重新探测一次，调用随即对重新探测的后端执行；仍不可用（如 `none`）才向调用方返回「后端不可用」明确报错。**仅当 `Available()=false`（命令缺失）触发**；firewalld 服务停止（命令仍存在）不算失效、保持原后端（#H）。**v0.9.11 审计修复（#A6）**：`none` 后端短路不触发自愈重置（重复 LookPath 无意义），运行期新装防火墙由漂移巡检（#X）/面板手动刷新兜底。接入点：`GetFirewallBackendStatus` / `ListHostFirewallRules` / `EnableHostFirewall` / `DisableHostFirewall` / `ensureHostFirewallRule` / `deleteHostFirewallRuleBySpec` / `ManageHostFirewallRule` / `IsHostFirewallActive` / `GetHostFirewallBackendName`。
2. 面板「防火墙」页提供手动刷新入口（复用现有刷新按钮），触发 `ResetFirewallBackendCache()` + 重新拉取 `GET /firewall/host/status`。
3. 服务重启时自然重新探测。

**统一规则表示**：后端接口直接使用现有 `HostFirewallRule`（Action/Protocol/PortStart/PortEnd/SourceCIDR/Comment），各后端负责 `EnsureRule/DeleteRule` 的差异，上层 `normalizeHostFirewallRuleRequests` / `mergeHostFirewallRules` / `hostFirewallRuleID` / 保护规则标记全部复用。

### 4.3 GLIBC 档位与二进制选优（P2 解决）

**设计原则（修正 v0.4，可靠性关键）**：**compat 默认基线保持不变**（amd64=2.2.5 / arm64=2.17），保证存量 Debian 系（Ubuntu 16.04=2.23、18.04=2.27、Debian 9=2.24）的默认兼容面**零变化**。2.28 作为**新增的高兼容档**加入，不替换默认档——国产系统命中高档，老 Debian 不受影响。

**GLIBC 目标表（自包含）**：

| 档位 | 目标架构 | Zig 目标 | 最高 GLIBC 依赖 | 何时使用 |
| --- | --- | --- | --- | --- |
| compat 默认档 | amd64 | `x86_64-linux-gnu.2.2.5` | 2.2.5 | 任何系统兜底；Ubuntu 16.04/18.04、Debian 9、CentOS 7 等 |
| compat 默认档 | arm64 | `aarch64-linux-gnu.2.17` | 2.17 | 同上（aarch64） |
| 高兼容档 | amd64/arm64 | `...-gnu.2.28` | 2.28 | 国产系统服务器版（麒麟 V10=2.28+、UOS=2.28、openEuler=2.28/2.34，均 ≥ 2.28 命中本档） |
| native | amd64/arm64 | 构建机工具链 | `native-glibc.txt` 记录 | glibc ≥ native 需求且（非 x86_64 或 AVX2） |

> **国产系统 glibc 事实（v0.7 补充，仅服务器版）**：麒麟服务器版 V10 基于 CentOS 系（dnf + firewalld），glibc 2.28+；UOS 1060 基于 Debian 10，glibc 2.28、apt + ufw；openEuler 20.03 = 2.28、22.03 = 2.34。**桌面版暂不分析**（如麒麟桌面版 V10 = Debian 11 系/apt+ufw，与 Ubuntu/Debian 走既有路径）。服务器版 glibc 均 ≥ 2.28，`--high-compat-glibc 2.28` 高兼容档可全量覆盖；openEuler 22.03（2.34）等满足 native 阈值时仍可优先 native。

兼容版构建必须用 Zig（`zig cc -target <三元组>`）锁定 GLIBC 上限，避免继承构建机符号版本；构建后 `readelf --version-info -W <bin> | grep -oE 'GLIBC_[0-9.]+' | sort -Vu` 校验实际最高依赖 ≤ 目标，超限即构建失败。

- build.sh：`--compat-glibc` 默认值**不变**（amd64=2.2.5 / arm64=2.17）；新增 `--high-compat-glibc 2.28`（可选），多构建一个高兼容档 `kvm-console-compat-2.28`。
- 发布包结构：

```
kvm-console                      # compat 默认档（2.2.5/2.17，兼容面不变）
kvm-console-compat-2.28          # 高兼容档（仅 --high-compat-glibc 时存在）
kvm-console-native               # 原生版（可选）
native-glibc.txt                 # build.sh 用 readelf 探测写入的 native 需求 glibc
```

> **评审结论（#2）修正**：`kvm-console-compat-2.17` 与 `kvm-console-compat-2.28` 均不进发布包；发布包只含默认 compat 档（兼容面最大）。需要 2.28 高档的发行方通过 `bash build.sh --high-compat-glibc 2.28` 构建，CentOS 7 等更老场景仍用默认档。这样**默认发布包对全部存量系统保持可用**，国产系统由发行方选择是否追加高档。

- install.sh 选优逻辑（交互式，见 §6.1；选择结果写入 `.env` 的 `KVM_BINARY_TIER` 持久化，供 update 复用）：

```
1. 检测 glibc_ver、AVX2（v0.9.11：AVX2 仅 x86_64 探测并上报；aarch64 恒不涉及）
2. 存在 .env 的 KVM_BINARY_TIER 且 glibc 未变化 → 直接复用上次选择（update 场景）
3. native 可用且 glibc_ver >= native-glibc.txt 中记录的需求版 且 (非 x86_64 或 AVX2)
     → 推荐 native，主程序 = kvm-console-native
4. 否则存在 kvm-console-compat-${HIGH_COMPAT_VER}（发布包内最高高兼容档，v0.9.11 动态发现）且 glibc_ver >= ${HIGH_COMPAT_VER}
     → 主程序 = kvm-console-compat-${HIGH_COMPAT_VER}（高兼容档）
5. 否则 → 主程序 = kvm-console（compat 默认档，兼容面最广）
```

> **高兼容档版本动态发现（v0.9.11 审计修复 #AP）**：`select_binary_tier` 不再硬编码 `compat-2.28`，改为扫描发行包 `kvm-console-compat-{VER}` 取最高版本（白名单 `^[0-9]+\.[0-9]+(\.[0-9]+)?$` + `sort -V` 比较）得 `HIGH_COMPAT_VER`，与 build.sh `--high-compat-glibc` 任意值对齐；无该档文件时 `HIGH_COMPAT_VER` 为空 → 规则 4 自然不命中，回落默认档。

> 步骤 5 兜底：默认 compat 档（2.2.5/2.17）可在任何现代 Linux 运行，**任何情况下都保留 kvm-console 为可运行兜底**，杜绝「选优后无可执行二进制」。

> **P0-3 glibc 2.17 真兼容实测（v0.10 竞品差异吸收，✅ M8.3 已实施 v0.12）**：Zig `x86_64-linux-gnu.2.2.5` 目标从符号表角度保证了 GLIBC 上限 ≤ 2.2.5，但「理论兼容 ≠ 实测通过」——部分 CGO 路径（`pthread_cond_clockwait` 等 glibc 2.30+ 符号）在 glibc 2.17（CentOS 7 基线）上可能出现 "Symbol not found" 崩溃。因此 **compat 档必须经过 glibc 2.17 实机验证**后才可宣称「兼容 glibc ≥ 2.17」：
> 1. `.github/workflows/build.yml` 新增 job `verify-centos7-glibc217`：`docker run centos:7` → 挂载 build 输出 compat 二进制 → 执行 `kvm-console --version` + `kvm-console --smoke-selfcheck`（打开一次 SQLite + 空连 libvirt，验证 CGO 符号可解析）。
> 2. install.sh `select_binary_smoke_test()`（L2209）在 `--version` 之外新增 `--smoke-selfcheck` 子命令（后端 main.go 实现：`db.AutoMigrate` 空结构体 + libvirt `Connect` 超时 2s），冒烟即验证真实运行面。
> 3. CI 通过后正式在 §9 兼容性矩阵声明「兼容 glibc ≥ 2.17」；若失败定位缺符号，手动在 CGO LDFLAGS 用 `-Wl,--wrap` 桥接。
> **验收**：§8 新增「centos7-glibc217 实测」用例（见 M8.3）。

> **选优切换时机（update 场景，与现 install.sh:2294 `install_files()` 行为一致）**：二进制切换发生在 `install_files()` 内、服务已停止之后。**切换前先对目标二进制做运行态验证**（`"$target_bin" --version` 冒烟测试），失败则保留原主程序并告警——避免 `mv` 后才发现新档不可运行、服务无法启动。update 时旧档保留为 `kvm-console-compat`，新档落位 `kvm-console`（现 install.sh 已有此惯例，设计沿用并增加冒烟测试）。

> **`.env` 值白名单校验（v0.8 新增，#M，防路径注入）**：读取/复用 `KVM_BINARY_TIER`（及新增的 `FW_BACKEND`）时，值必须命中白名单（`KVM_BINARY_TIER`：`compat`/`native`/`compat-${HIGH_COMPAT_VER}`（v0.9.11：不再固定 `compat-2.28`，按发行包动态档位校验）；`FW_BACKEND`：`ufw`/`firewalld`/`none`），否则**告警并回退默认档/自动探测**，杜绝 `.env` 被篡改后（如 `KVM_BINARY_TIER=../../..`）执行任意路径。主程序名拼接只允许 `kvm-console[-compat|-native|-compat-${HIGH_COMPAT_VER}]` 固定集合，禁止从配置派生任意文件名。**发行方以其他 VERSION 构建高档时**（§5.9 `--high-compat-glibc VERSION` 可配），白名单按实际发布包内含档位同步扩展（仍须与 `get_release` 后探测到的产物一致，不得从配置自由派生）。

> **运维补充项 #A（正确性修正，现有隐患）**：现 `install.sh:1662` 用**硬编码 `glibc >= 2.34`** 判断 native 可用性，这是错误假设——CI 在 `ubuntu-latest` 构建 native 版，其真实需求由构建机 glibc 决定。若国产系统 glibc 为 2.34~2.38（如 openEuler 22.03 = 2.34），会被硬编码逻辑错误切换 native，运行时因缺符号直接崩溃。
>
> **修正**：install.sh 必须读取 `native-glibc.txt`（build.sh 用 `readelf --version-info` 探测 native 实际最高 GLIBC 符号后写入）与该文件比较，**废弃 2.34 硬编码**。**同时 CI 构建镜像必须固定为 glibc 2.35 档（如 ubuntu-22.04）**，不得使用随动的 `ubuntu-latest`，保证 `native-glibc.txt` 稳定且既有 Ubuntu 22.04 / Debian 12 用户的 native 资格不变（见 §9 注 3）。本项为**优先实施项**（先于或并行于 M4）。
>
> **实施要点（v0.7 补充，v0.9.8 核实已实施）**：`build.yml` amd64 已固定 `runs-on: ubuntu-22.04`（build.yml:41，glibc 2.35，#A/M0.5 已落地）；arm64 保留 `runs-on: ubuntu-24.04-arm`（build.yml:125，glibc 2.39，GitHub 无 ubuntu-22.04-arm 公共 runner），build.yml:123 注释已记录「arm64 native 基于 glibc 2.39 构建」，aarch64 Ubuntu 22.04（2.35）用户据此回落 compat 是**预期**而非漂移。
> 注：`release` job（build.yml:209）用 `ubuntu-latest`，但该 job **不编译 Go 二进制**，仅下载 artifact + 创建 GitHub Release，无需固定 glibc。

### 4.4 国产系统交互式安装流程（P1/P2 落地载体）

install.sh `run_install_or_update` 中新增步骤：

```
check_kvm_hardware
check_and_install_deps
configure_qemu_for_rpm
configure_libvirt_nonroot
setup_selinux
ensure_kvm_runtime
setup_quota
configure_port
detect_firewall_backend        # 新增：探测 + 展示 + 确认（须在 check_and_install_deps 之后，firewalld 已装）
precheck_domestic               # 新增：端口占用/多防火墙/NM 环境预检（仅告警不弹交互）
get_release
select_binary_tier             # 新增：探测 + 推荐 + 确认（须在 get_release 之后，检查发布包内实际档位）
install_files
...
```

> **顺序依据（v0.4 补充，v0.9.11 更新）**：`select_binary_tier` 必须在 `get_release`（解包）之后执行——选优步骤 4 需确认发布包内是否存在高兼容档（v0.9.11 起按 `kvm-console-compat-{VER}` 动态发现 `HIGH_COMPAT_VER`），提前探测会误判。`detect_firewall_backend` 必须在 `check_and_install_deps` 之后——RPM 系首次安装时 firewalld 由依赖步骤装上，提前探测会误报 none。

---

## 5. 详细设计

### 5.1 后端：firewalld 后端（backend_firewalld.go）

**语义映射**：

| 语义 | firewalld 实现 | 备注 |
| --- | --- | --- |
| 状态 active | `firewall-cmd --state` == `running` | 服务未运行时 Active()=false，读操作返回空不报错（#H） |
| 默认入站 deny | 专用 zone `qvm-host` `--set-target=DROP`，**绑定到上行接口/源** | 写 `/etc/firewalld/zones/qvm-host.xml` 持久化 |
| 默认出站 allow | firewalld 默认 outgoing 放行 | 无需改动 |
| 默认转发 allow | **VM 桥接口（`br-ovs`、`vpcsw*`）绑定 `trusted`（ACCEPT）；firewalld ≥0.9 另建 policy 放行 uplink→VM 转发**；0.8.x 依赖面板 iptables FORWARD（现 port_forward.go 已做） | v0.5 修正，见决策 4/10 |
| 规则列表 | `firewall-cmd --zone=qvm-host --list-all` + `--list-rich-rules` | 解析为 HostFirewallRule |
| 放通端口 | `--permanent --zone=qvm-host --add-port=PORT/PROTO` | 端口范围 `PORT:PORT2` |
| 来源限定 | `--add-rich-rule='rule family=ipv4 source address=CIDR port port=PORT protocol=PROTO accept'` | |
| 备注/面板标记 | rich-rule `description='kvm-console:...'` | 复用 hostFirewallPanelPrefix 语义 |
| 持久化 | 一律 `--permanent` + `firewall-cmd --reload` | |
| 启停 | `systemctl enable/start/stop firewalld`（不 mask） | 服务停止时读操作空降级（#H） |

**关键决策**：
1. **专用 zone `qvm-host`**：不污染系统默认 `public` zone，卸载时只需 `--delete-zone=qvm-host` 回滚安全；`ManagedByPanel` 通过 description 前缀 `kvm-console:` 判定。
2. **默认策略持久化**：启动防火墙时生成 `/etc/firewalld/zones/qvm-host.xml` 并 `firewall-cmd --reload`，避免 `--runtime-to-permanent` 依赖版本。
3. **旧版 firewalld 兼容（v0.10 扩展三档分级，P0-2）**：
   - **版本探测口径**：`firewall-cmd --version`（install.sh `DETECTED_FW_VER` 与后端 `DetectUpgradeAdvice` 同口径）。
   - **≥ 0.9**：完整能力（policy + zone 原子操作），healthy。
   - **0.6.x ~ 0.8.x**（openEuler 20.03 = 0.8.x）：无 policy 能力，routed 默认策略字段返回「未管理」，前端展示为提示而非报错；VM 转发可靠性由「面板直写 iptables 在 iptables 后端 `-I FORWARD 1` 先于 firewalld 链求值」保证（决策 10）；component_health 报 warning（缺 policy）。
   - **< 0.6**（CentOS 7 = 0.4.4.4、麒麟 V10 0.6.x）：`Enable()` 直接返回 `FirewalldOldVersion` 错误（`error_code` + hint「面板不启用宿主机防火墙统一管理，请升级 firewalld ≥ 0.6 或使用发行版 iptables-service」），**不写 zone 文件、不进入绑定序列**；读操作（ListRules/Defaults）仍可用（返回空 + warning）。component_health 报 warning（不完整支持，非 critical）。
   - **阈值单一来源（#V）**：build.sh `COMPONENT_REQ_FIREWALLD` 由 `"0.8.0|0.9.0"` 下调为 **`"0.4.0|0.9.0"`**，`versions.conf` / `compat-manifest.json` / §5.11.2 表格同源，保证 CentOS 7 基线（0.4.4.4）不再误报 critical 阻断安装。
4. **与面板 iptables 共存（v0.5 重大修正）**：**firewalld 的 zone target 并不只作用于 INPUT**——man 页明确转发包按 **ingress zone** 的 target 决定放行/丢弃（若 ingress zone 为 DROP/REJECT 则转发包被拒）。因此必须**显式绑定**：
   - `qvm-host`（DROP）只绑定**上行接口/源**（宿主机入站防护），不得绑定 VM 桥接口。
   - **`br-ovs`、`vpcsw*` 等 VM 桥接口绑定 `trusted`（ACCEPT）**，VM 转发流量（VPC NAT、端口转发目标侧）与 dnsmasq 入站（UDP 67/53、TCP 53）天然放行，无需面板再写 INPUT 规则——同时解决 §8 中 dnsmasq 在 firewalld 下的放行缺口（#F）。
   - 面板既有 `iptables -I FORWARD` ACCEPT 规则保留作双保险；但**不得作为唯一依赖**（见决策 10 nftables 顺序问题）。
   - 上述绑定在 `qvm-host.xml` 及 `/etc/firewalld/zones/trusted.xml`（或专用 `qvm-vm` zone）持久化。
5. **启用原子性 + 预检（新增，运维补充项 #C）**：ufw 后端现状是逐条写规则、中途失败无回滚；firewalld 是全新后端，应利用 firewalld 的预检能力：
   - 先渲染完整 `qvm-host.xml`（含 trusted 绑定）→ `firewall-cmd --check-config`（或 `nft -c`）校验语法 → 通过后 `--reload` 一次性生效。
   - 任一规则写入失败即中止，不留下半成品 zone；失败时还原旧 zone 文件。
   - `Enable(progress)` 的进度文案与 ufw 后端共用一组。
   - **重复 Enable 幂等（新增）**：ufw 后端 `ensureHostFirewallRule` 已按 `hostFirewallRuleEquivalent`（host.go:425-430）对既有规则去重，重复点「启用」不产生重复规则；firewalld 后端每次 Enable 整段重渲染 `qvm-host.xml` 原子替换，天然幂等。auto-confirm / update 模式下重放同一批规则均不重复落盘。
6. **Docker 兼容需重验（新增，运维补充项 #D，v0.5 修正结论）**：`host.go:43-44` 现硬编码 `DockerCompatible=true`（文案「不写入 Docker 链，Docker bridge 模式不受面板防火墙约束」）。**修正**：`docker -p` 发布端口经 **PREROUTING DNAT → FORWARD → DOCKER 链** 到达容器，**不经 INPUT**；在 iptables 后端下 qvm-host zone DROP 与 `docker0` 通常不冲突（Docker 规则先于 firewalld 链），但在 **nftables 后端下 firewalld `--reload` 后链注册顺序不保证**，存在 DROP 先于 DOCKER 链求值的风险。真正需要重验的是：
   - firewalld 自身的 **FORWARD 策略**（若系统 firewalld 策略默认丢转发，需放行 docker0/bridge 转发）；以及 firewalld `--reload` 是否波及 Docker 自建的 DOCKER/DOCKER-USER 链。
   - **`docker0` 接口绑定**：推荐将 `docker0` 加入 `trusted`（ACCEPT）或 policy egress，保证 Docker 转发不受 qvm-host DROP 影响（与决策 4 同一机制）。
   - 后端不得对 firewalld 无条件返回 `DockerCompatible=true`，需在 `BackendStatus` 增加 `docker_compatible` 探测（验证 `docker -p` 发布端口在 qvm-host zone DROP 下可达；不可达时提供 zone/policy 放行方案）。
   - 验证用例列入 §8 验收矩阵（firewalld × Docker 共存）。
7. **SELinux context（新增，运维补充项 #S1）**：面板以 root 写 `/etc/firewalld/zones/qvm-host.xml`，麒麟/openEuler Enforcing 下文件需 `etc_t` context。`setup_selinux` 需扩展：写入后对**单个文件**执行 `restorecon /etc/firewalld/zones/qvm-host.xml`（v0.4 修正：避免 `restorecon -R /etc/firewalld` 无谓改动系统自带 zone 文件的 context，遵循「只动自己写的东西」原则；必要时 `chcon -t etc_t` 兜底），避免服务读取被拒。
8. **rich-rule 注入防护（新增，运维补充项 #S3）**：`description`/`source`/`port` 来自用户输入，构造 `firewall-cmd --add-rich-rule` 命令时沿用 `utils.ShellSingleQuote` 式转义；ufw 后端 `buildUFWRuleArgs` 的 comment 参数存在同样风险，一并修复。
9. **policy 放行 uplink→VM 转发（新增，运维补充项 #F，firewalld ≥0.9）**：端口转发/SPICE 公网暴露的入站流量先经上行接口（ingress=qvm-host DROP）再转发到 VM（egress=trusted）。仅靠 trusted 绑定只放行了 VM 出站与 bridge 内转发；**入站→VM 的转发路径仍受 ingress zone（qvm-host DROP）拦截**。因此 firewalld ≥0.9 需建 policy：
   - `firewall-cmd --permanent --new-policy qvm-host-forward --add-ingress-zone qvm-host --add-egress-zone trusted --set-target ACCEPT`
   - 0.8.x（无 policy）依赖面板直写 iptables FORWARD ACCEPT（iptables 后端下 `-I FORWARD 1` 先于 firewalld 链，可靠），与决策 3 口径一致。
10. **zone 绑定是生效前提（v0.5 新增，可靠性关键）**：firewalld zone 未绑定接口/源/默认 zone 时**完全不生效**（DROP 惰性，安全假象）。因此：
    - `Enable()` 必须完成「建 zone → 写规则 → 绑定接口/源 → reload」全序列，任一环节失败整体回滚（#C）。
    - 上行接口探测复用 install.sh `detect_default_uplink`（install.sh:1360）口径，后端用 `ip route show default`；多网卡时绑定全部非 VM 桥物理接口。
    - **NetworkManager 交互（v0.6 补充，#J）**：firewalld `--add-interface` 绑定在 NM 管理的连接上可能被 NM 自带 zone 覆盖（`connection.zone`）。绑定物理接口时同步执行 `nmcli connection modify <conn> connection.zone qvm-host`（存在 NM 且接口归属 NM 时；无 NM 跳过），并列入 §8 验收「NM 重启后 zone 绑定保持」。
    - **nftables 后端顺序风险**：面板直写 iptables（`-I FORWARD 1`）在 nftables 后端（firewalld 0.9+/1.x，nftables 后端）不保证先于 firewalld 链求值。因此 VM 转发可靠性**必须主要依赖 zone/policy 绑定**，面板 iptables 规则仅作兼容层；此约束写入 §8 验收（nftables 后端下 VPC/端口转发/dnsmasq 全链路验证）与 §10 风险表。**落地判据（v0.8，#O）：以 `iptables -V` 探测结果为准——legacy 才可依赖面板 iptables，nf_tables 一律依赖 zone/policy。**
11. **zone 文件原子写入（v0.8 新增，#P，崩溃安全）**：`/etc/firewalld/zones/qvm-host.xml` 一律「临时文件写入（`*.xml.tmp`）→ `sync` → `mv`/`rename` 原子替换」，禁止直接覆盖，避免写一半崩溃留下损坏 XML 导致 firewalld 拒绝启动。文件权限 `0644 root:root`（与系统自带 zone 一致）。回滚规则：首次（无旧文件）失败=删除 tmp；已有旧文件先备份 `*.bak` 再替换，回滚=`mv .bak` 还原。**SELinux restorecon 时序（v0.8，#P）：在 rename 完成后的最终文件上执行**（决策 7 的 `restorecon /etc/firewalld/zones/qvm-host.xml` 保持单文件，但放在替换后）。
12. **Enable 后自检（v0.8 新增，#L，报错反馈/可靠性）**：firewalld `Enable()` 完成 reload 后自动执行**自检清单**：① `firewall-cmd --state` == running；② `--zone=qvm-host --list-all` 确认 target=DROP 且已绑定上行接口/源；③ `br-ovs`/`vpcsw*` 确在 trusted 或专用 VM zone；④ 本机探测面板端口/SSH 端口（`ss -lnt`）仍在放行（保护规则未丢）；⑤ dnsmasq UDP 67/53 端口在 VM 桥监听且 zone 放行。任一失败 → `Enable` 返回失败 + 失败项清单，任务进度条推送「自检失败: <项>」，前端展示失败项并可一键回滚（见 §5.7）。自检项全部通过才报成功（100%）。
13. **错误结构化（v0.8 新增，#R，报错反馈）**：后端错误统一为 `error_code` + 可操作 `hint`，示例：`FIREWALLD_NOT_RUNNING`（hint：`systemctl start firewalld`）、`FIREWALLD_OLD_VERSION`（hint：升级 firewalld 或依赖 iptables 路径）、`ZONE_NOT_BOUND`（hint：重跑 Enable 绑定序列）、`DBUS_ERROR`（hint：重启 firewalld 服务）、`PERMISSION_DENIED`（hint：确认 root 与 SELinux 放行）。`BackendStatus.LastError` 保持现有字符串字段兼容（填充 `message: hint`），新增 `error_code` 字段供前端按码分支展示。

**firewalld 规则 → HostFirewallRule 解析**（backend_firewalld.go）：

```
# firewall-cmd --zone=qvm-host --list-all 输出示例
ports: 5900:5999/tcp
rich rules:
        rule family="ipv4" source address="192.168.1.0/24" port port="8080" protocol="tcp" accept description='kvm-console:web'
→ HostFirewallRule{Action:allow, Protocol:tcp, PortStart:5900, PortEnd:5999, ManagedByPanel:true}
```

### 5.2 后端：ufw 后端（backend_ufw.go）

- 将 host.go 现有 `ufw ...` 命令与 `parseUFWAddedRules` / `parseUFWDefaults` / `buildUFWRuleArgs` 原样迁入，行为零变化。
- `Available()` = `command -v ufw` 成功。
- **行为差异（M4，评审补充）**：ufw 后端 `Enable()` 设 `default allow routed`，即启用「宿主机防火墙」后 VM↔VM、VM↔主机及宿主机转发流量仍全开放；firewalld 后端则是 `qvm-host` zone DROP + VM 桥绑 `trusted` 的收敛策略。两后端转发语义不同属既有行为（ufw 为迁移保持），不在本改造收敛；**运维侧需知晓：Debian/UOS 上启用宿主机防火墙不隔离 VM 间流量，需转发隔离时另行配置**。

### 5.3 后端：host.go / host_portfwd.go 改造

- `GetHostFirewallStatus()`（host.go:18）改为：
  ```go
  backend := DetectHostFirewallBackend()
  status := &HostFirewallStatus{ Backend: backend.Name(), ... }
  status.Active, _ = backend.Active()
  status.DefaultIncoming, status.DefaultOutgoing, status.DefaultRouted, _ = backend.Defaults()
  status.Rules, _ = backend.ListRules()
  status.UFWAvailable = backend.Available()  // 兼容前端旧字段
  ```
- `EnableHostFirewall` / `DisableHostFirewall`（host.go:81,124）改为调用 `backend.Enable(progress)` / `backend.Disable()`。
- `IsHostFirewallActive()`（host_portfwd.go:11）改用 `backend.Active()`。
- 新增 `GetHostFirewallBackendName() string` delegate 供 handler/API 使用。

### 5.4 后端：network/helpers.go 改造

```go
func GetUFWStatus() (string, error) {
    b := fwpkg.DetectHostFirewallBackend()
    if b.Name() == "none" { return "", errors.New("宿主机防火墙后端不可用") }
    // 返回该后端的状态文本（ufw 原样；firewalld 返回 list-all 文本）
}
func ManageUFWRule(action, rule string) error { /* 委托 fwpkg 的 ManageHostFirewallRule */ }
```

为避免 `network` 包与 `firewall` 包产生新依赖（network 已通过 hook 反向解耦），在 `firewall` 包新增 `ManageHostFirewallRule(action, rule string) error`，`network/helpers.go` 通过新增 `HookManageHostFirewallRule` 调用（v0.6 统一，见 §2.2）。

**命令注入防护（v0.6 修正，#S3 扩展）**：`network/helpers.go:21-39` 现状 `fmt.Sprintf("ufw allow %s", rule)` 走 `ExecShell`，且 `handler/network.go:666` 接受任意字符串 rule——这是现存真实注入面（上游代码就存在，非本设计引入，但本设计改造时一并修复）。修正：

- 新后端 `ManageHostFirewallRule` 内部**不得使用 shell 字符串拼接**；按后端分发：ufw → `ExecCommand("ufw", args...)` 结构化 argv；firewalld → 解析 rule（proto/port 二元组）构造 `--add-port`/rich-rule 结构化命令。
- 迁移期对 `rule` 输入做白名单校验：`^[0-9a-zA-Z:/,. -]+$`，不合规直接拒绝（防 `;`、`|`、`$()`、反引号等），并在 §8 补对应注入用例（#S3 验收扩展：`/network/ufw/rule` 传恶意字符串必须被拒绝）。
- spice 侧 `HookManageUFWRule("allow", port+"/tcp")` 传的是面板内部构造的 port/proto，天然合规；校验主要拦截 API 直接调用。

### 5.5 后端：types.go 字段扩展

```go
type HostFirewallStatus struct {
    // ...现有字段...
    Backend         string `json:"backend"`         // ufw/firewalld/none
    BackendName     string `json:"backend_name"`    // UFW/Firewalld/不可用
    UFWAvailable    bool   `json:"ufw_available"`   // 保留，语义=后端可用
}
```

### 5.6 API 契约

| 方法 | 路径 | 变更 |
| --- | --- | --- |
| GET | `/firewall/host/status` | 响应新增 `backend` / `backend_name` 字段 |
| POST | `/firewall/host/reset-backend` | **M3 新增（#R）**：清后端探测缓存 + 立即重拉 status 返回（前端「重新检测」按钮） |
| GET | `/system-info` | **v0.9.3 收敛**：复用现有接口（router.go:535），响应增量扩展 `glibc` / `cpu.avx2` / `selinux.mode` / `firewall.*`（含 `upgrade_advice`）字段 |
| GET | `/network/ufw/status` | 行为兼容（后端不可用时返回明确错误文案） |
| POST | `/network/ufw/rule` | 行为兼容，内部走新后端 |

router.go 行尾补中文注释（AGENTS.md 约定）；`endpointDescriptions.ts` 补 `/system-info` 扩展字段文案；`generate-api-endpoints.mjs` 自动刷新 endpoints.json。

### 5.7 前端设计

**HostFirewallTab.tsx**（web/src/views/firewall/components/HostFirewallTab.tsx:206-210）：

- 「UFW」标签 → 「防火墙后端」，值显示 `backend_name`，颜色语义沿用（可用绿/不可用红）。
- 当 `backend === 'none'` 时，banner 下方追加一行 `Banner`（Semi 的 `Banner type="warning"`）提示：当前系统无 ufw/firewalld，宿主机防火墙不可用，端口转发仍会写入 iptables。
- 「端口转发仍会写入 UFW 持久放通规则」→ 中性表述「端口转发仍会写入防火墙持久放通规则」。
- banner 默认策略字段对 firewalld 同样适用（`default_routed` 为空时显示「未管理」Tag 而非 `-`）。
- **错误 hint 展示（v0.8 新增，#R）**：当 `error_code` 非空时，运行状态卡显示可操作提示（如「firewalld 服务未运行」+ `systemctl start firewalld`），并提供「重新检测」按钮（触发 `ResetFirewallBackendCache` + 刷新，复用页头刷新）。
- **Enable 自检结果（v0.8 新增，#L）**：启用任务完成后若 `LastError` 含自检失败项，在状态横幅下方展示失败项清单（Tag 红色 + Tooltip 原因）并提供「回滚」操作入口（走 `POST /firewall/host/disable`，二次确认）。
- **`ip_backend` 展示（v0.8 新增，#O）**：当 `default_routed` 为「未管理」时，Tooltip 依据 `ip_backend` 区分文案——legacy：「依赖面板 iptables FORWARD（可靠）」；nf_tables：「依赖 zone/policy 绑定，勿依赖面板 iptables 顺序」。
- **组件升级提示 advice（v0.9.3 新增）**：HostFirewallTab 读取 `/system-info` 响应的 `firewall.upgrade_advice`，按优先级展示**至多一条** Banner（`type="warning"`）：`firewalld_old` →「firewalld 版本过旧，端口转发/SPICE 公网可靠性受限，建议升级至 0.9+」；`glibc_low_for_native` →「当前使用 compat 档，升级 glibc 后系统将满足 native 档启用条件」；`selinux_enforcing` →「SELinux Enforcing 下 zone 文件已 restorecon 处理」。Banner 可关闭，不阻断操作。
- **文档同步（v0.8，#S）**：本设计实施后，`docs/firewall-page.md` 的「宿主机防火墙（UFW）」章节需同步措辞——「UFW 可用性」→「防火墙后端可用性（backend_name）」、增加 none Banner、「默认转发未管理」Tag 说明；不新增独立页面（§2.6 约定）。

**web/src/api/firewall.ts**：`HostFirewallStatus` 增加 `backend?: string` / `backend_name?: string` / `ip_backend?: string` / `error_code?: string`。

**遵循 Semi 约定**：不新增按钮中文文案；无新弹窗；无新增状态文字。

**M3 实施修订（v0.9.7 落地记录，与设计原文的差异）**：设计已按上述实施，个别路径与设计原文略有出入，记录如下以便评审——
1. **「重新检测」按钮数据流**：设计原文「触发 `ResetFirewallBackendCache` + 刷新，复用页头刷新」；实施新增 `POST /firewall/host/reset-backend` 端点（router.go:366），服务端清缓存后**立即重拉** `GetHostFirewallStatus()` 直接返回，前端 `handleResetBackend` 用返回结果替换 `hostStatus`（不必再等轮询），更符合「点一下立刻出结果」预期。
2. **Enable 自检失败项数据源**：设计原文「启用任务完成后若 `LastError` 含自检失败项」；实施发现任务失败时 `LastError` 不落在 `HostFirewallStatus` 上，改为**订阅 taskqueue 的 `enable_host_firewall` 任务终态**，从任务 `message`（`任务失败: 启用后自检失败: <项>; <项>`）用正则解析失败项清单（web/src/views/firewall/index.tsx）。
3. **「回滚」入口文案**：自检失败时按钮文案为「回滚（关闭防火墙）」并带二次确认，比设计原文的「回滚」更明确操作后果。
4. **advice Banner 可关闭**：除设计原文的「Banner 可关闭」外，实施在**关闭后本次会话不再弹出**（`adviceDismissed` state，依赖组件生命周期保留缩小离场动画，遵循 AGENTS.md Modal 约定）。
5. **none Banner 措辞**：设计原文「端口转发仍会写入 iptables」；实施为避免误导（后端抽象后实际写 ufw/firewalld 持久规则）改为「端口转发仍会写入防火墙持久放通规则」，仅后端为 none 时才提示「无 ufw/firewalld，宿主机防火墙不可用」。
6. **`error_code` 数据源**：设计原文未说明 error_code 从何而来；实施在 `GetFirewallBackendStatus` 捕获 `backend.Active()`/`Defaults()` 的结构化错误，经 `errorCodeOf()`（`errors.As` 提取 `FirewallError.Code`）填充，使 `FIREWALLD_NOT_RUNNING` 等错误码真正上抛（此前恒空）。
7. **死码错误码产生点（v0.9.7 代码修订）**：原仅 `FIREWALLD_NOT_RUNNING` 有产生点，`DBUS_ERROR` / `PERMISSION_DENIED` / `FIREWALLD_OLD_VERSION` / `ZONE_NOT_BOUND` 全仓库无引用。现补齐——`classifyFirewalldExecError`（backend_firewalld.go）将 `firewall-cmd` 挂死超时映射为 `DBUS_ERROR`（hint `systemctl restart firewalld`）、stderr 权限不足映射为 `PERMISSION_DENIED`；`firewalldEnsureForwardPolicy` 对 <0.9 版本返回 `FIREWALLD_OLD_VERSION`；`firewalldBindTrustedInterfaces` 绑定失败返回 `ZONE_NOT_BOUND`。前述错误经 `Active()`/`Defaults()` 上抛后可在状态卡 hint 区渲染（#N/#O 验收可执行）。
8. **`last_error` 字段**：`HostFirewallStatus`/`BackendStatus` 均含 `last_error`，后端在规则读取失败时填充；前端目前**不渲染该字段**（仅 `error_code` 驱动 hint），属兼容保留字段。
9. **advice 读取与缓存**：前端在**页面挂载时**拉取一次 `/system-info` 的 `firewall.upgrade_advice`（不在 status 轮询周期内）；后端 `DetectUpgradeAdvice` 每次探测涉及版本/glibc/selinux 多个子进程，**v0.9.7 起带 TTL 缓存（10 分钟，RWMutex 双检）**，避免 `/system-info` 高频触发重复探测。

### 5.8 install.sh 设计

**新增 `detect_firewall_backend()`**（放在 `check_and_install_deps` 之后、`get_release` 之前，见 §4.4 顺序依据）：

```bash
detect_firewall_backend() {
    local backend="none"
    # 非交互 / 环境变量覆盖（#M）：前端已导出 FW_BACKEND 时跳过探测
    if [ -n "${FW_BACKEND:-}" ]; then
        case "$FW_BACKEND" in
            ufw|firewalld|none) backend="$FW_BACKEND" ;;
            *) warn "FW_BACKEND 非法值，回退自动探测" ;;
        esac
    elif [ "$MODE" = "update" ] && [ -f "$ENV_FILE" ]; then
        # v0.9.11（M2）：update 模式复用 .env 已持久化的后端，避免自动探测静默切后端
        local persisted
        persisted=$(grep -E '^FW_BACKEND=' "$ENV_FILE" 2>/dev/null | tail -n1 | cut -d= -f2- | tr -d '"' | tr -d '[:space:]')
        case "$persisted" in
            ufw|firewalld|none) backend="$persisted" ;;
            *) : ;;
        esac
    fi
    if [ "$backend" = "none" ]; then
        if command -v ufw >/dev/null 2>&1; then backend="ufw"
        elif command -v firewall-cmd >/dev/null 2>&1; then backend="firewalld"
        fi
    fi
    FW_BACKEND="$backend"
    if [ "$backend" = "none" ]; then
        warn "未检测到 ufw / firewalld，宿主机防火墙功能将不可用（端口转发仍使用 iptables）"
        return 0
    fi
    info "检测到宿主机防火墙后端: ${backend}"
    # firewalld 版本探测（#Q：安装期 advice 判断 <0.9 缺 policy）
    if [ "$backend" = "firewalld" ] && command -v firewall-cmd >/dev/null 2>&1; then
        DETECTED_FW_VER=$(firewall-cmd --version 2>/dev/null | head -n1 | tr -d '[:space:]' || true)
    fi
    # RPM 系 firewalld 未运行则询问是否启动（仅 install 模式；update 模式只展示不询问）
    if [ "$MODE" = "install" ] && [ "$backend" = "firewalld" ] && ! systemctl is-active --quiet firewalld 2>/dev/null; then
        read_tty -rp "检测到 firewalld 未运行，是否立即启动并设为开机自启? [y/N]: " ans
        if [[ "${ans:-N}" =~ ^[Yy]$ ]]; then
            systemctl enable --now firewalld
        fi
    fi
}
```

**新增 `select_binary_tier()`**（放在 `get_release` 之后、`install_files` 之前，见 §4.4 顺序依据——需先确认发布包内含哪些档位）：探测后展示推荐档位与理由，`read -rp` 允许手动覆盖；选择结果写入 `KVM_BINARY_TIER` 变量供 `install_files()` 使用，并持久化到 `.env`（`env_set KVM_BINARY_TIER`，update 模式复用）。默认档（2.2.5/2.17）在任何系统均可运行，故**不存在「无可用档」兜底失败**；若发布包含 2.28 高档但系统 glibc < 2.28，仅提示将使用默认档，无需指导自构建。

**非交互 / 环境变量覆盖（v0.8 新增，#M，灵活性）**：`detect_firewall_backend` 与 `select_binary_tier` 支持无人值守注入，与 `configure_port`（install.sh:1109）对 `KVM_PORT` 的交互方式对齐：

```bash
# 前端已导出 FW_BACKEND=firewalld / KVM_BINARY_TIER=native 时，跳过探测与 read
if [ -n "$FW_BACKEND" ]; then
    case "$FW_BACKEND" in ufw|firewalld|none) backend="$FW_BACKEND" ;;
        *) warn "FW_BACKEND 非法值: $FW_BACKEND，回退自动探测" ;;
    esac
fi
# 非 TTY 或 CI=1 时跳过 read，直接采用推荐值并记录（v0.13：TTY 下改用 3 秒读秒倒计时，超时自动默认）
if [ ! -t 0 ] || [ "${CI:-}" = "1" ]; then
    info "非交互模式，自动采用推荐档位: $recommended"
    KVM_BINARY_TIER="$recommended"
else
    # countdown_read_line（QVM_READ_TIMEOUT=3）：3 秒读秒，超时无输入自动采用默认档
    read_tty -rp "选择二进制档位 [默认 $recommended]: " ans
    ans=${ans:-$recommended}
fi
```

- 覆盖值白名单校验见 §4.3（防路径注入），具体白名单：`FW_BACKEND ∈ {ufw, firewalld, none}`、`KVM_BINARY_TIER ∈ {compat, native, compat-${HIGH_COMPAT_VER}}`（v0.9.11 动态档位；install.sh:1244-1256 `write_env` 白名单校验 + 1736-1763 `detect_firewall_backend` 覆盖值校验 + 2149-2166 `select_binary_tier` 持久化复用校验，非法值告警回退自动探测/选优）；`FW_BACKEND` 与 `KVM_BINARY_TIER` 均持久化到 `.env`，后端 `DetectHostFirewallBackend` 读取覆盖（§4.2 探测顺序步骤 0）。
- **敏感文件/目录权限加固（v0.10 新增，P1-4，✅ v0.11 已实施 M8.4）**：`write_env` 末尾对 `.env` 保持 `chmod 600`（install.sh:1229 已实施）；`ensure_directories` 对 `/etc/kvm-console/**/*.json`、`/etc/kvm-console/firewall/rules|policies` 等 `chmod 600`，对 `/etc/kvm-console`、`/etc/kvm-portforward`、`/etc/libvirt/vm-access` 等目录 `chmod 700`，避免含密钥/转发映射的敏感配置权限过宽。

**安装日志与失败定位（v0.8 新增，#K，报错反馈）**：install.sh 全局日志双写 + 失败定位，对齐商用安装器「分阶段日志 + 错误码 + 失败定位」：

- 新增 `LOG_FILE="$INSTALL_DIR/logs/install-$(date +%Y%m%d-%H%M%S).log"`（`ensure_directories` 确保目录），`info/warn/error/success` 全部 `tee -a "$LOG_FILE"` 双写；保留最近 5 份滚动。
- 新增 `step()` 包装器：`step "检测防火墙后端" detect_firewall_backend` 打印 `[STEP n/N]`；被包装函数非零退出时输出 `[ERROR] 失败步骤: <名称>，原因: <输出尾部>，完整日志: $LOG_FILE`，并给出排查命令（如 `journalctl -u firewalld`、`systemctl status firewalld`）后退出。**v0.13 扩展（#AR）**：`step()` 每步自动计时（`date +%s.%N`，不支持 %N 的环境退回整秒），耗时写入 `[TIMING]` 日志并在流程结尾由 `print_step_timing_summary` 按耗时倒序汇总，同时输出本次安装/更新流程总耗时——与 M8.12 镜像源优化配合定位「安装卡在哪个环节」（现场定位安装慢的首要抓手）。
- 新增 `print_install_report`（安装末尾、`show_info` 后调用）：总结「防火墙后端 / 二进制档位与选择原因 / glibc / CPU 指令集 / SELinux 状态 / 降级项清单 / 日志路径」，一次览全。**v0.9.3 追加组件升级提示**：依 `/system-info` 同一口径输出 `upgrade_advice` 命中项（firewalld <0.9 建议升级 / glibc 未达 native 提示当前 compat 档 / SELinux Enforcing 提示 restorecon 已处理），与运行期面板 Banner 文案一致。**v0.9.8 补齐实现细节**：`DETECTED_FW_VER` 由 `detect_firewall_backend` 探测（`firewall-cmd --version`，firewalld 后端时），glibc 建议复用 `NATIVE_GLIBC_REQUIRED` 全局（`select_binary_tier` 提前读取 native-glibc.txt，update 模式同样可得）；`DEGRADED_NOTES` 降级项清单由 `detect_firewall_backend`（后端为 none）与 `install_files`（native/compat-2.28 档冒烟测试回退）累积写入。

**预检（v0.8 新增，#L，完整性；v0.10 扩展 P0-1/P2-8/P3-12）**：`detect_firewall_backend` 之后、进入安装前执行 `precheck_domestic`：

- **端口占用**：`ss -ltn` 检查 `KVM_PORT` 是否已被占用；被占用则明确列出占用进程并提示更换端口（退出或让用户重输）。
- **多防火墙共存**：后端为 firewalld 时，检查 `iptables -L`（legacy）或 `nft list ruleset`（nf_tables）是否存在**非 firewalld 管理**的规则；存在则提示「检测到既有防火墙规则，面板仅管理自建 zone，不会清理第三方规则」。
- **NM 环境**：记录物理接口归属 NM 与否（供 #J 绑定决策）；无 NM 时日志注明「无 NetworkManager，跳过 connection.zone 同步」。
- **CPU 厂商探测（v0.10 新增，P0-1，✅ v0.11 已实施 M8.1）**：读 `/proc/cpuinfo` 厂商字段判定 `Intel | AMD | HygonGenuine | Phytium | Zhaoxin | Kunpeng | Unknown`（`server/service/arch/domestic_cpu.go` `DetectCPUVendor()`），写入 `.env DOMESTIC_CPU_VENDOR`；海光 7000/5000 追加提示「如遇嵌套页表异常可加 `kvm_amd.npt=0`」，飞腾/鲲鹏 ARM64 检查 kvm 模块加载顺序（kvm → kvm_arm → hyp/vhe）并提示。
- **麒麟 KYSEC 探测（v0.13 新增，Kylin 专项）**：KYSEC（麒麟内核安全机制）为麒麟 V10+ 内生安全框架，其强制访问控制/可信度量链可能限制内核模块加载（`modprobe kvm`）与 `/dev/kvm` 访问导致 KVM 无法启用或虚拟机启动异常。**install.sh** `check_kysec()`（`ensure_kvm_runtime` STEP 内）防御性多重回退探测：`kysec_ctl` 命令 → `/sys/kernel/security/kysec` → `/proc/kysec` → `/etc/kysec`，命中任一 → `info` 提示放行建议（`kysec_ctl` 放行 qemu/libvirt 策略）并写入 `KYSEC_STATE` 供 `print_install_report` 复述；非麒麟 `not-detected` 不输出。**面板侧** `server/service/arch/kysec.go` `DetectKYSEC()`（同口径探测点）接入：component_health 新增 `kysec` 条目（仅麒麟命中时上报，`diag` + `warning`「请用 kysec_ctl 放行 qemu/libvirt 相关策略」）+ `/system-info` admin `cpu.kysec` 字段（`enabled`/空，非麒麟不展示）。
- **kdump 建议（v0.10 新增，P2-8，✅ v0.11 已实施 M8.8）**：`systemd-detect-virt` 判定裸金属且 `/proc/cmdline` 无 crashkernel 参数时，向 `print_install_report` 追加 warn「建议启用 kdump（裸金属 crashkernel=2048M,high / 虚拟化 512M）」。
- **国内镜像源测速（v0.10 新增，P3-12，✅ v0.11 已实施 M8.12；v0.13 补 nju 与探测顺序；v0.13 补麒麟排除）**：openEuler 优先推荐南京大学源 `mirrors.nju.edu.cn`（linuxmirrors.cn 高优先级教育网镜像，可达即直接选用，避免阿里云偶发限流/404），不可达时对清华/阿里做 `curl` 计时取最快，写入 `.env DEPS_MIRROR=tsinghua|aliyun|163|nju|system|offline`；`DEPS_MIRROR=offline` 时 `check_and_install_deps` 跳过 `apt/dnf install`，仅 `command -v` 扫缺包并汇总到 `print_install_report`（专网环境提示从内网源手动安装）。**v0.13（#AS）顺序修正**：openEuler 关键包可用性探测（`dnf makecache` + `list available`）不再于 `check_os` 的 `enable_openeuler_repos` 阶段对官方慢源执行，改为 `probe_critical_rpm_packages()` 在 `apply_system_mirror` 之后调用（此时已切快源且 metalink 已注释）。**v0.13（麒麟，Kylin 专项）**：麒麟服务器版（`OS_ID=kylin|neokylin`）基于 CentOS 8 系但使用自有 `archive.kylinos.cn` 官方源、**无公开国内镜像**——`test_mirror_speed` 对 centos/centos-vault 测速无意义且 `apply_rpm_mirror` 写入 `centos-vault` 源会拉取 CentOS 包污染麒麟，故 `test_mirror_speed`/`apply_rpm_mirror` 对麒麟直接走 system（官方源，不写 kvm-console 镜像文件），依赖安装靠 dnf `--setopt=timeout/minrate/retries` 兜底。

**安装状态持久化（v0.10 新增，P1-4，✅ v0.11 已实施 M8.4）**：新增 `${INSTALL_DIR}/.install_state/` 目录，写入 `stage=<STEP_NUM>` / `last_error=` / `degraded_notes=` / `binary_tier=` / `component_summary=` / `release_sha256=`；install.sh 新增 `--resume` 参数（读 stage 从失败步骤继续，对齐 HCI vmp.pkg 的 `ing`/`outcfg`/`die_file` 阶段状态设计）。

**交互读取倒计时（v0.13 新增，#AR）**：`read_user_input`（`choose_mode` 等菜单）与 `read_tty`（路径/端口/确认等）统一走 `countdown_read_line()`（`QVM_READ_TIMEOUT=3`）：`printf '\r\033[K%s（%2ds 后无输入将自动默认）'` 每秒刷新读秒数，输入写入全局 `QVM_READ_INPUT` 返回 0，超时返回 1（调用方 `warn "等待超时（3s）"` 后落默认值）。`/dev/tty` 存在且可写时从 `/dev/tty` 隔离读取（防 stdout 回灌污染，见 #M 背景）；非 TTY/CI=1 直接取默认不倒计时。**v0.13 核对补丁**：`read_tty` 补上与 `read_user_input` 一致的非交互守卫（`CI=1` 或 `! -t 0` 立即返回、由调用方 `${var:-默认}` 落默认值）——此前 read_tty 漏掉该守卫，网页终端/管道场景每个提示硬等 5 秒（实测 5 次「等待超时」浪费 25s），`QVM_READ_TIMEOUT` 同步 5→3s。**set -e 静默退出脚枪（v0.13，#AQ）**：install.sh:6 `set -Eeuo pipefail` 下，`var=$(grep ... | cut | tr)` 在 grep 无匹配退出 1 时经 pipefail 传播 → `set -e` 静默结束脚本（现场「更新超时后退出」实为此 bug，`.env` 缺 `INSTALL_DIR=` 行即触发，见 §12 #AQ）。已对 10 个代码位置共 14 条命令替换统一加 `|| true`，并立下写死约束：**install.sh 所有 `$(...)` 管道命令替换必须带 `|| true` 或置于条件上下文，禁止裸 `var=$(grep|ls|find|... | ...)`**。

**安装期命令审计（v0.10 新增，P2-8，✅ v0.11 已实施 M8.8）**：新增辅助步骤 `setup_bash_audit`（失败 warn 不阻断）：对 `/root/.bashrc` + `/etc/skel/.bashrc` 追加 `PROMPT_COMMAND`（记录时间+whoami+RC 到 `/var/log/bash.log`），`chattr +a` 追加-only 失败时降级 `chmod 622`。

**update 模式（v0.4 补充，避免破坏既有运行）**：`run_install_or_update` 同时服务 install 与 update，但 update 时**不得重复弹交互提示**：

- `select_binary_tier` 在 update 模式下静默复用上次选择：优先读取 `/opt/QVMConsole/.env` 的 `KVM_BINARY_TIER`（新写入）或推断当前主程序（`readlink /opt/QVMConsole/kvm-console`），仅当 glibc 探测结果变化时才重新评估并提示。
- `detect_firewall_backend` 在 update 模式下只展示结果、不询问启动 firewalld（服务已在运行，避免误停）。

**依赖清单（自包含）**：

- RPM 系新增 `firewalld` 显式进 `RPM_PKG_MAP`（当前 `["ufw"]="firewalld"` 映射保留，install.sh:115）；qemu 主机依赖在 RPM 系已有 `qemu-kvm → qemu` 回退、`edk2-ovmf`/`edk2-aarch64` UEFI 映射。
- 防火墙后端命令：`ufw`（Debian/Ubuntu）与 `firewall-cmd`（国产 RPM 系）均须纳入 `COMMAND_CHECKS` 软性检查（缺失仅警告，宿主机防火墙功能不可用，不阻断安装）。
- 其他国产化相关依赖已在 `docs/dependencies.md` 登记：dmidecode、qemu-utils、Linux 来宾磁盘自动化包（含 RPM 系包名差异 `cloud-utils`/`cloud-utils-growpart`）。本设计不再新增系统包。

**dnsmasq INPUT 放行（v0.5 新增，#F）**：install.sh:1527 现有 `ensure_local_dnsmasq_input_rules` 直写 `iptables -I INPUT` 放行 UDP 67/53、TCP 53（vpcsw 网桥接口）。该方案在 **ufw / iptables 后端下有效，但 firewalld（nftables 后端）下不保证先于 firewalld 链求值**。修正：

- 首次安装（install）检测到 `FW_BACKEND=firewalld` 时，改用 firewalld 原生放行：将 `vpcsw*`、`br-ovs` 网桥接口加入 `trusted` zone（或本设计新增的专用 VM zone），dnsmasq 入站即天然放行，**不再写 iptables INPUT 规则**（避免双份规则冲突）。
- 若 firewalld 后端已由面板启用（qvm-host zone 方案），dnsmasq 放行由后端 zone 绑定统一管理（§5.1 决策 4），install.sh 不得重复写 iptables。
- 存量环境（已装面板再切国产系统）由 `install_files` 幂等迁移：检测到既有 dnsmasq iptables 规则且后端为 firewalld 时，先加 zone 绑定再清理对应 iptables 规则。
- 更新（update）模式不触碰 dnsmasq 放行现状（与 #S2 升级不碰 zone 同原则），只展示后端结论。

**apt/dpkg 锁防护（v0.12.4，参考宝塔 `Fix_Apt_Lock`）**：

`wait_apt_dpkg_lock()` 函数在 `pkg_update_index()` 和 `pkg_install()` 的 apt 分支调用，检查 `/var/lib/dpkg/lock`、`/var/lib/apt/lists/lock`、`/var/cache/apt/archives/lock` 三个锁文件（`fuser` 命令，来自 `psmisc` 包，已纳入 `APT_DEPS`），最多等待 60s。超时 `fuser -k` 强制释放 + `dpkg --configure -a` + `apt-get install -f -y` 修复损坏状态。dnf/yum 无锁机制，不调用。避免首次安装时 GUI 包管理器或并发 apt 持锁导致 `E: Unable to acquire the dpkg frontend lock`。

**系统源备份与回滚（v0.12.4，参考宝塔 `Set_Repo_Url` / `Check_And_Fix_Debian_Ubuntu_Source`）**：

`check_and_install_deps()` 在 `test_mirror_speed` 后调用 `apply_system_mirror()`，按 `DEPS_MIRROR`（tsinghua/aliyun/163）生成系统源文件：

- apt 系：`/etc/apt/sources.list.d/kvm-console-mirror.sources`（codename 通过 `lsb_release -cs` 或 `/etc/os-release VERSION_CODENAME` 检测，失败报错跳过）
- RPM 系：`/etc/yum.repos.d/kvm-console-local-mirror.repo`（baseurl 使用 `centos-vault` 路径，匹配 EOL 发行版；**麒麟 `OS_ID=kylin|neokylin` 除外**——无对应 centos 镜像，禁止落入 centos-vault 分支，`apply_rpm_mirror` 对麒麟直接返回走系统源）

写入前 `backup_system_sources()` 按时间戳隔离备份（`${TMPDIR:-/tmp}/kvm_console_mirror_backup/<timestamp>/`），apt 系备份 `sources.list` + `sources.list.d/*`，RPM 系备份 `yum.repos.d/*kvm*`。写入后 `apt-get update` / `dnf repolist` 验证，失败自动 `restore_system_sources()` 回滚。备份目录自动清理 7 天前的旧备份。`system`/`offline` 模式不修改系统源。

**openEuler 仓库职责拆分（v0.13 新增，#AS）**：`enable_openeuler_repos()`（`check_os` 阶段）只做**仓库文件层**三件事——清理历史遗留 CentOS 风格 kvm-console 坏源、探测 SP 版本目录、补写缺失的 EPOL/everything Section（curl `-m 10` 有界探测，不跑 dnf）；关键包可用性探测（makecache + `list available`）移至 `probe_critical_rpm_packages()`，必须由 `check_and_install_deps` 在 `apply_system_mirror` 之后调用（快源 + metalink 已注释），避免安装前期对官方慢源数分钟卡顿。

**捆绑包原生源回退收敛（v0.13 新增，#AT）**：`install_bundled_packages` Phase 1 的 `dnf provides` 回退仅覆盖 5 条核心命令（`virt-filesystems virt-customize guestfish virt-win-reg growpart`），`timeout 10s` 且 `DEPS_MIRROR=offline` 整段跳过；其余 virt-* 工具由 Phase 2 捆绑包提取（`lgft_bins`）兜底，避免逐条慢查询重复触发元数据下载。

### 5.9 build.sh 设计

1. `get_compat_glibc_default`：**不变**（amd64=2.2.5 / arm64=2.17）。
2. 新增 `--high-compat-glibc VERSION`（可选，如 2.28）：多构建一个高兼容档 `kvm-console-compat-{VER}`；不传则只产出默认档。**高兼容档同样走 `verify_compat_glibc` 校验**（确保实际最高 GLIBC 依赖 ≤ 目标），避免高兼容档越界。**注意（v0.8，#S；v0.9.3 升级为硬校验）**：`verify_compat_glibc` 现状在**无 readelf 时 warn 跳过校验**（build.sh:68-71），存在「静默漏检」风险——本地构建因缺 binutils 而漏检，产物 GLIBC 上限可能越界。**修正（v0.9.3）：缺失 readelf 时改为构建失败并输出安装命令**——`apt install binutils`（Debian/Ubuntu）或 `dnf install binutils`（RPM 系），提示后退出，不再放行跳过。CI 固定镜像（#A 的 ubuntu-22.04）自带 binutils，天然满足；本地构建按提示安装即可。这样高兼容档的 GLIBC 上限校验**在任何环境都真实执行**。
3. 兼容版 `CGO_CFLAGS="-O2 -mno-avx2 -mno-fma -mno-avx"` 保持（跨 CPU 安全）。
4. native 构建完成后用 `readelf --version-info` 探测最高 GLIBC 符号 → 写入 `native-glibc.txt`。
5. 打包清单、帮助文本、底部内容打印同步更新。
6. `.github/workflows/build.yml`：核实（v0.6 修正）——`bash build.sh -v "$VERSION"` 在 `BUILD_VARIANT=""` 下**同时构建 compat + native 双档**（build.sh:252 默认双档、install.sh:1645 真实检查 `kvm-console-native`），**双档二进制结构（`kvm-console` + `kvm-console-native`）是现状而非新增；`native-glibc.txt` 文件为本设计待实施产物（见 §13.10——build.sh 现有 `verify_compat_glibc` 仅做校验、不写该文件）**。CI 构建命令保持不追加 `--high-compat-glibc`（官方发布包仍为默认档 + native，高兼容档由发行方按需构建——与评审结论 #2 一致）。**zig 0.14.0（CI 现行版本，build.yml:65）已支持 `x86_64-linux-gnu.2.28` 目标，高兼容档无需升级 zig**（原「zig 0.16.0」表述过度保守，修正）。**实施 #A 时同步固定 runner：amd64 `ubuntu-latest`→`ubuntu-22.04`、arm64 `ubuntu-24.04-arm`→固定 2.35 档（详见 §4.3 实施要点）**。

### 5.10 qvmc-manage.sh 改造

端口修改流程（qvmc-manage.sh:253-315）现为「检测 `command -v ufw` → 检测 `ufw status` active → 更新规则」。改造后按后端分支（检测逻辑与 install.sh `detect_firewall_backend` 口径一致，qvmc-manage.sh 自维护一份）：

```bash
# 后端检测（复用口径：优先 ufw，其次 firewalld，最后 none）
local fw_backend="none"
if command -v ufw >/dev/null 2>&1; then fw_backend="ufw"
elif command -v firewall-cmd >/dev/null 2>&1; then fw_backend="firewalld"
fi

case "$fw_backend" in
    ufw)
        if ufw status 2>/dev/null | grep -q "Status: active"; then
            ufw allow "${new_port}/tcp" && ufw delete allow "${old_port}/tcp"
        fi
        ;;
    firewalld)
        if firewall-cmd --state 2>/dev/null | grep -q "^running$"; then
            # 先加新端口再删旧端口（避免误锁自身），--permanent 持久化
            # 面板曾启用 qvm-host zone 时须同步进该 zone：上行接口绑 qvm-host（DROP 拦入站），
            # 仅写默认 zone 会在改端口后自锁（§5.1 决策 4/10）
            if firewall-cmd --permanent --get-zones 2>/dev/null | grep -qw qvm-host; then
                firewall-cmd --permanent --zone=qvm-host --add-port=${new_port}/tcp
                firewall-cmd --permanent --zone=qvm-host --remove-port=${old_port}/tcp 2>/dev/null || true
            fi
            firewall-cmd --permanent --add-port=${new_port}/tcp
            firewall-cmd --permanent --remove-port=${old_port}/tcp 2>/dev/null || true
            if ! firewall-cmd --reload; then
                warn "firewall-cmd --reload 失败，持久化规则未生效，请用 firewall-cmd --state 排查"
                return 1
            fi
        fi
        ;;
    *) warn "未检测到防火墙后端，请手动放行端口 ${new_port}" ;;
esac
```

> 与 ufw 分支保持同等「仅在后端 active 时操作」语义；firewalld 分支同样先 add 新端口再删旧端口（避免误锁自身），并使用 `--permanent` 持久化。**面板曾启用 qvm-host zone 时，端口规则必须同步写入该 zone**（上行接口绑 qvm-host = DROP 拦入站，仅写默认 zone 会导致改端口后自锁，见 §5.1 决策 4/10）。仍不补 UDP（评审结论 #5）。
> **v0.8 补充（#M/#N）**：`new_port`/`old_port` 在拼接进 `firewall-cmd --add-port=` 前必须命中 `^[0-9]+$` 校验（qvmc-manage.sh:235 已有，保持不回归）；后端检测同样支持 `FW_BACKEND` 环境变量覆盖（与 install.sh `detect_firewall_backend` 共享口径）；`firewall-cmd --reload` 失败时明确报错（提示 `firewall-cmd --state` 排查），不再静默 `|| true` 吞掉 reload 错误——reload 失败意味着持久化规则未生效，必须告知用户。

### 5.11 组件版本检测与升级提示（v0.9.4 新增）

> **目标**：在构建、部署、运行三阶段建立「组件版本检测 + 升级提示」闭环，覆盖项目涉及的所有系统级组件。**仅检测与提示，不自动升级**（理由见决策 1）。前端展示位于「系统设置 → 诊断页」（与现有 diagnostics 合并，决策 3）。
>
> **现状落地范围（v0.9.7 明确）**：§5.11.2 的 18 项清单是 **M7 全量目标**；当前（M0-M5 后）仅 `server/service/firewall/advice.go` 的 **3 项 advice 已落地**——`firewalld_old`（版本 <0.9 无 policy）/ `glibc_low_for_native`（glibc < native-glibc.txt 需求版）/ `selinux_enforcing`（restorecon 已处理），经 `/system-info → firewall.upgrade_advice` 上报、前端至多一条可关闭 Banner、install.sh `print_install_report` 同步输出。manifest 生成 / `check_component_versions` / `component_health` / 前端诊断页卡片均属 M7 待实施（§13.10）。

#### 5.11.1 设计原则与决策

**决策 1：仅检测 + 提示，不自动升级**

不自动升级任何系统组件，理由：
- **firewalld 升级风险**：可能重置系统默认 zone、丢失用户自建规则、改变 iptables/nftables 后端
- **glibc 升级风险**：glibc 是系统根基库，升级可能导致整个系统不稳定（国产系统通常有严格的基线要求）
- **包源受限**：国产系统（尤其是涉密/内网环境）可能没有外网包源，自动 `dnf upgrade` 会失败
- **职责边界**：「面板安装/运行」不应越权做「系统组件升级」决策，升级由运维人员手动执行

检测到版本不满足时，统一行为：**红色（critical）= 中止安装/相关功能禁用；黄色（warning）= 提示并让用户确认是否继续；绿色（healthy）= 正常**。同时给出对应包管理器的升级命令（按 `pkg_mgr` 自动适配 apt/dnf/yum）。

**决策 2：组件清单覆盖项目全部系统级组件**

经核查 [install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) 的 `APT_DEPS` / `RPM_DEP_MAP` / `COMMAND_CHECKS` / `DEPENDENCIES.md` 与 [server/service/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/) 实际命令调用，项目涉及的系统级组件共 **18 项**（另有 cpu_vendor 厂商检测项，共 19 行探测），分为「核心必装」「磁盘/镜像/初始化」「诊断/扩展」三类（详见 §5.11.2）。

**决策 3：前端展示位置 = 系统设置 → 诊断页**

复用现有 `/settings/diagnostics/*`（categories / export，router.go:114-115）扩展，不新增独立页面：
- 已有 diagnostics 接口收集系统诊断类别并导出 ZIP（`service/diagnostics` 聚合各模块状态）
- 本设计扩展为「组件版本健康度」分区，与既有诊断导出并列（新增 `POST /settings/diagnostics/refresh` 触发重新探测，**已实施 M7.2**）
- 管理员登录后路径：系统设置 → 诊断 → 「组件版本健康度」卡片

#### 5.11.2 组件清单（18 项系统级组件 + cpu_vendor 厂商检测，共 19 行）

> 来源：[install.sh:43-69 APT_DEPS](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L43-L69)、[install.sh:87-115 RPM_DEP_MAP](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L87-L115)、[install.sh:276-309 COMMAND_CHECKS](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L276-L309)、[DEPENDENCIES.md](file:///Volumes/cs/QVMConsole/jeoQVMConsole/DEPENDENCIES.md)、[server/service/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/) 命令调用核查

| # | 组件 | 包名（apt / dnf） | 探测命令 | 最低版本 | 推荐版本 | 用途与影响 |
| --- | --- | --- | --- | --- | --- | --- |
| **核心必装** |
| 1 | glibc | libc6 / glibc | `ldd --version` | 2.17（arm64）/ 2.2.5（amd64 兜底） | 与构建档位匹配 | 二进制可运行性；不满足则面板无法启动 |
| 2 | qemu-kvm | qemu-kvm / qemu-system-x86 | `qemu-system-x86_64 --version` 或 `qemu-kvm --version` | 6.0 | 8.0+ | KVM 虚拟化核心；< 6.0 缺少现代机器类型与 virtio 特性 |
| 3 | qemu-img | qemu-utils | `qemu-img --version` | 6.0 | 8.0+ | 磁盘镜像操作（创建/转换/快照 commit/克隆 backing chain） |
| 4 | libvirt | libvirt-daemon-system / libvirt | `libvirtd --version` 或 `virsh --version` | 7.0 | 8.0+ | 虚拟机生命周期管理；< 7.0 缺少部分 XML schema 与 QMP 兼容 |
| 5 | openvswitch | openvswitch-switch / openvswitch | `ovs-vsctl --version` | 2.13 | 2.15+ | VPC 逻辑交换机、端口转发底层；< 2.13 缺少部分 OpenFlow 1.4 特性 |
| 6 | dnsmasq | dnsmasq-base / dnsmasq | `dnsmasq --version` | 2.80 | 2.86+ | VPC DHCP/DNS；< 2.80 缺少部分 DHCPv6 与 lease-script 特性 |
| 7 | firewalld（RPM 系） | — / firewalld | `firewall-cmd --version` | 0.4.0（v0.10 下调） | 0.9.0+ | 国产系统防火墙后端；<0.6 仅「不完整支持」warning（Enable 显式降级），<0.9 缺 policy 功能，VM 转发依赖 iptables 路径（§5.1 决策 3/10） |
| 8 | ufw（Debian 系） | ufw / — | `ufw --version` | 0.36 | 0.36+ | Debian/Ubuntu 防火墙后端 |
| **磁盘/镜像/初始化** |
| 9 | virt-install | virtinst / virt-install | `virt-install --version` | 3.0 | 4.0+ | 虚拟机创建（XML 模板生成） |
| 10 | virt-customize | libguestfs-tools | `virt-customize --version` | 1.40 | 1.48+ | Linux 克隆离线初始化（注入 hostname/密码/SSH key） |
| 11 | guestfish | libguestfs-tools | `guestfish --version` | 1.40 | 1.48+ | Windows 克隆注入、分区列举、NTFS 操作（[windows_init.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/clone/windows_init.go)） |
| 12 | genisoimage / xorriso / mkisofs | genisoimage / xorriso | `genisoimage --version` / `xorriso --version` | 任一可用即可 | — | Windows ConfigDrive ISO 生成（[windows_configdrive.go:189](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/clone/windows_configdrive.go#L189) 多工具回退） |
| 13 | growpart | cloud-guest-utils / cloud-utils-growpart | `growpart --version` | 0.30 | 0.30+ | VM 内分区在线扩容（[guest_automation/disk.go:338](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/guest_automation/disk.go#L338)） |
| 14 | ntfsresize / ntfsfix | ntfs-3g / ntfsprogs | `ntfsresize --version` | 2022.5 | 2022.5+ | Windows NTFS 分区调整与修复 |
| 15 | edk2-ovmf (x86) / edk2-aarch64 (ARM) | edk2-ovmf / edk2-aarch64 | 文件存在性检查 `/usr/share/OVMF/OVMF_CODE.fd` | — | — | UEFI 固件（UEFI 引导类型 VM 创建） |
| **诊断/扩展（可选）** |
| 16 | tcpdump | tcpdump | `tcpdump --version` | 4.9 | 4.99+ | 网络抓包诊断（VM 流量分析） |
| 17 | tc | iproute2 | `tc -V` | 5.0 | 5.10+ | 带宽限速（QoS） |
| 18 | kvm_stat | linux-tools / qemu-kvm-tools | `kvm_stat --version` | — | — | 可选辅助指标（热迁移 dirty-rate 判断的补充，[install.sh:500](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L500) 缺失仅警告） |
| 19 | cpu_vendor（v0.10 新增，P0-1，✅ v0.11 已实施 M8.1） | —（CPU 厂商检测，非系统包） | `/proc/cpuinfo` 厂商字段 | 白名单（Intel/AMD/Hygon/Phytium/Zhaoxin/Kunpeng） | — | 国产 CPU 兼容性；不在白名单 → warning「未认证厂商」（§5.11.3 manifest 键 `system_requirements.cpu_vendor.whitelist`；SMTXOS 对海光有显式启动参数分支，见 §5.8 precheck_domestic） |

> **分类规则**：
> - **核心必装**（1-8 + 19 cpu_vendor）：版本低于最低要求 → critical（中止安装）；**缺失 → warning**（`check_and_install_deps` 已先安装缺失包，此时缺失多为发行版无此包/后装场景，面板功能受限而非崩溃）；cpu_vendor 不在白名单 → warning「未认证厂商」（非 critical）
> - **磁盘/镜像/初始化**（9-15）：缺失或版本不足 → warning（功能降级，如 Windows 克隆不可用）
> - **诊断/扩展**（16-18）：缺失 → info（仅提示，不影响核心功能）
> - 安装期与运行期同口径：**仅「版本低于最低」触发 critical**；缺失一律 warning/info，不阻断（与 §8 验收用例一致）

#### 5.11.3 构建期：产物附带 compat-manifest.json

**目标**：构建产物中附带一份「兼容性清单」，作为 install.sh 部署时版本比对的「权威来源」，避免构建-部署口径漂移。

**实现位置**：build.sh 构建完成后、打包前写入 `release/kvm-console-linux-${TARGET_ARCH}/compat-manifest.json`。

**文件结构**（与 §5.11.2 的组件清单一一对应，版本号在 build.sh 顶部声明为变量便于维护）：

```json
{
  "manifest_version": "1.0",
  "build_time": "<构建机 date -u +%Y-%m-%dT%H:%M:%SZ>",
  "build_host": "<构建机 hostname>",
  "target_arch": "amd64",
  "binaries": {
    "kvm-console": {
      "type": "compat-default",
      "max_glibc": "2.2.5",
      "notes": "兼容面最广，任何现代 Linux 可运行"
    },
    "kvm-console-compat-2.28": {
      "type": "compat-high",
      "max_glibc": "2.28",
      "notes": "国产系统服务器版推荐档（v0.9.4 新增）"
    },
    "kvm-console-native": {
      "type": "native",
      "min_glibc": "<读取 native-glibc.txt（已实施 M0.5）>",
      "notes": "性能最佳，需 glibc >= native 阈值"
    }
  },
  "system_requirements": {
    "glibc":           { "min_version_amd64": "2.2.5", "min_version_arm64": "2.17", "category": "core" },
    "qemu-kvm":        { "min_version": "6.0",  "recommended": "8.0",  "category": "core" },
    "qemu-img":        { "min_version": "6.0",  "recommended": "8.0",  "category": "core" },
    "libvirt":         { "min_version": "7.0",  "recommended": "8.0",  "category": "core" },
    "openvswitch":     { "min_version": "2.13", "recommended": "2.15", "category": "core" },
    "dnsmasq":         { "min_version": "2.80", "recommended": "2.86", "category": "core" },
    "firewalld":       { "min_version": "0.4.0","recommended": "0.9.0","category": "core", "os": "rpm" },
    "ufw":             { "min_version": "0.36", "recommended": "0.36", "category": "core", "os": "debian" },
    "virt-install":    { "min_version": "3.0",  "recommended": "4.0",  "category": "disk" },
    "virt-customize":  { "min_version": "1.40", "recommended": "1.48", "category": "disk" },
    "guestfish":       { "min_version": "1.40", "recommended": "1.48", "category": "disk" },
    "genisoimage":     { "min_version": "any",  "alternatives": ["xorriso", "mkisofs"], "category": "disk" },
    "growpart":        { "min_version": "0.30", "recommended": "0.30", "category": "disk" },
    "ntfsresize":      { "min_version": "2022.5","recommended": "2022.5","category": "disk" },
    "edk2-ovmf":       { "min_version": "any", "category": "disk", "arch": "x86_64" },
    "edk2-aarch64":    { "min_version": "any", "category": "disk", "arch": "aarch64" },
    "tcpdump":         { "min_version": "4.9", "recommended": "4.99", "category": "diag" },
    "tc":              { "min_version": "5.0", "recommended": "5.10", "category": "diag" },
    "kvm_stat":        { "min_version": "any", "category": "diag", "optional": true },
    "cpu_vendor":      { "whitelist": ["Intel", "AMD", "Hygon", "Phytium", "Zhaoxin", "Kunpeng"], "category": "core" }
  },
  "os_compat": {
    "kylin-v10-server":  { "firewall": "firewalld", "glibc": "2.28",  "recommended_tier": "compat-2.28", "support_level": "S", "certified_hardware": ["Kunpeng", "Phytium", "Hygon", "Zhaoxin", "Intel", "AMD"] },
    "openEuler-22.03":   { "firewall": "firewalld", "glibc": "2.34",  "recommended_tier": "compat-2.28", "support_level": "B", "certified_hardware": [] },
    "openEuler-20.03":   { "firewall": "firewalld", "glibc": "2.28",  "recommended_tier": "compat-2.28", "support_level": "B", "certified_hardware": [] },
    "uos-1060":          { "firewall": "ufw",       "glibc": "2.28",  "recommended_tier": "compat-2.28", "support_level": "B", "certified_hardware": [] },
    "ubuntu-22.04":      { "firewall": "ufw",       "glibc": "2.35",  "recommended_tier": "native",      "support_level": "S", "certified_hardware": ["Dell PowerEdge", "H3C UniServer"] },
    "debian-12":         { "firewall": "ufw",       "glibc": "2.36",  "recommended_tier": "native",      "support_level": "B", "certified_hardware": [] },
    "centos-7":          { "firewall": "firewalld", "glibc": "2.17",  "recommended_tier": "compat-default", "support_level": "B", "certified_hardware": [] }
  }
}
```

> **support_level / certified_hardware（v0.10 新增，P3-11，✅ M8.11 已实施 v0.12）**：`support_level ∈ {S, A, B, C}`——S=官方全量回归、A=核心功能回归、B=社区自测、C=理论兼容；`certified_hardware` 为认证硬件清单（对齐 SMTXOS/HCI 公开 HCL 做法）。install.sh `check_support_level()` 检测到 `support_level=C` → warn「理论兼容，生产请升级到认证基线」+ 报告复述。`cpu_vendor`（P0-1）作为 `system_requirements` 新条目：不在白名单 → component_health warning「未认证厂商」。

**build.sh 实现要点**：
1. 阈值集中维护在 `write_compat_manifest()` 使用的 `COMPONENT_REQ_*` 变量中（v0.9.9 评审修复 #V：由 build.sh 顶层变量唯一维护，`compat-manifest.json` 与新增 `versions.conf` 同源生成，不再直接写数字进 heredoc），与 §5.11.2 表格一致；`--compat-glibc`/`--high-compat-glibc` 影响 `binaries` 段。
2. 两处调用（build.sh:395/538）：后端编译前写 `server/service/diagnostics/compat-manifest.json`（`go:embed` 编译期读取，native min 暂写 `"pending"`）；打包段写 `release/.../compat-manifest.json`（带真实 native min）与 `release/.../versions.conf`（install.sh 纯 shell 读取）。`build_time`/`build_host`/`target_arch` 用变量替换。
3. `binaries` 段的 `max_glibc` 从 `--compat-glibc` 与 `--high-compat-glibc` 参数推导（`BUILD_COMPAT`/`BUILD_NATIVE` 布尔位决定 `kvm-console`/`kvm-console-compat-{VER}`/`kvm-console-native` 条目），与实际构建档位一致。
4. `native` 档的 `min_glibc` 读取 `native-glibc.txt`（已实施 M0.5）；若该文件不存在则写 `"pending"` 并打 warn。

#### 5.11.4 部署期：install.sh check_component_versions()

**位置**：在 install.sh `get_release()` 之后、`select_binary_tier()` 之前新增 `check_component_versions()` 函数（§13.7.2 主步骤 12）。**必须放在 `get_release` 之后**：版本阈值取自构建产物 `release/.../versions.conf`（v0.9.9 评审修复 #V；旧产物回退 `compat-manifest.json` → 内置默认值），依赖本地发行目录或下载目录就绪；`check_and_install_deps`（主步骤 2）已先行安装缺失依赖包，此处仅做版本比对。

**核心逻辑**：

```bash
check_component_versions() {
    info "检查关键组件版本..."
    # v0.9.9 评审修复（#V）：阈值来源改为 build.sh 同源产出的 versions.conf（纯 shell 读取，去掉 python3 依赖）
    local vconf="${RELEASE_SOURCE_DIR}/versions.conf"
    local warnings=() criticals=() healthy_count=0 total_count=0

    # 1. 读取构建产物中的 versions.conf（key=min|rec，缺失回退内置默认值）
    local min_firewalld="0.4.0" min_qemu="6.0" min_libvirt="7.0" min_ovs="2.13" min_dnsmasq="2.80"
    if [ -f "$vconf" ]; then
        while IFS= read -r line; do
            [ -n "$line" ] || continue
            case "$line" in
                \#*) continue ;;
                GLIBC_MIN_AMD64=*) min_glibc_amd64="${line#*=}" ;;
                GLIBC_MIN_ARM64=*) min_glibc_arm64="${line#*=}" ;;
                firewalld=*) min_firewalld="${line#*=}"; min_firewalld="${min_firewalld%|*}" ;;
                # ... 其他组件同理
            esac
        done < "$vconf"
        success "已加载组件版本阈值: $vconf"
    else
        warn "未找到 versions.conf，使用内置默认版本阈值（可能与构建档位不匹配）"
    fi

    # 2. 版本比较函数（语义化版本 < major.minor.patch >）
    version_lt() { [ "$(printf '%s\n%s' "$1" "$2" | sort -V | head -n1)" = "$1" ]; }

    # 3. 逐项检测（以 firewalld 为例，其他组件同理）
    if [ "$FW_BACKEND" = "firewalld" ] && command -v firewall-cmd >/dev/null 2>&1; then
        local fw_ver
        fw_ver=$(firewall-cmd --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1 || echo "0")
        total_count=$((total_count + 1))
        info "  firewalld 版本: $fw_ver（最低要求 $min_firewalld）"
        if version_lt "$fw_ver" "$min_firewalld"; then
            criticals+=("firewalld $fw_ver < $min_firewalld（policy 功能缺失，VM 转发依赖 iptables 路径）")
            warn "  升级命令: sudo $PKG_MGR install -y firewalld  # 或联系系统管理员"
        elif version_lt "$fw_ver" "0.9.0"; then
            warnings+=("firewalld $fw_ver 可运行，但推荐 >= 0.9.0 以获得完整 policy 支持")
        else
            healthy_count=$((healthy_count + 1))
        fi
    fi

    # 4. QEMU / libvirt / OVS / dnsmasq / virt-customize / guestfish / ... 同理检测
    # ...（按 §5.11.2 清单逐项，省略重复模板）

    # 5. 汇总输出（v0.8 #K 风格）
    echo ""
    info "==================== 组件版本检测报告 ===================="
    info "  总计: $total_count 项，健康: $healthy_count 项"
    if [ ${#criticals[@]} -gt 0 ]; then
        error "  关键不满足 (${#criticals[@]} 项):"
        for c in "${criticals[@]}"; do error "    - $c"; done
        error "========================================================="
        error "检测到 ${#criticals[@]} 个关键组件版本不满足最低要求，安装中止。"
        error "请按上述升级命令升级后重试，或使用 --skip-version-check 跳过（不推荐）。"
        return 1
    fi
    if [ ${#warnings[@]} -gt 0 ]; then
        warn "  警告 (${#warnings[@]} 项):"
        for w in "${warnings[@]}"; do warn "    - $w"; done
        warn "========================================================="
        if [ -z "$NON_INTERACTIVE" ]; then
            read_tty -rp "检测到 ${#warnings[@]} 个组件版本偏低，功能可能受限。是否继续安装? [Y/n]: " ans
            if [[ "${ans:-Y}" =~ ^[Nn]$ ]]; then
                info "已取消安装，请升级组件后重试"
                exit 0
            fi
        fi
    fi
    success "========================================================="
    return 0
}
```

**关键设计点**：

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| 版本阈值来源 | 优先读 `versions.conf`（build.sh 同源产出），缺失回退内置默认值 | 与构建档位保持一致；回退保证旧产物兼容 |
| 解析方式 | 纯 shell 逐行读取（`key=min|rec`） | v0.9.9 评审修复（#V）：消除 python3 依赖，兼容无 python3 的国产最小化系统 |
| 版本比较 | `sort -V` + 字符串比较 | 语义化版本通用方案；不引入 `dpkg --compare-versions`（Debian 专属） |
| critical 处理 | 中止安装 + 提示升级命令 | 核心组件版本不足会导致面板运行崩溃，不应继续 |
| warning 处理 | 提示 + 非交互模式默认继续，交互模式让用户确认 | 平衡安全与可用性；CI 环境（`NON_INTERACTIVE=1`）不阻塞 |
| 跳过开关 | `--skip-version-check` | 仅供特殊场景（如离线环境已知版本满足但探测失败）使用，默认不跳过 |
| 多工具回退 | genisoimage/xorriso/mkisofs 任一可用即 healthy | 与 [windows_configdrive.go:189](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/clone/windows_configdrive.go#L189) 多工具回退逻辑一致 |
| 架构专属 | edk2-ovmf（x86）/ edk2-aarch64（ARM）按 `$ARCH` 检测 | 与 [install.sh:128-131](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L128-L131) 一致 |

**与现有 install.sh 集成**：
- 在 `get_release` 之后调用（先读取发行目录中的 compat-manifest.json 阈值；`check_and_install_deps` 已先安装缺失包）
- 在 `select_binary_tier` 之前调用（GLIBC 档位选择依赖 glibc 版本检测结果）
- `print_install_report`（#K）汇总输出中追加「组件版本检测」段（总计/健康/警告/关键不满足）

#### 5.11.5 运行期：后端 component_health + 前端诊断页

**后端扩展**（**v0.9.3 收敛一致**：`component_health` 并入 §4.1 的 `GET /system-info` 增量扩展，复用现有 authorized 只读接口；诊断导出沿用现有 `/settings/diagnostics/*`，**不新建 `/system/*` 分组**）：

```go
// GET /system-info 新增 component_health 字段（v0.9.3 收敛后挂载点）
{
  "os": { ... },
  "arch": "x86_64",
  "glibc": "2.34",
  "firewall": { "backend": "firewalld", "version": "0.9.3", ... },
  "component_health": {
    "overall": "warning",  // healthy | warning | critical
    "last_check": "2026-07-31T12:00:00Z",
    "items": [
      {
        "component": "firewalld",
        "category": "core",
        "status": "healthy",
        "current_version": "0.9.3",
        "required_version": "0.4.0",
        "recommended_version": "0.9.0",
        "message": "版本满足推荐要求"
      },
      {
        "component": "qemu-kvm",
        "category": "core",
        "status": "warning",
        "current_version": "6.2.0",
        "required_version": "6.0",
        "recommended_version": "8.0",
        "message": "QEMU 6.2 可运行，但推荐 >= 8.0 以获得 virtio-mem 与现代机器类型支持",
        "upgrade_hint": "sudo dnf update -y qemu-kvm"
      },
      {
        "component": "virt-customize",
        "category": "disk",
        "status": "critical",
        "current_version": "1.38",
        "required_version": "1.40",
        "message": "virt-customize < 1.40，Linux 克隆初始化可能失败（CVE 修复与 NTFS 兼容性）",
        "upgrade_hint": "sudo dnf install -y libguestfs-tools"
      },
      // ... 其余 15 项按 §5.11.2 清单
    ]
  }
}
```

**探测实现**：
- **复用现有 `diagnostics` 子包**（已有 `collector.go`，语义聚合），新建 `server/service/diagnostics/component_health.go`（不新建 `system` 子包，避免子包碎片化）。**已实施（M7.2）为包级函数实现**：`GetComponentHealth()`/`ResetComponentHealthCache()` 对外接口 + `detectComponentHealth()` 全量探测 + `buildItem()` 统一三态判定 + `detectVersion()`（命令不存在→空串 / 解析失败→`"unknown"` 降级 warning，§8 验收）
- 版本阈值从构建时嵌入的 `compat-manifest.json` 读取（`go:embed`，build.sh 编译前写入源码树）
- 探测结果缓存（`RWMutex` 双检，与 `DetectHostFirewallBackend` 同口径，§4.2），管理员手动刷新触发 `ResetComponentHealthCache()`

**探测频率**：
- 服务启动时探测一次（写入缓存）
- 管理员手动刷新时重新探测（前端「刷新」按钮 → **复用现有 `/settings/diagnostics` 组新增 `POST /settings/diagnostics/refresh`**（Auth + Admin），与现有 `/settings/diagnostics/categories` / `/settings/diagnostics/export` 同组一致；不新建 `/system/*` 路由。**已实施 M7.2**）
- **不设置定时轮询**（避免频繁调用 `firewall-cmd --version` 等命令增加系统负担）

**前端展示**（系统设置 → 诊断页，与现有 diagnostics 合并）：

```
┌─────────────────────────────────────────────────────────────┐
│  系统诊断                              [刷新] [导出报告]    │
├─────────────────────────────────────────────────────────────┤
│  ▸ 服务状态（现有）                                          │
│    libvirtd: running   openvswitch: running   dnsmasq: ...  │
├─────────────────────────────────────────────────────────────┤
│  ▾ 组件版本健康度（v0.9.4 新增）            整体: ⚠️ 警告    │
│                                                              │
│  核心组件                                                    │
│  ● firewalld       0.9.3    ✅ 健康   推荐 ≥ 0.9.0          │
│  ● qemu-kvm        6.2.0    ⚠️ 警告   推荐 ≥ 8.0            │
│    QEMU 6.2 可运行，但推荐 >= 8.0 以获得 virtio-mem 支持    │
│    [复制升级命令] sudo dnf update -y qemu-kvm                │
│  ● libvirt         8.0.0    ✅ 健康   推荐 ≥ 8.0            │
│  ● openvswitch     2.17     ✅ 健康   推荐 ≥ 2.15           │
│  ● glibc           2.34     ✅ 健康   命中 compat-2.28 档   │
│                                                              │
│  磁盘/镜像/初始化                                            │
│  ● virt-customize  1.38     ❌ 不满足 最低 ≥ 1.40           │
│    Linux 克隆初始化可能失败（CVE 修复与 NTFS 兼容性）        │
│    [复制升级命令] sudo dnf install -y libguestfs-tools       │
│  ● guestfish       1.48     ✅ 健康                         │
│  ● genisoimage     (缺失)   ⚠️ 警告   可用 xorriso 替代      │
│    Windows ConfigDrive ISO 生成将回退到 xorriso              │
│    [复制安装命令] sudo dnf install -y xorriso                │
│  ...                                                         │
│                                                              │
│  诊断/扩展（可选）                                           │
│  ● kvm_stat        (缺失)   ℹ️ 提示   热迁移辅助指标不可用   │
│  ...                                                         │
├─────────────────────────────────────────────────────────────┤
│  ▸ 网络状态（现有）                                          │
└─────────────────────────────────────────────────────────────┘
```

**前端交互细节**（遵循 AGENTS.md Semi UI 约定）：
- 每个组件行：状态 Tag（颜色：green=healthy / orange=warning / red=critical / grey=info）+ 组件名 + 当前版本 + 状态文案
- 升级命令用 `<Typography.Text copyable>` 包裹，支持一键复制
- 「导出报告」按钮：导出 JSON（含全部 18 项检测结果），便于离线排查或提 issue 时附带
- 深色模式：状态 Tag 颜色遵循 `body[theme-mode='dark']` 覆盖，避免高亮刺眼

#### 5.11.6 与现有里程碑的关系

| 本节内容 | 所属里程碑 | 依赖 | 优先级 |
| --- | --- | --- | --- |
| build.sh 生成 `compat-manifest.json` | M4（GLIBC 档位）扩展 | 无 | 中（与 #A 并行） |
| install.sh `check_component_versions()` | M5（install.sh 交互）扩展 | `compat-manifest.json` | 中 |
| 后端 `component_health` 探测 | M2（`/system-info` 扩展）扩展 | M1（firewalld 后端） | 中 |
| 前端诊断页「组件版本健康度」卡片 | M3（前端适配）扩展 | 后端 `component_health` | 低 |
| `go:embed compat-manifest.json` | M4 扩展 | `compat-manifest.json` 存在 | 中 |

**建议新增 M7 里程碑**（与 M0-M6 并行，可独立交付）：

```
M7.0  build.sh 生成 compat-manifest.json（1 天）
M7.1  install.sh check_component_versions() + install_report 集成（2 天）
M7.2  后端 component_health 探测 + go:embed（2 天）
M7.3  前端诊断页「组件版本健康度」卡片（1 天）
M7.4  文档同步：DEPENDENCIES.md 补 18 项版本阈值表（0.5 天）
```

#### 5.11.7 风险与回滚

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| `compat-manifest.json` 缺失或格式错误 | install.sh 回退内置默认值；后端 `component_health` 不展示版本阈值 | 内置默认值兜底；JSON 解析失败时 warn 但不阻塞 |
| 某组件 `--version` 输出格式异常（如国产系统定制） | 版本号解析为 "0" → 误报 critical | 解析失败时降级为 warning + 提示「无法解析版本，请人工确认」 |
| `python3` 缺失（极少见） | install.sh 无法解析 JSON | v0.9.9 评审修复（#V）：versions.conf 为纯 shell 可读格式，无 python3 依赖；仅旧产物无 versions.conf 时回退内置默认值 |
| 探测命令超时（如 firewalld 主进程挂死） | install.sh 阻塞 | 所有 `--version` 命令加 `timeout 5s` 前缀；超时按 warning 处理 |
| 前端诊断页信息过载（18 项全展示） | 管理员认知负担 | 按 category 分区折叠（核心/磁盘/诊断），默认展开「核心」 |
| `go:embed` 路径与构建产物不一致 | 后端读取不到 manifest | 构建时 `cp compat-manifest.json server/` 后再 `go build`；CI 验证 embed 成功 |

---

## 6. 交互设计（可交互点汇总）

### 6.1 安装期（install.sh）

| 交互点 | 触发 | 动作 | 默认值 |
| --- | --- | --- | --- |
| 防火墙后端探测 | `detect_firewall_backend`（install 模式） | 展示探测结果；firewalld 未运行则询问启动 | 不启动（N） |
| 防火墙后端探测 | `detect_firewall_backend`（update 模式） | 仅展示探测结果，不询问启动 | — |
| 安装路径选择（v0.12.4） | `choose_install_dir`（install 模式） | 交互选择安装路径；支持自定义绝对路径；校验父目录存在性与磁盘空间 | `/opt/QVMConsole` |
| 安装路径复用（v0.12.4） | `choose_install_dir`（update/uninstall/repair/rollback 模式） | 从已有 `.env` 读取 `INSTALL_DIR` 复用 | 上次安装路径 |
| 旧版路径迁移（v0.12.4） | `choose_install_dir`（install 模式） | 检测旧版 `/opt/kvm-console` 安装；用户选择新路径后提示是否自动迁移；迁移内容含二进制/配置/数据/systemd 服务 | 自动提示 |
| 二进制档位选择 | `select_binary_tier`（install 模式） | 展示 glibc/CPU/推荐档位，允许覆盖 | 自动推荐 |
| 二进制档位复用 | `select_binary_tier`（update 模式） | 读 `.env` KVM_BINARY_TIER 复用；glibc 变化才重新评估并提示 | 上次选择 |
| 高档降级提示 | 含 2.28 高档但系统 glibc < 2.28 | 提示将使用默认档（不阻断，默认档必可运行） | 默认档 |
| 选优切换冒烟测试 | install_files 切换主程序前 | 目标档 `--version` 验证；失败保留原主程序并告警 | — |
| 非交互跳过（v0.8） | 无 TTY 或 `CI=1`，或 `FW_BACKEND`/`KVM_BINARY_TIER` 已导出 | 跳过 `read`，采用推荐/覆盖值，日志标注「非交互模式自动采用」 | 推荐值 |
| 端口占用预检（v0.8） | `precheck_domestic` | `ss -ltn` 发现 KVM_PORT 被占用 → 列出占用方并要求更换端口 | 更换端口 |
| 组件升级提示（v0.9.3） | 安装末尾 `print_install_report` | 依 `/system-info` 口径输出 advice：firewalld <0.9 → 建议升级；glibc < native 需求 → 提示当前用 compat 档；SELinux Enforcing → 提示 restorecon 已处理 | 仅提示 |
| 安装总结报告（v0.8） | 安装末尾 `print_install_report` | 输出后端/档位/原因/glibc/CPU/SELinux/降级项/日志路径 | — |

### 6.2 面板运行期（前端）

| 交互点 | 触发 | 动作 |
| --- | --- | --- |
| 后端不可用提示 | `backend === 'none'` | Banner 警告 + 规则页可用但不可开启防火墙 |
| 默认转发未管理 | firewalld 0.8.x 无 policy 能力 | `default_routed` 显示「未管理」Tag + Tooltip 依据 `ip_backend` 说明可靠性路径（v0.8，#O） |
| 后端名展示 | HostFirewallTab 运行状态卡 | 绿/红 Tag 显示 `backend_name` |
| zone 绑定状态 | 面板校验 Enable 后 qvm-host 已绑定接口/源 | 绑定缺失则视为 Enable 失败并告警（§5.1 决策 10） |
| 错误 hint（v0.8） | `error_code` 非空 | 展示可操作提示（如「firewalld 服务未运行」+ 修复命令）+ 「重新检测」按钮（#R） |
| Enable 自检失败（v0.8） | 自检清单任一失败 | 任务进度展示失败项清单 + 「回滚」入口（二次确认走 disable）（#L） |
| 组件升级提示（v0.9.3） | `firewall.upgrade_advice` 任一命中 | 至多一条 Banner：firewalld 过旧 / glibc 可用 native / SELinux Enforcing（§5.7，优先级 firewalld_old > glibc_low > selinux） |

### 6.3 API / 任务交互

- `POST /firewall/host/enable` 与 `disable` 保持走 taskqueue（SSE 进度），后端内部 `Enable(progress)` 复用 ufw 现有进度文案，firewalld 复用同一组文案。
- 规则 CRUD（add/update/delete）为即时操作，不走队列——与现有一致。

### 6.4 异常与降级

| 场景 | 行为 |
| --- | --- |
| 系统无 ufw/firewalld | `none` 后端，`Available=false`，接口返回明确错误，前端 Banner，**不 panic 不静默** |
| firewalld 服务停止 | `Active()` 返回 false；`ListRules/Defaults` 返回空、不报错（#H）；面板可一键 `Disable()`→显示关闭状态，`Enable()` 负责 `systemctl start` |
| firewalld 旧版（0.8.x）无 policy | 仅 `default_routed` 字段为空（「未管理」），转发依赖面板 iptables FORWARD（iptables 后端 `-I FORWARD 1` 可靠），规则照常 |
| firewalld zone 未绑定 | **不成立**：Enable 必须完成 zone 绑定序列（§5.1 决策 10），未绑定视为 Enable 失败并回滚，杜绝「DROP 惰性」安全假象 |
| 从 ufw 环境切换到国产系统 | 无数据迁移（规则存于各后端自身），面板重新 ListRules 展示现状 |
| 面板升级（update） | **不触碰 `/etc/firewalld/zones/qvm-host.xml`**（运维补充项 #S2）；`install_files` 只覆盖程序与 web-dist，防火墙规则由后端各自治 |
| 面板卸载（uninstall） | 删除 `qvm-host` zone（先检查 `firewall-cmd --state`：running 则 `--delete-zone=qvm-host` + 删 XML + reload；**服务未运行则直接删 XML 文件、跳过 reload**）；ufw 后端同样清理 `kvm-console:` 前缀规则并 `ufw --force disable`（如启用中） |
| firewall-cmd 挂死 / dbus 无响应（v0.8，#N） | 命令带超时（`ExecCommandWithTimeout` 60s+），超时返回 `DBUS_ERROR` + hint（`systemctl restart firewalld`）；面板不阻塞 |
| `.env` 非法值（v0.8，#M） | `KVM_BINARY_TIER`/`FW_BACKEND` 值不在白名单 → 告警并回退默认档/自动探测（防路径注入） |
| reload 失败导致持久化未生效（v0.8） | 规则 CRUD 与 qvmc-manage.sh 的 `--reload` 失败均显式报错（不再 `|| true` 吞错），提示 `firewall-cmd --state` 排查 |

---

## 7. 实施计划

| 里程碑 | 内容 | 依赖 | 风险 | 验收 |
| --- | --- | --- | --- | --- |
| M0 | 后端抽象骨架：backend.go + backend_detect.go + none 后端 + ufw 后端迁移 + backendExec 互斥锁 | 无 | 低 | ufw 回归零变化；并发 CRUD 不冲突（**✅ 已实施，见 §13.10**） |
| M0.5 | **优先项 #A**：build.sh 输出 `native-glibc.txt` + install.sh 选优改读该文件（废弃 2.34 硬编码）+ CI native 构建镜像固定 glibc 2.35 档 | 无 | 低 | glibc 2.34~2.38 系统不再错误切 native；Ubuntu 22.04 native 资格保持不变；日志展示所选档位及原因（**✅ 已实施，见 §13.10**） |
| M1 | firewalld 后端（含 zone 持久化、**接口/源绑定序列**、policy 放行 uplink→VM、rich-rule 解析、`--check-config` 预检原子性、Docker 兼容探测、SELinux restorecon） | M0 | 中 | openEuler/麒麟服务器版：开/关/规则 CRUD/端口转发/SPICE 暴露/**VPC dnsmasq DHCP/DNS** 全通过；Docker × firewalld 共存验证（iptables 与 nftables 后端）；连续 reload 后转发仍可达（**✅ 已实施，见 §13.10**） |
| M2 | network/helpers.go + hook 接入 + types 字段 + `/system-info` 增量扩展（glibc/cpu.avx2/firewall.{backend,available,active,version,ip_backend,nm_managed,docker_compatible,error_code,upgrade_advice}/selinux.mode） | M0/M1 | 低 | 接口返回正确；`/network/ufw/*` 兼容；advice 字段随后端/版本正确上报（**✅ 已实施，见 §13.10**） |
| M3 | 前端 HostFirewallTab 适配 + api/firewall.ts 类型 + 错误 hint / Enable 自检失败项 / ip_backend Tooltip + firewall-page.md 同步（#S） | M2 | 低 | 后端名展示 + none Banner + docker 兼容状态 + error_code hint 渲染 + 自检失败项展示与回滚入口（**✅ 已实施，见 §13.10**） |
| M4 | GLIBC 档位：默认基线不变 + `--high-compat-glibc 2.28` 可选高档（数据源 `native-glibc.txt` 随 M0.5 优先落地；`KVM_BINARY_TIER` 持久化随 M5 的 select_binary_tier 落盘） | 无（与 M0-M3 并行；M0.5 优先） | 低 | 默认档回归零变化；2.28 高档可产出且通过 verify_compat_glibc；§4.3 选优规则 3-5 正确（**✅ 已实施，见 §13.10**） |
| M5 | install.sh detect_firewall_backend + select_binary_tier（get_release 后）+ 安装日志/step 包装/总结报告（#K）+ 预检（#L）+ 非交互与环境变量覆盖（#M）+ update 模式非交互复用 + 选优切换前冒烟测试 + qvmc-manage.sh 后端检测分支（reload 失败显式报错）+ 升级/卸载 zone 边界 + 安装期组件升级提示（print_install_report 输出 advice） | M1/M4 | 低 | 安装日志与交互符合 §6.1；update 不重复弹窗、不碰 zone；uninstall 清 zone（firewalld 未运行也正常）；选优切换前验证失败时保留旧档；CI=1 全程无 read 阻塞；日志/总结报告完整（**✅ 已实施，见 §13.10**） |
| M6 | 文档：build-compatibility.md（--high-compat-glibc + verify_compat_glibc readelf **硬校验**）/ dependencies.md / endpointDescriptions.ts + rich-rule 注入转义（ufw 后端一并修） | M4 | 低 | 文档与实现一致；注入用例通过（**✅ 已实施，见 §13.10**） |
| **M7**（v0.9.4 新增，**已实施**） | **组件版本检测闭环**：build.sh 生成 `compat-manifest.json`（§5.11.3）+ install.sh `check_component_versions()`（§5.11.4）+ 后端 `component_health` 探测与 `go:embed`（§5.11.5）+ 前端诊断页「组件版本健康度」卡片（§5.11.5）+ DEPENDENCIES.md 补 18 项版本阈值表 | M1（后端探测依赖 firewalld 后端） | 低 | 18 项组件全部探测正确；构建产物含 manifest.json；install.sh critical 中止/warning 确认/healthy 通过；前端诊断页分区展示 + 升级命令一键复制；manifest 缺失时回退默认值不阻塞（**✅ 已实施，见 §13.10**） |
| **M8**（v0.10 新增，**12/12 已实施**） | **竞品差异吸收**（HCI 6.11 / SMTXOS 6.2）：M8.1~M8.12 共 12 条 P0-P3 优化（CPU 厂商细分 / firewalld 三档分级 / glibc 2.17 CI / stage 持久化 / DB migrations / 发行版回滚 / 包校验 / bash 审计 / VM 看门狗 / 健康探针 / 支持等级 / 镜像源 offline），详见 §14 | M7（component_health 口径）；独立于 M0-M7 | 低~中 | ✅ **12/12 全部已实施**（v0.11：M8.1/M8.2/M8.4/M8.8/M8.12；v0.12：M8.3/M8.5/M8.6/M8.7/M8.9/M8.10/M8.11，见 §8 M8 用例块 + §14.2/§14.4 逐条标注） |

M0.5 与 M0 相互独立可并行，M0.5 优先实施；M0-M3 为 P1（防火墙），M4 为 P2（GLIBC），M1 含 #C/#D/#F/#J/#S1/#P/#L/#R（#F 为本里程碑核心，zone 绑定与转发可靠性），M2 含 #G/#S3 扩展（ManageUFWRule 注入）/#Q/#N/#O（backend 探测与加固随 M0 骨架落地）及 advice（v0.9.3），M3 含 #S（firewall-page.md 同步）与前端 advice Banner（v0.9.3），M5 含 #F 迁移与 #S2/#K/#L/#M 及安装期 advice，M6 含 #S3/#S（build-compatibility.md），M2/M4 含 #H，#I 为文档澄清项（随 M0.5/M4 落地时更新 §5.9 描述即可）。**M7（v0.9.4 新增）跨 M4/M5/M2/M3 扩展，可与 M0-M6 并行独立交付，M7.0（manifest 生成）与 M0.5/M4 并行，M7.1 随 M5 落地，M7.2 随 M2 落地，M7.3 随 M3 落地，M7.4 文档随 M6 落地**。

> **实施进度汇总（v0.9.7 核对，v0.9.8 更新，v0.9.11 增补）**：
> - **M0 / M0.5 / M1 / M2 / M3 / M4 / M5 / M6 — ✅ 全部已落地**（§13.10 逐项核对，16 项抽查全部已实施，含后端抽象骨架 9 个文件、install.sh 三档选优 + 后端探测 + 冒烟测试、build.sh `--high-compat-glibc` + `native-glibc.txt` + readelf 硬校验、`/system-info` 扩展字段、前端 HostFirewallTab 适配、`POST /firewall/host/reset-backend` 路由、qvmc-manage.sh firewalld 分支、network/helpers.go 注入修复、firewall-page.md / build-compatibility.md / endpointDescriptions.ts 文档同步）
> - **M7 — ✅ 已全部落地**（5 个子项均已完成，见 §13.10）：
>   - M7.0：build.sh `write_compat_manifest()` 生成 `compat-manifest.json`（编译前写 `server/service/diagnostics/` 供 go:embed，打包段写 `release/.../` 带真实 native min；`BUILD_COMPAT`/`BUILD_NATIVE` 布尔位 + 输出摘要 manifest 行）
>   - M7.1：install.sh `check_component_versions()` + `--skip-version-check`（18 项探测，critical 中止 / warning 确认 / healthy 通过；manifest 缺失回退内置默认值；`STEP_TOTAL` 17→18，新增「组件版本检测」步骤）
>   - M7.2：`server/service/diagnostics/component_health.go` + `go:embed compat-manifest.json`（复用现有 `diagnostics` 子包）+ `POST /settings/diagnostics/refresh` 路由 + `/system-info` 增 `component_health` 字段 + 启动异步预热缓存
>   - M7.3：前端诊断页「组件版本健康度」卡片（DiagnosticsTab.tsx 重构：状态 Tag + 类别分区 + 升级命令一键复制 + 刷新 + 导出报告 JSON；`refreshDiagnostics()` API；深色模式样式）
>   - M7.4：DEPENDENCIES.md 补 18 项版本阈值表 + endpointDescriptions.ts 补 refresh/component_health 文案
> - **M8（v0.10 新增，v0.12 全量实施）**：✅ **12/12 全部已实施**（v0.11 首批：M8.1/M8.2/M8.4/M8.8/M8.12；v0.12 剩余：M8.3/M8.5/M8.6/M8.7/M8.9/M8.10/M8.11），详见文档头部 v0.12 修订要点与 §14.2/§14.4 逐条标注。
> - **下一步（v0.12.1 更新，v0.13 追加安装期健壮性修复）**：M8 已全量落地，§14.5 候选①诊断面板实时数据 / ②看门狗灵敏度配置 / ③支持等级矩阵展示 / ⑤看门狗事件查看 均已实施；**v0.13 追加**：install.sh 安装期健壮性/性能修复 4 项（#AQ set -e 静默退出脚枪、#AS openEuler 慢源前置卡顿、#AT dnf provides 收敛、#AR 交互倒计时 + 步骤计时汇总，见文档头部 v0.13 修订要点）；剩余可选：④minisign 离线签名（M8.7 SHA256SUMS 已交付，当前无构建密钥基础设施）与定期同步 `origin/main`/`upstream` 上游提交（`docs/merge-from-upstream.md`，只合并后端）。
> - **v0.9.11 审计修复（对照上游原生代码审计）**：M0-M7 全部落地后，对上游基线 `51330e5` 全量 diff 审计，发现兼容性错误与未完善项 20 处，已全部修复（#AN/#AO/#AP 高严重度：firewalld deny 无来源规则语义反置→统一 rich-rule、firewalld <0.7 无 `--new-zone`→原子写 zone 文件、`--delete-policy` 版本门控、RPM 系 UEFI 固件包映射缺失→`edk2-ovmf`/`edk2-aarch64`/`qemu` 自映射、install.sh 硬编码 `compat-2.28`→`HIGH_COMPAT_VER` 动态发现；中低严重度 17 处：`ss -H`/`-K` 旧 iproute2 兼容、update 模式 `.env` 后端复用（M2）、repair 不清 KVM_PORT（M3）、`net-destroy default` 仅 install（M4）、`resolveQEMUCmd` arm64 回退 qemu-kvm、UEFI 探针对齐 arch profile、CPU 旗标 Fields 分词、firewalld Defaults 空 target 回退默认 zone、none 后端不重置缓存、`execCommandWithLogLevel` timeout≤0 语义对齐、OVS systemd 单元名按 PKG_MGR、麒麟桌面回退 apt、AVX2 仅 x86、prepare-bridge.sh 在 firewalld 下不写 iptables INPUT 规则）。详见文档头部 v0.9.11 修订要点与 §12 #AN-#AP。

## 8. 验证与验收

**用例编号映射（v0.9.2 新增）**：本节用例与 §12 运维/后端补充项（#A-#S）的对应关系——

| 用例类别 | 涉及补充项编号 | 关键验收点 |
| --- | --- | --- |
| 环境矩阵 | — | 仅服务器版（麒麟 V10 / openEuler / UOS / Ubuntu 22.04 回归 / CentOS 7 仅 GLIBC） |
| 防火墙基础用例 | — | 开/关/规则 CRUD/端口转发/SPICE 公网暴露/VNC 5900-5999/重启持久化/卸载回滚 |
| zone 绑定与转发全链路 | **#F** | VM 出站/入站端口转发/SPICE 公网/VPC dnsmasq DHCP·DNS（iptables 与 nftables 后端分别验证 + 连续 reload） |
| Docker 共存 | **#D** | `docker -p` 可达性（两后端分别验证）+ docker0 绑 trusted/policy 放行方案 |
| 并发 | **#B** | Enable + 规则 CRUD 并发，无命令冲突/丢规则 |
| 原子性 | **#C** | 非法规则使 Enable 中途失败 → zone 状态回滚，无半成品 |
| 注入防护 | **#S3** | comment/CIDR/`/network/ufw/rule` 恶意字符串必须被白名单拒绝 |
| NM zone 绑定保持 | **#J** | 重启 NM/重连物理连接，qvm-host 绑定不被 NM 覆盖 |
| GLIBC 选优 | **#A** | `readelf` 校验各档；2.17/2.28/2.34/2.35 环境 `--version` 通过；默认档老系统回归零变化 |
| 选优切换冒烟 | — | glibc 达标但二进制缺符号时，`--version` 失败 → 保留原主程序 + 告警 |
| update 非交互 | — | 已安装环境 update 全程无 `read -rp` 阻塞；`.env` KVM_BINARY_TIER 复用 |
| 安装日志与失败定位 | **#K** | 人为失败 → 日志双写 + 失败步骤/原因/日志路径/排查命令 + `print_install_report` |
| 非交互 | **#M** | `CI=1`/无 TTY 全程无 `read -rp`；非法 `.env` 值告警 + 回退 |
| Enable 自检 | **#L** | 自检失败场景 → 失败项清单 + 前端展示 + 可回滚；全部通过 100% 成功 |
| 命令加固 | **#N** | PATH 前置同名脚本不被劫持；dbus 挂死超时返回 `DBUS_ERROR` |
| zone 原子写入 | **#P** | kill -9 中断 → 不残留 `.tmp`/损坏内容；`.bak` 回滚；restorecon 后 `etc_t` |
| ip_backend 探测 | **#O** | iptables/nftables 后端分别上报正确；Tooltip 文案随 `ip_backend` 切换 |
| 组件升级提示 advice | — | firewalld 0.8.x 主机上报 `firewalld_old=true`；glibc < native 需求主机上报 `glibc_low_for_native=true`；前端至多一条 Banner；install 末尾 `print_install_report` 输出对应建议 |
| 错误码与 hint | **#R** | 停 firewalld → `FIREWALLD_NOT_RUNNING` + `LastError` 含修复命令 |
| qvmc-manage.sh | — | firewalld 分支修改端口成功；reload 失败显式报错；`FW_BACKEND` 覆盖生效 |
| glibc 探测口径 | **#G** | 后端与 install.sh 同口径，无漂移 |
| firewalld 服务未运行 | **#H** | Available=true/Active=false；读操作返回空不报错 |
| CI 双档结构 | **#I** | `native-glibc.txt` 待实施产物（§13.10）；zig 0.14.0 支持 2.28 |
| **M7 组件版本检测**（v0.9.4 新增） | — | **见下方 §8.M7 用例块** |
| 安装期健壮性（v0.13 新增） | **#AQ** | **set -e 静默退出回归**：已有安装的 `.env` **存在但无 `INSTALL_DIR=` 行**时执行 update，选完默认 1 后必须继续打印「安装路径 / 安装日志」并进入 STEP 1，不得静默退回 shell；`grep` 无匹配、`ls` 空目录、`find` 无结果等命令替换场景均不中断脚本 |
| 安装性能（v0.13 新增） | **#AS / #AT** | openEuler 新装：STEP 2 无对官方慢源的 `dnf makecache`/`list available` 长卡顿（关键包探测在镜像切换后执行）；`dnf provides` 仅 5 条命令且 offline 跳过；日志 `[TIMING]` 记录每步耗时 + 结尾 `print_step_timing_summary` 倒序汇总 |
| 交互倒计时（v0.13 新增） | **#AR** | 交互菜单 3 秒读秒后无输入自动采用默认值（非 TTY/CI=1 直接默认，`read_tty` 与 `read_user_input` 同款守卫）；用户输入则立即接受并停止倒计时 |

> **M3 前端侧验收状态（v0.9.7 落地，标注 ✅）**：上表 #R / #L / #O / 组件升级提示 advice 行的**前端展示部分**已随 M3 落地并通过本地验证——`go build -o /dev/null ./server` + `go vet ./service/firewall/ ./handler/ ./router/` 通过；`npx tsc -b`、`npm run lint`（oxlint 0 告警）、`npm run build`（vite 打包成功，lottie-web eval 警告为既有依赖）通过；`npm run gen:api` 重生成 endpoints.json（315 端点，含 reset-backend）。**后端侧/运行时验收（真实 firewalld/ufw 环境）仍按本节在目标环境执行**，未以本地构建替代。

> **后端运行时验收方法（v0.9.7 补充，麒麟/openEuler 实机执行 §8 步骤）**：#N 的 `DBUS_ERROR` 构造 `systemctl stop firewalld` 后调用 `firewall-cmd --state`（等待 30s 超时），接口应返回 `error_code=DBUS_ERROR` 且 hint 含 `systemctl restart firewalld`；#R 的 `PERMISSION_DENIED` 可在受限账户下执行状态拉取验证；#O 的 `FIREWALLD_OLD_VERSION` 需 0.8.x 主机触发 `firewalldEnsureForwardPolicy` 告警（0.9+ 主机仅校验不触发，属预期）；`ZONE_NOT_BOUND` 在 trusted 绑定失败场景出现。#H 的「读操作返回空不报错」在 firewalld 停止时验证。组件升级提示 advice 连续两次 `GET /system-info` 应命中 TTL 缓存（`last_check`/探测次数不增长）。

> 注：§12 中 #E（探测缓存失效）、#S1（SELinux restorecon）、#S2（升级/卸载 zone 边界）、#M1（上游合并保护）、#Q（诊断字段扩展）、#S（文档同步）为无独立验收用例的项：#E 验收并入「后端失效自动重测（§4.2 规则 1，运行期卸载/安装后端命令后状态接口自动恢复）」+「Enable 自检 + 手动刷新」路径，#S1/#Q 随 #P/#O 及「组件升级提示 advice」行覆盖（restorecon 后 `etc_t`、`firewall.version`/`nm_managed`/`selinux.mode`/`upgrade_advice` 上报），#S2 并入「update 非交互」行（不触碰 zone）与卸载回滚用例，#M1/#S 为流程项不设 §8 用例。

- 环境矩阵（v0.7 明确：仅服务器版）：麒麟**服务器版** V10（firewalld，x86_64 + aarch64）、openEuler 22.03、openEuler 20.03、UOS 1060、Ubuntu 22.04（回归）、CentOS 7（仅 GLIBC 选优）。**桌面版暂不分析**（麒麟桌面版 V10 = Debian 系/ufw，与 Ubuntu/Debian 走既有路径，不纳入本设计验收）。
- 防火墙用例（两后端全量一致）：开/关防火墙、规则 CRUD（含保护规则禁改删）、端口转发持久放通、SPICE 公网暴露放行/收回、VNC 5900-5999 默认规则、重启后规则持久化、卸载回滚 zone。
- **zone 绑定与转发全链路（v0.5 新增，#F 验收）**：firewalld 后端启用后，必须验证——VM 出站（VPC NAT 上网）、入站端口转发到 VM、SPICE 公网暴露、**VPC dnsmasq DHCP/DNS 入站（UDP 67/53、TCP 53）** 在以下场景全部可达：① qvm-host 绑定上行接口 + br-ovs/vpcsw* 绑 trusted；② nftables 后端下连续 `firewall-cmd --reload` 数次后再验证（覆盖链注册顺序风险，决策 10）；③ 0.8.x（iptables 后端）下依赖面板 iptables FORWARD 的路径。
- **Docker 共存（运维补充项 #D 验收）**：firewalld 后端开启 qvm-host zone DROP 后，`docker run -p` 端口映射可达性（iptables 后端与 nftables 后端分别验证）；不可达时验证 docker0 绑 trusted 或 policy 放行方案生效，且 `DockerCompatible` 字段如实上报。
- **并发（运维补充项 #B 验收）**：任务队列同时触发 Enable + 规则 CRUD，无命令冲突、无 reload 期间丢规则。
- **原子性（运维补充项 #C 验收）**：构造非法规则使 Enable 中途失败，zone 状态回滚到启用前，无半成品。
- **注入防护（运维补充项 #S3 验收，v0.6 扩展）**：comment/CIDR 含单引号、空格、`;` 时，ufw 与 firewalld 后端均正确转义，不产生命令注入；`/network/ufw/rule` 传入恶意 rule 字符串（`;`, `|`, `$()`, 反引号）必须被白名单校验拒绝。
- **NetworkManager zone 绑定保持（v0.6 新增，#J 验收）**：firewalld 后端启用后重启 NetworkManager / 重连物理连接，qvm-host zone 绑定不被 NM 覆盖；含 `nmcli connection modify` 的场景验证连接 zone 持久化。
- GLIBC：`readelf --version-info` 检查各档；在 2.17/2.28/2.34/2.35 环境分别 `kvm-console --version`；确认默认档（2.2.5/2.17）在 Ubuntu 16.04（2.23）/Debian 9（2.24）等老系统仍可运行（回归零变化）；`--high-compat-glibc 2.28` 构建产物可被国产系统选择。
- **选优切换冒烟（v0.4 新增验收）**：构造「glibc 达标但二进制缺符号」场景，`install_files` 切换前 `--version` 失败 → 保留原主程序 + 告警，服务正常启动。
- **update 非交互（v0.4 新增验收）**：已安装环境执行 update，全程无 `read -rp` 阻塞；`.env` KVM_BINARY_TIER 被复用；glibc 变化时重新评估并仅提示一次。
- 交互：安装日志出现「检测到宿主机防火墙后端: firewalld」「推荐二进制档位」；`--high-compat-glibc 2.28` 构建产物可被国产系统选择，老系统（glibc < 2.28）回落默认档。
- **安装日志与失败定位（v0.8，#K 验收）**：任意步骤人为制造失败（如错误 glibc、端口被占用），日志文件 `$INSTALL_DIR/logs/install-*.log` 完整双写；失败输出含「失败步骤 / 原因 / 日志路径 / 排查命令」；成功安装末尾 `print_install_report` 完整输出后端、档位与原因、glibc、CPU、SELinux、降级项、日志路径（v0.9.8 补：降级项 `DEGRADED_NOTES` 由「后端 none / 档位冒烟回退」累积写入，并输出组件升级提示三行：firewalld <0.9 / glibc 未达 native / SELinux Enforcing）。
- **非交互（v0.8，#M 验收）**：`CI=1` 或管道输入（无 TTY）下安装全程无 `read -rp` 阻塞，自动采用推荐档位并在日志标注；`FW_BACKEND=none` 显式注入时后端为 none 且日志注明来自覆盖；`.env` 写入 `KVM_BINARY_TIER=../../..` 等非法值时告警并回退默认档，不执行任意路径。
- **Enable 自检（v0.8，#L 验收）**：构造「br-ovs 未绑 trusted」等自检失败场景，Enable 返回失败 + 失败项清单，前端展示并可回滚；全部通过时进度 100% 成功。
- **命令加固（v0.8，#N 验收）**：构造 `/usr/bin/firewall-cmd` 被 PATH 前置同名脚本时，后端仍调用绝对路径（不被劫持）；构造 `firewall-cmd` 挂死（dbus stop 后调用），命令在超时内返回 `DBUS_ERROR`，面板不阻塞。
- **zone 原子写入（v0.8，#P 验收）**：模拟写入中断（kill -9 后端进程），`/etc/firewalld/zones/qvm-host.xml` 不残留 `.tmp`/损坏内容；首次失败后无 zone 文件，已有旧文件失败后有 `.bak` 可回滚；SELinux restorecon 在替换后文件上生效（`ls -Z` 为 `etc_t`）。
- **ip_backend 探测与转发判据（v0.8，#O 验收）**：iptables 后端（legacy）与 nftables 后端（nf_tables）主机分别上报 `firewall.ip_backend` 正确；`default_routed`「未管理」Tooltip 文案随 `ip_backend` 切换；legacy 下面板 iptables FORWARD 路径可靠，nf_tables 下以 zone/policy 为准。
- **错误码与 hint（v0.8，#R 验收）**：停掉 firewalld 服务后拉取状态/操作规则，接口返回 `error_code=FIREWALLD_NOT_RUNNING` 且 `LastError` 含 `systemctl start firewalld`；前端渲染可操作 hint。
- **qvmc-manage.sh（v0.8 验收）**：firewalld 分支修改端口成功且 reload 失败时显式报错（不静默）；`FW_BACKEND` 环境变量覆盖生效；端口非数字输入仍被拒绝（回归 qvmc-manage.sh:235）。

**M7 组件版本检测用例块（v0.9.4 新增，对应 §5.11）**：

- **构建期 manifest 生成（§5.11.3）**：`bash build.sh` 完成后，`release/kvm-console-linux-${TARGET_ARCH}/compat-manifest.json` 存在且 JSON 格式合法；`manifest_version=1.0`；`binaries` 段的 `max_glibc` 与 `--compat-glibc`/`--high-compat-glibc` 实际值一致；`system_requirements` 段含 18 项组件且 `min_version` 与 §5.11.2 表格一致；`os_compat` 段含 7 个发行版条目。
- **构建期 native 档口径**：`native-glibc.txt` 存在时 `binaries.kvm-console-native.min_glibc` 读取该值；不存在时写 `"pending"` 且 build.sh 输出 warn（不阻断构建）。
- **部署期 manifest 加载（§5.11.4）**：install.sh 执行 `check_component_versions` 时打印「已加载兼容性清单: <path>」；manifest 缺失时打印 warn「未找到 compat-manifest.json，使用内置默认版本阈值」并继续（不阻塞）。
- **部署期版本检测 — critical 场景**：在 glibc 2.28 + firewalld 0.8.0 主机上执行 install.sh → `check_component_versions` 检测到 firewalld 0.8.0 < 0.9.0 推荐 but ≥ 0.4.0 最低（v0.10 下调）→ 输出 warning（不阻断）；若 firewalld 0.3.0（< 0.4.0 最低）→ 输出 critical + 中止安装 + 提示升级命令；`--skip-version-check` 跳过则继续。
- **部署期版本检测 — warning 场景**：交互模式下检测到 warning → 提示「是否继续安装? [Y/n]」；输入 n → 退出码 0 + 提示「请升级组件后重试」；输入 Y 或回车 → 继续；`NON_INTERACTIVE=1` → 默认继续 + 日志标注。
- **部署期版本检测 — 多工具回退**：genisoimage 缺失但 xorriso 可用 → 该项标记 healthy（任一可用即可）；mkisofs 可用同理。
- **部署期版本检测 — 架构专属**：x86_64 主机检测 edk2-ovmf（不检测 edk2-aarch64）；aarch64 主机检测 edk2-aarch64；缺失 → warning（UEFI VM 创建不可用）。
- **部署期版本检测 — 探测超时**：firewalld 主进程挂死时 `timeout 5s firewall-cmd --version` 超时 → 该项标记 warning + 提示「无法解析版本，请人工确认」，不阻塞安装。
- **部署期汇总报告**：`print_install_report` 输出含「组件版本检测」段：总计 N 项 / 健康 N 项 / 警告 N 项 / 关键不满足 N 项；与 §5.11.4 模板一致。
- **运行期 component_health 探测（§5.11.5）**：`GET /system-info` 返回 `component_health` 字段（v0.9.3 收敛挂载点）；`overall` 字段为 healthy/warning/critical 三值之一；`items` 数组含 18 项（或按架构缺失 edk2 对应项）；每项含 `component`/`category`/`status`/`current_version`/`required_version`/`recommended_version`/`message`/`upgrade_hint`（critical/warning 必含 upgrade_hint）。
- **运行期探测缓存**：连续两次 `GET /system-info` → 第二次 `last_check` 不变（缓存命中）；`POST /settings/diagnostics/refresh`（**已实施 M7.2**）→ 缓存重置 + `last_check` 更新。
- **运行期版本解析失败降级**：某组件 `--version` 输出格式异常（如国产系统定制前缀）→ `current_version="unknown"` + `status=warning` + `message="无法解析版本，请人工确认"`，不报 critical。
- **前端诊断页展示（§5.11.5）**：系统设置 → 诊断页存在「组件版本健康度」卡片；按 category 分三区（核心/磁盘/诊断）；每行含状态 Tag + 组件名 + 版本 + 状态文案；warning/critical 行展示升级命令且支持一键复制（`<Typography.Text copyable>`）；「导出报告」按钮下载 JSON 含 18 项完整检测项。
- **前端深色模式**：状态 Tag 颜色在 `body[theme-mode='dark']` 下不刺眼（green/orange/red/grey 遵循 AGENTS.md 深色模式约定）。
- **manifest 缺失回退**：后端 `go:embed` 失败或 manifest 文件为空 → `component_health.items` 中 `required_version`/`recommended_version` 字段为空字符串 + `message` 标注「版本阈值未加载，仅展示当前版本」；不报错。
- **18 项组件探测覆盖**：在 openEuler 22.03（firewalld）+ Ubuntu 22.04（ufw）两台主机分别验证 `component_health.items` 数组长度 ≥ 17（edk2-aarch64 在 x86 不出现）；每项 `current_version` 非空（除 kvm_stat 可选缺失）。

**M8 竞品差异吸收用例块（v0.10 新增，对应 §14，✅ v0.11 已实施 M8.1/M8.2/M8.4/M8.8/M8.12）**：

- **P0-2 firewalld 版本三档（M8.2，✅ 已实施）**：CentOS 7（0.4.4.4）上 `check_component_versions` 不再报 critical（min=0.4.0）；面板点「启用宿主机防火墙」→ 返回 `FirewalldOldVersion`（error_code + hint「请升级 firewalld ≥ 0.6 或使用发行版 iptables-service」），qvm-host zone 文件不被写入；读操作（状态/规则列表）仍可用。
- **P0-3 glibc 2.17 实测（M8.3，✅ 已实施）**：`.github/workflows/build.yml` `verify-centos7-glibc217` job 在 `docker run centos:7` 下执行 compat 二进制 `--version` + `--smoke-selfcheck`（别名 `--db-probe`）通过；install.sh `select_binary_smoke_test` 的 `--smoke-selfcheck` 在 glibc 2.17 环境返回 0。
- **P0-1 CPU 厂商（M8.1，✅ 已实施）**：海光/飞腾/鲲鹏/兆芯主机安装时 `.env DOMESTIC_CPU_VENDOR` 写入正确；component_health 上报 `cpu_vendor` 白名单命中/未命中（warning）。
- **P1-4 stage 持久化（M8.4，✅ 已实施）**：人为在步骤 N 失败 → `${INSTALL_DIR}/.install_state/stage=N` 存在且 `last_error` 记录；`install.sh --resume` 从步骤 N+1 继续；`.env`/`/etc/kvm-console/**` 权限符合 600/700。
- **P1-5 migrations（M8.5，✅ 已实施）**：`migrations/0001_scheduler_events_vm_status_index.go` 在含旧 schema 的库上正确执行且不重复（schema_migrations 表跳过已应用版本）；纯增量 DDL，无数据丢失。
- **P1-6 回滚（M8.6，✅ 已实施）**：update 前 `.release_backup/{01|02|03}` 生成且保留最近 3 份；`qvmc-manage.sh` 回滚菜单从备份槽位还原后服务正常（数据/配置不受影响）。
- **P1-7 包校验（M8.7，✅ 已实施）**：`SHA256SUMS` 校验失败 → error 退出中止安装；`.sha256` 缺失仅跳过校验不阻断。
- **P2-8 bash 审计/kdump（M8.8，✅ 已实施）**：安装后 `/root/.bashrc` 含 PROMPT_COMMAND 审计；`chattr +a /var/log/bash.log` 生效（或降级 chmod 622）；裸金属无 crashkernel 时 print_install_report 出现 kdump 建议。
- **P2-9 watchdog/大页（M8.9，✅ 已实施）**：guest-agent 连续 3 次无响应 → 看门狗硬重置（HookResetVM）+ `VMWatchdogEvent` 审计入库；内存 ≥128GB 且 HugePages_Total=0 → component_health warning「建议开启大页」。
- **P2-10 健康探针（M8.10，✅ 已实施）**：`${KVM_HEALTH_DIR:-/opt/QVMConsole/.health}/latest.json` 每分钟更新；停 libvirtd → Dashboard 灯变黄；面板停 → 前端轮询超时报红。
- **P3-11 支持等级（M8.11，✅ 已实施）**：`compat-manifest.json` os_compat 各条目含 `support_level` 与 `certified_hardware`；`support_level=C` 发行版安装时 warn「理论兼容，生产请升级到认证基线」。
- **P3-12 镜像源/offline（M8.12，✅ 已实施）**：`DEPS_MIRROR=offline` 时跳过 `apt/dnf install` 仅扫缺包汇总；专网主机安装成功且 report 列出缺失包与内网源提示。

## 9. 兼容性矩阵

| 系统 | 包管理器 | 防火墙后端 | 二进制档位 | SELinux |
| --- | --- | --- | --- | --- |
| Ubuntu 22.04 | apt | ufw | native（glibc 2.35 ≥ 2.35 阈值） | AppArmor |
| Debian 11 | apt | ufw | compat-2.28（glibc 2.31 < 2.35） | AppArmor |
| 麒麟服务器版 V10 | dnf | firewalld | compat-2.28（glibc 2.28+） | Enforcing（已有 setup_selinux） |
| openEuler 22.03 | dnf | firewalld | compat-2.28（glibc 2.34） | Enforcing |
| openEuler 20.03 | dnf | firewalld | compat-2.28（glibc 2.28） | Enforcing |
| UOS 1060 | apt | ufw | compat-2.28（glibc 2.28） | AppArmor |

> 注 1：UOS 服务器版基于 Debian，含 ufw；如个别版本缺失，按探测顺序自动切到 firewalld，并在面板/安装期**展示实际选中的后端供用户知情**（评审结论 #4）。
> 注 2：qvmc-manage.sh 修改端口时 firewalld 分支仅处理 TCP，与 ufw 分支一致，不补 UDP（评审结论 #5）。
> 注 3（v0.4 修正）：native 可用性取决于 `native-glibc.txt`（CI 构建机 glibc）。**CI 构建镜像必须固定为 glibc 2.35 档（如 ubuntu-22.04），不得用随动的 ubuntu-latest**——否则 native 需求漂移到 2.39，既有 Ubuntu 22.04/Debian 12 用户会被静默降级到 compat（违反目标 6）。openEuler 22.03 glibc 2.34 < 2.35，按阈值走 compat-2.28，这是**预期正确行为**（见 #A）。**v0.7 实施要点：build.yml 现状 amd64=`ubuntu-latest`（=2.39）、arm64=`ubuntu-24.04-arm`（=2.39），实施 #A 时须同步固定 runner（amd64→ubuntu-22.04；arm64 固定 2.35 档），详见 §4.3 实施要点。**

## 10. 风险与回滚

| 风险 | 缓解 | 回滚 |
| --- | --- | --- |
| firewalld 与既有 iptables 面板规则冲突 | 专用 zone + 显式接口/源绑定；VM 桥绑 trusted、qvm-host 只绑上行 | `--delete-zone=qvm-host` + reload |
| **firewalld zone 未绑定导致 DROP 惰性（安全假象）** | Enable 必须完成绑定序列，未绑定视为失败回滚（§5.1 决策 10） | 回滚 zone 文件 + reload |
| **nftables 后端链求值顺序：reload 后 zone DROP 先于面板 iptables 规则，丢转发流量** | VM 转发依赖 zone/policy 绑定而非面板 iptables；连续 reload 复验（§8）；0.8.x 走 iptables 后端路径 | 调整 zone/policy 绑定 |
| **VPC dnsmasq DHCP/DNS 入站被 firewalld 拦截（#F）** | VM 桥绑 trusted zone 天然放行；install.sh 迁移清理旧 iptables 规则 | 移除 trusted 绑定恢复原状 |
| 旧版 firewalld 无 policy（0.8.x） | 探测能力降级字段「未管理」；转发依赖面板 iptables（iptables 后端可靠） | 字段空 + 前端提示 |
| 兼容版 GLIBC 提高后 CentOS7 无法运行 | v0.4 起默认 compat 基线不变（2.2.5/2.17），CentOS 7 用默认档即可；2.28 仅作可选高档 | 默认发布包始终可运行 |
| 后端抽象引入回归 | ufw 后端为原逻辑迁移，M0 即做回归 | M0 单独合入 |
| install.sh 硬编码 2.34 误切 native（#A） | 废弃硬编码，读 `native-glibc.txt` 比较 | M0.5 优先实施 |
| firewalld Enable 中途失败留半成品（#C） | `--check-config` 预检 + 失败还原旧 zone | 手动删 zone XML + reload |
| Docker 端口映射被拦截（#D） | v0.5 修正：`docker -p` 走 FORWARD 不经 INPUT，iptables 后端通常不受影响；nftables 后端验证 reload 后链顺序，不可达时 docker0 绑 trusted / policy 放行 | 调整 zone/policy 放行规则 |
| SELinux Enforcing 拒读 zone 文件（#S1） | `restorecon` 单文件 `qvm-host.xml` | — |
| 命令注入（#S3） | `utils.ShellSingleQuote` 转义，ufw 一并修 | — |
| glibc 探测两处口径漂移（#G） | 后端与 install.sh 统一 `ldd --version`/`getconf` 口径（§4.1） | — |
| NM 重连覆盖 firewalld zone 绑定（#J） | `nmcli connection.modify connection.zone` 同步 + 验收「NM 重启后绑定保持」 | 重跑 Enable 绑定序列 |
| ManageUFWRule 现存命令注入面（#S3 扩展） | 新后端结构化 argv + rule 白名单校验（§5.4） | 拒绝非法输入 |
| firewall-cmd/dbus 挂死导致面板阻塞（v0.8，#N） | 命令固定绝对路径 + 全部带超时（60s+），超时返回 `DBUS_ERROR` + hint | 重启 firewalld 服务 |
| `.env` 篡改导致路径注入/错误档位（v0.8，#M） | `KVM_BINARY_TIER`/`FW_BACKEND` 白名单校验 + 主程序名固定集合 | 回退默认档/自动探测 |
| iptables/nftables 后端误判导致转发依赖错误（v0.8，#O） | `iptables -V` 显式探测并暴露 `ip_backend`；nf_tables 一律依赖 zone/policy | 调整 zone/policy 绑定 |
| zone 文件写一半崩溃损坏（v0.8，#P） | 临时文件 + rename 原子替换 + `.bak` 备份；失败删除 tmp | `mv .bak` 还原 + reload |
| 安装失败无日志/无法定位（v0.8，#K） | 全局日志双写 + `step` 序号 + 失败步骤/原因/排查命令输出 | 查看 install-*.log |
| **上游合并覆盖本仓库独有文件（新增 v0.4）** | `backend*.go`、国产化新增文件（能力探测的扩展字段相关 handler/service 等）是本仓库独立实现，与上游 `server/` 合并冲突时保留本仓库版本后手动处理；上游新增 `ufw` 硬编码调用点时，合并后必须回归后端抽象（M0 验收） | 合并前备份分支 |

## 11. 评审结论（2026-07-31 已确认）

| # | 议题 | 结论 |
| --- | --- | --- |
| 1 | firewalld 旧版转发控制（openEuler 20.03 / 0.8.x） | **暂不修改，保持默认**：不做 iptables FORWARD 兼容实现；0.8.x 无 policy 时 `default_routed` 探测不到则降级为「未管理」，前端提示即可。VM 转发在 iptables 后端下由面板直写 iptables（`-I FORWARD 1` 先于 firewalld 链）保证；0.9+ 走 zone/policy 绑定（#F） |
| 2 | `kvm-console-compat-2.17` / `kvm-console-compat-2.28` 是否进发布包 | **均不进默认包**：默认包只含 compat 默认档（amd64=2.2.5 / arm64=2.17，兼容面最广，任何现代 Linux 可运行）。需要 2.28 高档的发行方通过 `bash build.sh --high-compat-glibc 2.28` 构建追加；CentOS 7 等更老场景继续用默认档 |
| 3 | 能力探测接口（v0.9.3 收敛为扩展现有 `GET /system-info`）是否纳入 API Key 白名单 | **允许（v0.9.3 自然覆盖）**：复用现有 `authorized` 分组只读接口，无需新列入白名单——现有 `/system-info` 已兼容 API Key，增量字段一并继承 |
| 4 | UOS 个别版本无 ufw 时后端归属 | **接受探测顺序自动切到 firewalld**，但需**交互确认**（面板/安装期展示实际选中的后端，供用户知情） |
| 5 | qvmc-manage.sh 修改端口是否补 UDP | **不补**：与现有 ufw 分支保持一致，仅处理 TCP |

以上决策已固化到 §4.3 / §5.1 / §5.8 / §6 / §9 对应小节。

## 12. 运维/后端补充项（2026-07-31 评审追加，v0.8 增至 23 项，v0.9.4 增至 24 项，v0.9.9 评审修复增至 29 项，v0.9.10 增至 39 项，v0.9.11 增至 43 项，v0.13 增至 47 项）

以下为后端/运维视角评审补充的设计项，均已按编号固化到正文对应小节。

| # | 补充项 | 严重度 | 固化位置 |
| --- | --- | --- | --- |
| A | `native-glibc.txt` 取代 install.sh 2.34 硬编码（正确性修正）+ CI native 镜像固定 glibc 2.35 档（v0.7：实施时同步改 build.yml runner——amd64 `ubuntu-latest`→`ubuntu-22.04`、arm64 固定 2.35 档，见 §4.3 实施要点） | **高（优先实施 M0.5）** | §4.3 / §7 / §9 |
| B | backend 级互斥锁串行化防火墙命令（taskqueue 并发踩踏） | 中 | §4.2 |
| C | firewalld Enable 原子性：`--check-config` 预检 + 失败回滚旧 zone | 中 | §5.1 / §8 |
| D | Docker 兼容字段不得对 firewalld 无条件为 true，需探测验证（v0.4 修正：焦点在 FORWARD 策略与 reload 对 DOCKER 链影响） | 中 | §5.1 / §8 |
| E | 探测缓存失效策略：规则 1 后端失效自动重测（`resolveBackend()`，v0.9.8 落地）+ 规则 2 面板手动刷新 | 低 | §4.2 |
| S1 | SELinux：zone 文件 restorecon 单文件（`qvm-host.xml`，`etc_t`） | 低 | §5.1 |
| S2 | 升级不触碰 zone、卸载清理 zone 的边界 | 低 | §6.4 / §7 |
| S3 | rich-rule/ufw comment 命令注入转义（`utils.ShellSingleQuote`） | 中 | §5.1 / §8 / §10 |
| M1 | 上游合并保护本仓库独有后端文件（`backend*.go` 等），避免上游覆盖国产化适配 | 低 | §10 |
| F | **firewalld zone 绑定 + VM 转发可靠性（v0.5 新增）**：`qvm-host` 必须显式绑定上行接口/源（否则 DROP 惰性）；`br-ovs`/`vpcsw*` 绑 `trusted`（ACCEPT）放行 VM 转发与 dnsmasq 入站；≥0.9 建 policy 放行 uplink→VM 转发；**nftables 后端下不得依赖面板 iptables `-I FORWARD 1` 顺序** | **高** | §5.1 / §5.8 / §6.4 / §8 / §10 |
| G | **系统信息 glibc 探测口径统一**：后端 `/system-info` 扩展字段与 install.sh 同用 `ldd --version` 首行末 token / `getconf GNU_LIBC_VERSION` 回退，防两处漂移 | 中 | §4.1 |
| H | **firewalld 服务未运行读边界**：Available=true 但 Active=false；`ListRules/Defaults` 返回空不报错；`Enable()` 负责启动 | 低 | §5.1 / §6.4 |
| I | **CI 双档结构与 zig 版本澄清（v0.6）**：CI 已产 compat+native 双档二进制（`native-glibc.txt` 文件为待实施产物，见 §13.10）；zig 0.14.0 已支持 2.28 目标，高兼容档无需升级 zig | 低 | §5.9 |
| J | **NetworkManager 与 zone 绑定交互（v0.6）**：`nmcli connection.modify connection.zone` 同步，防 NM 覆盖 qvm-host 绑定 | 中 | §5.1 / §8 / §10 |
| K | **安装日志与失败定位（v0.8）**：全局 `install-*.log` 双写 + `step` 序号包装 + 失败步骤/原因/日志路径/排查命令 + `print_install_report` 总结 | 中 | §5.8 / §6.1 / §8 / §10 |
| L | **预检与 Enable 自检（v0.8）**：`precheck_domestic`（端口占用/多防火墙/NM 环境）+ Enable 后自检清单（zone 激活/绑定/保护端口/dnsmasq），失败项前端展示并可回滚 | 中 | §5.1 / §5.8 / §6.2 / §8 |
| M | **非交互与环境变量覆盖（v0.8）**：`FW_BACKEND`/`KVM_BINARY_TIER` 注入覆盖（探测顺序步骤 0）、非 TTY/`CI=1` 跳过 `read`、`.env` 值白名单校验防路径注入 | 中 | §4.2 / §4.3 / §5.8 / §5.10 / §8 |
| N | **命令执行加固（v0.8）**：命令绝对路径（`exec.LookPath` 缓存防 PATH 劫持）+ 全部带超时（防 dbus 挂死）+ 禁用 `--command=`/`--direct` | 高 | §4.2 / §6.4 / §8 / §10 |
| O | **iptables/nftables 后端探测（v0.8）**：`iptables -V` 探测 `ip_backend`，作为 §5.1 决策 10「面板 iptables 是否可依赖」落地判据（legacy 可靠 / nf_tables 依赖 zone/policy） | **高** | §4.2 / §5.1 / §5.7 / §8 / §10 |
| P | **zone 文件原子写入（v0.8）**：临时文件 + rename + `.bak` 备份，崩溃安全；SELinux restorecon 时序后移至替换后 | 中 | §5.1 / §8 / §10 |
| Q | **系统信息诊断字段扩展（v0.8）**：`firewall.version` / `ip_backend` / `nm_managed`、`selinux.mode`，一次调用完成故障诊断；**v0.9.3 追加 `firewall.upgrade_advice`**（firewalld 过旧 / glibc 可用 native / SELinux Enforcing），供安装期与面板 Banner 复用 | 低 | §4.1 |
| R | **后端错误结构化（v0.8）**：`error_code` + 可操作 `hint`（FIREWALLD_NOT_RUNNING 等），`LastError` 兼容保留，前端按码分支 | 中 | §5.1 / §5.7 / §6.2 / §8 |
| S | **文档同步（v0.8）**：M3 更新 `firewall-page.md`（UFW→后端措辞/none Banner/未管理 Tag）；M6 更新 `build-compatibility.md`（`--high-compat-glibc` + `verify_compat_glibc` readelf **v0.9.3 硬校验**：缺 binutils 时构建失败并给出安装命令） | 低 | §5.7 / §5.9 / §7 |
| **T**（v0.9.4 新增） | **组件版本检测与升级提示闭环**：build.sh 生成 `compat-manifest.json`（18 项组件阈值 + 7 个 os_compat 条目）+ install.sh `check_component_versions()`（critical 中止/warning 确认/healthy 通过 + `--skip-version-check` 跳过）+ 后端 `component_health` 探测（`go:embed` manifest + `sync.Once` 缓存 + `POST /settings/diagnostics/refresh` 重置，**挂载于 `/system-info`**）+ 前端诊断页「组件版本健康度」卡片（3 区分类展示 + 升级命令一键复制 + 导出报告）+ DEPENDENCIES.md 补 18 项版本阈值表。**仅检测 + 提示，不自动升级**（决策 1） | 中 | §5.11 / §6.1 / §7 (M7) / §8 (M7 用例块) |
| **U**（v0.9.9 评审修复新增） | **firewalld Enable 保留已有放行规则（C1 修复）**：重建 `qvm-host` zone 前 `captureFirewalldZone()` 捕获端口/服务/来源/富规则，写回骨架后 `restoreFirewalldZoneContent()` 恢复，消除「启用防火墙即擦除 SSH/面板端口 → 远程失守」；自检增「受保护端口必须仍在 qvm-host 放行」断言 | **高** | §5.1 / §5.3 / §8 |
| **V**（v0.9.9 评审修复新增） | **组件版本阈值单一来源 + 安装提示包名修正（H1/H2）**：阈值唯一维护点在 build.sh `COMPONENT_REQ_*` 变量（同源产出 `compat-manifest.json` 与新增 `versions.conf`）；install.sh 改读 `versions.conf`（纯 shell，去掉 python3 依赖，min/rec 均读取，glibc 按架构）；qemu-kvm 安装提示改用真实包名（apt `qemu-system-x86`/`qemu-system-arm`、RPM `qemu-kvm`），Go `componentPkgMap` dnf 包名同步（qemu-kvm、tc→iproute） | 中 | §5.11 / §6.1 / §7 (M7) |
| **W**（v0.9.9 评审修复新增） | **组件健康度刷新单飞（H3）**：`RefreshComponentHealth()` 带 5s 冷却，冷却期内复用缓存，防前端高频「刷新」在低配国产机上多次触发 19 组件全量探测 | 低 | §5.11.5 |
| **X**（v0.9.9 评审修复新增） | **防火墙后端漂移巡检（M1）**：`StartFirewallDriftMonitor()` 每日强制重测后端，发现「命令存在但 firewalld 服务长期停止且面板曾启用（qvm-host zone 存在）」时日志告警；不改变后端解析规则（#H） | 低 | §4.2 / §5.1 |
| **Y**（v0.9.9 评审修复新增） | **宿主机规则 ID 与去重口径统一（M3）**：`hostFirewallRuleID` 不再含备注，与 `mergeHostFirewallRules` 去重键一致（规格等价即同一条规则），备注为元数据不参与身份判定 | 低 | §5.3 |
| **Z**（v0.9.9 评审修复新增） | **组件版本检测批量缺失短路（L2）**：install.sh `check_component_versions` 先一次性 `command -v` 探测全部检测命令（结果入 `PRESENT_CMDS`），缺失命令直接短路返回，不再逐个走 `timeout 5s` 探针，避免串行检测逼近 60×5s 上界 | 低 | §5.11.4 |
| **AA**（v0.9.9 评审修复新增） | **firewalld reload 失败重试（M2）**：`firewalldReload()` 对临时性失败（dbus/daemon 抖动）重试 3 次（500ms 起退避），避免 `--add-port` 已写入永久配置但 reload 失败导致运行态与持久态不一致 | 低 | §5.1 / §5.3 |
| **AB**（v0.9.10 产品评审批量修复新增） | **/system-info 角色收敛（§3.3）**：`glibc/cpu/selinux/firewall/component_health` 改为管理员专属字段，普通用户仅返回基础信息；`kernel/qemu/libvirt/qemu_spice/arch` 保留公开（关于页与新建 VM 表单依赖，属既有产品决策） | 中 | §13.10 |
| **AC**（v0.9.10 产品评审批量修复新增） | **/system-info 聚合 TTL 缓存**：整包探测缓存 30s（缓存全冷时单次可派生 ~30 个子进程），页面切换不重复触发；探测在锁内串行，避免并发重复派生子进程 | 中 | §13.10 |
| **AD**（v0.9.10 产品评审批量修复新增） | **QEMU 版本/SPICE 按架构探测**：`resolveQEMUCmd()` 复用 component_health 口径（aarch64 → `qemu-system-aarch64`），修复 ARM 主机上 qemu 恒 "-"、SPICE 恒 false | 高 | §13.10 |
| **AE**（v0.9.10 产品评审批量修复新增） | **version.go 子进程统一超时 + 路径缓存**：`getKernelVersion/getLibvirtVersion/getQEMUVersion/CheckQEMUSPICESupport` 改走 `utils.ExecCommandWithTimeout(5s)`，消除裸 `exec.Command` 挂起请求风险 | 中 | §13.10 |
| **AF**（v0.9.10 产品评审批量修复新增） | **component_health 缓存 TTL（§4.3）**：`healthCacheTTL=30min` 自动失效，组件升级后无需手动刷新即反映，与 advice 10min TTL 口径对齐 | 低 | §5.11.5 |
| **AG**（v0.9.10 产品评审批量修复新增） | **glibc 探测单一来源（§4.5）**：新增 `utils.DetectGlibcVersion()`（含 `ValidGlibcToken`），firewall/advice.go 与 diagnostics/component_health.go 共用，消除重复实现 | 低 | §4.1 / §5.11.5 |
| **AH**（v0.9.10 产品评审批量修复新增） | **update 模式 native 可行提示（§2.5）**：`NATIVE_FEASIBLE` 在档位分支前计算，`print_install_report` 在「当前 glibc 已满足 native 需求但沿用 compat 档」时输出优化提示 | 低 | §4.3 / §5.8 |
| **AI**（v0.9.10 产品评审批量修复新增） | **命令路径缓存单一来源（§2.3）**：新增 `utils.LookupCmdPath()`，firewall `firewallCommandPath` 与 diagnostics `detectVersion` 共用，消除 19 组件探测重复 LookPath | 低 | §4.2 / §5.11.5 |
| **AJ**（v0.9.10 产品评审批量修复新增） | **backendExec 可安全调用清单文档化（§2.4）**：注释明确「锁内可安全调用的无锁原语清单」与「严禁调用的公共方法清单」，降低未来误踩 sync.Mutex 重入死锁 | 低 | §4.2 / #B |
| **AK**（v0.9.10 产品评审批量修复新增） | **build.sh build_host JSON 转义（§3.2）**：写入 manifest 前转义反斜杠/双引号，防 hostname 含特殊字符破坏 JSON 结构 | 低 | §5.11.3 |
| **AL**（v0.9.10 产品评审批量修复新增） | **install.sh versions.conf 单循环解析（§4.4）**：合并「读入 manifest_data + 二次解析」两段为单循环直接覆盖阈值，消除中间态易误改 | 低 | §5.11.4 |
| **AM**（v0.9.10 产品评审批量修复新增） | **前端健康度刷新冷却提示**：`last_check` 未变即 5s 冷却命中，提示「检测进行中」而非误导「已刷新」 | 低 | §5.11.5 |
| **AN**（v0.9.11 审计修复新增） | **firewalld deny 无来源规则语义修复（审计 H1）**：`firewalldAddRuleToZone` 无来源 deny 此前落 `--add-port`（放行），与 ufw deny 语义相反；现 deny 规则（有无来源均）统一走 rich-rule（reject），`buildFirewalldRichRule` 来源可选；`parseFirewalldRichRuleLine` 动作 token 行尾匹配 | **高** | §5.1 / §5.3 |
| **AO**（v0.9.11 审计修复新增） | **firewalld <0.7 优雅降级（审计 H2/H3）**：`firewalldEnsureZoneExists`/`firewalldDeleteZone` 对 <0.7 原子写/删 `qvm-host.xml`（daemon 运行才 reload）；`Disable()` 的 `--delete-policy` 加版本门控，消除半失败态 | **高** | §5.1 / §6.4 |
| **AP**（v0.9.11 审计修复新增） | **高兼容档版本动态发现（审计 build.sh H1）**：install.sh `select_binary_tier`/`install_files`/`write_env` 从发行包扫描 `kvm-console-compat-{VER}` 取最高档，取代 `compat-2.28` 硬编码，与 build.sh `--high-compat-glibc` 任意值对齐；RPM 系补 `edk2-ovmf`/`edk2-aarch64`/`qemu` 包映射（审计 H4） | **高** | §4.3 / §5.8 |
| **AQ**（v0.13 安装期健壮性新增） | **`set -euo pipefail` 下 `$(grep|ls|find|cut|tr...)` 无兜底的静默退出脚枪（现场 P1 级 bug）**：install.sh:6 全局 `set -Eeuo pipefail` 开启后，任何命令替换 `var=$(grep ... | cut | tr)` 在**上游 grep/find/ls 无匹配退出码 1** 时，pipefail 把整条管道判为失败 → `set -e` 直接结束脚本且**无任何错误输出**。现场表现：update 模式 `choose_install_dir` 读已有 `.env`（`grep -m1 '^INSTALL_DIR='`），当 `.env` 存在但无该行时，选完交互默认 1（更新）后**静默退回 shell**，不打印「安装路径 / 安装日志」，用户误以为「倒计时超时后退出」。**修复（v0.13）**：10 个代码位置共 14 条命令替换内统一加 `|| true`（install.sh:568-570、604-605、1086、2423、2623、2943、3278、3768、3929、3931），空值交给下游 `[ -z ]`/默认值逻辑处理，行为不变仅不再被 `set -e` 误杀。**工程约束（写死）**：install.sh 中所有 `$(...)` 管道命令替换必须带 `|| true` 或置于 `if`/`&&` 条件中，禁止裸 `var=$(grep ... | ...)`——新增代码评审项 | **高（现场静默失败）** | §5.8 / §8（新增回归用例）/ §13.10 |
| **AR**（v0.13 交互体验新增） | **交互读取 3 秒读秒倒计时 + 步骤耗时统计**：`read_user_input`/`read_tty` 由阻塞式 `read -rp` 改为 `countdown_read_line`（`QVM_READ_TIMEOUT=3`，每秒刷新剩余秒数，`/dev/tty` 隔离读取防 stdout 回灌污染；超时返回 1 由调用方 `${var:-默认}` 落默认值）；`step()` 每步计时（`date +%s.%N`，`[TIMING]` 双写日志）+ 结尾 `print_step_timing_summary` 按耗时倒序汇总 + 本次流程总耗时（定位耗时最久的环节，配合 M8.12 镜像源优化排查「安装卡在某步」）。非 TTY/CI=1 走既有直接默认逻辑（#M）不倒计时；**v0.13 核对**：`read_tty` 补上与 `read_user_input` 一致的非交互守卫（此前网页终端/管道场景每个提示硬等 5 秒，实测 5 次「等待超时」浪费 25s），`QVM_READ_TIMEOUT` 5→3s | 低 | §5.8 / §6.1 / §12 #K |
| **AS**（v0.13 安装性能新增） | **openEuler 仓库校验前置慢源卡顿修复（配合 M8.12）**：`enable_openeuler_repos()`（`check_os` 阶段）原对官方 `repo.openeuler.org`（metalink 未禁用）执行 `dnf makecache` + 5× `dnf list available`，早于镜像切换 → 安装前期可达数分钟卡顿。**修复（v0.13）**：从 `enable_openeuler_repos` 移除 makecache/可用性探测，新增 `probe_critical_rpm_packages()`（install.sh:665）在 `check_and_install_deps` 的 `apply_system_mirror` **之后**调用——此时 baseurl 已切 nju/清华/阿里且 metalink 已注释，命中快源；`system`/`offline` 直接跳过。`enable_openeuler_repos` 仅保留仓库文件清理 + EPOL/everything Section 补写（curl `-m 10` 有界探测） | 中 | §5.8 / §14.4 M8.12 |
| **AT**（v0.13 安装性能新增） | **`dnf provides` 逐命令回退收敛**：`install_bundled_packages` Phase 1 原对 16 条命令逐个 `timeout 30s dnf provides`（最坏 ~8 分钟，命中慢源时呈「卡死」），且与 Phase 2 捆绑包提取（`lgft_bins` 覆盖全部 virt-* 工具）职责重复。**修复（v0.13）**：收敛回 5 条核心命令（`virt-filesystems virt-customize guestfish virt-win-reg growpart`，对齐 0.3.0.5 平滑基线）、`timeout` 30s→10s、`DEPS_MIRROR=offline` 整段跳过；其余 virt-* 工具由 Phase 2 捆绑包提取兜底。**工程约束**：新增需 `dnf provides` 的组件优先检查 Phase 2 提取覆盖，避免重复慢查询 | 中 | §5.8 |

> **文档定位（v0.6）**：本文档为国产化适配的**单一事实来源**，自包含 GLIBC 目标表（§4.3）、依赖清单（§5.8）、验证命令（§8）。其余文档（`build-compatibility.md`、`dependencies.md` 等）如需同步，以此为基准。

---

## 13. 项目架构总览（v0.9 新增）

> 本章为本设计文档的「项目上下文附录」，目的是让读者在不阅读其他文档的情况下，理解 QVMConsole 项目的整体架构、模块职责、关键抽象、依赖关系与运行方式，从而更好地理解 §1-§12 中针对国产化适配的具体改造点所基于的代码骨架。本章为参考性内容，不引入新的设计决策；如与 §1-§12 的改造设计冲突，以 §1-§12 为准。
>
> **阅读路径指引（v0.9.2 新增）**：建议按角色选读——
> - **首次接触本项目的实施者**：13.1 → 13.2 → 13.3.1（启动 15 步）→ 13.5.3（调用链微图）→ 13.10（设计 vs 现状差异）→ 回到 §1-§12
> - **防火墙改造点的开发者**：13.3.2（路由）→ 13.3.3（firewall 子包）→ 13.4.6（前端拦截器）→ 13.5.3（点击 Enable 全链路）→ 13.9（本设计与架构对应表）→ §4-§5
> - **GLIBC 档位的开发者**：13.7.2（构建/安装/CI）→ 13.6.3（系统级依赖）→ 13.10（`native-glibc.txt` 现状澄清）→ §4.3 / §5.9
> - **运维 / 部署人员**：13.7.1（开发模式）→ 13.7.2（生产模式 18 主步骤 + 6 辅助配置）→ 13.7.3（管理模式）→ 13.7.5（测试环境凭据与冒烟路径）→ §6.1（安装期交互）→ §8（验收用例）
> - **代码评审 / 回归测试**：13.3.4（中间件清单）→ 13.3.6（taskqueue）→ 13.8（设计模式汇总）→ §10（风险表）→ §8（验收矩阵）

### 13.1 项目定位与核心价值

QVMConsole 是面向小型企业和个人私有云场景的开源 KVM/QEMU 虚拟化管理平台。核心价值：

- **降低运维门槛**：Web 控制台 + RESTful API 双入口，无需掌握 virsh/qemu 命令即可管理 VM 生命周期。
- **模板即点即用**：预制 Linux/Windows/OpenWrt/fnOS 模板，数分钟内完成 VM 创建，自动处理磁盘格式、引导类型、网络配置。
- **模块化设计**：可插拔网络后端（OVS）、可插拔防火墙后端（ufw/firewalld，本设计 §4-§5）、可插拔 CPU 架构（x86_64/aarch64）。
- **可观测性**：异步任务队列 + SSE 实时推送，长耗时操作可观测可中断。
- **多租户**：弹性云 + 轻量云两种模式，细粒度配额（CPU/内存/磁盘/VM 数/存储/带宽/流量/公网 IP/端口转发/快照）。

### 13.2 整体架构分层

```
┌──────────────────────────────────────────────────────────────────┐
│            前端 Web 控制台（React 19 + Semi Design 2.101）           │
│  路由（react-router 8）→ views 业务页面 → features/vm-form 共享表单  │
│  Zustand 状态分片 + Axios 拦截器统一处理 428/401/进度条 + SSE 任务推送 │
└──────────────────────────────┬───────────────────────────────────┘
                               │ HTTP/JSON + SSE + WebSocket（VNC/终端）
┌──────────────────────────────┴───────────────────────────────────┐
│            后端 API 层（Gin + Middleware 链）                       │
│  RequestLogger → SafeRecovery → CORS → SecurityHeaders →          │
│  RequestFilter → RequestGuard → RateLimit → 路由分组               │
│  → handler/ 45 个 .go 文件（43 个处理器 + types.go/helpers.go 支撑；薄层，仅参数校验与响应） │
└──────────────────────────────┬───────────────────────────────────┘
                               │
┌──────────────────────────────┴───────────────────────────────────┐
│         service 业务层（委托模式 + hook 注入 + 注册表）              │
│  arch / bandwidth / clone / firewall / host / lightweight /       │
│  network / ovs / public_ip / rescue / scheduler / security /      │
│  share / snapshot / spice / storage / template / traffic /        │
│  upload / user / vm / vnc / ...                                   │
│  *_wire.go 跨子包 hook 装配 + *_delegate.go 薄委托                  │
│  + *_register.go init 注入                                        │
└──────┬──────────────────┬──────────────────┬──────────────────────┘
       │                  │                  │
┌──────┴───────┐  ┌───────┴───────┐  ┌───────┴────────┐  ┌────────────┐
│  model (GORM) │  │ taskqueue (3  │  │ libvirt_rpc   │  │ utils       │
│ SQLite WAL   │  │ workers + SSE)│  │ go-libvirt    │  │ cmd/fs      │
│ 29 表        │  │ 50 TaskType │  │ 自动重连       │  │ 命令执行    │
│              │  │               │  │               │  │ 原子写      │
└──────────────┘  └───────────────┘  └───────────────┘  └────────────┘
       │                  │                  │
       └──────────────────┼──────────────────┘
                          │
        ┌─────────────────┴─────────────────────────────────────┐
        │  宿主机基础设施（KVM/QEMU/OVS/dnsmasq/iptables/         │
        │  firewalld/ufw）                                        │
        │  systemd 服务 kvm-console.service + kvm-console-ovs-    │
        │  dnsmasq.service                                         │
        └────────────────────────────────────────────────────────┘
```

### 13.3 后端模块职责

#### 13.3.1 入口与启动流程（[server/main.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/main.go)）

后端启动按顺序执行 15 个步骤：

1. `ensureLargeTempDir()` — tmpfs 空间检测，<20GB 重定向到可执行文件同级 `tmp/multipart`
2. `config.Init()` — 加载 .env → 环境变量 → 构建 GlobalConfig；自动生成 `VMCredentialSecret` / `SecuritySecret`
3. `logger.InitWithConsoleConfig()` — 4 个 lumberjack Writer（app/request/cmd/libvirt）+ 每日轮转
4. `model.InitDB()` — SQLite WAL + 29 张表 AutoMigrate + 11 个 `migrateXxx` 数据修复函数（10 个直接调用 + 1 个辅助）+ 默认管理员
5. `model.LoadFromDB()` — 从数据库加载持久化系统设置（`config.GlobalConfig.LoadFromDB`，main.go:70-73），覆盖环境变量默认值；`ValidateSecurity` 依赖它先于自身执行
6. `libvirt_rpc.InitLibvirtRPC()` — 连接 `/var/run/libvirt/libvirt-sock`，失败 fatal
7. `service.BootstrapVMCacheFromHost()` — 从宿主机同步 VM 缓存
8. `config.ValidateSecurity()` — 默认 JWT 密钥非开发模式直接退出
9. `initCloneDeps()` — 注入 clone 子包 60+ 依赖函数（**唯一在 main() 中而非 init() 中注入的 Deps**，因需运行时 config 值）
10. `registerTaskHandlers()` — 注册 50 个 TaskType 处理器（main.go:142-996，含 VM 生命周期/存储/网络/防火墙/维护模式等全部分类，含通过 `registerGuestDiskHandler` 闭包注册的 3 个磁盘处理器）
11. `taskqueue.Start(3)` — 启动 3 worker + 24h 自动清理
12. 10 个后台调度器（`StartStatsCollector` / `StartMemoryBalloonScheduler` / `StartSchedulerEventCleanup` / `StartVMScheduleRunner` / `StartJWTSecretRotator` / `StartFirewallDriftMonitor` / `StartExpiredUploadSessionCleanup` / `StartPasswordBreachScheduler` / `StartVMWatchdog`（M8.9，Guest Agent 失联硬重置）/ `StartHealthProbe`（M8.10，每分钟写 .health/latest.json））+ 异步 `GetComponentHealth()` 预热（缓存组件健康度，不阻塞启动）
13. 7 个网络运行态恢复函数（`SyncSSHDenyConfig` / `EnsureAllActiveUsersDefaultSecurityGroup` / `EnsureSystemBaseNetwork` / `EnsureAllNetworkBridgesRuntime` / `RestorePortForwardRules` / `EnsureAllVPCSwitchRuntime` / `RestorePublicIPRules`）
14. `router.Setup()` — Gin 引擎 + 全局中间件链 + 路由分组 + SPA 静态文件回退
15. `r.Run(":port")` — 默认 8080

**Go runtime 隐式 init() 执行顺序**（在 `main()` 之前按依赖拓扑序执行，service 根包内按文件名字母序）：`hooks_init.go` → `ip_resolver_registry.go` → `snapshot_register.go` → `template_register.go` → `vm_register.go` → `vpc_register.go` → `firewall_wire.go` / `host_wire.go` / `network_wire.go` / `ovs_wire.go` / `*_wire.go`（22 个） → `arch/x86_64.go` / `arch/aarch64.go` → `vm/migration/register.go`。

**关键 init 顺序陷阱**：`vm_register.go` 的 `init()` 先于 `vm/migration/register.go` 执行。若 `vm_register.go` 直接捕获 `HookEnsureVMNotMigrating` 变量值会得到 nil。因此使用 **wrapper 函数**（`HookEnsureVMNotMigrating: EnsureVMNotMigrating` 而非直接传变量），wrapper 在调用时才解析变量，此时已被 `vm/migration/register.go` 的 `init()` 赋值。`service/hooks.go` 中 `ApplyVMUnderMigrationStatus` / `DetectMigrationModeFromState` / `LiveMigrationMode` 同理采用延迟解析。

#### 13.3.2 路由分组与中间件链

文件：[server/router/router.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/router/router.go)

**全局中间件链**（按注册顺序）：

1. `RequestLoggerMiddleware()` — slog 按状态码分级（≥500 Error / ≥400 Warn / 其他 Info）
2. `SafeRecoveryMiddleware()` — panic 捕获 + 500 响应
3. `CORSMiddleware()` — 白名单/开发模式 `*`，支持 `X-API-Key-*` 头
4. `SecurityHeadersMiddleware()` — `X-Content-Type-Options / X-Frame-Options: DENY / CSP` 等，API 路径 `no-store`
5. `RequestFilterMiddleware()` — 危险路径模式（`..`/`%2e`/`%00`/`//`/扫描器探测路径）+ 请求体危险模式 + Host 头校验
6. `RequestGuardMiddleware()` — Body 大小限制（默认 2MB，白名单路径放行大上传）+ Content-Type 强制
7. `RateLimitMiddleware()` — IP 滑动窗口限频（公开 20/min，认证 0=不限），5 分钟清理

**路由分组**（全部挂载在 `/api` 下）：

| 分组 | 路径前缀 | 鉴权中间件 | 主要功能 |
| --- | --- | --- | --- |
| 公开 | `/public/*` + `/system/health/latest` | 无 | 站点信息、版本、周期健康探针（M8.10，Dashboard 状态灯轮询） |
| 认证 | `/auth/*` | 部分 `TokenType=login/bootstrap` | 登录、注册、邀请、找回密码、2FA、邮箱、高风险验证 |
| 系统设置 | `/settings/*` | Auth + Admin | 设置 CRUD、SMTP 测试、JWT 轮换、日志管理、诊断导出、组件版本刷新（`POST /diagnostics/refresh`，M7.2）、支持等级矩阵（`GET /diagnostics/os-support`，M8.11） |
| 安全 | `/security/*` | Auth + Admin | 密码泄漏状态/扫描 |
| 虚拟机 | `/vm/*` | Auth + `VMAccessMiddleware` | 列表、SSE、详情、操作、克隆、快照、VNC、SPICE、磁盘、救援、共享、迁移、锁定、直通；部分敏感操作叠加 `ElasticCloudOnlyMiddleware` 与 `AdminMiddleware` |
| 模板 | `/template/*` | Auth + ElasticCloudOnly | 制作、上传、导入、导出、发布、删除；`prepare-linux` 等叠加 Admin |
| 网络 | `/network/*` | Auth | 静态 IP、端口转发、UFW、网桥、公网 IP、抓包诊断；管理员子路径叠加 Admin |
| VPC | `/vpc/*` | Auth | 交换机、安全组、ACL、绑定、流量重置；写操作叠加 ElasticCloudOnly |
| 防火墙 | `/firewall/*` | Auth + Admin | KVM 网络防火墙策略 + 宿主机防火墙规则/连接（**本设计 §4-§5 改造点**） |
| OVS 诊断 | `/ovs/*` | Auth + Admin | OVS 状态/端口/租约/检查/修复 |
| 存储池 | `/storage-pool/*` | Auth + ElasticCloudOnly | 存储池列表/详情/格式化/分区/LVM；管理操作叠加 Admin |
| 节点 | `/nodes/*` | Auth + Admin | 跨节点主机管理 |
| 迁移 | `/migration/*` | Auth + Admin | `adopt-vm` |
| 用户管理 | `/user/*` | Auth + Admin | 用户 CRUD、配额、SSH、邀请、流量、轻量云注册 |
| 用户自助 | `/self/*` | Auth | 配额、VM 列表/SSE、存储池、分片上传、ISO、挂载；VM 操作叠加 ElasticCloudOnly |
| 宿主机 | `/host/*` | Auth | stats/SSE/历史/CPU/内存/磁盘/KSM/zRAM/直通；硬件信息叠加 Admin |
| 任务 | `/task/*` | Auth | 列表/SSE/详情/取消/清理 |
| 调度中心 | `/scheduler/*` | Auth + Admin | 调度器列表、事件列表、事件 SSE |
| VM 看门狗 | `/vm-watchdog/events` | Auth + Admin | 看门狗自动重置/告警/恢复事件列表（M8.9，分页 + status/vm_name/时间筛选） |

前端静态文件：`setupStaticFileServing()` 检测 `web-dist` 目录，挂载 `/assets`、favicon，`NoRoute` 回退到 `index.html`（API 路径返回 404 JSON，含路径遍历防护）。

#### 13.3.3 service 子包职责矩阵

service 包采用「根包 + 子包」结构，根包通过 `_wire.go` / `_delegate.go` / `_register.go` 装配子包。

| 子包 | 核心职责 |
| --- | --- |
| `arch` | CPU 架构注册表（x86_64/aarch64），提供 emulator 路径、机型、引导方式、UEFI 固件路径等架构差异抽象 |
| `appliance` | OVF/OVA 虚拟机包的解析、打包与文件系统操作 |
| `bandwidth` | VM/VPC 带宽限速（TC/OVS/qos）、全局带宽池分配与用户带宽再平衡 |
| `clone` | VM 克隆（普通/原生链式/批量）、重装系统、来宾系统初始化（Linux cloud-init/Windows/OpenWrt/fnOS） |
| `diagnostics` | 系统诊断信息收集器（聚合各模块状态供导出） |
| `firewall` | KVM 网络防火墙策略 CRUD + GeoIP + 宿主机 UFW/firewalld 规则 + 连接管理（**本设计 §4-§5 改造点**） |
| `guest_agent` | QEMU Guest Agent 客户端封装 |
| `guest_automation` | 来宾磁盘自动挂载/扩容自动化编排 |
| `host` | 宿主机硬件/磁盘/KSM/zRAM/iGPU 直通/维护模式/节点探测/资源采集 |
| `ip_resolver` | VM IP 多源解析器（VPC 静态绑定 / OVS 静态主机 / DHCP 租约，按 MAC 解析） |
| `libvirt_rpc` | go-libvirt RPC 连接单例（自动重连 + 降级 virsh） |
| `lightweight` | 轻量云 VM 注册/开通/配额/流量/运行时长限制 |
| `network` (根) | 端口转发（iptables 持久化）、静态 IP 绑定、网络导出聚合 |
| `network/bridge` | 宿主机网桥创建/删除/IP 迁移/systemd-networkd 配置 |
| `network/diagnostics` | VM 网络抓包（BPF 过滤、会话存储、模板渲染） |
| `network/vpc` | VPC 逻辑交换机/安全组/ACL/VM 绑定/运行时应用/流量统计/推理 |
| `ovs` | OVS 网桥管理、DHCP 租约、静态主机、诊断 |
| `public_ip` | 公网/浮动 IP CRUD、绑定/解绑/迁移、NAT 规则应用 |
| `rescue` | 救援系统 ISO 启动/关闭 |
| `scheduler` | 调度器注册表 + 调度事件中心 + SSE 推送 |
| `security` | 账户安全、JWT 签发/轮换、TOTP/2FA、密码指纹、HIBP k-匿名泄漏检测、SMTP、登录限流 |
| `share` | VM 共享目录管理（9p/virtiofs tag） |
| `snapshot` | VM 快照创建/恢复/删除/批量、NVRAM 修复、overlay backing chain 校验、配额 |
| `spice` | SPICE 图形配置、外部暴露、`.vv` 客户端文件生成 |
| `storage/disk` | VM 磁盘 CRUD/调整/挂载、CDROM/软盘、IOPS 限制、PCIe root port |
| `storage/pool` | 宿主机存储池/分区/格式化挂载/LVM 卷/ISO 树/VM 用量注入 |
| `storage/quota` | 存储配额校验 |
| `diagnostics`（v0.9.4 计划扩展，**已实施 M7.2**） | **系统组件版本健康度探测**（在 [现有 diagnostics 子包](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/diagnostics/) 中新增 `component_health.go`，非新建 `system` 子包）：18 项系统级组件（核心 8 + 磁盘镜像 7 + 诊断扩展 3，另含 cpu_vendor 厂商白名单项，共 19 项探测）`--version` 探测 + `go:embed compat-manifest.json` 阈值比对 + `sync.Once` 缓存 + `ResetComponentHealthCache()` 手动刷新。**本设计 §5.11.5 新增**。现有 `diagnostics/collector.go` 聚合各模块状态，组件健康度与之语义聚合 |
| `diagnostics`（**M8.10 周期健康探针，已实施 v0.12**） | `periodic_probe.go`：`StartHealthProbe()` 每分钟原子写 `${KVM_HEALTH_DIR:-/opt/QVMConsole/.health}/latest.json`（libvirt 状态 / daemon / 维护模式 / 启动时间 / 版本）；handler `health_probe.go` 暴露 public `GET /api/system/health/latest`；前端 HealthLight 30s 轮询（§14 M8.10） |
| `vmwatchdog`（**M8.9 新建，已实施 v0.12**） | VM 看门狗：`StartWatchdog()` 60s 周期探测运行中 VM 的 Guest Agent，`missCounts` 内存计数连续失联 3 次 → `HookResetVM` 硬重置 + `VMWatchdogEvent` 入库；维护模式/无 libvirt 跳过；`vmwatchdog_wire.go` Hook 注入；component_health 增 `hugepages` 项（mem≥128GB 且 HugePages_Total=0 → warning）（§14 M8.9） |
| `template` | 模板制作/导入/导出/传输/元数据/发布/boot 检测/删除预览 |
| `traffic` | 用户流量配额统计 |
| `upload` | 分片上传会话（init/chunk/complete/status/cancel） |
| `user` | 用户 CRUD、VM 分配、配额、SSH 开关、存储目录管理、轻量云注册 |
| `vm` (根) | VM 核心生命周期、配置、XML 操作、CPU/内存/磁盘/网络接口/快照接口、缓存、凭据、定时任务、导出 |
| `vm/memory` | 动态内存气球调度（memballoon 配置 + 增长/回收阈值） |
| `vm/migration` | 跨节点迁移（评估/预览/执行/锁/adopt） |
| `vm/vmimport` | VM/磁盘导入（普通/OVF/OVA/绝对路径磁盘） |
| `vm_xml` | XML 规范化/解析工具集（boot type、display、guest agent、kvm features、PAE、SMBIOS、直通 display） |
| `vnc` | VNC 图形启用/禁用/改密/暴露/WebSocket 代理 |

#### 13.3.4 中间件职责

文件：[server/middleware/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/middleware/)

| 中间件 | 文件 | 职责 |
| --- | --- | --- |
| `RequestLoggerMiddleware` | `request_logger.go` | slog 按状态码分级（≥500 Error / ≥400 Warn / 其他 Info） |
| `SafeRecoveryMiddleware` | 内置 | panic 捕获 + 500 响应 |
| `CORSMiddleware` | `cors.go` | 白名单/开发模式 `*`，支持 `X-API-Key-*` 头 |
| `SecurityHeadersMiddleware` | `security_headers.go` | `X-Content-Type-Options / X-Frame-Options: DENY / CSP` 等，API 路径 `no-store` |
| `RequestFilterMiddleware` | `request_filter.go` | 危险路径模式（`..`/`%2e`/`%00`/`//`/扫描器探测路径）+ 请求体危险模式 + Host 头校验 |
| `RequestGuardMiddleware` | `request_guard.go` | Body 大小限制（默认 2MB，白名单路径放行大上传）+ Content-Type 强制 |
| `RateLimitMiddleware` | `ratelimit.go` | IP 滑动窗口限频（公开 20/min，认证 0=不限），5 分钟清理 |
| `AuthMiddleware` | `auth.go` | JWT（HS256 + iss/aud 校验）+ API Key 双轨认证 |
| `TokenTypeMiddleware` / `JWTTokenTypeMiddleware` | `auth.go` | 支持 access/login/bootstrap 等多类型 token；`JWTTokenTypeMiddleware` 拒绝 API Key |
| `VMAccessMiddleware` | `auth.go` | 读取 `/etc/libvirt/vm-access/<username>` 文件校验 VM 归属（含路径遍历防护） |
| `ForcePasswordChangeMiddleware` | `auth.go` | 强制改密白名单放行改密/退出 |
| `ElasticCloudOnlyMiddleware` | `auth.go` | 拦截轻量云用户访问弹性云专属功能 |
| `AdminMiddleware` | `auth.go` | 管理员校验 |
| `FingerprintMiddleware` | `fingerprint.go` | SHA256(IP 前3段 + User-Agent) 取前 12 字节 base64，token 中携带指纹，请求时重新生成比对 |

#### 13.3.5 数据库层

文件：[server/model/db.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/model/db.go)

- SQLite WAL + `_busy_timeout=5000` + `_txlock=immediate`，并发写安全
- `AutoMigrate` 涵盖 29 张表（User/UserAPIKey/VmStatsRecord/PortForwardIP/HostStatsRecord/UserTrafficDaily/SystemSetting/VMCredential/VMCache/AuthActionToken/SecurityChallenge/SchedulerEvent/VMSchedule/NetworkBridge/HostStoragePool/HostNode/LightweightVMQuota/LightweightVMTrafficMonthly/LightweightVMRegistration/VPCSwitch/VPCSecurityGroup/VPCSecurityGroupRule/VPCVMBinding/VPCSwitchTrafficMonthly/PublicIP/PublicIPBinding/VMLock/UploadSession/**VMWatchdogEvent**）
- 11 个 `migrateXxx` 数据修复函数定义，其中 10 个在 `InitDB` 中直接调用（db.go:99-108：`migrateUserCloudType` / `migrateUserPortForwardQuota` / `migrateUserPortForwardFeature` / `migrateUserSnapshotQuota` / `migrateLightweightSnapshotQuota` / `migrateLightweightRuntimeQuota` / `migratePublicIPCIDRColumn` / `migrateVPCBindingInterfaceOrder` / `migrateVPCBindingInterfaceOrderNormalize` / `migrateVPCSwitchCIDRColumn`），另 1 个 `migrateVPCBindingUniqueIndex`（db.go:264）作为 `migrateVPCBindingInterfaceOrder` 的内部辅助函数被间接调用
- `allowedIndexNames` 白名单防 SQL 注入（SQLite DDL 不支持参数化）
- GORM 自定义 logger `gormAppLogger` 路由到 `logger.App`，慢查询告警

#### 13.3.6 任务队列

文件：[server/taskqueue/queue.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/taskqueue/queue.go)

- 3 worker + 100 缓冲 channel + 24h 自动清理
- `TaskFunc` 签名：`func(ctx context.Context, task *model.Task, progress func(int, string)) (string, error)`
- `processTask()` 在执行 handler 前构造 `progressFn`：调用时**同时**更新内存 `taskStore` 中的 task 字段 **并** `broadcastEvent(TaskEvent{...})` 推送到所有 SSE 客户端
- 取消机制：`CancelTask()` 对 Pending 直接标记，对 Running 调用存储的 `context.CancelFunc`；handler 通过 `ctx.Done()` 或 `ErrTaskCanceled` 感知
- 任务存储纯内存（`taskStore map[uint]*model.Task`），不持久化，24 小时后自动清理已结束任务
- `redactTaskParams()` 自动脱敏 `password/passwd/token/secret/private_key/credential` 字段为 `******`，避免 SSE/列表泄露
- TaskType 枚举 50 种（`model/task.go`，registerTaskHandlers 注册 50 个，含 3 个 `registerGuestDiskHandler` 闭包注册的磁盘处理器）
- 任务权限：`canAccessTask` admin 全权，普通用户仅限 `CreatedBy == username`

#### 13.3.7 logger 与 utils

**logger**（[server/logger/logger.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/logger/logger.go) + `rotation.go`）：

- 4 个独立 logger（App/Request/CMD/Libvirt）各自 lumberjack Writer
- `multiHandler` 支持文件与终端不同级别
- 每日凌晨 00:00 `rotateAll()`，可通过 `rotationDone` channel 优雅停止

**utils**（[server/utils/cmd.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/utils/cmd.go) + `fs.go`）：

- `buildCmdEnv` — 剔除父进程继承的 `LANG/LANGUAGE/LC_*` 后再强制 `LANG=C LC_ALL=C`（**v0.13 修复**：原 `append(os.Environ(), ...)` 因 exec 直接传 envp、子进程 getenv 取首个匹配，C 覆盖值被父进程 zh_CN 的 `LC_*` 遮蔽而未生效，使 virsh 输出中文、`virsh metadata` 的无元数据被误判为硬错误；现为 `*ExecCommand*`/`*ExecCommandQuiet*` 统一入口）。配套：`vm/config_metadata.go` 新增 `isMetadataNotFoundError()`，对"无元数据"的**中英文**同时兜底（读/删两路径），防本地化泄漏导致开通/启动任务误判失败
- `ExecCommandContextWithTimeout` — 经 `buildCmdEnv` 强制 C 语言环境、process group 管理、超时/取消 kill 整棵进程树
- `ExecCommandQuiet` 变体 — 非零退出码仅 DEBUG 日志（适用于预期失败的查询）
- `ExecCommandSensitiveLongRunning` — 敏感参数日志脱敏
- `ShellSingleQuote` — shell 单引号转义防注入（本设计 §5.1 决策 8 rich-rule 注入防护的基础）
- `AtomicWriteFile` — tmp + rename 原子写（本设计 §5.1 决策 11 zone 文件原子写入的基础）
- `ChownLibvirtQEMU` — 多发行版兼容的 libvirt-qemu 用户解析
- `SetFileImmutable` / `RemoveFileImmutable` / `IsFileImmutable` — chattr +i/-i/lsattr
- `SetLargeUploadDiskMode` — 大文件上传落盘模式标记

### 13.4 前端模块职责

#### 13.4.1 启动链路

1. [web/src/main.tsx](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/main.tsx) 顶部强制导入 `@douyinfe/semi-ui/react19-adapter`（否则 Semi 全局组件 Toast/Modal/Tooltip 无法在 React 19 下工作）
2. 引入全局样式：nprogress → `index.css` → `aurora.css`
3. `createRoot().render(<StrictMode><App /></StrictMode>)`
4. [web/src/App.tsx](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/App.tsx) 调用 `useTheme()` 完成主题初始化，异步 `syncPublicSiteInfo()` 拉站点信息，包裹 `<ConfigProvider locale={zh_CN}>` 注入 Semi 中文语言包，挂载 `<RouterProvider router={router} />` 与全局单例 `<HighRiskChallengeModal />`
5. 路由守卫 `RequireAuth` 校验 token，无 token 跳转 `/login?redirect=...`；轻量云非管理员仅允许白名单路径（`/dashboard`、`/vm`、`/task`、`/api-docs`、`/about` 及 `*/vnc-window` 子路径）
6. `Layout` 渲染极光背景 + `Sidebar` + `TopBar`（含历史标签页 `PageTabsBar`） + `<main><Outlet /></main>` + 底部 `TaskBar`；登录后启动任务 SSE

#### 13.4.2 路由清单

文件：[web/src/router/index.tsx](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/router/index.tsx)

**公共页面**（不嵌套在 Layout 内）：`/login`、`/invite`、`/reset-password`、`/vm/:id/vnc-window`

**主框架页面**（RequireAuth + Layout 嵌套，所有页面 `React.lazy` 懒加载）：

| 路径 | 业务领域 | 页面 |
| --- | --- | --- |
| `/dashboard` | 概览 | 仪表盘（管理员 `AdminDashboard` / 普通用户 `UserDashboard` 两套视图） |
| `/vm` | 计算 | 虚拟机列表 |
| `/vm/detail/:id` | 计算 | 虚拟机详情（含 Edit/Info/Snapshot/Vnc/Spice/Schedule/Monitor 子标签） |
| `/template` | 计算 | 模板管理 |
| `/network` | 网络 | 网络中心（Overview/Switches/SecurityGroups/Acl 标签） |
| `/public-ip` | 网络 | 公网 IP |
| `/firewall` | 网络 | 防火墙（KvmFirewall/HostFirewall/Connections 标签，**本设计 §5.7 改造点**） |
| `/storage-pool` | 存储 | 存储池 |
| `/my-storage` | 存储 | 我的存储 |
| `/user` | 系统 | 用户管理 |
| `/nodes` | 系统 | 节点管理 |
| `/scheduler` | 系统 | 调度事件 |
| `/vm-watchdog` | 系统 | 看门狗事件（M8.9 前端页，`VMWatchdogEvent` reset/warning/recovered 类型/虚拟机/时间筛选） |
| `/task` | 系统 | 任务中心 |
| `/settings` | 系统 | 系统设置（Basic/Host/Security/StorageMaintain/StorageNetwork/Advanced/Diagnostics/Log 标签） |
| `/security` | 系统 | 安全中心（ApiKey/Email/Password/Totp/Username 分区） |
| `/api-docs` | 支持 | API 文档（自动生成 `endpoints.json` + 手动 `endpointDescriptions.ts`） |
| `/about` | 支持 | 关于 |

#### 13.4.3 共享表单架构（features/vm-form/）

文件：[web/src/features/vm-form/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/features/vm-form/)

`CreateVmWizard.tsx`（创建向导，全屏 Modal + Steps 步骤导航）与 `EditVmForm.tsx`（编辑表单，Tabs 卡片标签页）共用 `sections/` 下的分区组件。设计核心是 `scopeContext.ts` 的 `VmFormScope` React Context，由 `scope.tsx` 的 `VmFormProvider` 统一注入三件套：

- `form` — 表单状态 + 联动规则（来自 `useVmForm`）
- `options` — ISO/模板/存储池/VPC/磁盘文件/直通设备/宿主信息等选项数据（来自 `useVmFormOptions`）
- `ctx` — 运行上下文（mode: 'create'/'edit'、isAdmin、vmStatus、guestType、guestAgentConnected、hostArch、hostCores、spiceSupported、registration）

**sections 子组件**（两处共用，新增字段必须两处同步——AGENTS.md 强约束）：

- 通用结构：`SectionCard.tsx`、`FormField.tsx`、`TextSwitch.tsx`
- 创建/编辑共用：`BasicInfoSection.tsx`、`CpuMemorySection.tsx`、`VirtEngineSection.tsx`、`NicSection.tsx`、`BootOrderSection.tsx`、`SystemBehaviorSection.tsx`、`AdvancedSection.tsx`、`PassthroughSection.tsx`、`ConfirmSection.tsx`、`storageTargetUtils.ts`
- 仅创建用：`CreateModeSection.tsx`、`TemplateSection.tsx`、`StoragePoolSection.tsx`、`IsoStorageSection.tsx`、`ImportStorageSection.tsx`、`ApplianceImportSection.tsx`、`ExtraDiskSection.tsx`
- 仅编辑用：`DiskManageSection.tsx`、`NicManageSection.tsx`

**配套 hooks 与工具**：

- `useVmForm.ts` — 表单状态、`buildEditFormState` 详情回填、所有联动规则（OS/ISO/模板/架构/机型/引导切换、动态内存推荐）
- `useVmFormOptions.ts` — 选项数据加载与缓存
- `useVmEditDevices.ts` — 编辑模式设备列表加载（纯数据操作，无 JSX）
- `payload.ts` — 提交载荷构建（`buildCreatePayload` / `buildClonePayload` / `buildBatchClonePayload` / `buildImportPayload` / `buildEditPayload` / `captureEditFormSnapshot` / `captureEditDiskIopsSnapshot` / `buildCPUAffinityPayload` / `getEffectiveSpiceEnabled`），编辑模式仅发送与快照不同的字段
- `defaults.ts` / `validators.ts` / `recommend.ts` / `templateUtils.ts` — 默认值、必填校验、推荐值、模板元数据解析

**`VmFormProvider` 嵌套边界**（AGENTS.md「新增字段必须在两个位置同步」的强约束之根因）：

- **必须嵌套**：[web/src/views/vm-create/CreateVmWizard.tsx](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/views/vm-create/CreateVmWizard.tsx)（创建向导顶层）与 [web/src/views/vm-detail/EditVmForm.tsx](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/views/vm-detail/EditVmForm.tsx)（编辑表单顶层）；两者**各套一份** `VmFormProvider`，互不共享状态
- **绝不允许嵌套**：[web/src/views/vm-detail/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/views/vm-detail/) 下除 `EditVmForm.tsx` 外的子页面（Info/Snapshot/Vnc/Spice/Monitor 等），这些页面用 `/api/vm/:id` 直读后端，不经 `VmFormScope`
- **嵌套粒度**：`<VmFormProvider>` 必须**直接包裹 `<CreateVmWizard>` / `<EditVmForm>` 的根 JSX**，不能放进更外层 `Layout`——否则离开创建页后 Provider 仍存活，`useVmForm` 仍可被误用，违反 Context 边界
- **sections/ 子组件**：可无差别消费 `VmFormScope`，因为它们的调用方永远是 Provider 内的两个入口之一；新增 section 组件时无需关心是 create 还是 edit 调用，由 `ctx.mode` 区分
- **校验**：[web/src/features/vm-form/scopeContext.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/features/vm-form/scopeContext.ts) 中 `VmFormScope` 的 TypeScript 类型用 `Symbol` 注入，缺失 Provider 时 `useVmForm` 抛运行时错误（编译期 + 运行期双层保护）

#### 13.4.4 关键 hooks

文件：[web/src/hooks/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/hooks/)

| Hook | 文件 | 职责 |
| --- | --- | --- |
| `useTheme` | `useTheme.ts` | 主题管理，挂载时 `applyThemeToDOM`，system 模式下监听 `matchMedia`，暴露 `isDark`/`setThemeMode`/`toggleTheme` |
| `useMountModalLifecycle` | `useMountModalLifecycle.ts` | 按需挂载的 Semi Modal 保留完整离场动画：`requestClose` 先 `visible=false`，`afterClose` 触发后再卸载（AGENTS.md 强约束） |
| `useHostStatsSSE` | `useHostStatsSSE.ts` | 宿主机实时状态，首屏先 `getHostStats()` 再订阅 SSE，断线 5 秒重连 |
| `useHostMemOptimize` | `useHostMemOptimize.ts` | KSM/zRAM 内存优化状态轮询（60s 间隔） |
| `useMediaQuery` | `useMediaQuery.ts` | 通用响应式断点判断 |

#### 13.4.5 状态管理（Zustand store 分片）

文件：[web/src/stores/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/stores/)

| Store | 文件 | 职责 |
| --- | --- | --- |
| `useAppStore` | `app.ts` | 主题模式、侧边栏折叠、站点标题、HIBP 检测开关、SPICE 默认开关；`applyThemeToDOM` 与 `buildDocumentTitle` 在此导出 |
| `useUserStore` | `user.ts` | token/username/role/cloudType/security，全部持久化到 localStorage；`isAdmin()`、`logout()` |
| `useHighRiskStore` | `highRisk.ts` | 428 挑战流程状态（pending/ask/submit/cancel），用模块级 Promise resolver 句柄避免状态序列化 |
| `useTaskStore` | `task.ts` | 任务列表 + SSE 增量更新，全局通知去重 |
| `usePageTabsStore` | `pageTabs.ts` | 顶部历史标签页，工作台 pin 不可关 |
| `useVmStore` | `vm.ts` | 虚拟机列表缓存 + 最近访问虚拟机记录（localStorage，最多 8 条） |

#### 13.4.6 API 客户端拦截器

文件：[web/src/api/client.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/api/client.ts)

`service`（主实例，挂拦截器）与 `rawClient`（不挂拦截器，专用于 `/auth/high-risk/verify` 避免递归）。

**请求拦截器**：非 `silent` 请求触发 NProgress 进度条（引用计数 `requestCount`），自动注入 `Authorization: Bearer <token>`

**响应拦截器（成功）**：停进度条；blob/arraybuffer 直接放行；业务 code 非 200/0 → `Toast.error` + reject；code 401 → `handleUnauthorized`；成功则把 `res` 当作 `AxiosResponse` 返回

**响应拦截器（失败）**：HTTP 428 → `handleHighRisk(error)`：

1. `highRiskLock` 防双弹窗
2. 跳过 `_highRiskRetried` 或 `skipHighRiskHandler` 标记的请求
3. `useHighRiskStore.getState().ask(responseData)` 弹出 `HighRiskChallengeModal`，等待用户输入验证码
4. `rawClient.post('/auth/high-risk/verify', payload)` 取 `verification_token`
5. 把 `X-High-Risk-Token` 注入原请求头，标记 `_highRiskRetried=true`，重试 `service(originalConfig)`
6. `HighRiskCancelledError` 静默（用户取消时不弹错误提示）

HTTP 401 → `handleUnauthorized`；其他 → `Toast.error`

#### 13.4.7 Semi Design 约定落地

- **行内操作按钮**：纯图标 + Tooltip 模式（class `qvm-act-ic`），按钮内禁止中文；加载态用 `<IconRefresh spin />`；超过 2~3 个操作时高频保留 1 个在外，其余收入 `Dropdown`（`trigger="click"`、`position="bottomRight"`、`clickToHide`，危险项 `type="danger"`）
- **Switch**：必须且仅用 `checkedText`/`uncheckedText` 各一个字符（通用"开/关"，语义特殊时单字符），禁止外部追加状态文字
- **Modal 弹窗离场动画**：按条件挂载的弹窗必须用 `useMountModalLifecycle`（`requestClose` → `afterClose` → 卸载）
- **深色模式**：设计令牌 `aurora.css` 中 `--qvm-` 前缀，浅色优先，深色通过 `body[theme-mode='dark']` 覆盖；深色模式下大面积标题/正文不要用近白色 `--qvm-text-0`（#e7ebf3），应覆盖为柔和灰（如 #b8c1cf）
- **React 19 适配**：`main.tsx` 顶部强制导入 `@douyinfe/semi-ui/react19-adapter`
- **ConfigProvider locale**：`App.tsx` 用 `<ConfigProvider locale={zh_CN}>` 包裹路由器
- **业务侧高风险操作接入**：敏感操作（创建/删除虚拟机、重置密码、旋转 API Key 等）即使走 API Key 也必须触发后端 428，前端通过 `client.ts` 拦截器统一处理，业务侧无需自行实现

### 13.5 关键类与函数说明

#### 13.5.1 后端关键接口与结构体

| 名称 | 文件 | 作用 |
| --- | --- | --- |
| `config.GlobalConfig` | [server/config/config.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/config/config.go) | 全局配置单例，80+ 字段，三层优先级（环境变量 > 数据库 > 默认值） |
| `config.PersistableKeys` / `keyToEnvVar` | 同上 | 可通过界面持久化的配置白名单 |
| `model.DB` | [server/model/db.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/model/db.go) | GORM SQLite 单例，WAL + busy_timeout + immediate txlock |
| `logger.App/Request/CMD/Libvirt` | [server/logger/logger.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/logger/logger.go) | 4 个独立 slog Logger + lumberjack 轮转 |
| `libvirt_rpc.libvirtConn` | [server/service/libvirt_rpc/connection.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/libvirt_rpc/connection.go) | go-libvirt RPC 连接单例，`GetLibvirt()` 自动重连（3 次指数退避 1s/2s/4s）+ 降级 virsh（`UseGoLibvirt=false` 或连接失败时） |
| `scheduler.SchedulerDefinition` / `scheduler.schedulerRegistry` | [server/service/scheduler/center.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/scheduler/center.go) | 调度器注册表（`RegisterScheduler` / `ListSchedulers`）+ 事件中心（`StartSchedulerEvent` / `FinishSchedulerEventSuccess` / `FinishSchedulerEventFailed`）+ SSE 推送（`schedulerSSEHub`）+ 168h 自动清理。与 taskqueue 区别：scheduler 持久化到 SQLite，taskqueue 纯内存 |
| `arch.ArchProfile` | [server/service/arch/types.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/arch/types.go) | 架构抽象接口（**19 个**方法：Arch/DisplayName/EmulatorPath/DefaultMachineType/SupportedMachineTypes/DefaultBootType/SupportedBootTypes/DefaultCPUMode/DefaultCPUModel/SupportedDiskBus/GetCDROMBus/SupportedNicModels/UEFIFirmwarePath/UEFIVarsTemplatePath/SupportsBIOS/SupportsSecureBoot/SupportsPAE/SupportsAPIC/DefaultWatchdogModel） |
| `arch.RegisterProfile` / `arch.GetProfile` / `arch.DetectHostArch` | [server/service/arch/registry.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/arch/registry.go) / `detect.go` | 注册表 API，未知回退 x86_64；`sync.Once` 缓存探测结果 |
| `arch.NormalizeArch` / `arch.IsX86Arch` / `arch.IsAarch64Arch` / `arch.IsRiscv64Arch` | [server/service/arch/normalize.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/arch/normalize.go) | 架构别名归一化统一入口（×86_64/aarch64/riscv64）；各调用方收敛裸字符串比较到该 API（v0.12.3） |
| `taskqueue.TaskFunc` | [server/taskqueue/queue.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/taskqueue/queue.go) | 任务处理函数签名：`func(ctx, task, progress) (string, error)` |
| `taskqueue.taskStore` / `sseClients` / `handlers` | 同上 | 任务存储（纯内存）+ SSE 客户端 + handler 注册表 |
| `taskqueue.redactTaskParams` | 同上 | 递归脱敏 password/passwd/token/secret/private_key/credential 字段 |
| `vm.Deps` | [server/service/vm/deps.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/vm/deps.go) | vm 子包外部依赖函数字段集合（约 50+ 字段），由 `vm_register.go` init 注入 |
| `clone.Deps` | [server/service/clone/deps.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/clone/deps.go) | clone 子包外部依赖（约 60+ 字段），由 `main.go initCloneDeps()` 注入（**唯一非 init 注入**） |
| `service.HookApplyVPCBindingRuntime` 等 | [server/service/hooks.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/hooks.go) | hook 函数变量，由子包 `*_register.go` init 反向赋值，`hooks.go` 提供 wrapper 延迟解析规避 init 顺序问题 |
| `model.Task` | [server/model/task.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/model/task.go) | 任务模型，50 种 TaskType 枚举（注册 50 个，含 3 个 `registerGuestDiskHandler` 闭包） |
| `model.User` / `UserAPIKey` / `VMCredential` / `VMCache` | [server/model/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/model/) | 用户/API Key/VM 凭据/VM 缓存模型 |
| `utils.ExecCommandContextWithTimeout` | [server/utils/cmd.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/utils/cmd.go) | 经 `buildCmdEnv` 强制 C 语言环境（剔除父进程 `LC_*` 后再追加，v0.13 修复 locale 遮蔽）、process group 管理、超时/取消 kill 整棵进程树 |
| `utils.ShellSingleQuote` | 同上 | shell 单引号转义防注入（本设计 §5.1 决策 8 基础） |
| `utils.AtomicWriteFile` | [server/utils/fs.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/utils/fs.go) | tmp + rename 原子写（本设计 §5.1 决策 11 基础） |
| `diagnostics` 组件版本健康度（v0.9.4 计划新建，**已实施 M7.2**） | [server/service/diagnostics/component_health.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/diagnostics/component_health.go) | **18 项组件版本探测器**（实现为包级函数，非结构体）：`detectComponentHealth()` 全量探测 + `buildItem()` 统一三态判定（critical < 最低 < warning < 推荐 < healthy）+ `detectVersion()`（命令不存在→空串 / 解析失败→`"unknown"` 降级 warning）+ `detectGlibcVersion()`；`go:embed compat-manifest.json` 读取版本阈值；`GetComponentHealth()` 缓存探测结果；`ResetComponentHealthCache()` 手动刷新。**本设计 §5.11.5 新增** |
| `diagnostics.ComponentHealthItem`（v0.9.4 计划新建，**已实施 M7.2**） | 同上 | 单项组件健康度结构：`Component`/`Category`/`Status`/`CurrentVersion`/`RequiredVersion`/`RecommendedVersion`/`Message`/`UpgradeHint`。**本设计 §5.11.5 新增** |
| `//go:embed compat-manifest.json`（v0.9.4 计划新建，**已实施 M7.2**） | 同上 | Go 1.16+ embed 指令，将构建产物 `compat-manifest.json` 嵌入二进制；构建时由 build.sh 编译前写 `server/service/diagnostics/compat-manifest.json` 后再 `go build`。**本设计 §5.11.5 新增** |
| `vmwatchdog.StartWatchdog`（v0.12 新增，**已实施 M8.9**） | [server/service/vmwatchdog/watchdog.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/vmwatchdog/watchdog.go) | VM 看门狗：60s 周期探测运行中 VM Guest Agent，`missCounts` 内存计数连续失联 3 次 → `HookResetVM` 硬重置 + `VMWatchdogEvent` 入库；维护模式/无 libvirt 跳过；可通过 config `VMWatchdogEnabled/IntervalSeconds/MaxMisses`（.env `KVM_VM_WATCHDOG_*`）调节，每轮重读配置 |
| `model.VMWatchdogEvent` | [server/model/vm_watchdog_event.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/model/vm_watchdog_event.go) | 看门狗事件模型（第 29 张表）：类型 reset/warning/recovered、VM 名、状态、时间戳、detail；`handler.GetVMWatchdogEventList` 分页查询 |
| `diagnostics.StartHealthProbe`（v0.12 新增，**已实施 M8.10**） | [server/service/diagnostics/periodic_probe.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/diagnostics/periodic_probe.go) / [server/handler/health_probe.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/handler/health_probe.go) | 周期健康探针：每分钟原子写 `${KVM_HEALTH_DIR:-/opt/QVMConsole/.health}/latest.json`（libvirt 就绪 / daemon / 维护模式 / 启动时间 / 版本）；handler 暴露 public `GET /api/system/health/latest` |
| `migrations.Register` / `migrations.Run`（v0.12 新增，**已实施 M8.5**） | [server/model/migrations/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/model/migrations/) | schema_migrations 版本化迁移（单事务），InitDB AutoMigrate 前调用；0001 scheduler_events `(vm_name,status)` 复合索引 |

#### 13.5.2 前端关键组件与函数

| 名称 | 文件 | 作用 |
| --- | --- | --- |
| `VmFormScope` | [web/src/features/vm-form/scopeContext.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/features/vm-form/scopeContext.ts) | 共享表单 React Context，约束"必须用在 VmFormProvider 内" |
| `VmFormProvider` | [web/src/features/vm-form/scope.tsx](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/features/vm-form/scope.tsx) | 统一注入 `{ form, options, ctx }` 三件套 |
| `useVmForm` | [web/src/features/vm-form/useVmForm.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/features/vm-form/useVmForm.ts) | 表单状态 + `buildEditFormState` 详情回填 + 所有联动规则 |
| `useVmFormOptions` | [web/src/features/vm-form/useVmFormOptions.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/features/vm-form/useVmFormOptions.ts) | 选项数据加载与缓存 |
| `payload.buildCreatePayload` / `buildEditPayload` / `captureEditFormSnapshot` | [web/src/features/vm-form/payload.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/features/vm-form/payload.ts) | 提交载荷构建，编辑模式仅发送差异字段 |
| `service` / `rawClient` | [web/src/api/client.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/api/client.ts) | axios 主实例（挂拦截器）与原始实例（不挂拦截器，用于高风险验证） |
| `handleHighRisk` | 同上 | HTTP 428 统一处理：弹 `HighRiskChallengeModal` → 取 `verification_token` → 重试原请求 |
| `useMountModalLifecycle` | [web/src/hooks/useMountModalLifecycle.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/hooks/useMountModalLifecycle.ts) | 按需挂载 Modal 离场动画（AGENTS.md 强约束） |
| `applyThemeToDOM` | [web/src/stores/app.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/stores/app.ts) | 主题切换：设 `body[theme-mode]` → CSS 变量级联生效 |
| `applyDocumentTitle` | [web/src/config/site.ts](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/config/site.ts) | 拼接 `页面名 - 站点名` 同步浏览器标题 |
| `syncPublicSiteInfo` | 同上 | 从后端 `/public/settings` 拉站点标题、HIBP 检测开关、SPICE 默认开关到 `useAppStore` |
| `HealthLight`（v0.12 新增，**已实施 M8.10**） | [web/src/components/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/components/)（`health.ts` 30s 轮询 `GET /system/health/latest`） | Dashboard 顶部状态灯：libvirt 不可用→黄 / 接口超时不可达→红 / 正常→绿；深色模式适配 |
| `getHealthProbeLatest` / `watchdog.ts`（v0.12 新增，**已实施 M8.9/M8.10**） | [web/src/api/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/api/) | 健康探针快照与看门狗事件接口封装 |
| `VmWatchdogPage`（v0.12 新增，**已实施 M8.9**） | [web/src/views/vm-watchdog/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/src/views/vm-watchdog/) | 系统菜单「看门狗事件」页：`GET /vm-watchdog/events` 列表 + reset/warning/recovered 类型 / 虚拟机 / 时间筛选 |

#### 13.5.3 关键调用链（从用户点击到宿主机执行）

**前端一次典型请求的生命周期**（以「点击 Enable 宿主机防火墙」为例）：

```
[用户点击 Enable 按钮]
   │
   ▼
[Semi Button onClick] (HostFirewallTab.tsx)
   │ props: useTaskStore + service (axios)
   ▼
[service.post('/api/firewall/host/enable', {})]
   │  拦截器：注入 Bearer token + NProgress 启动
   ▼
[HTTP POST /api/firewall/host/enable] ──── Gin Engine ────►
   │
   ▼
[全局中间件链] RequestLogger → SafeRecovery → CORS → SecurityHeaders
   │   → RequestFilter → RequestGuard → RateLimit
   ▼
[路由分组 /firewall/* + AdminMiddleware] (router.go)
   │
   ▼
[handler/firewall.go: EnableHostFirewall]   ← 薄层
   │   1. 解析 request body
   │   2. 校验用户 + requireHighRiskVerification (敏感操作)
   │   3. 调用 service.EnableHostFirewall(req)
   ▼
[service/firewall_wire.go: EnableHostFirewall]   ← 根包 delegate（firewall_wire.go:151）
   │   转发到子包
   ▼
[service/firewall/host.go: Enable]   ← 业务逻辑
   │   1. resolveBackend() → RWMutex 双检缓存 → 'firewalld' | 'ufw' | 'none'（后端失效自动重测，#E）
   │   2. backend.Enable(ctx, progress)   ← 走新抽象
   ▼
[service/firewall/backend_firewalld.go: (*firewalldBackend).Enable]
   │   调用 utils.ExecCommandContextWithTimeout
   │   绝对路径 /usr/bin/firewall-cmd + --permanent + --reload
   ▼
[宿主机进程 fork+exec /usr/bin/firewall-cmd] ── D-Bus ──► firewalld daemon
   │
   ▼
[progress(50, "写入 zone XML ...")]
   │   ↑ 进度回调
   ▼
[taskqueue.processTask] 广播 SSE → 所有订阅 /api/task/stream 的客户端
   │
   ▼
[前端 useTaskStore] 更新任务状态 + 进度条 UI
```

**后端启动到服务可用的完整调用链**：

```
[os.Args 解析] → main() (server/main.go)
   │
   ├─ Go runtime 先按依赖拓扑序执行 init()
   │     ├─ service/hooks_init.go
   │     ├─ service/ip_resolver_registry.go
   │     ├─ service/{snapshot,template,vm,vpc}_register.go
   │     ├─ service/firewall_wire.go ← HookManageUFWRule 赋值
   │     └─ service/arch/{x86_64,aarch64}.go ← RegisterProfile
   │
   ├─ main() 顺序 (15 步)
   │     ├─ config.Init() → GlobalConfig 单例
   │     ├─ logger.InitWithConsoleConfig() → 4 个 lumberjack Writer
   │     ├─ model.InitDB() → SQLite WAL + 29 张表 AutoMigrate + 11 个 migrate 函数
   │     ├─ model.LoadFromDB() → 数据库设置覆盖环境变量默认值
   │     ├─ libvirt_rpc.InitLibvirtRPC() → /var/run/libvirt/libvirt-sock
   │     ├─ service.BootstrapVMCacheFromHost() → 同步 VM 缓存
   │     ├─ config.ValidateSecurity() → JWT 密钥安全校验
   │     ├─ initCloneDeps() → 注入 clone 子包 Deps (60+ 字段)
   │     ├─ registerTaskHandlers() → 50 个 TaskType 处理器
   │     ├─ taskqueue.Start(3) → 3 worker + 24h 清理
   │     ├─ 10 个后台调度器 StartXxx()（含 StartVMWatchdog/StartHealthProbe）+ 异步组件健康预热
   │     ├─ 7 个网络运行态恢复 RestoreXxx()
   │     ├─ router.Setup() → Gin Engine + 中间件链 + 路由分组
   │     └─ r.Run(":8080")
   │
   └─ 进入 epoll 事件循环，接收 HTTP 请求
```

**调用链与 §1-§12 改造点的对应位置**（v0.9.2 新增，让读者一眼看到本设计改在哪一段）：

| 调用链段 | 现状代码 | 本设计改造点 | 设计章节 |
| --- | --- | --- | --- |
| `[Semi Button onClick]` | `HostFirewallTab.tsx` 现展示 `ufw_available` | 改读 `backend` / `backend_name`，none Banner，错误 hint，Enable 自检失败项展示与回滚 | §5.7 |
| `[service.post /api/firewall/host/enable]` | `web/src/api/firewall.ts` 现类型无 `backend` 字段 | 类型扩展 `backend?` / `backend_name?` / `ip_backend?` / `error_code?` | §5.7 |
| `[handler/firewall.go: EnableHostFirewall]` | 现直接调 `service.EnableHostFirewall` | 行为不变（薄层） | §4.1 / §5.6 |
| `[service/firewall_wire.go: EnableHostFirewall]` | delegate 转发至 `firewall/host.go`（firewall_wire.go:151-157） | **不动 delegate 签名**，仅修改转发目标的内部实现 | §2.3 / §5.3 |
| `[service/firewall/host.go: Enable]` | **硬编码 `ufw --force enable`**（host.go:81-122，其中 `--force enable` 在 host.go:114） | 改为 `DetectHostFirewallBackend().Enable(ctx, progress)` | §3.1 / §5.3 |
| `[service/firewall/backend_firewalld.go]` | **当前不存在**（§13.10 待实施项） | 新建：zone 持久化 + 接口/源绑定序列 + policy 放行 + `--check-config` 预检 + 原子写 + 自检 | §4.2 / §5.1 |
| `[service/firewall/backend_ufw.go]` | host.go 现有 ufw 命令内联 | 原逻辑迁移至此文件，行为零变化 | §5.2 |
| `[utils.ExecCommandContextWithTimeout]` | 现相对路径 + 部分缺超时 | `exec.LookPath` 缓存绝对路径 + 全部带超时（60s+） | §4.2 / #N |
| `[宿主机 fork+exec /usr/bin/firewall-cmd]` | 现无此调用路径 | 新增；禁止 `--command=`/`--direct`；rich-rule 经 `ShellSingleQuote` 单 argv 传入 | §4.2 / #S3 |
| `[progress 回调 → taskqueue SSE 广播]` | 现已存在（taskqueue + SSE） | 沿用，文案与 ufw 后端共用一组 | §2.4 |

### 13.6 依赖关系

#### 13.6.1 后端依赖（[server/go.mod](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/go.mod)）

| 依赖 | 版本 | 用途 |
| --- | --- | --- |
| `gin-gonic/gin` | v1.12.0 | Web 框架 |
| `gorm.io/gorm` + `gorm.io/driver/sqlite` | v1.31.1 / v1.6.0 | ORM + SQLite 驱动（CGO） |
| `digitalocean/go-libvirt` | v0.0.0-20260217163227-273eaa321819 | libvirt RPC 客户端 |
| `golang-jwt/jwt/v5` | v5.3.1 | JWT 认证（HS256 + iss/aud） |
| `pquerna/otp` | v1.5.0 | TOTP 两步验证 |
| `gorilla/websocket` | v1.5.3 | WebSocket（VNC/终端代理） |
| `golang.org/x/crypto` | v0.53.0 | bcrypt 密码哈希（cost=10） |
| `natefinch/lumberjack.v2` | v2.2.1 | 日志轮转 |
| `mattn/go-sqlite3` | 隐含 | SQLite CGO 驱动（构建时需 gcc） |
| Go 工具链 | 1.26.0 | 模块声明版本 |

#### 13.6.2 前端依赖（[web/package.json](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/package.json)）

| 依赖 | 版本 | 用途 |
| --- | --- | --- |
| `react` | 19.2.7 | UI 框架 |
| `react-router` | 8.3.0 | 路由（懒加载 + 数据路由） |
| `@douyinfe/semi-ui` | 2.101.1 | Semi Design 组件库 |
| `@douyinfe/semi-ui/react19-adapter` | — | React 19 适配（必须在 main.tsx 顶部导入） |
| `zustand` | 5.0.14 | 状态管理 |
| `axios` | — | HTTP 客户端 |
| `echarts` | 6.1.0 | 图表（仪表盘、监控） |
| `@novnc/novnc` | 1.7.0 | VNC 浏览器客户端 |
| `@xterm/xterm` | 6.0.0 | 终端模拟器 |
| `qrcode` / `spark-md5` / `nprogress` | — | 二维码 / 分片上传 MD5 / 进度条 |
| `vite` | 8.1.1 | 构建工具 |
| `typescript` | 6.0.2 | 类型系统 |
| `oxlint` | 1.71.0 | Lint（替代 ESLint） |
| `sass-embedded` | 1.100.0 | Sass 预处理 |
| `@vitejs/plugin-react` | 6.0.3 | Vite React 插件 |
| Node.js | ≥22.22 | 构建工具链要求 |

#### 13.6.3 系统级依赖

宿主机运行时依赖（由 [install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) 自动安装）：

| 类别 | Debian/Ubuntu 包 | RPM 系（麒麟/openEuler）包 | 用途 |
| --- | --- | --- | --- |
| 虚拟化 | `qemu-kvm` / `qemu-utils` | `qemu-kvm` / `qemu-img` | KVM/QEMU 后端 |
| libvirt | `libvirt-daemon-system` / `libvirt-clients` | `libvirt` / `libvirt-client` | libvirt 管理 |
| 网络 | `openvswitch-switch` | `openvswitch` | OVS 网桥 |
| 网络 | `dnsmasq-base` | `dnsmasq` | DHCP/DNS |
| 防火墙 | `ufw` | `firewalld`（RPM_PKG_MAP 映射） | 宿主机防火墙（**本设计核心**） |
| UEFI 固件 | `ovmf` | `edk2-ovmf` / `edk2-aarch64` | UEFI 引导 |
| 工具 | `virtinst` / `libguestfs-tools` / `cloud-utils` / `cloud-utils-growpart` / `genisoimage` / `xorriso` / `arp-scan` / `nftables` / `conntrack` | 同名或映射 | VM 创建/磁盘操作/IP 扫描/防火墙规则 |

构建工具链：

| 工具 | 版本 | 用途 |
| --- | --- | --- |
| Go | 1.22+（推荐 1.26.0） | 后端编译 |
| Node.js | ≥22.22 | 前端构建 |
| npm | ≥9 | 前端包管理 |
| `air` | v1.61.7 | Go 热重载 |
| `zig` | 0.14.0 | 兼容版构建的 C 编译器（锁定 GLIBC 上限，**本设计 §4.3 关键**） |
| `readelf`（binutils） | — | 兼容版 GLIBC 上限校验（**本设计 §5.9**） |
| `gcc` 交叉编译器 | `gcc-x86-64-linux-gnu` / `gcc-aarch64-linux-gnu` | native 版交叉编译（按目标架构） |

#### 13.6.4 模块间依赖（核心包级循环依赖打破机制）

```
service 根包（_wire.go / _delegate.go / _register.go / hooks.go）
   ↑                          ↑                          ↑
   │ init() 注入               │ init() 反向赋值           │ main.go 显式调用
   │                          │                          │
┌──┴──────────────┐  ┌────────┴─────────┐  ┌────────────┴─────────┐
│ vm 子包          │  │ snapshot 子包     │  │ clone 子包            │
│ Deps 结构体      │  │ HookStartVM 等   │  │ Deps 结构体           │
│ D.XXX() 调用根包 │  │ 子包调用根包函数  │  │ D.XXX() 调用根包      │
└─────────────────┘  └──────────────────┘  └──────────────────────┘
       ↑                                                  ↑
       │ 镜像类型（alias）解决循环                          │ 唯一在 main() 注入
       │                                                  │（需运行时 config 值）
┌──────┴──────────┐  ┌──────────────────┐  ┌──────────────┴────────┐
│ vpc 子包         │  │ firewall 子包     │  │ network 子包            │
│ StaticHost alias│  │ HookOvsBridgeName│  │ HookGetFirewallPolicy  │
└─────────────────┘  └──────────────────┘  └────────────────────────┘
```

打破循环依赖的三种机制（本设计 §2.2 已分析）：

1. **Deps 结构体注入**（重场景）：子包定义 `Deps` 结构体持有所有外部依赖函数字段，根包 `init()` 调用 `InitDeps(&Deps{...})` 注入；子包通过 `D.XXX()` 调用，完全不 import 根包。镜像类型（alias）解决类型循环（如 `vmpkg.TemplateMetaAlias` 是 `templatepkg.TemplateMeta` 的字段子集）。
2. **Hook 函数变量**（轻场景）：子包定义 `HookXXX` 函数变量，根包 `init()` 赋值；适用于回调场景（如 `snapshot.HookStartVM`）。
3. **_wire.go 装配层**：service 根包作为中间装配层，在 `init()` 中将一个子包的 hook 变量赋值为另一个子包的函数，打破子包间循环依赖（如 `firewall_wire.go` 中 `fwpkg.HookOvsBridgeName = ovspkg.OvsBridgeName`）。

### 13.7 项目运行方式

#### 13.7.1 开发模式

```bash
./start-dev.sh
```

- **后端**：`cd server && KVM_DEVELOPMENT_MODE=true air &`（air 监听 `.go` 文件，延迟 1s 重建，输出 `server/tmp/kvm-console`），端口 8080
- **前端**：`cd web && npx vite --host 0.0.0.0 &`（端口 5173），`predev` 钩子自动执行 `gen:api` 从 `router.go` 解析接口清单 → `endpoints.json`
- **联动**：vite 代理 `/api → :8080`，前后端独立进程，任一退出整体退出（`trap cleanup`）
- **日志环境变量**：`KVM_LOG_CONSOLE_LEVEL=warn`（终端只看 warn 以上）

#### 13.7.2 生产模式（构建 + 安装 + systemd）

**构建**：

```bash
bash build.sh                              # 默认构建 compat + native 双档
bash build.sh --variant compat             # 仅 zig 兼容版（amd64 默认 GLIBC 2.2.5，arm64 默认 2.17）
bash build.sh --variant native             # 仅宿主机原生版
bash build.sh --compat-glibc 2.17          # 自定义兼容版 GLIBC 上限
bash build.sh --target-arch arm64          # 交叉构建 arm64
bash build.sh --skip-frontend              # 跳过前端（需已存在 web/dist）
```

构建产物结构（`release/kvm-console-linux-${TARGET_ARCH}/`）：

| 文件/目录 | 说明 |
| --- | --- |
| `kvm-console` | zig 兼容版后端二进制（GLIBC 上限 ${COMPAT_GLIBC_VERSION}） |
| `kvm-console-native` | 宿主机原生版二进制（双构建时存在） |
| `web-dist/` | 前端静态文件（由 `web/dist` 复制） |
| `install.sh` | 安装脚本（含可执行权限） |
| `bundled/` | 捆绑的 RPM 包目录（arp-scan、libguestfs-tools-c、libguestfs-tools） |
| `compat-manifest.json`（v0.9.4 新增，**已实施 M7.0**） | **兼容性清单**（§5.11.3）：`binaries`（三档 GLIBC）+ `system_requirements`（18 项系统级组件阈值 + cpu_vendor 白名单）+ `os_compat`（7 个发行版推荐档位）。install.sh `check_component_versions()` 读取此文件做版本比对；后端 `go:embed` 嵌入此文件做运行时探测。缺失时 install.sh 回退内置默认值、后端不展示版本阈值 |
| `SHA256SUMS` / `*.tar.gz.sha256`（v0.12 新增，**已实施 M8.7**） | 打包段生成包内 `SHA256SUMS` + 外部 `.tar.gz.sha256`；install.sh `extract_tarball` 解压前校验（不匹配 exit 1），下载分支拉 `${url}.sha256` |
| `versions.conf`（v0.9.9 新增 #V，**已实施**） | 组件版本阈值（`COMPAT_GLIBC=2.28` 等）+ `SUPPORT_LEVEL_<os>`（M8.11，v0.12 扩展）；install.sh `check_component_versions` 读取，旧产物回退内置默认值 |
| `*.minisig` + `minisign.pub`（v0.12.1 **已实施 §14.5 候选④**） | minisign 离线签名：build.sh 签名前将公钥拷贝进包内 + `release/` 目录（tarball 旁，供 install.sh 下载/本地安装同目录兜底），私钥存在时对 `.tar.gz` 签名产出 `.minisig`。install.sh `extract_tarball` 解压前用公钥 `minisign -V -m`（实测 `-Vm` 合并解析异常，须分开传参）验证，被篡改包 exit 1。私钥缺失时跳过（仅 SHA256），不阻断构建。CI artifact/Release/OSS 均随包发布 `.minisig` 与 `minisign.pub` |

最终打包：`release/kvm-console-linux-${TARGET_ARCH}.tar.gz`

**CI**（[.github/workflows/build.yml](file:///Volumes/cs/QVMConsole/jeoQVMConsole/.github/workflows/build.yml)，手动触发 `workflow_dispatch`）：

| Job | Runner | 说明 |
| --- | --- | --- |
| `build`（amd64） | `ubuntu-22.04` | `zig-linux-x86_64-0.14.0.tar.xz` |
| `build-arm64` | `ubuntu-24.04-arm` | `zig-linux-aarch64-0.14.0.tar.xz` |
| `verify-centos7-glibc217`（M8.3，v0.12 新增） | `ubuntu-22.04` + `centos:7` 容器 | 高兼容档产物 glibc 2.17 兼容性验证：容器内 `--smoke-selfcheck` 冒烟（**当前仅覆盖 amd64**） |
| `verify-arm64-glibc-low`（v0.12.1 新增） | `ubuntu-24.04-arm` + `arm64v8/ubuntu:20.04` 容器 | arm64 compat 档低 glibc 冒烟：`centos:7` 无 arm64 变体，改用 glibc 2.31 最低档实测 `--smoke-selfcheck`（覆盖「arm64 compat 档低 glibc 可运行」核心目标） |
| `release` | `ubuntu-latest` | 汇总各 arch 产物，可选上传 GitHub Release（仅下载 artifact + 创建 Release，不编译 Go 二进制，无 glibc 漂移风险）；v0.12.1 起含可选 minisign 签名步骤（`secrets.MINISIGN_KEY` base64 私钥，未配置跳过）+ 必做 SHA256 校验 + 上传 `.tar.gz`/`.sha256`/`.minisig` |

产物可选上传到 GitHub Release（`softprops/action-gh-release@v2`）或阿里云 OSS（`ossutil cp`）。

**安装**：

```bash
sudo ./install.sh    # 首次安装 / 更新 / 卸载 / 修复（交互式选择模式）
```

install.sh `run_install_or_update`（install.sh:3080）串行执行 **19 个主步骤**（`step` 包装，失败 `exit 1`）+ **7 个辅助配置**（失败仅 `warn` 不阻断或无失败语义），共 26 次函数调用（v0.9.8：新增「组件版本检测」步骤；v0.11：辅助新增 `setup_bash_audit`；v0.13 核对实际含 `open_frontend_port` 与 `setup_ovs_foundation`，`STEP_TOTAL=18→19`）：

**主步骤（STEP_TOTAL=19，失败阻断安装）**：

1. `check_kvm_hardware` — CPU 硬件虚拟化检测
2. `check_and_install_deps` — 依赖安装（APT_DEPS / RPM_PKG_MAP / 捆绑 RPM）
3. `configure_qemu_for_rpm` — RPM 系 QEMU 权限配置
4. `configure_libvirt_nonroot` — libvirt 非 root 配置
5. `setup_selinux` — SELinux Enforcing 放行
6. `ensure_kvm_runtime` — modprobe kvm 模块
7. `setup_quota` — 项目配额文件系统（稀疏镜像 + ext4 project quota）
8. `configure_port` — 网页端口（默认 8080）
9. `detect_firewall_backend` — **本设计 §4.4 新增**：探测 ufw/firewalld/none + 环境变量 `FW_BACKEND` 覆盖 + update 模式复用 `.env` 持久化后端（M2，防自动探测静默切后端）+ firewalld 未运行时询问启动（仅 install 模式）+ firewalld 版本探测（`DETECTED_FW_VER`，供安装期 advice）
10. `open_frontend_port` — 前端端口（KVM_PORT）防火墙放行（ufw 后端 `allow KVM_PORT` / firewalld 后端 trusted zone 放行）
11. `precheck_domestic` — **本设计 §5.8 #L 新增**：端口占用 / 多防火墙共存 / NM 环境预检（仅告警不弹交互）
12. `get_release` — 本地发行目录优先，否则下载官方 tar.gz
13. `check_component_versions` — **本设计 §5.11.4 / M7.1 新增**：组件版本检测（读取 `release/.../versions.conf` 阈值（v0.9.9 #V，旧产物回退内置默认值）；批量缺失短路（#Z）；18 项探测，critical 中止 / warning 交互确认 / healthy 通过；`--skip-version-check` 跳过 critical 中止）
14. `select_binary_tier` — **本设计 §4.3/#A 新增**：`native-glibc.txt` 阈值（提前读取为 `NATIVE_GLIBC_REQUIRED` 全局，update 模式亦可得）+ 高兼容档版本动态发现（`HIGH_COMPAT_VER`，v0.9.11 #AP，取代硬编码 compat-2.28）+ 三档选优（compat/native/compat-${HIGH_COMPAT_VER}）+ 冒烟测试 + `.env` 持久化复用（update 模式按动态档位白名单校验）
15. `install_files` — 复制二进制与 web-dist；按 `select_binary_tier` 结果落位主程序（§4.3/#A **已实施**）
16. `write_env` — 写入 `.env`（chmod 600）；KVM_PORT 为空（repair 模式）保持已有值不清空（M3）；KVM_BINARY_TIER 按动态档位白名单校验写入
17. `ensure_directories` — 创建模板/克隆/ISO/OVS/VPC 目录，ARM 部署旧版 AAVMF
18. `ensure_apparmor_storage_access` — libvirt 自定义存储 AppArmor 访问规则
19. `setup_ovs_foundation` — OVS 网桥 br-ovs + dnsmasq + iptables NAT（firewalld 后端下不写 iptables INPUT 规则，dnsmasq 入站由 trusted zone 保证，v0.9.11 M1）；OVS systemd 单元名按 PKG_MGR 区分（apt=openvswitch-switch，dnf/yum=openvswitch）；`virsh net-destroy default` 仅 install 模式（M4）

**辅助配置（失败仅 `warn` 不阻断，或无失败语义）**：

20. `ensure_sysctl_network` — 启用 IPv4 转发（`net.ipv4.ip_forward=1`）；失败 `warn "sysctl 网络优化配置失败"`
21. `setup_sshd_foundation` — sshd Include `/etc/ssh/sshd_config.d/`；失败 `warn "SSHD 地基配置失败"`
22. `setup_bash_audit` — **本设计 §5.9 / M8.8 新增**：bash 命令审计（PROMPT_COMMAND 记录 + `chattr +a` 审计日志/降级 622）；失败 `warn "bash 命令审计配置失败"`
23. `setup_service` — 写入 `/etc/systemd/system/kvm-console.service`
24. `start_service` — `systemctl restart` 并校验启动
25. `show_info` — 显示访问地址、安装目录、配置文件、默认账号、运维命令
26. `print_install_report` — **本设计 §5.8 #K 新增**：安装报告（防火墙后端 / 二进制档位 / GLIBC / CPU 指令集 / SELinux / 组件升级提示（#Q） / 降级项 / 组件版本统计（M7.1）/ 日志路径）

**systemd 服务**：

- `kvm-console.service`（主服务）：`Type=simple`，`EnvironmentFile=${INSTALL_DIR}/.env`，强制 `LANG=C.UTF-8`，`Restart=on-failure`，`LimitNOFILE=65536`
- `kvm-console-ovs-dnsmasq.service`（OVS DHCP/DNS）：由 `setup_ovs_foundation` 创建

**安装目录**：`/opt/QVMConsole`（含 `kvm-console` 二进制、`web-dist/`、`data/kvm_console.db`、`.env`、`firmware/`）

#### 13.7.3 管理模式

```bash
sudo ./qvmc-manage.sh
```

菜单功能：

1. 重置默认管理员（admin/admin123）密码，并清除 TOTP/邮箱绑定
2. 清除单个用户的 TOTP
3. 查看所有用户（ID/用户名/角色/状态/TOTP/邮箱）
4. 修改 KVM_PORT，自动更新 UFW 规则（加新端口、删旧端口）——**本设计 §5.10 改造点**：增加 firewalld 分支
5. 修改管理员密码（≥12 位 + HIBP k-匿名性在线泄露检测）
6. 回滚到上一版本发行版（**M8.6，v0.12 新增**）：从 `.release_backup/{01|02|03}` 选槽位恢复程序文件与前端（不影响数据库与配置，恢复后校验服务运行）

依赖：sqlite3、python3+bcrypt（与 Go bcrypt cost=10 兼容，前缀 `$2a$`）

#### 13.7.4 接口文档自动生成

```bash
cd web && npm run gen:api    # 手动触发
# 或在 dev/build 时由 predev/prebuild 钩子自动执行
```

[web/scripts/generate-api-endpoints.mjs](file:///Volumes/cs/QVMConsole/jeoQVMConsole/web/scripts/generate-api-endpoints.mjs) 从 `server/router/router.go` + `server/handler/*.go` 解析：

- 路由分组（Group/Use/GET|POST|PUT|PATCH|DELETE）
- 中间件识别（AuthMiddleware/AdminMiddleware/ElasticCloudOnlyMiddleware/VMAccessMiddleware/JWTTokenTypeMiddleware）
- `requireHighRiskVerification(c, "...")` 标记高风险接口
- 路由行尾 `//` 中文注释作为 comment

输出：`web/src/views/api-docs/generated/endpoints.json`（含 method/path/handler/auth/admin/elasticOnly/vmAccess/highRisk/comment）

#### 13.7.5 测试环境

部署目录：`/opt/project/QVMConsole/`（v0.12.x 前曾为 `/opt/project/new-web/`，代码 `qvmc-manage.sh`/install.sh/固件路径均已迁移到 QVMConsole）

**测试账号**：

| 角色 | 用户名 | 密码 | 用途 |
| --- | --- | --- | --- |
| 管理员 | `admin` | `admin123` | 全功能测试（用户管理、系统设置、防火墙、OVS 等所有 Admin 路由） |
| 普通用户 | `test` | `Qw133133133133@` | 配额、VM 自助、API Key 申请等普通用户路径 |

**运行时信息**：

| 项 | 值 |
| --- | --- |
| 数据库 | `/opt/project/QVMConsole/data/kvm_console.db`（SQLite） |
| 数据迁移 | 启动时 AutoMigrate 29 张表 + 11 个 `migrateXxx` 数据修复函数（§13.3.5） |
| 文件同步 | 文件修改自动同步到 `/opt/project/QVMConsole/` 并触发热重载（air / vite） |
| 后端进程 | `kvm-console` systemd unit（日志 `journalctl -u kvm-console -f`） |
| 前端访问 | `http://<host>:8080/`（生产构建）或 `http://<host>:5173/`（Vite dev） |
| 端口 | 默认 8080（`KVM_PORT` 环境变量覆盖） |
| 数据备份 | 停服后 `cp /opt/project/QVMConsole/data/kvm_console.db{,.bak}`；恢复 `cp ... bak` 回滚 |

**运维入口**（与本设计 §5.10 相关）：

- [qvmc-manage.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/qvmc-manage.sh) — 直接在宿主机管理面板（重置密码、清除 TOTP、修改端口、**本设计改造后增加 firewalld 分支**）
- `/opt/project/QVMConsole/data/` — 数据库与上传缓存目录（持久卷）
- `/etc/libvirt/qemu.conf` — install.sh 已配 `user=root, group=root`（§13.7.2）
- `/etc/firewalld/zones/qvm-host.xml` — 本设计 §5.1 firewalld 后端落地产物（**面板升级 update 模式不触碰**）

**冒烟测试最小路径**（验证部署后基本可用）：

1. 浏览器打开 `http://<host>:8080/`，用 `admin/admin123` 登录
2. 进入「仪表盘」，确认无 5xx 报错
3. 进入「系统设置 → 诊断」，导出 `diagnostics.zip` 验证 §13.3.2 所有路由可达
4. 进入「任务中心」，确认无残留 `failed` 任务
5. 创建一台最小 VM（1C1G、Linux 模板）→ 开机 → SSH 进入 → 关机 → 删除
6. 若测后端能力：执行本设计 §8 防火墙用例中的 ufw 分支（测试机为 Ubuntu/Debian），确认 `GET /system-info` 返回 `firewall.backend=ufw`；国产系统 firewalld 主机另执行 §8 zone 绑定全链路用例，并核对 §5.1 决策 12 自检清单 ①-⑤（zone 激活 / qvm-host 已绑上行 / trusted 桥 / 保护端口放行 / dnsmasq 放行），同时确认 `firewall.upgrade_advice` 字段按 §4.1 口径上报

#### 13.7.6 CVE 缓解工具

[security/cve-2026-53359/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/security/cve-2026-53359/)：

- **CVE-2026-53359（Januscape）**：KVM x86 影子页表 use-after-free 漏洞（CVSS 7.0，存在 16 年），需 Guest root + 嵌套虚拟化启用 + /dev/kvm 可访问
- `auto-fix.sh`：检测内核版本/KVM 模块/嵌套虚拟化/dev/kvm 权限/运行中 VM → 高风险时禁用嵌套虚拟化（`/etc/modprobe.d/kvm-nested-disable.conf`）+ 重新加载 KVM 模块
- `restore.sh`：4 个恢复选项（完全恢复/仅删除配置/创建启用配置/仅重新加载 KVM 模块），内置安全检查防止在未修补内核上恢复
- 已修补内核版本：7.1.3+、6.18.38+、6.12.95+、6.6.144+、6.1.177+、5.15.211+、5.10.260+
- 风险级别判定：KVM 未加载=无风险 / 内核已修补+嵌套禁用=低 / 内核已修补+嵌套启用=中 / 内核未修补+嵌套禁用=中 / 内核未修补+嵌套启用=**高**

### 13.8 关键设计模式（本设计 §2 已分析，此处汇总）

| 模式 | 文件范例 | 适用场景 |
| --- | --- | --- |
| **ArchProfile 注册表** | [server/service/arch/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/arch/)`{types,registry,detect,x86_64,aarch64,domestic_cpu,normalize}.go` | 可插拔后端：接口 + 注册表 + sync.Once 缓存 + init() 自注册 + 未知回退 + 别名归一化（`NormalizeArch`/`IsX86Arch` 等，v0.12.3）。**本设计 §4.2 防火墙后端抽象直接复制此模式** |
| **Deps 结构体注入** | [server/service/vm/deps.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/vm/deps.go) + [vm_register.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/vm_register.go) + `main.go initCloneDeps()` | 重场景：子包需要调用根包大量函数。`Deps` 持有 50+ 函数字段，根包 `init()` 注入；镜像类型解决类型循环 |
| **Hook 函数变量** | [server/service/hooks.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/hooks.go) + 各子包 `hooks.go` + `*_register.go` | 轻场景：回调式依赖。子包定义 `HookXXX` 变量，根包 `init()` 赋值；wrapper 延迟解析规避 init 顺序问题 |
| **delegate 薄委托** | [server/service/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/)`{vm,clone,jwt_secret,quota_fs,resource_check}_delegate.go` | 保持 handler 层 `service.XXX()` 调用形式不变，向后兼容。`vm_delegate.go` 转发 100+ 函数 |
| **_wire.go 装配层** | [server/service/](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/)`*_wire.go`（22 个文件） | 跨子包 hook 装配 + 类型别名 + 委托函数。service 根包作为中间装配层打破子包间循环依赖 |
| **taskqueue progress 回调** | [server/taskqueue/queue.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/taskqueue/queue.go) + `main.go registerTaskHandlers()` | 长耗时操作异步化 + SSE 实时进度推送 + 取消机制 + 敏感参数脱敏。**本设计 §2.4 沿用** |
| **install.sh 交互式主流程** | [install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) `main()` + `run_install_or_update()` | 串行执行 + `read_user_input`/`read_tty` 交互选择（v0.13 起带 3 秒读秒倒计时）+ info/warn/success 风格。**本设计 §5.8 新增步骤沿用** |
| **前端架构专属显示** | `arch.DetectHostArch()` → 前端系统信息接口 | AGENTS.md「架构专属功能：前端仅在对应架构上显示」。**本设计 §2.6 防火墙后端信息同理** |
| **go:embed + sync.Once 缓存**（v0.9.4 计划新建，**已实施 M7.2**） | [server/service/diagnostics/component_health.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/diagnostics/component_health.go) + [server/service/arch/detect.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/arch/detect.go) | 构建期资产嵌入二进制（`//go:embed compat-manifest.json`）+ 运行时探测结果 `sync.Once` 缓存（避免频繁调用 `--version`）+ 手动刷新 API（`ResetComponentHealthCache()`）。**本设计 §5.11.5 新增，复用 `arch.DetectHostArch` 的 sync.Once 模式** |

### 13.9 本设计与项目架构的对应关系

本设计文档（§1-§12）的所有改造点都「长在现有骨架上」，对应关系：

| 本设计章节 | 改造点 | 依赖的现有架构（§13） |
| --- | --- | --- |
| §2.1 ArchProfile 注册表 | 复用为防火墙后端抽象 | §13.3.3 arch 子包 + §13.8 注册表模式 |
| §2.2 deps.go hook 注入 | 新增 `HookManageHostFirewallRule` | §13.8 Hook 函数变量模式 + [firewall_wire.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/firewall_wire.go) |
| §2.3 委托模式 | 新增 delegate 遵循现有风格 | §13.8 delegate 薄委托模式 |
| §2.4 taskqueue + progress | firewalld Enable/Disable 沿用 | §13.3.1 taskqueue + §13.8 progress 回调 |
| §2.5 install.sh 交互式 | 新增 `detect_firewall_backend` / `select_binary_tier` / `check_component_versions` 步骤 | §13.7.2 install.sh 18 主步骤 + §13.8 交互式主流程 |
| §2.6 前端架构专属显示 | HostFirewallTab 展示后端名 | §13.4.1 前端架构专属显示约定 |
| §4.1 GET /system-info | **v0.9.3 收敛**：复用现有接口增量扩展字段 | §13.3.2 路由分组（现有 authorized /system-info，version.go:123）+ §13.6.3 系统级依赖 |
| §4.2 防火墙后端抽象 | 新增 backend.go / backend_ufw.go / backend_firewalld.go | §13.3.3 firewall 子包 + §13.8 注册表/Deps/Hook 模式 |
| §4.3 GLIBC 档位与二进制选优 | build.sh `--high-compat-glibc` + install.sh `select_binary_tier` | §13.7.2 build.sh zig 构建 + install.sh `install_files` |
| §5.7 前端 HostFirewallTab | 后端名展示 + none Banner + 错误 hint | §13.4.2 路由 /firewall + §13.4.4 hooks + §13.4.5 store |
| §5.8 install.sh 设计 | `detect_firewall_backend` + `select_binary_tier` + `check_component_versions` + 安装日志 | §13.7.2 install.sh 18 主步骤 + §13.6.3 系统依赖 |
| §5.9 build.sh 设计 | `--high-compat-glibc` + `native-glibc.txt` | §13.7.2 build.sh + §13.6.3 zig + readelf |
| §5.10 qvmc-manage.sh 改造 | 增加 firewalld 分支 | §13.7.3 qvmc-manage.sh |
| **§5.11 组件版本检测**（v0.9.4 新增） | build.sh manifest + install.sh check + 后端 component_health + 前端诊断页 | §13.7.2 build.sh + §13.7.2 install.sh 18 主步骤 + §13.3.3 diagnostics 子包（已扩展 M7.2）+ §13.4.3 诊断页（现有）+ §13.8 go:embed + sync.Once 模式 |

### 13.10 设计待实施项与现状代码的差异（v0.9 澄清）

经核对当前代码库，§4-§5 中描述的以下产物最初均为**本设计的待实施产物**，经 M0-M7 里程碑逐一落地（当前 [build.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh) / [install.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh) 均已包含）：

> **实施进度标注（v0.9.x 更新）**：下列已标注 ✅ 的项已在 M0/M0.5/M1/M2/M3/M4/M5/M6/M7 里程碑落地（M7 为 v0.9.4 新增的组件版本检测闭环）。

| 待实施项 | 设计章节 | 现状 |
| --- | --- | --- |
| `native-glibc.txt` | §4.3 / §5.9 / #A | ✅ **已实施（M0.5）**：build.sh `write_native_glibc()`（L91-116）用 readelf 探测 native 二进制最高 GLIBC 依赖写 `native-glibc.txt`（native 构建后调用 L370）；install.sh 读该文件以 `sort -V` 比较取代 2.34 硬编码（缺失/非法内容时 `NATIVE_GLIBC_REQUIRED` 置空，native 资格失效，回落 compat / compat-2.28 判定，**无 2.34 回退**） |
| `kvm-console-compat-2.28` | §4.3 / §5.9 | ✅ **已实施（M4）**：build.sh 新增 `--high-compat-glibc VERSION`（默认空），`build_compat_tier()` 重构兼容构建（tier_glibc/tier_target/tier_output），指定时额外产出 `kvm-console-compat-{VER}` 并进发布包；install.sh `select_binary_tier` 三档决策 + `install_files` case 分支落位。**v0.9.11 审计修复（#AP）**：install.sh 不再硬编码 `compat-2.28`，从发行包动态发现最高 `kvm-console-compat-{VER}` 档 |
| `detect_firewall_backend` / `FW_BACKEND` | §4.4 / §5.8 / §6.1 | ✅ **已实施（M5）**：install.sh 新增 `detect_firewall_backend()`（FW_BACKEND 环境变量覆盖 → ufw → firewalld → none，白名单校验，RPM 系 firewalld 未运行仅 install 模式询问启动），结果持久化到 .env（`env_set FW_BACKEND`）供后端 `DetectHostFirewallBackend` 读取覆盖。**v0.9.11 审计修复**：update 模式优先复用 .env 持久化 FW_BACKEND（防自动探测静默切后端，M2）；`write_env` KVM_PORT 空时取 env_default 不清旧值（M3）；`virsh net-destroy default` 仅 install 模式（M4） |
| `select_binary_tier` / `KVM_BINARY_TIER` | §4.3 / §5.8 / §6.1 | ✅ **已实施（M5）**：install.sh 新增 `select_binary_tier()`（get_release 后调用）：探测 glibc（ldd 首行末 token → getconf GNU_LIBC_VERSION 回退）+ AVX2（/proc/cpuinfo）+ 读 native-glibc.txt，三档决策（native / compat-2.28 / compat）；非交互（CI=1 或非 TTY）自动采用推荐值，update 模式复用 .env 持久化值不弹窗；`install_files` 按 KVM_BINARY_TIER case 分支，切换前 `select_binary_smoke_test`（`--version` 冒烟，失败保留旧档）。**v0.9.10 产品评审批量修复（#AH）**：新增 `NATIVE_FEASIBLE` 在档位分支前计算，`print_install_report` 输出「当前 glibc 已满足 native 需求但沿用 compat 档」优化提示。**v0.9.11 审计修复（#AP）**：高兼容档版本改由 `HIGH_COMPAT_VER` 动态发现（白名单 `compat-*` + `sort -V` 取最高），`write_env` 与 update 持久化复用按动态档位白名单校验（`compat-${HIGH_COMPAT_VER}`），不再硬编码 2.28 |
| `--high-compat-glibc` | §4.3 / §5.9 | ✅ **已实施（M4）**：build.sh 参数解析（L44 默认空、L127 usage、L171-174 校验），`BUILD_COMPAT` 时先构建默认档 kvm-console，指定时经 `get_compat_zig_target` 追加构建高兼容档，打包段 chmod + 内容清单回显 |
| `/system-info` 扩展字段（glibc / cpu.avx2 / firewall.* / selinux.mode / upgrade_advice） | §4.1 / §5.6 | ✅ **已实施（M2）**：复用现有路由（router.go:535）与 handler/version.go，响应增量扩展 `glibc` / `cpu.avx2` / `fma` / `selinux.mode` / `firewall.{backend,available,active,version,ip_backend,nm_managed,upgrade_advice}`；endpointDescriptions.ts 已补文案，endpoints.json 重生成（314 端点）。**v0.9.10 产品评审批量修复（#AB-#AE）**：handler/version.go 重构为 `systemInfoSnapshot` 快照 + 30s 聚合 TTL 缓存（`getSystemInfoSnapshot`，锁内串行探测）；角色收敛——`glibc/cpu/selinux/firewall/component_health` 管理员专属，`kernel/qemu/libvirt/qemu_spice/arch` 保留公开（关于页/VM 表单依赖）；`resolveQEMUCmd()` 按架构解析（ARM 修复）；子进程统一 `utils.ExecCommandWithTimeout(5s)`。**v0.9.11 审计修复（#A9/#B2）**：`resolveQEMUCmd()` arm64 加 qemu-kvm 回退（无 qemu-system-aarch64 时）；`detectCPUFlags` 改 `strings.Fields` 分词（AVX2 旗标末尾空格漏判修复）；`DetectSELinuxMode` 单次调用 |
| `verify_compat_glibc` readelf 硬校验 | §5.9 | ✅ **已实施（M4）**：build.sh:69-80 无 readelf 时改为构建失败 + 输出 `apt-get install -y binutils` / `dnf install -y binutils`，不再 warn 跳过 |
| `Backend` 接口 / `backend_firewalld.go` / `backend_ufw.go` | §4.2 / §5.1 / §5.2 | ✅ **已实施（M0/M1/M2）**：`server/service/firewall/` 新增 backend.go（Backend 接口 + BackendStatus + backendMu 互斥锁 + firewallCommandPath 缓存）、backend_detect.go（探测序 + ResetFirewallBackendCache + resolveBackend + probeDockerCompatibility + detectNMZoneManaged）、backend_ufw.go（原 host.go 逻辑迁移）、backend_firewalld.go（zone 持久化 / trusted 绑定 / policy 放行 / 自检）、backend_none.go、backend_register.go、errors.go、advice.go（UpgradeAdvice + DetectUpgradeAdvice + DetectGlibcVersion + readNativeGlibcRequired + DetectSELinuxMode）、manage_rule.go（ManageHostFirewallRule 注入防护）；host.go 重写为纯编排；firewall_wire.go 接线 delegate。**v0.9.10 产品评审批量修复（#AJ/#AI/#AG）**：backendExec 注释文档化「锁内可安全调用清单 vs 严禁调用公共方法清单」（防重入死锁）；firewallCommandPath 复用 `utils.LookupCmdPath()`；advice.go `detectGlibcVersion` 统一走 `utils.DetectGlibcVersion()`。**v0.9.11 审计修复（#AN/#AO）**：deny 无来源规则不再落 `--add-port`（放行）改走 rich-rule（reject），`buildFirewalldRichRule` 来源可选，`parseFirewalldRichRuleLine` 动作 token 行尾匹配；`firewalldEnsureZoneExists`/`firewalldDeleteZone` <0.7 原子写/删 `qvm-host.xml`、`Disable()` `--delete-policy` 版本门控；`firewalldDefaultZone` 在 qvm-host 未创建时回退系统默认 zone 判定（#A7）。**注：firewall 子包实际 19 个 .go 文件，此处仅列与本设计改造直接相关者；其余 host_conn.go / host_portfwd.go / host_rules.go / rules.go / policy.go / types.go / deps.go / exemption.go 为本设计前已有或弱相关，其中 exemption.go 为端口转发区域豁免策略（PortForwardExemptions），不属于国产化适配范围** |
| `POST /firewall/host/reset-backend`（#R 重新检测） | §5.7 / #R | ✅ **已实施（M3）**：router.go:366 注册（行尾中文注释），handler/firewall.go `ResetHostFirewallBackendCache` → `service.ResetFirewallBackendCache()` 清探测缓存 + 立即重拉 `GetHostFirewallStatus()` 返回；web/src/api/firewall.ts `resetHostFirewallBackendCache()`，endpointDescriptions.ts 补文案，endpoints.json 重生成（315 端点） |
| `error_code` 实际上报 | §5.7 / #R | ✅ **已实施（M3）**：`GetFirewallBackendStatus` 捕获 `backend.Active()`/`Defaults()` 的结构化错误，`errorCodeOf()`（errors.go，errors.As 提取 `FirewallError.Code`）填充 `BackendStatus.ErrorCode`（此前该字段恒空）；host.go L30 透传到 HostFirewallStatus |
| 前端 HostFirewallTab 适配 | §5.7 | ✅ **已实施（M3）**：`web/src/views/firewall/components/HostFirewallTab.tsx` —— 「UFW」标签→「防火墙后端」显示 `backend_name`；`backend==='none'` 时横幅下方 warning Banner；端口转发放通中性措辞；`default_routed` 空→「未管理」Tag（Tooltip 依 `ip_backend` legacy/nf_tables 区分，#O）；`error_code` 非空→运行状态卡 hint + 修复命令 + 「重新检测」按钮（`resetHostFirewallBackendCache`，#R）；Enable 自检失败项清单（Tag 红 + Tooltip）+ 「回滚（关闭防火墙）」入口（走 `POST /firewall/host/disable` 二次确认，#L）；`upgrade_advice` 至多一条可关闭 Banner（firewalld_old > glibc_low > selinux，#Q） |
| 前端错误 hint / 自检失败数据流 | §5.7 / #R / #L | ✅ **已实施（M3）**：firewall/index.tsx —— `handleResetBackend`（reset + 替换 hostStatus）、`handleRollbackEnable`（confirmModal 二次确认 + 清空失败项）；挂载时拉 `/system-info` 取 `firewall.upgrade_advice` 存 `upgradeAdvice` state；订阅 `useTaskStore` tasks 解析 `enable_host_firewall` 失败任务的 `message`（正则 `自检失败[:：]...` 拆 `;`）得 `selfCheckFailures`，经 props 透传 HostFirewallTab |
| `web/src/api/firewall.ts` 类型扩展 | §5.7 | ✅ **已实施（M3）**：`HostFirewallStatus` 增 `backend?` / `backend_name?` / `ip_backend?` / `error_code?`；settings.ts `PublicSystemInfo` 增 `firewall?.upgrade_advice`（`UpgradeAdvice` 类型） |
| `firewall-page.md` 同步（#S） | §5.7 / #S | ✅ **已实施（M3）**：`docs/firewall-page.md` 「宿主机防火墙（UFW）」→「宿主机防火墙（后端抽象：UFW / Firewalld / none）」；运行状态卡措辞「UFW 可用性」→「防火墙后端可用性（backend_name）」；增 none Banner、转发默认未管理 Tag、错误 hint + 重新检测、启用自检回滚、组件升级提示行；接口清单补 `POST /firewall/host/reset-backend`；与旧版差异增 7/8 两条；不新增独立页面（§2.6） |
| 死码错误码产生点（`DBUS_ERROR` / `PERMISSION_DENIED` / `FIREWALLD_OLD_VERSION` / `ZONE_NOT_BOUND`） | §4.2 / #N / #O / #R | ✅ **已实施（v0.9.7 代码修订）**：`classifyFirewalldExecError` 将 `firewall-cmd` 挂死超时映射 `DBUS_ERROR`（hint `systemctl restart firewalld`）、stderr 权限不足映射 `PERMISSION_DENIED`；`firewalldEnsureForwardPolicy` 对 <0.9 版本返回 `FIREWALLD_OLD_VERSION`；`firewalldBindTrustedInterfaces` 绑定失败返回 `ZONE_NOT_BOUND`。均经 `Active()`/`Defaults()` 上抛到 `error_code`，消除 §13.10 此前「4 个错误码仅 errors.go 定义无产生点」的死码状态 |
| firewalld Enable 保留已放行规则（#U） | §5.1 / #U | ✅ **已实施（v0.9.9 评审修复）**：`backend_firewalld.go` Enable 前 `captureFirewalldZone()` 捕获端口/服务/来源/富规则，骨架写入后 `restoreFirewalldZoneContent()` 写回，最后统一 reload；`firewalldSelfCheck` 增受保护端口（SSH/面板）放行断言——修复「启用即擦除 SSH/面板端口」 |
| 防火墙后端漂移巡检（#X） | §4.2 / #X | ✅ **已实施（v0.9.9 评审修复）**：新增 `firewall_drift.go` `StartFirewallDriftMonitor()`（main.go 启动调用）：启动 1 分钟后首检、每日巡检，firewalld 服务未运行且面板曾启用（qvm-host zone 存在）时写 Warn 日志；不改后端解析规则（#H） |
| 宿主机规则 ID 与去重口径统一（#Y） | §5.3 / #Y | ✅ **已实施（v0.9.9 评审修复）**：`hostFirewallRuleID` 不再含备注（与 `mergeHostFirewallRules` 去重键一致），备注为元数据 |
| firewalld reload 失败重试（#AA） | §5.1 / #AA | ✅ **已实施（v0.9.9 评审修复）**：`firewalldReload()` 对临时性失败（dbus/daemon 抖动）重试 3 次（500ms 起退避），避免 `--add-port` 已写入永久配置但 reload 失败导致运行态与持久态不一致 |
| `DetectUpgradeAdvice` 缓存 | §4.1 / §5.11 / #Q | ✅ **已实施（v0.9.7）**：advice.go 带 TTL 缓存（10 分钟，RWMutex 双检），避免 `/system-info` 高频触发版本/glibc/selinux 重复探测；TTL 内 `upgrade_advice` 稳定，组件升级后超时自动重探 |
| `GetHostFirewallStatus` 探测复用 | §4.2 / §5.3 | ✅ **已实施（v0.9.7）**：host.go 复用 `GetFirewallBackendStatus` 结果（Active/Defaults/规则/IPBackend/Docker 一次探测），仅做保护标记 + 排序，消除 status 轮询时的重复子进程（原先 Active/Defaults/ListRules/Docker 均探测两次） |
| `UpdateHostFirewallRule` 等价规则误删修复 | §5.4 | ✅ **已实施（v0.9.7）**：规格等价（仅备注/保护标记不同）时直接返回，不再「ensure 命中跳过 + delete 删规则」导致规则丢失；规格变化仍先幂等 ensure 再 delete 旧规则 |
| M6 文档同步（build-compatibility.md / dependencies.md / ufw 注入转义核实） | §7 M6 / §4.3 / §5.8 | ✅ **已实施（M6）**：`docs/build-compatibility.md` 增三档结构（默认/高兼容/native）+ `native-glibc.txt` + `verify_compat_glibc` 硬校验（缺 readelf 构建失败）+ 构建示例；`docs/dependencies.md` 增「构建期依赖」节（binutils/zig）；`endpointDescriptions.ts` 已随 M3 补 reset-backend 文案；ufw 后端核实走结构化 argv（`exec.Command(name, args...)`，comment/CIDR 均为独立 argv），无 shell 拼接注入面 |
| `compat-manifest.json`（构建产物附件） | §5.11.3 / #T | ✅ **已实施（M7.0）**：build.sh 新增 `write_compat_manifest()`——编译前写 `server/service/diagnostics/compat-manifest.json`（native min 暂时 `pending`，供 go:embed），打包段写 `release/.../compat-manifest.json`（带真实 native min）；新增 `BUILD_COMPAT`/`BUILD_NATIVE` 布尔位，输出摘要补 manifest 行。**v0.9.9 评审修复（#V）**：阈值收敛为 build.sh `COMPONENT_REQ_*` 变量唯一维护，新增 `write_versions_conf()` 同源产出 `release/.../versions.conf` |
| `check_component_versions()` / `--skip-version-check` | §5.11.4 / #T | ✅ **已实施（M7.1）**：install.sh 新增 `check_component_versions()`（`detect_firewall_backend` 与 `select_binary_tier` 之间，get_release 后）：18 项探测（命令不存在/超时不阻塞），critical 中止 / warning 交互确认 / healthy 通过；`--skip-version-check` 降级 critical 为警告；manifest 缺失回退内置默认值；`STEP_TOTAL` 17→18 新增「组件版本检测」步骤；`print_install_report` 汇总行。**v0.9.9 评审修复（#V）**：改读 `versions.conf`（纯 shell，去掉 python3 依赖），min/rec 均读取，glibc 按架构取阈值；qemu-kvm 安装提示用真实包名。**v0.9.9 评审修复（#Z）**：批量缺失短路——先 `command -v` 探测全部命令（`PRESENT_CMDS`），缺失直接短路，不走 `timeout 5s` 探针。**v0.9.10 产品评审批量修复（#AL）**：versions.conf 解析合并为单循环直接覆盖阈值 |
| `component_health` 字段 / `ComponentHealthChecker` | §5.11.5 / #T | ✅ **已实施（M7.2）**：`server/service/diagnostics/component_health.go`（复用现有 `diagnostics` 子包，非新建 `system` 子包）——18 项 `--version` 探测 + manifest 阈值比对 + `GetComponentHealth`/`ResetComponentHealthCache` 缓存；`diagnostics_wire.go` 委托；main.go 启动异步预热。**v0.9.10 产品评审批量修复（#AF/#AG/#AI）**：缓存加 `healthCacheTTL=30min` 自动失效；`detectGlibcVersion` 统一走 `utils.DetectGlibcVersion()`；`detectVersion` 命令路径走 `utils.LookupCmdPath()` 缓存。**v0.9.11 审计修复（#A8）**：UEFI 固件探针对齐 arch profile 路径（x86 优先 `OVMF_CODE_4M.fd` 再 `OVMF_CODE.fd`；aarch64 补 `qemu-efi-aarch64/QEMU_EFI.fd`），避免仅有 4M 变体/efi 包的发行版误报缺失 |
| `POST /settings/diagnostics/refresh` 路由 + handler | §5.11.5 / #T | ✅ **已实施（M7.2）**：router.go `/settings/diagnostics` 组新增 `POST /diagnostics/refresh`（Auth + Admin，兼容 API Key），handler/diagnostics.go `RefreshDiagnostics` → 清缓存 + 立即重探返回。**v0.9.9 评审修复（#W）**：改走 `RefreshComponentHealth()`，带 5s 冷却单飞。**v0.9.10 产品评审批量修复（#AM）**：前端 DiagnosticsTab 刷新时比对 `last_check`，冷却命中提示「检测进行中」而非误导「已刷新」 |
| `go:embed compat-manifest.json` | §5.11.5 / #T | ✅ **已实施（M7.2）**：`component_health.go` 顶部 `//go:embed compat-manifest.json`（build.sh 编译前写入源码树）；manifest 缺失/非法时回退空阈值，仅展示当前版本不报错 |
| 前端诊断页「组件版本健康度」卡片 | §5.11.5 / #T | ✅ **已实施（M7.3）**：DiagnosticsTab.tsx 重构——状态 Tag（健康/警告/不满足/提示）+ 类别分区（core/disk/diag）+ 当前/最低/推荐版本 + 升级命令一键复制 + overall 汇总 + 「重新探测」按钮（`refreshDiagnostics()`）+ 「导出报告」按钮（组件健康度 JSON 下载）；settings.css 新增 stg-comp-* 样式 + 深色模式降对比 |
| **minisign 离线签名（§14.5 候选④，v0.12.1 已实施）** | §5.11.7 / §13.7.2 | ✅ **已实施（v0.12.1）**：构建侧 build.sh 用 minisign 私钥（`MINISIGN_KEY`/`--minisign-key`，缺失降级跳过仅 SHA256）对 `.tar.gz` 签名产出 `.minisig`；**打包前**按探测顺序（`--minisign-pub-file`/`MINISIGN_PUB_FILE` > `docs/minisign.pub` > 当前目录 `minisign.pub`）将公钥拷入包内 + release 目录（tarball 旁，供 install.sh 下载分支/本地安装同目录兜底）。安装侧 install.sh `extract_tarball` 解压前调 `verify_minisign_signature()`（实测须 `-V -m` 分开传参，`-Vm` 解析异常）——公钥优先级：内嵌 `MINISIGN_PUBLIC_KEY` > `INSTALL_DIR/minisign.pub` > 发行包同目录 `minisign.pub`（包内公钥因解压前验证不可用，不纳入）；有签名+公钥但验证失败 exit 1，无 minisign 命令/签名/公钥则降级 SHA256。密钥基建：离线私钥（`.minisign-sec/`，gitignore 不入库）+ 真实公钥已入库 `docs/minisign.pub` 与 install.sh 内嵌，签发流程见 `docs/minisign-publishing.md`，依赖登记 `docs/dependencies.md` |

**现状代码的硬编码行为**（本设计改造目标正是替换）：

- install.sh `install_files` 中以 `glibc >= 2.34 && AVX2` 硬编码判断切换 native —— ✅ **已替换（M0.5/M5）**：改为 `select_binary_tier` 三档决策 + `native-glibc.txt` 阈值
- [server/service/firewall/host.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/firewall/host.go) 直接调用 `ufw status verbose` / `ufw show added` / `ufw --force enable|disable` 等命令 —— ✅ **已替换（M0/M1）**：host.go 重写为纯编排，命令统一走 Backend 接口
- [server/service/network/helpers.go](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/network/helpers.go) 用 `fmt.Sprintf("ufw allow %s", rule)` 走 `ExecShell` —— ✅ **已替换（M2）**：改走 `HookManageHostFirewallRule`（结构化 argv + 白名单）
- [qvmc-manage.sh](file:///Volumes/cs/QVMConsole/jeoQVMConsole/qvmc-manage.sh) 修改端口时仅有 ufw 分支，无 firewalld 分支 —— ✅ **已实施（M5）**：FW_BACKEND env 优先探测，firewalld 分支用 `--permanent --add-port/--remove-port` + `--reload`，reload 失败显式报错

> **结论**：§1-§12 + §5.11 描述的是「目标设计」，§13.1-§13.9 描述的是「现状架构」。本设计的实施过程（§7 实施计划 M0-M7）就是将 §1-§12 + §5.11 的目标设计落地到 §13 描述的现状代码上，替换上述硬编码与缺失项。**M0-M7 已全部落地（✅）**，上表全部为已实施标注，设计文档与代码库现状完全对齐。**M8（§14 竞品差异吸收，v0.10 新增）12/12 已全部实施（v0.11 首批 5 条 + v0.12 剩余 7 条），不改变 M0-M7 基线。**

---

## 14. 竞品差异吸收（v0.10 新增，v0.10.1 与代码对照核实，对照 HCI 6.11 / SMTXOS 6.2）

> **背景**：对深信服 HCI 6.11.1 R1 x86 与 SmartX SMTXOS 6.2.0-P2 两个竞品 ISO 解压分析（完整分析见 `docs/差异分析.md`）。QVMConsole 与二者根本差异是「发布形态」（附加式面板 vs 自编译发行版 vs 自解压大包），但竞品在**安装可靠性、升级工程化、安装期安全基线、无人值守**四方面有 12 条不涉及造 OS 的可吸收优化点。
>
> **状态**：**12/12 全部已实施（✅）**——v0.11 首批 M8.1/M8.2/M8.4/M8.8/M8.12 + v0.12 剩余 M8.3/M8.5/M8.6/M8.7/M8.9/M8.10/M8.11（不改变 M0-M7 已落地基线）。实施前逐条走 §12 确认表。

### 14.1 目标对比摘要

| 维度 | SMTXOS / HCI 做法 | 本项目现状 | 吸收动作 |
|---|---|---|---|
| 安装可靠性 | SMTX 磁盘三级识别；HCI `ing/outcfg/die_file` 阶段状态 | `run_install_or_update` 靠 exit code，无 stage 文件 | P1-4 stage 持久化 + `--resume` |
| 升级健壮性 | HCI pid 锁 + df 预检 + 失败清理；SMTX hotfix `%pretrans` 备份 | `get_release` 无锁无空间预检、update 直接覆盖 | P1-6 回滚保留 + P1-7 包校验 |
| 数据库迁移 | SMTX elfvirt `%post` 版本化 SQL | `db.AutoMigrate` 无版本化 | P1-5 migrations/ 目录 |
| 命令审计 | SMTX `/var/log/bash.log` chattr +a | 无 | P2-8 setup_bash_audit |
| 国产 CPU | SMTX HygonGenuine 显式分支 | 仅 avx2/fma | P0-1 CPU 厂商细分 |
| firewalld | SMTX 0.4.x 直接 disabled | min=0.8 误报 critical | P0-2 三档分级 + min 0.4.0 |
| glibc 兼容 | SMTX el7 基线重新编译 | 理论兼容无实测 | P0-3 centos7-glibc217 CI |
| 观测 | SMTX consul + prometheus + bash 审计 | 手动诊断导出 | P2-9 watchdog/大页 + P2-10 健康探针 |
| 生态 | 官网 HCL 三档 | 无支持等级/认证矩阵 | P3-11 support_level S/A/B/C |
| 离线 | 依赖打包进 ISO | 直接命中系统源 | P3-12 镜像源自测 + offline（v0.13 补：openEuler 推荐 nju + 关键包探测后移避免慢源前置卡顿，见 §5.8/#AS/#AT） |

### 14.2 待实施里程碑 M8.1~M8.12

> **v0.10.1 代码对照后状态标注**：M8.2 已具备 `firewalldVersionAtLeast` 版本探测基础（缺口收窄为阈值 + Enable 分支 + advice 判定）；M8.4 已具备 `.env` chmod 600（缺口为目录权限 + stage 文件）；其余 M8 项均无已改代码基础，维持待实施。逐条复用点/落点见 §14.4。
>
> **v0.11 实施状态标注**：**M8.1 / M8.2 / M8.4 / M8.8 / M8.12 已实施**（详见 §14.4 各行的 ✅ 标注）。
>
> **v0.12 实施状态标注**：**M8.3 / M8.5 / M8.6 / M8.7 / M8.9 / M8.10 / M8.11 已实施**（详见 §14.4 各行的 ✅ 标注），**M8 全量 12/12 落地**。**v0.14 修正**：M8.1/M8.5/M8.9/M8.10/M8.11 的后端已迁入当前仓库 `jeochQVMConsole/server` 并验证（✅）。**v0.15 修正**：M8.9/M8.10/M8.11 的前端展示（看门狗配置分区 / HealthLight 灯 / vm-watchdog 事件页 / 诊断页 os-support 与面板运行状态卡）也已迁入当前仓库 `web/`（✅），M8 前后端全部对齐。

| 里程碑 | 内容 | 设计落点 | 依赖 | 风险 | 验收（§8 M8 用例块） |
|---|---|---|---|---|---|
| **M8.1** | **P0-1 CPU 厂商细分**：`server/service/arch/domestic_cpu.go` `DetectCPUVendor()` + `.env DOMESTIC_CPU_VENDOR` + manifest 键 `system_requirements.cpu_vendor.whitelist` + §5.11.2 cpu_vendor 行 | §5.8 / §5.11.2 / §5.11.3 / §4.1 | M7（component_health 口径） | 低 | ✅ 已实施（v0.11）：`DetectCPUVendor()`（Intel/AMD/Hygon/Phytium/Zhaoxin/Kunpeng）+ precheck_domestic 写 `.env` + component_health cpu_vendor 白名单项 |
| **M8.2** | **P0-2 firewalld 三档分级**：`COMPONENT_REQ_FIREWALLD` → `0.4.0|0.9.0` + `Enable()` <0.6 返回 FirewalldOldVersion | §5.1 决策 3 / §5.11.2 / §5.11.3 / §5.11.4 | 无（复用 `firewalldVersionAtLeast`） | 低 | ✅ 已实施（v0.11）：阈值 0.4.0 + `Enable()` `<0.6` 早退 + advice `firewalld_unsupported` 三档 |
| **M8.3** | **P0-3 glibc 2.17 真兼容**：build.yml `verify-centos7-glibc217` job + `--smoke-selfcheck` 子命令 | §4.3 / §5.9 / §8 | CI 镜像 | 中 | ✅ 已实施（v0.12）：docker centos:7 下 `--version` + `--smoke-selfcheck` 通过 |
| **M8.4** | **P1-4 stage 持久化 + 权限加固**：`.install_state/` + `--resume` + 目录 chmod 600/700 | §5.8 / §8 | 无 | 低 | ✅ 已实施（v0.11）：`.install_state/` stage/last_error + `--resume` + ensure_directories chmod 700/600 |
| **M8.5** | **P1-5 DB schema 版本化**：`migrations/` + gormigrate（或自写 migrate 表） | §13.3.5 / §14 | 无 | 中 | ✅ 已实施（v0.12）：`migrations/` 子包（schema_migrations 表 + 单事务），0001 scheduler_events 复合索引，跨版本升级零数据丢失 |
| **M8.6** | **P1-6 发行版回滚**：`INSTALL_DIR.bak-N` + `qvmc-manage.sh rollback` | §13.7.2 / §14 | 无 | 低 | ✅ 已实施（v0.12）：`.release_backup/{01-03}` 保留 3 份 + 菜单回滚，update 失败一键回滚 |
| **M8.7** | **P1-7 安装包完整性**：`SHA256SUMS` + minisign 签名校验 | §5.9 / §14 | 构建密钥 | 低 | ✅ 已实施（v0.12）：`SHA256SUMS` 生成 + extract_tarball 解压前校验，篡改包被拒（minisign 签名留待后续） |
| **M8.8** | **P2-8 bash 审计 + kdump 提示**：`setup_bash_audit` + `precheck_domestic` kdump 检测 | §5.8 / §8 | 无 | 低 | ✅ 已实施（v0.11）：`setup_bash_audit`（PROMPT_COMMAND + chattr +a / 降级 622）+ `check_kdump_suggestion` |
| **M8.9** | **P2-9 VM 看门狗 + 大页建议**：`vmwatchdog/` 子包 + hugepages 字段 | §13.3.3 / §14 | taskqueue 复用 | 中 | ✅ 已实施（v0.12）：`StartWatchdog()` 失联 3 次 HookResetVM + 事件入库；内存 ≥128GB 且无大页 → warning |
| **M8.10** | **P2-10 周期健康探针 + Dashboard 灯**：`periodic_probe.go` + `.health/latest.json` + 前端灯 | §13.3.3 / §14 | 前端 Dashboard | 中 | ✅ 后端已实施（v0.12，v0.14 迁入当前仓库）：`GET /api/system/health/latest` + `StartHealthProbe()`；**前端 HealthLight 30s 轮询灯位已迁移（v0.15）**：`web/src/api/health.ts` + `dashboard/components/HealthLight.tsx` + AdminDashboard 引入 |
| **M8.11** | **P3-11 支持等级 S/A/B/C + 硬件认证矩阵**：manifest `os_compat` 扩展 + 安装期 warn | §5.11.3 / §5.8 | 无 | 低 | ✅ 已实施（v0.12）：`support_level` + `certified_hardware`，C 级发行版安装时 warn 升级 |
| **M8.12** | **P3-12 国内镜像源自测 + offline 模式**：`test_mirror_speed` + `DEPS_MIRROR=offline` | §5.8 / §8 | 无 | 低 | ✅ 已实施（v0.11）：`test_mirror_speed`（openEuler 推荐 nju、清华/阿里计时选最快）+ offline 跳过 install 仅扫缺包；**v0.13（#AS）**探测顺序后移——关键包可用性在镜像切换后经 `probe_critical_rpm_packages()` 探测 |

> **实施优先级（ROI，同 `docs/差异分析.md` §5）**：P0-2 → P0-1 → P1-4 → P1-5 前 4 条（共约 5 人日）可堵住国产服务器上最常见的坑（firewalld 误报、CPU 无感知、安装不可恢复、DB 升级事故）；总计约 9 人日。

### 14.3 远程仓库分叉与合并口径

- **现状**：本地 `main` 已**合并 origin/main 全部功能提交**（VPC 交换机与安全组联动、用户账号管理、删除轻量云 VM、关于页技术栈折叠），分叉已消除——`d534974`（chore: merge origin/main 功能提交，消除远程分叉）合入后，`origin/main` 再无未并入的功能提交（当前 `origin/main` = `a2e9473`，本地领先 origin 14 个提交）。
- **上游关系（v0.12.1 更新）**：`git merge-base HEAD upstream/main == 51330e5`（upstream/main 最新 = `51330e5`），即 **HEAD 已包含 upstream/main 全部提交**，上游独立提交为空。后续同步上游只需按 `docs/merge-from-upstream.md` 定期拉取（只合并后端），无需处理分叉合并。
- **合并口径**：按 `docs/merge-from-upstream.md` 约定（只合并后端），合并时**必须保留本地全部国产化文件**（`server/service/firewall/backend*.go`、`server/service/diagnostics/component_health.go`、`docs/design-domestic-component-compat.md`、`build.sh`/`install.sh` 的国产化分支等），并重新核对 `router.go` 行号引用（`origin/main` 曾删除 `POST /settings/diagnostics/refresh` 与 `POST /firewall/host/reset-backend` 两路由注释，本地合入后需恢复——已通过 d534974 确认恢复）。
- **M8 新增代码与功能提交无冲突**：M8.1~M8.12 均为新增代码/脚本，不触碰 VPC/用户管理/轻量云功能路径，可独立排期（已实施部分 M8.1/M8.2/M8.4/M8.8/M8.12 已验证无冲突）。

### 14.4 M8 与已修改代码的对照核实（v0.10.1 新增）

> 逐条核对当前代码库（提交 `04d46ab`，含 M0-M7 + 审计修复），明确每个 M8 项的**可复用基础**、**具体落点**与**缺口**，供实施时直接定位。

| 里程碑 | 可复用代码基础（已存在） | 新增/修改落点 | 缺口范围 |
|---|---|---|---|
| **M8.1** | `detectCPUFlags()` [version.go:202](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/handler/version.go#L202)（`/proc/cpuinfo` Fields 分词模式） | ✅ **已实施（v0.11）**：`server/service/arch/domestic_cpu.go` 新增 `DetectCPUVendor()`；install.sh `precheck_domestic` 写 `.env DOMESTIC_CPU_VENDOR`；component_health 增 cpu_vendor 白名单项（`Whitelist` 字段）；version.go `/system-info` `cpu.cpu_vendor` | 已交付（~100 行 Go + 20 行 shell） |
| **M8.2** | `firewalldVersionAtLeast()` [backend_firewalld.go:810](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/firewall/backend_firewalld.go#L810)；`firewalldEnsureZoneExists` L403 / `firewalldDeleteZone` L643 的 `<0.7` 写文件分支；`FirewalldOldVersion` [errors.go:27](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/firewall/errors.go#L27) | ✅ **已实施（v0.11）**：[build.sh:74](file:///Volumes/cs/QVMConsole/jeoQVMConsole/build.sh#L74) 阈值 0.4.0；`Enable()` [L196](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/service/firewall/backend_firewalld.go#L196) `<0.6` 早退返回 FirewalldOldVersion；advice.go `firewalld_unsupported` 三档判定；前端 Banner + install.sh print_install_report 同步 | 已交付 |
| **M8.3** | `utils.DetectGlibcVersion()` [cmd.go:78](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/utils/cmd.go#L78)；`select_binary_smoke_test` [install.sh:2284](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L2284) | ✅ **已实施（v0.12）**：`server/main.go` 新增 `--smoke-selfcheck`（别名 `--db-probe`）+ `runSmokeSelfcheck()`（SQLite 内存库 AutoMigrate + libvirt 空连 2s 超时）；`LibvirtSocketPath()` 导出；build.yml `verify-centos7-glibc217` job | 已交付（~40 行 Go + ~15 行 yml） |
| **M8.4** | `.env` `chmod 600` [install.sh:1285](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L1285)（`write_env`） **已落地** | ✅ **已实施（v0.11）**：`ensure_directories` 补 chmod 700/600（/etc/kvm-console 系目录）；新增 `.install_state/`（stage/last_error/release_sha256/binary_tier 等）+ `--resume` 参数 | 已交付 |
| **M8.5** | `AutoMigrate` [model/db.go:92](file:///Volumes/cs/QVMConsole/jeoQVMConsole/server/model/db.go#L92)（28 张表）；现有 `migrateVPCBindingUniqueIndex` 等 11 个迁移函数（db.go:99-108）模式 | ✅ **已实施（v0.12）**：`server/model/migrations/` 子包（schema_migrations 表 + `Register`/`Run` 单事务），InitDB AutoMigrate 前调用；0001 scheduler_events `(vm_name,status)` 复合索引 | 已交付（~70 行 Go） |
| **M8.6** | `install_files()` [install.sh:2493](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L2493)（现直接 mv 覆盖） | ✅ **已实施（v0.12）**：`backup_previous_release()`（`.release_backup/{01|02|03}` 循环保留 3 份）+ update 分支停服后调用 + `INSTALL_DIR/.version`；qvmc-manage.sh `rollback_release()` + 菜单项 | 已交付（~60 行 shell） |
| **M8.7** | `extract_tarball()` [install.sh:2355](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L2355)（v0.12.1 起含 `verify_minisign_signature()` L2306 调用） | ✅ **已实施（v0.12）**：build.sh 打包段生成 tarball `.tar.gz.sha256` + 包内 `SHA256SUMS`；extract_tarball 解压前校验（不匹配 exit 1），下载分支拉 `${url}.sha256`。**v0.12.1**：新增 minisign 验签（见 §13.10） | 已交付（~35 行 shell） |
| **M8.8** | `precheck_domestic` [install.sh:2814](file:///Volumes/cs/QVMConsole/jeoQVMConsole/install.sh#L2814)（现有端口/多防火墙/NM 探测挂点） | ✅ **已实施（v0.11）**：新增辅助步骤 `setup_bash_audit`（PROMPT_COMMAND + chattr +a / 降级 622）；`check_kdump_suggestion`（systemd-detect-virt 裸金属 + crashkernel 判定） | 已交付 |
| **M8.9** | taskqueue 机制（§13.3.6，`taskqueue` 包可复用）；`server/service/` 子包模式 | ✅ **已实施（v0.12）**：`server/service/vmwatchdog/` 子包（`StartWatchdog()` 60s 周期、missCounts 内存计数、失联 3 次 HookResetVM + `VMWatchdogEvent` 入库；维护模式/无 libvirt 跳过）；`vmwatchdog_wire.go` Hook 注入；config `VMWatchdogEnabled/IntervalSeconds/MaxMisses`；component_health 增 `hugepages` 项（mem≥128GB 且 HugePages_Total=0 → warning）。**前端已迁移（v0.15）**：系统设置「VM 看门狗」分区（`AdvancedTab.tsx`，开关 + 间隔 + 阈值带校验）+「看门狗事件」页（`views/vm-watchdog/index.tsx` + `api/watchdog.ts`） | 已交付（~200 行 Go）；前端已迁移 |
| **M8.10** | `server/service/diagnostics/` 现有 collector.go；`main.go` 启动异步预热模式 | ✅ **后端已实施（v0.12，v0.14 迁入当前仓库）**：`periodic_probe.go`（`StartHealthProbe()` 每分钟原子写 `${KVM_HEALTH_DIR:-/opt/QVMConsole/.health}/latest.json`）；handler `health_probe.go`；public 路由 `GET /api/system/health/latest`。**前端已迁移（v0.15）**：`web/src/api/health.ts` + `dashboard/components/HealthLight.tsx`（30s 轮询）+ AdminDashboard 顶部灯位 + 诊断页「面板运行状态」卡 | 后端已交付（~150 行 Go）；前端已迁移 |
| **M8.11** | manifest 生成 `write_compat_manifest()`（build.sh，M7.0 已实施） | ✅ **已实施（v0.12）**：build.sh `os_compat` 段扩展 `support_level`/`certified_hardware` + versions.conf `SUPPORT_LEVEL_<os>`；嵌入 manifest 同步；install.sh `check_support_level()`（`support_level=C` → warn「理论兼容，生产请升级到认证基线」+ 报告复述）。**前端已迁移（v0.15）**：`api/settings.ts` `getOsSupport` + 诊断页「发行版支持等级」卡（S/A/B/C 标签 + certified_hardware + 当前系统高亮） | 已交付（manifest 结构 + 1 个判定 + ~30 行 shell）；前端已迁移 |
| **M8.12** | `check_and_install_deps`（install.sh L580）+ precheck_domestic 挂点 | ✅ **已实施（v0.11）**：新增 `test_mirror_speed`（openEuler 推荐 nju、清华/阿里 curl 计时取最快）+ `.env DEPS_MIRROR` + offline 分支（跳过 install 仅扫缺包汇总报告）。**v0.13 补（#AS）**：`enable_openeuler_repos` 不再对官方慢源 makecache/list，改 `probe_critical_rpm_packages()` 于 `apply_system_mirror` 之后探测 | 已交付 |

> **结论（v0.10.1）**：与已修改代码逐条对照后，**M8.2 与 M8.4 实施范围显著收窄**（复用已有版本探测与权限逻辑）；**M8.1/M8.3/M8.5/M8.11 有明确可复用锚点**；其余 M8 项为全新代码但落点已定位。建议实施顺序保持 P0-2 → P0-1 → P1-4 → P1-5，其中 **M8.2 预计 0.5 人日即可完成**（阈值 + Enable 早退 + advice 判定）。
>
> **结论（v0.11）**：**M8.1 / M8.2 / M8.4 / M8.8 / M8.12 共 5 条已实施交付**（P0-1/P0-2/P1-4/P2-8/P3-12），覆盖国产现场最高频的 firewalld 误报、CPU 无感知、安装不可恢复、操作审计、专网部署五个问题。
>
> **结论（v0.12）**：**M8 全量 12/12 已实施交付**——v0.11 首批 5 条 + v0.12 剩余 **M8.3（glibc 2.17 CI + --smoke-selfcheck）/ M8.5（DB migrations）/ M8.6（发行版回滚）/ M8.7（包校验）/ M8.9（VM 看门狗）/ M8.10（健康探针）/ M8.11（支持等级）**，M8 里程碑收尾。后续演进候选见 §14.5。

### 14.5 后续演进候选（v0.12 新增，M8 全量落地后可选）

M8 已全量实施（后端 v0.14 迁入当前仓库 + 前端 v0.15 迁入当前仓库，前后端已对齐），以下为可选演进方向（v0.12.1 已实施候选⑥；候选①②③⑤前端展示已随 v0.15 迁移，标注从「待迁移」更新为「已迁移」）：

1. **诊断面板接入健康探针实时数据（✅ 已实施 v0.12/0.14 后端 + v0.15 前端）**：`GET /api/system/health/latest` 已暴露；诊断页「面板运行状态」卡片（libvirt 就绪/daemon/维护模式/运行时长快照，与 `component_health` 卡并列）已迁入当前仓库 `web/`（`DiagnosticsTab.tsx`）。
2. **看门狗灵敏度配置界面（✅ 已实施 v0.15，前端配置分区已迁移）**：`KVM_VM_WATCHDOG_*` 已纳入 config 全链路（`keyToEnvVar`/`LoadFromDB`/`ToSettingsMap` + watchdog 每轮重读）；系统设置「调度与高级」Tab 的「VM 看门狗」分区（开关 + 探测间隔 + 失联次数阈值，带校验）已迁入当前仓库 `web/`（`AdvancedTab.tsx`）。
3. **支持等级矩阵前端展示（✅ 已实施 v0.12/0.14 后端 + v0.15 前端）**：`GET /settings/diagnostics/os-support`（`os_compat` + `meta` 等级定义）；诊断页「发行版支持等级」卡片（S/A/B/C 标签 + certified_hardware + 当前系统高亮）已迁入当前仓库 `web/`（`DiagnosticsTab.tsx`）。
4. **minisign 签名完整化（✅ 已实施 v0.12.1，见 §13.7.2/§13.10）**：M8.7 已交付 SHA256SUMS 校验；minisign 离线签名 + 安装期验证已落地（构建 `build.sh` 签名 + 安装 `install.sh` 验签 + 公钥分发链路补全 + 依赖登记）。v0.12.1 已生成真实密钥并公钥入库（`docs/minisign.pub` + install.sh 内嵌 `MINISIGN_PUBLIC_KEY`），私钥 `.minisign-sec/` gitignore 离线保管；CI `release` 可选签名待配置 `secrets.MINISIGN_KEY` 后启用。
5. **看门狗事件前端查看（✅ 已实施 v0.12/0.14 后端 + v0.15 前端）**：`GET /vm-watchdog/events`（分页 + status/vm_name/时间筛选）+ 系统菜单「看门狗事件」页（列表 + 类型/虚拟机/时间筛选）——接口后端已就绪；事件页已迁入当前仓库 `web/`（`views/vm-watchdog/index.tsx` + `config/nav.tsx` 菜单 + `router` 路由）。
6. **合并 `origin/main` 上游功能（✅ 已完成 v0.12.1）**：`d534974` 已将 `origin/main` 功能提交全部并入本地（§14.3），分叉已消除且 HEAD 已包含 upstream/main 全部提交；后续仅需定期拉取上游新提交（`docs/merge-from-upstream.md` 策略，只合并后端）。

**未完成 / 优化登记（v0.12.1 综合评估）**：

| 项 | 类别 | 状态 | 说明 |
| --- | --- | --- | --- |
| minisign 离线签名 | 供应链安全 | 已实施（v0.12.1） | 验证命令 `minisign -V -m`（`-Vm` 解析异常）；公钥分发链路补全（打包前拷入包内+同目录/内嵌/CI 上传）。CI `release` 签名待配置 `secrets.MINISIGN_KEY`（§13.7.2/§13.10） |
| 麒麟桌面版 V10 | 已知边界（明确排除） | 不再提示 | `install.sh:280` 已有 apt 回退（部分兼容），但按 §3.2/§4.3 口径「桌面版暂不分析」，正式排除、后续不再提示 |
| CI arm64 glibc 验证 | 工程化优化 | 已实现（v0.12.1） | `verify-centos7-glibc217` 仅覆盖 amd64；v0.12.1 新增 `verify-arm64-glibc-low` job（arm64 compat 档在 `arm64v8/ubuntu:20.04` glibc 2.31 冒烟，`centos:7` 无 arm64 变体），见 §13.7.2 |
| `release` job runner | 维护性 | 已说明 | 用 `ubuntu-latest` 但仅下载 artifact + 创建 Release、不编译 Go 二进制，无 glibc 漂移风险（§4.3/#A 注），保持现状 |
| minisign 公钥发布机制 | 供应链安全（配合项） | 已登记 | 启用 minisign 时公钥放仓库根/官网，避免与发布包同源可写；随候选④一并落地 |
