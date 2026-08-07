#!/bin/bash
# ============================================================
# QVMConsole 安装 / 更新 / 卸载脚本
# ============================================================

set -Eeuo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# #K：安装日志双写。LOG_FILE 由 init_log_file 初始化（ensure_directories 前亦可，目录按需创建）。
LOG_FILE=""
init_log_file() {
    local log_dir="${INSTALL_DIR}/logs"
    mkdir -p "$log_dir" 2>/dev/null || true
    LOG_FILE="${log_dir}/install-$(date +%Y%m%d-%H%M%S).log"
    : > "$LOG_FILE" 2>/dev/null || true
    # 保留最近 5 份滚动
    ls -1t "$log_dir"/install-*.log 2>/dev/null | tail -n +6 | xargs -r rm -f 2>/dev/null || true
}

# 写入日志文件（ANSI 转义剥离），文件不可写时静默降级
log_write() {
    [ -n "$LOG_FILE" ] || return 0
    printf '%s\n' "$*" >> "$LOG_FILE" 2>/dev/null || true
}

info() { echo -e "${GREEN}[INFO]${NC} $1"; log_write "[INFO] $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; log_write "[WARN] $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; log_write "[ERROR] $1"; }
success() { echo -e "${GREEN}[✓]${NC} $1"; log_write "[OK] $1"; }

# #K：步骤包装器，失败定位（§5.8）。用法：step "步骤名" fn arg...
# 非交互（CI=1 / 非 TTY）同样生效；被包装函数非零退出时打印定位信息并退出。
# 每步自动计时（%N 纳秒，部分环境退回整秒），耗时记入日志并在结尾 print_step_timing_summary 汇总。
STEP_NUM=0
STEP_TOTAL=0
STEP_TIMES_SUMMARY=""
step() {
    local name="$1"
    shift
    STEP_NUM=$(( STEP_NUM + 1 ))
    # P1-4：--resume 从失败步骤继续（skip 已完成的步骤）
    if [ "$RESUME_FROM" -gt 0 ] && [ "$STEP_NUM" -lt "$RESUME_FROM" ]; then
        info "[STEP ${STEP_NUM}/${STEP_TOTAL}] ${name}（--resume 已跳过）"
        return 0
    fi
    local t_start t_end t_cost
    t_start=$(date +%s.%N 2>/dev/null || date +%s)
    info "[STEP ${STEP_NUM}/${STEP_TOTAL}] ${name}"
    if ! "$@"; then
        local reason
        reason=$(tail -n 5 "$LOG_FILE" 2>/dev/null | tr '\n' ' ')
        state_set "last_error" "$name: ${reason:-见上方输出}"
        error "失败步骤: ${name}，原因: ${reason:-见上方输出}，完整日志: ${LOG_FILE:-（未启用）}"
        error "重试: 修复问题后执行 $0 --resume 从失败步骤继续"
        case "$name" in
            *防火墙*) echo "排查命令: systemctl status firewalld; journalctl -u firewalld -n 50" ;;
            *安装*)   echo "排查命令: journalctl -u $SERVICE_NAME -n 50" ;;
        esac
        exit 1
    fi
    # #K：记录本步耗时（用于定位耗时最久的环节）
    t_end=$(date +%s.%N 2>/dev/null || date +%s)
    t_cost=$(awk -v s="$t_start" -v e="$t_end" 'BEGIN{if (s ~ /N/ || e ~ /N/) {print "0"} else {printf "%.2f", e-s}}')
    STEP_TIMES_SUMMARY="${STEP_TIMES_SUMMARY}${STEP_NUM}|${name}|${t_cost}\n"
    log_write "[TIMING] STEP ${STEP_NUM}/${STEP_TOTAL} ${name} 耗时 ${t_cost}s"
    info "[STEP ${STEP_NUM}/${STEP_TOTAL}] ${name} 完成，耗时 ${t_cost}s"
    state_set "stage" "$STEP_NUM"
}

# ── P1-4：安装状态持久化（§5.8） ──
# stage=<已完成的最后步骤> / last_error=<失败步骤与原因>，供 --resume 续跑与失败定位。
state_set() {
    local key="$1" value="$2"
    mkdir -p "$STATE_DIR" 2>/dev/null || return 0
    local tmp
    tmp="$(mktemp "${STATE_DIR}/.state.XXXXXX")" 2>/dev/null || return 0
    local found=""
    if [ -f "$STATE_FILE" ]; then
        while IFS= read -r line; do
            case "$line" in
                "${key}="*)
                    printf '%s=%s\n' "$key" "$value" >> "$tmp"
                    found="1"
                    ;;
                *)
                    printf '%s\n' "$line" >> "$tmp"
                    ;;
            esac
        done < "$STATE_FILE"
    fi
    if [ -z "$found" ]; then
        printf '%s=%s\n' "$key" "$value" >> "$tmp"
    fi
    mv "$tmp" "$STATE_FILE"
    chmod 700 "$STATE_DIR"
    chmod 600 "$STATE_FILE"
}

state_get() {
    local key="$1"
    [ -f "$STATE_FILE" ] || return 1
    sed -n "s|^${key}=||p" "$STATE_FILE" | tail -n 1
}

APP_NAME="QVMConsole"
_DEFAULT_INSTALL_DIR="/opt/QVMConsole"
_LEGACY_INSTALL_DIR="/opt/kvm-console"
INSTALL_DIR="$_DEFAULT_INSTALL_DIR"
SERVICE_NAME="kvm-console"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
ENV_FILE="${INSTALL_DIR}/.env"
# §14.5 候选④：minisign 公钥（发行方随 install.sh 内嵌分发，用于安装期验签；单一来源，不做公钥文件探测）
# 完整公钥文本（untrusted comment 行 + base64）作为字符串内嵌；写临时文件后供 minisign -V -m 使用。
# 对应私钥由发行方离线保管（不入库），更换需按 docs/GCHSJ/minisign-publishing.md 轮换。
MINISIGN_PUBLIC_KEY="${MINISIGN_PUBLIC_KEY:-"untrusted comment: minisign public key F605F4243FA08760
RWRgh6A/JPQF9pcbwfp+pBgy4JHpuZTa2etSqEuKLO5wuxXutDGl9bQs"}"
# P1-4：安装状态持久化（§5.8，#V 竞品差异 P1-4；对齐 HCI vmp.pkg 阶段状态设计）
STATE_DIR="${INSTALL_DIR}/.install_state"
STATE_FILE="${STATE_DIR}/state"
RESUME_FROM=0
# 开源版官方下载源（按架构区分）
DOWNLOAD_URL_AMD64="https://download.xiaozhuhouses.asia/download/v1/links/YsxWkWgFPiZFrc8I0r2F8SpdLbhBA_O7PMnD0TDS0wM"
DOWNLOAD_URL_ARM64="https://download.xiaozhuhouses.asia/download/v1/links/SSr8OGj6KLbxHHKK746R_-CvpoFj1Skh9XIkjkNNzZ0"

STORAGE_IMG="/var/lib/kvm-user-storage.img"
STORAGE_MOUNT="/var/lib/kvm-user-storage"
STORAGE_DEFAULT_BACKING_DIR="/var/lib"
STORAGE_IMG_FILENAME="kvm-user-storage.img"
OVS_CONFIG_DIR="/etc/kvm-console/ovs"
OVS_STATE_DIR="/var/lib/kvm-console/ovs"
OVS_DNSMASQ_UNIT="kvm-console-ovs-dnsmasq.service"
OVS_DNSMASQ_SERVICE_FILE="/etc/systemd/system/${OVS_DNSMASQ_UNIT}"
PORT_FORWARD_DIR="/etc/kvm-portforward"
VM_ACCESS_DIR="/etc/libvirt/vm-access"
FIREWALL_DIR="/etc/kvm-console/firewall"
VPC_CONFIG_DIR="/etc/kvm-console/vpc"
# OVS systemd 单元名（detect_pkg_manager 按 PKG_MGR 填充；RPM=openvswitch，Debian=openvswitch-switch）
OVS_SERVICE_NAME=""

MODE=""
KVM_PORT=""
RELEASE_SOURCE_DIR=""
# H1 评审：高兼容档版本从发行包动态发现（kvm-console-compat-{VER}），不硬编码 2.28，
# 与 build.sh --high-compat-glibc 任意值对齐。select_binary_tier 前置填充。
HIGH_COMPAT_VER=""
# M7.1：--skip-version-check 指定时跳过组件版本检测的 critical 中止（不推荐），仅警告继续
SKIP_VERSION_CHECK=""
# M7.1：check_component_versions 汇总计数（print_install_report 复用）
COMP_VER_TOTAL=0
COMP_VER_HEALTHY=0
COMP_VER_WARN=0
COMP_VER_CRIT=0

APT_DEPS=(
    "ca-certificates"
    "curl"
    "tar"
    "gzip"
    "qemu-utils"
    "libvirt-daemon-system"
    "libvirt-daemon-driver-qemu"
    "libvirt-clients"
    "openvswitch-switch"
    "dnsmasq-base"
    "virtinst"
    "libguestfs-tools"
    "ntfs-3g"
    "genisoimage"
    "sshpass"
    "cloud-image-utils"
    "lvm2"
    "cloud-guest-utils"
    "quota"
    "e2fsprogs"
    "util-linux"
    "nftables"
    "iproute2"
    "iptables"
    "tcpdump"
    "ufw"
    "nmap"
    "arp-scan"
    "conntrack"
    "openssh-client"
    "openssh-server"
    "parted"
    "dmidecode"
    "psmisc"
    "swtpm"
)

# 架构特有依赖：在 check_and_install_deps 中根据 $ARCH 动态追加
QEMU_PKG_X86="qemu-system-x86"
EFI_PKG_X86="ovmf"
QEMU_PKG_ARM="qemu-system-arm"
EFI_PKG_ARM="qemu-efi-aarch64"

# ==================== RPM 系发行版包名映射 ====================
# key = APT_DEPS 中的 Debian 包名，value = 对应的 RPM 包名
# 注意：openEuler/麒麟 的 libvirt 为单一包（含 daemon+client），非 Debian 拆分方式
declare -A RPM_PKG_MAP
RPM_PKG_MAP=(
    ["ca-certificates"]="ca-certificates"
    ["curl"]="curl"
    ["tar"]="tar"
    ["gzip"]="gzip"
    ["qemu-utils"]="qemu-img"
    ["qemu-kvm"]="qemu-kvm"
    ["qemu"]="qemu"
    ["edk2-ovmf"]="edk2-ovmf"
    ["edk2-aarch64"]="edk2-aarch64"
    ["libvirt-daemon-system"]="libvirt"
    ["libvirt-daemon-driver-qemu"]=""          # openEuler 上已包含在 libvirt 包中
    ["libvirt-clients"]="libvirt-client"
    ["openvswitch-switch"]="openvswitch"
    ["dnsmasq-base"]="dnsmasq"
    ["virtinst"]="virt-install"
    ["libguestfs-tools"]="libguestfs-tools"
    ["ntfs-3g"]="ntfs-3g"
    ["genisoimage"]="genisoimage"
    ["sshpass"]="sshpass"
    ["cloud-image-utils"]="cloud-utils"
    ["lvm2"]="lvm2"
    ["cloud-guest-utils"]="cloud-utils-growpart"
    ["quota"]="quota"
    ["e2fsprogs"]="e2fsprogs"
    ["util-linux"]="util-linux"
    ["nftables"]="nftables"
    ["iproute2"]="iproute"
    ["iptables"]="iptables"
    ["tcpdump"]="tcpdump"
    ["ufw"]="firewalld"
    ["nmap"]="nmap"
    ["arp-scan"]="arp-scan"
    ["conntrack"]="conntrack-tools"
    ["openssh-client"]="openssh-clients"
    ["openssh-server"]="openssh-server"
    ["parted"]="parted"
    ["dmidecode"]="dmidecode"
    ["psmisc"]="psmisc"
    ["swtpm"]="swtpm"
)

# RPM 系架构特有包名（openEuler 官方文档确认）
# QEMU: openEuler 24.03 用 qemu-kvm，部分旧版/麒麟可能只有 qemu，安装时自动回退
# UEFI: x86 用 edk2-ovmf，AArch64 用 edk2-aarch64
QEMU_PKG_X86_RPM="qemu-kvm"
QEMU_PKG_X86_RPM_FALLBACK="qemu"
EFI_PKG_X86_RPM="edk2-ovmf"
QEMU_PKG_ARM_RPM="qemu-kvm"
QEMU_PKG_ARM_RPM_FALLBACK="qemu"
EFI_PKG_ARM_RPM="edk2-aarch64"

# RPM 系中可能不存在的可选包（缺失时不报错，仅警告）
# 这些包在部分麒麟/openEuler 源中可能不存在或包名不同
# 注意：genisoimage 在 apt 系是 APT_DEPS 必装项，此处标记为 RPM 可选（可用 xorriso 替代）
RPM_PKG_SOFT=(
    "libguestfs-tools"
    "cloud-utils"
    "cloud-utils-growpart"
    "genisoimage"
    "arp-scan"
)

PKG_MGR=""
# 发行版 ID（detect_pkg_manager 填充，供 test_mirror_speed/apply_rpm_mirror 等按发行版分支）
OS_ID=""

# ==================== 包管理器辅助函数 ====================

# detect_pkg_manager 检测当前系统的包管理器 (apt/dnf/yum)
detect_pkg_manager() {
    PKG_MGR=""
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        local os_id="${ID:-}"
        local os_like="${ID_LIKE:-}"
        os_id="${os_id,,}"
        os_like="${os_like,,}"
        OS_ID="$os_id"

        # Debian/Ubuntu 系列
        if [[ "$os_id" == "ubuntu" ]] || [[ "$os_id" == "debian" ]]; then
            PKG_MGR="apt"
        elif [[ "$os_like" == *"debian"* ]] || [[ "$os_like" == *"ubuntu"* ]]; then
            if command -v apt-get &>/dev/null; then
                PKG_MGR="apt"
            fi
        fi

        # 显式识别已知 RPM 系发行版
        if [ -z "$PKG_MGR" ]; then
            case "$os_id" in
                kylin|neokylin|openeuler|centos|rhel|anolis|rocky|alma|fedora)
                    if command -v dnf &>/dev/null; then
                        PKG_MGR="dnf"
                    elif command -v yum &>/dev/null; then
                        PKG_MGR="yum"
                    else
                        # 麒麟桌面版为 Debian 系（ID_LIKE 常为空），RPM 命令缺失时回退 apt
                        if command -v apt-get &>/dev/null; then
                            PKG_MGR="apt"
                        fi
                    fi
                    ;;
            esac
        fi

        # ID_LIKE 继承链（仅当上方未匹配时）
        if [ -z "$PKG_MGR" ] && { [[ "$os_like" == *"rhel"* ]] || [[ "$os_like" == *"fedora"* ]] || [[ "$os_like" == *"kylin"* ]] || [[ "$os_like" == *"openeuler"* ]]; }; then
            if command -v dnf &>/dev/null; then
                PKG_MGR="dnf"
            elif command -v yum &>/dev/null; then
                PKG_MGR="yum"
            fi
        fi

        # 通用 RPM 回退：优先 dnf，回退 yum
        if [ -z "$PKG_MGR" ]; then
            if command -v dnf &>/dev/null; then
                PKG_MGR="dnf"
            elif command -v yum &>/dev/null; then
                PKG_MGR="yum"
            fi
        fi
    fi

    # 最终回退：按命令可用性检测
    if [ -z "$PKG_MGR" ]; then
        if command -v apt-get &>/dev/null; then
            PKG_MGR="apt"
        elif command -v dnf &>/dev/null; then
            PKG_MGR="dnf"
        elif command -v yum &>/dev/null; then
            PKG_MGR="yum"
        else
            error "未检测到支持的包管理器 (apt/dnf/yum)"
            exit 1
        fi
    fi

    # OVS systemd 单元名（v0.9.11 审计修复：RPM 系为 openvswitch，Debian 为 openvswitch-switch）
    case "$PKG_MGR" in
        apt) OVS_SERVICE_NAME="openvswitch-switch" ;;
        dnf|yum) OVS_SERVICE_NAME="openvswitch" ;;
    esac
}

# pkg_name 将 Debian 包名转换为当前系统的 RPM 包名，Debian 系原样返回
pkg_name() {
    case "$PKG_MGR" in
        apt) echo "$1" ;;
        dnf|yum)
            local rpm_name="${RPM_PKG_MAP[$1]:-}"
            if [ -z "$rpm_name" ]; then
                # 无映射则跳过（该包在此发行版不可用）
                return 1
            fi
            echo "$rpm_name"
            ;;
    esac
}

# run_with_progress 包装耗时子命令（组件下载 / 源刷新等），周期性输出心跳提示，
# 避免长时间无输出被误判为卡死。$1=操作描述  $2=quiet（1=隐藏子命令原始输出仅保留
# 心跳，用于 makecache 等冗长输出；0=透传子命令原始输出并叠加心跳，用于组件下载）。
# 其余参数为要执行的命令；返回子命令退出码。
run_with_progress() {
    local desc="$1" quiet="$2"
    shift 2
    local t0 pid rc el
    t0=$(date +%s 2>/dev/null || echo 0)
    info "开始: ${desc}（可能耗时较长，请耐心等待）..."
    if [ "$quiet" = "1" ]; then
        "$@" &>/dev/null 2>&1 &
    else
        "$@" &
    fi
    pid=$!
    # 子进程结束后为僵尸态（kill -0 仍会命中），改用 ps 状态判断是否真正结束
    while ps -p "$pid" -o stat= 2>/dev/null | grep -qv '^Z'; do
        sleep 15
        el=$(( $(date +%s 2>/dev/null || echo 0) - t0 ))
        [ "$el" -lt 0 ] && el=0
        info "仍在执行: ${desc}（已耗时 ${el}s，请继续等待）..."
    done
    wait "$pid" || rc=$?
    return "${rc:-0}"
}

# pkg_install 安装指定包，失败时指数退避重试（1s→2s→4s），最多 3 次
pkg_install() {
    local rc=0
    local max_retries=3
    local attempt=0
    local delay=1

    while [ $attempt -le $max_retries ]; do
        rc=0
        case "$PKG_MGR" in
            apt)
                wait_apt_dpkg_lock
                run_with_progress "下载并安装依赖包（apt，共 $# 个）" 0 env DEBIAN_FRONTEND=noninteractive apt-get install -y "$@" || rc=$?
                ;;
            dnf) run_with_progress "下载并安装依赖包（dnf，共 $# 个）" 0 dnf install -y "$@" || rc=$? ;;
            yum) run_with_progress "下载并安装依赖包（yum，共 $# 个）" 0 yum install -y "$@" || rc=$? ;;
        esac
        if [ "$rc" -eq 0 ]; then
            return 0
        fi
        attempt=$((attempt + 1))
        if [ $attempt -le $max_retries ]; then
            warn "包安装失败（exit $rc），${delay}s 后重试（${attempt}/${max_retries}）..."
            sleep "$delay"
            delay=$((delay * 2))  # 指数退避：1s → 2s → 4s
        fi
    done

    warn "批量安装失败（exit $rc），尝试逐个安装..."
    local pkg install_rc hint out
    for pkg in "$@"; do
        install_rc=0
        hint=""
        out=""
        info "逐个安装: ${pkg} ..."
        case "$PKG_MGR" in
            apt)
                out=$(DEBIAN_FRONTEND=noninteractive apt-get install -y "$pkg" 2>&1) || install_rc=$?
                ;;
            dnf)
                # 单包失败时捕获输出尾部（排除下载进度，便于 openEuler 等源问题排查），
                # 同时带超时避免坏源挂起；一次调用既装包又拿错误信息，不重复执行
                out=$(dnf --setopt=timeout=20 --setopt=minrate=1000 --setopt=retries=1 install -y "$pkg" 2>&1) || install_rc=$?
                ;;
            yum)
                out=$(yum install -y "$pkg" 2>&1) || install_rc=$?
                ;;
        esac
        if [ "$install_rc" -ne 0 ]; then
            hint=$(printf '%s\n' "$out" | grep -viE '^\s*Downloading|^\s*[0-9]+%|Progress' | tail -n 4 || true)
            warn "  $pkg 安装失败（exit $install_rc）${hint:+: $hint}"
            log_write "[WARN] pkg_install individual failed: $pkg (exit $install_rc) $hint"
        fi
    done
}

# wait_apt_dpkg_lock 等待 apt/dpkg 锁释放（参考宝塔 Fix_Apt_Lock），最多等待 60s，
# 强制清理卡死进程并执行 dpkg --configure -a 修复（仅 apt 分支）。
wait_apt_dpkg_lock() {
    [ "$PKG_MGR" = "apt" ] || return 0
    local wait_seconds=0 max_wait=60
    info "等待 apt/dpkg 锁释放（最多 ${max_wait}s）..."
    while fuser /var/lib/dpkg/lock >/dev/null 2>&1 || \
          fuser /var/lib/apt/lists/lock >/dev/null 2>&1 || \
          fuser /var/cache/apt/archives/lock >/dev/null 2>&1; do
        if [ "$wait_seconds" -ge "$max_wait" ]; then
            warn "apt/dpkg 锁超时（${max_wait}s），尝试强制清理..."
            # 杀掉持锁进程
            local pids
            pids=$(fuser /var/lib/dpkg/lock /var/lib/apt/lists/lock /var/cache/apt/archives/lock 2>/dev/null)
            for pid in $pids; do
                kill -9 "$pid" 2>/dev/null || true
            done
            # 修复 dpkg 状态
            dpkg --configure -a 2>/dev/null || true
            apt-get install -f -y 2>/dev/null || true
            break
        fi
        sleep 1
        wait_seconds=$((wait_seconds + 1))
    done
    if [ "$wait_seconds" -gt 0 ] && [ "$wait_seconds" -lt "$max_wait" ]; then
        info "apt/dpkg 锁已释放（等待 ${wait_seconds}s）"
    fi
}

# pkg_update_index 更新包索引
pkg_update_index() {
    case "$PKG_MGR" in
        apt)
            wait_apt_dpkg_lock
            run_with_progress "更新软件源索引（apt-get update）" 0 apt-get update
            ;;
        dnf|yum) : ;;  # RPM 系通常不需要单独更新索引
    esac
}

# is_pkg_installed 检查指定包是否已安装
is_pkg_installed() {
    case "$PKG_MGR" in
        apt) dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q "install ok installed" ;;
        dnf|yum) rpm -q "$1" &>/dev/null ;;
    esac
}

# pkg_is_available 检查包在当前源中是否可用（仅 RPM 系）
# 注意：dnf repoquery 需要 dnf-plugins-core，缺失时自动安装
pkg_is_available() {
    case "$PKG_MGR" in
        apt) apt-cache show "$1" &>/dev/null ;;
        dnf)
            # dnf repoquery 需要 dnf-plugins-core，缺失时先安装
            if ! dnf repoquery --available "$1" &>/dev/null 2>&1; then
                if ! rpm -q dnf-plugins-core &>/dev/null 2>&1; then
                    dnf install -y dnf-plugins-core &>/dev/null 2>&1 || true
                fi
                dnf repoquery --available "$1" &>/dev/null
            fi
            ;;
        yum) yum list available "$1" &>/dev/null ;;
        *) return 1 ;;
    esac
}

COMMAND_CHECKS=(
    "virsh"
    "qemu-img"
    "virt-install"
    "virt-filesystems"
    "virt-customize"
    "guestfish"
    "virt-win-reg"
    "ntfsclone"
    "ntfsfix"
    "ntfsresize"
    "genisoimage"
    "sshpass"
    "ovs-vsctl"
    "ovs-ofctl"
    "dnsmasq"
    "nft"
    "ip"
    "iptables"
    "tcpdump"
    "tc"
    "setquota"
    "repquota"
    "chattr"
    "mkfs.ext4"
    "lsblk"
    "findmnt"
    "blkid"
    "wipefs"
    "mount"
    "growpart"
    "parted"
    "partprobe"
)

cleanup_tmp() {
    if [ -n "${TMP_RELEASE_DIR:-}" ] && [ -d "$TMP_RELEASE_DIR" ]; then
        rm -rf "$TMP_RELEASE_DIR"
    fi
}
trap cleanup_tmp EXIT

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error "请使用 root 用户或 sudo 运行此脚本"
        exit 1
    fi
}

# enable_openeuler_repos 在 openEuler 上启用必要的仓库（EPOL + everything）
# 现状（openEuler 24.03 SPx 实测）：系统自带 openEuler.repo 已默认启用
# [everything]/[EPOL]/[update]（含 metalink），路径带 SP 后缀
# （如 openEuler-24.03-LTS-SP4/，非 openEuler-24.03-LTS/）。
# 因此本函数只做三件事：① 清理历史遗留的 CentOS 风格 kvm-console 坏源；
# ② 确认关键 Section 存在（缺失才补写，避免与系统文件重复）；③ 带超时刷新缓存。
# 镜像结构（linuxmirrors.cn 同款）：{base}/openEuler-{version}/{OS|everything|EPOL/main|update}/{arch}
enable_openeuler_repos() {
    if [ ! -f /etc/os-release ]; then
        return 0
    fi
    . /etc/os-release
    local os_id="${ID:-}"
    os_id="${os_id,,}"
    [ "$os_id" = "openeuler" ] || return 0

    local version_id="${VERSION_ID:-}"
    info "检测到 openEuler ${version_id}，检查仓库状态..."

    # 清理历史遗留的 CentOS 风格 kvm-console-local-mirror 源文件。
    # 早期版本 apply_rpm_mirror 曾在 openEuler 上写入 centos-vault 镜像源，
    # 该源 repomd.xml 404 会拖垮所有 dnf 操作（且文件残留跨次运行），必须移除。
    rm -f /etc/yum.repos.d/*kvm-console* /etc/yum.repos.d/*KVM-Console* \
        /etc/yum.repos.d/*local-mirror* 2>/dev/null || true

    # 从系统现有仓库文件探测实际版本目录（SP 后缀优先，如 openEuler-24.03-LTS-SP4），
    # 保证 everything/EPOL 补写时与系统其它源同目录，避免 SP 与基础版混用。
    # 优先解析系统自带 openEuler.repo（避免被旧版遗留的 openeuler-*.repo 非 SP 路径干扰）。
    local repo_suffix=""
    repo_suffix=$(grep -rhoE "openEuler-2[0-9]+\.[0-9]+-LTS-SP[0-9]+" /etc/yum.repos.d/openEuler.repo 2>/dev/null | head -1 || true)
    [ -z "$repo_suffix" ] && repo_suffix=$(grep -rhoE "openEuler-2[0-9]+\.[0-9]+-LTS" /etc/yum.repos.d/openEuler.repo 2>/dev/null | head -1 || true)
    [ -z "$repo_suffix" ] && repo_suffix=$(grep -rhoE "openEuler-2[0-9]+\.[0-9]+-LTS(-SP[0-9]+)?" /etc/yum.repos.d/*.repo 2>/dev/null | sort -u | head -1 || true)
    if [ -z "$repo_suffix" ]; then
        case "$version_id" in
            20.*) repo_suffix="openEuler-20.03-LTS" ;;
            22.*) repo_suffix="openEuler-22.03-LTS" ;;
            24.*) repo_suffix="openEuler-24.03-LTS" ;;
            *)    repo_suffix="openEuler-${version_id}" ;;
        esac
    fi
    info "openEuler 仓库目录: ${repo_suffix}"

    # 架构映射：此时 ARCH 尚未设置（check_os 在 detect_arch 之前调用），用 uname 实时探测
    local host_arch
    host_arch=$(uname -m)
    local repo_arch="$host_arch"
    case "$host_arch" in
        aarch64|arm64) repo_arch="aarch64" ;;
        x86_64|amd64)  repo_arch="x86_64" ;;
    esac

    # 各源基地址。DEPS_MIRROR 已配置镜像时优先使用（结构同官方 repo.openeuler.org）；
    # 镜像切换由 apply_rpm_mirror 统一负责（重写系统源 baseurl 主机），这里仅用于探测与补写。
    local base_url="https://repo.openeuler.org"
    local mirror_name
    mirror_name=$(env_get "DEPS_MIRROR" || true)
    case "$mirror_name" in
        nju)      base_url="https://mirrors.nju.edu.cn/openeuler" ;;
        tsinghua) base_url="https://mirrors.tuna.tsinghua.edu.cn/openeuler" ;;
        aliyun)   base_url="https://mirrors.aliyun.com/openeuler" ;;
    esac

    # 探测并补写缺失的 Section。已有 Section（系统自带）不重复写文件，避免 dnf 报重名告警。
    # 24.03 起 EPOL 结构为 EPOL/main/{arch}；20.03 早期为 EPOL/{arch}。
    local epol_exists everything_exists
    epol_exists=$(grep -rlE '^\[EPOL\]' /etc/yum.repos.d/*.repo 2>/dev/null | head -1 || true)
    everything_exists=$(grep -rlE '^\[everything\]' /etc/yum.repos.d/*.repo 2>/dev/null | head -1 || true)

    # 清理旧版本误生成的重复 Section 文件：若系统自带 openEuler.repo 已含同名 Section，
    # 则删除我们补写的重复文件（避免 dnf 报 "repo 'EPOL' 重复" 且 URL 可能指向过时镜像）。
    if [ -n "$epol_exists" ] && [ -f /etc/yum.repos.d/openeuler-epol.repo ]; then
        if [ "$epol_exists" != "/etc/yum.repos.d/openeuler-epol.repo" ]; then
            rm -f /etc/yum.repos.d/openeuler-epol.repo
        fi
    fi
    if [ -n "$everything_exists" ] && [ -f /etc/yum.repos.d/openeuler-everything.repo ]; then
        if [ "$everything_exists" != "/etc/yum.repos.d/openeuler-everything.repo" ]; then
            rm -f /etc/yum.repos.d/openeuler-everything.repo
        fi
    fi

    if [ -n "$epol_exists" ]; then
        success "openEuler EPOL 仓库已配置（${epol_exists}）"
    else
        local epol_url="${base_url}/${repo_suffix}/EPOL/main/${repo_arch}/"
        local epol_alt_url="${base_url}/${repo_suffix}/EPOL/${repo_arch}/"
        local epol_use="${epol_url}"
        if ! curl -sSL -m 10 -o /dev/null -w '%{http_code}' "${epol_url}repodata/repomd.xml" 2>/dev/null | grep -q '^200$'; then
            epol_use="${epol_alt_url}"
        fi
        cat > /etc/yum.repos.d/openeuler-epol.repo <<EOF
[EPOL]
name=EPOL - ${repo_suffix}
baseurl=${epol_use}
enabled=1
gpgcheck=0
EOF
        if curl -sSL -m 10 -o /dev/null -w '%{http_code}' "${epol_use}repodata/repomd.xml" 2>/dev/null | grep -q '^200$'; then
            success "openEuler EPOL 仓库已补写: ${epol_use}"
        else
            warn "openEuler EPOL 仓库不可用（${epol_url} / ${epol_alt_url} 均 404），将依赖 everything 与捆绑包"
        fi
    fi

    if [ -n "$everything_exists" ]; then
        success "openEuler everything 仓库已配置（${everything_exists}）"
    else
        local everything_url="${base_url}/${repo_suffix}/everything/${repo_arch}/"
        cat > /etc/yum.repos.d/openeuler-everything.repo <<EOF
[everything]
name=everything - ${repo_suffix}
baseurl=${everything_url}
enabled=1
gpgcheck=0
EOF
        if curl -sSL -m 10 -o /dev/null -w '%{http_code}' "${everything_url}repodata/repomd.xml" 2>/dev/null | grep -q '^200$'; then
            success "openEuler everything 仓库已补写: ${everything_url}"
        else
            warn "openEuler everything 仓库不可用（${everything_url}），OVS 等包可能需要手动安装"
        fi
    fi
}

# 关键包可用性探测：必须在镜像源切换（apply_system_mirror）之后调用，否则
# dnf makecache / list available 会命中官方慢源（metalink 未禁用）导致安装前期卡顿。
# 仅在镜像模式（非 system/offline）下执行，超时兜底、失败仅 warn 不阻断。
probe_critical_rpm_packages() {
    if [ "$PKG_MGR" = "apt" ]; then
        return 0
    fi
    local mirror="${DEPS_MIRROR:-system}"
    if [ "$mirror" = "system" ] || [ "$mirror" = "offline" ]; then
        return 0
    fi
    # dnf 超时兜底：坏源/慢源会挂起所有 dnf 操作，统一带上连接与最低速率阈值
    # （minrate=10KB/s：低于该速率直接判慢源快速放弃，避免下载超大 metadata 时长期卡死）
    local dnf_slow=(--setopt=timeout=20 --setopt=minrate=10000 --setopt=retries=1)
    run_with_progress "刷新软件源元数据缓存（dnf makecache，探测关键包可用性）" 1 dnf "${dnf_slow[@]}" makecache || true

    # openEuler 24.03 everything 源中 qemu-img 是独立包（qemu 系拆分），与 qemu 同时存在；
    # qemu-kvm 不存在（安装时自动回退 qemu），故同时检测 qemu-img 与 qemu
    local critical_pkgs=("openvswitch" "dnsmasq" "qemu-img" "qemu" "sshpass")
    local unavailable_pkgs=()
    local pkg
    for pkg in "${critical_pkgs[@]}"; do
        if ! dnf "${dnf_slow[@]}" list available "$pkg" &>/dev/null 2>&1; then
            unavailable_pkgs+=("$pkg")
        fi
    done
    if [ ${#unavailable_pkgs[@]} -gt 0 ]; then
        warn "以下关键包在已启用的仓库中不可用: ${unavailable_pkgs[*]}"
        warn "安装流程将尝试从系统源安装，失败时使用捆绑包兜底"
    fi
}

check_os() {
    if [ ! -f /etc/os-release ]; then
        error "无法识别操作系统"
        exit 1
    fi
    . /etc/os-release
    detect_pkg_manager
    info "检测到系统: ${PRETTY_NAME:-unknown}，包管理器: $PKG_MGR"
    enable_openeuler_repos
}

detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64|amd64)
            ARCH="x86_64"
            ;;
        aarch64|arm64)
            ARCH="aarch64"
            ;;
        *)
            error "不支持的 CPU 架构: $ARCH，仅支持 x86_64 / aarch64"
            exit 1
            ;;
    esac
    info "检测到 CPU 架构: ${ARCH}"
}

check_locale() {
    local lang="${LANG:-}"
    local lc_all="${LC_ALL:-}"
    local current="${lc_all:-$lang}"

    # 如果 LANG 和 LC_ALL 都为空，尝试用 locale 命令获取
    if [ -z "$current" ]; then
        current=$(locale 2>/dev/null | awk -F= '/^LANG=/ {print $2}' | tr -d '"' || true)
    fi

    # Linux locale 名大小写不敏感（zh_CN.UTF-8 与 zh_CN.utf8 等价），统一下转小写后按编码判定
    local current_lc current_lang
    current_lc="$(printf '%s' "${current:-}" | tr '[:upper:]' '[:lower:]')"
    current_lang="${current_lc%%[_@.-]*}"

    # 日语/韩语不支持（命令输出解析不可靠，且本次不纳入适配范围）——优先于编码放行判定
    if [[ "$current_lang" =~ ^(ja|ko)$ ]]; then
        warn "不支持日语（ja_*）/韩语（ko_*）语言环境（当前: ${current:-未知}）。"
        warn "请切换到英文 en_US.UTF-8 或中文 zh_CN.UTF-8，否则安装可能异常。"
        warn "安装将继续，面板将以 LANG=C.UTF-8 运行以规避本地化解析问题，但建议安装前修正系统语言。"
        return 0
    fi

    # 放行区间：
    #   ① 英文优先：en / C / POSIX 裸值或配 UTF-8 编码后缀
    #   ② 中文（含 zh_CN/zh_SG/zh_TW/zh_HK）等 UTF-8 locale
    #   ③ 任意 .utf8 / .utf-8 结尾的 locale
    # 以下不匹配 GBK / GB2312 / GB18030 / latin1 等非 UTF-8 编码（中文乱码会导致命令输出解析异常），
    # 国产系统（麒麟/openEuler/UOS 等）默认 zh_CN.UTF-8，直接放行，仅提示；
    # 面板服务由 setup_service 强制 LANG=C.UTF-8 启动，避免命令输出被本地化导致解析失败
    if [[ "$current_lc" =~ ^(c|posix)(\.utf-?8)?$ ]] || [[ "$current_lc" =~ ^en(_[a-z0-9]+)?(\.utf-?8)?$ ]] || [[ "$current_lc" =~ \.utf-?8$ ]]; then
        info "系统语言环境: ${current:-未知}（建议优先使用英文 en_US.UTF-8）"
        return 0
    fi

    warn "系统语言环境为 ${current:-未知}（非英文/中文 UTF-8）。"
    warn "强烈建议使用英文 en_US.UTF-8：QVMConsole 依赖命令返回的信息进行正确识别，英文环境最稳定。"
    warn "安装将继续，面板将以 LANG=C.UTF-8 运行，确保命令输出可正确解析。"
    return 0
}

check_arch() {
    detect_arch
}

check_kvm_hardware() {
    info "检测 KVM 硬件虚拟化能力..."
    if [ ! -r /proc/cpuinfo ]; then
        error "无法读取 /proc/cpuinfo，不能确认硬件虚拟化能力"
        exit 1
    fi
    if [ "$ARCH" = "x86_64" ]; then
        if ! awk -F: '/^(flags|Features)[[:space:]]*:/ { if ($2 ~ /(^|[[:space:]])(vmx|svm)([[:space:]]|$)/) found=1 } END { exit found ? 0 : 1 }' /proc/cpuinfo; then
            error "未检测到 CPU 硬件虚拟化标记（Intel VT-x/vmx 或 AMD-V/svm），请先在 BIOS/UEFI 中开启虚拟化后再安装"
            exit 1
        fi
    elif [ "$ARCH" = "aarch64" ]; then
        if [ ! -e /dev/kvm ]; then
            error "未检测到 /dev/kvm，ARM 虚拟化可能未启用或内核不支持 KVM"
            exit 1
        fi
    fi
    success "CPU 已开启硬件虚拟化标记"
}

# KYSEC 状态探测：麒麟内核安全机制（KYSEC），可能限制内核模块加载与关键路径访问。
# 探测方式防御性多重回退（kysec_ctl 命令 / sysfs / procfs / 配置目录），非麒麟返回 not-detected。
# 全局 KYSEC_STATE 写入探测结果供安装报告展示。
KYSEC_STATE=""
check_kysec() {
    KYSEC_STATE="not-detected"
    local detail=""
    if command -v kysec_ctl >/dev/null 2>&1; then
        KYSEC_STATE="enabled"
        detail="kysec_ctl"
    elif [ -d /sys/kernel/security/kysec ]; then
        KYSEC_STATE="enabled"
        detail="/sys/kernel/security/kysec"
    elif [ -d /proc/kysec ]; then
        KYSEC_STATE="enabled"
        detail="/proc/kysec"
    elif [ -d /etc/kysec ]; then
        KYSEC_STATE="enabled"
        detail="/etc/kysec"
    fi
    if [ "$KYSEC_STATE" = "enabled" ]; then
        info "检测到麒麟 KYSEC 安全机制启用（探测点: ${detail}）"
        info "KYSEC 强制访问控制可能限制内核模块加载与 /dev/kvm 访问；若 KVM 无法启用或虚拟机启动异常，请用 kysec_ctl 放行 qemu/libvirt 相关策略"
    fi
}

ensure_kvm_runtime() {
    info "检测 /dev/kvm 运行环境..."
    check_kysec
    if [ "$ARCH" = "x86_64" ]; then
        local vendor_module="kvm"
        if grep -q "GenuineIntel" /proc/cpuinfo 2>/dev/null; then
            vendor_module="kvm_intel"
        elif grep -q "AuthenticAMD" /proc/cpuinfo 2>/dev/null; then
            vendor_module="kvm_amd"
        fi
        modprobe kvm 2>/dev/null || true
        modprobe "$vendor_module" 2>/dev/null || true
    elif [ "$ARCH" = "aarch64" ]; then
        # ARM 平台 KVM 通常内置在内核中（builtin），也可能以模块存在
        modprobe kvm 2>/dev/null || true
    fi

    if [ ! -e /dev/kvm ]; then
        error "未检测到 /dev/kvm。通常是 BIOS/UEFI 未开启虚拟化、宿主机未开放嵌套虚拟化，或内核 KVM 模块无法加载"
        exit 1
    fi
    success "/dev/kvm 可用"
}

# #M：交互式读取辅助函数。部分自动化/网页终端环境会把脚本自身 stdout 回灌进 stdin，
# 导致 `read` 读到脚本自己的输出（如 "[✓] openEuler everything 仓库已启用: ..."）而非用户输入，
# 表现为 "无效的选择: [✓] ..."。此函数优先从控制终端 /dev/tty 读取（与 stdin 隔离），
# 且对输入做白名单校验，无效时重试（上限 3 次），仍无效则回退默认值，不再直接退出安装。
# 参数: $1=提示文案  $2=默认值  $3=合法值正则（可选，默认 ^[0-9a-zA-Z]*$）
# 输出: 全局变量 QVM_READ_RESULT 写入读取结果
read_user_input() {
    local prompt="$1" default="${2:-}" pattern="${3:-^[0-9a-zA-Z]*$}"
    local input="" attempt=0
    QVM_READ_RESULT="$default"
    # CI / 非交互：直接使用默认值
    if [ "${CI:-}" = "1" ] || [ ! -t 0 ]; then
        QVM_READ_RESULT="$default"
        return 0
    fi
    while [ "$attempt" -lt 3 ]; do
        input=""
        # #K：3 秒倒计时（读秒），超时无输入自动采用默认值
        if countdown_read_line "$prompt"; then
            input="$QVM_READ_INPUT"
        else
            warn "等待超时（${QVM_READ_TIMEOUT}s），使用默认值: ${default:-（空）}"
            QVM_READ_RESULT="$default"
            return 0
        fi
        [ -z "$input" ] && input="$default"
        if [[ "$input" =~ ^$pattern$ ]]; then
            QVM_READ_RESULT="$input"
            return 0
        fi
        attempt=$((attempt + 1))
        warn "输入无效（${input:-空}），请重新输入"
    done
    QVM_READ_RESULT="$default"
    return 0
}

# #K：倒计时读取一行输入（读秒）。$1=提示文案 $2=超时秒数（默认 QVM_READ_TIMEOUT=3）。
# 用户输入后写入全局 QVM_READ_INPUT 并返回 0；超时无输入返回 1（调用方按默认值处理）。
# 与 read_user_input 一致，优先从 /dev/tty 隔离读取，避免 stdout 回灌污染；\r\033[K 每秒刷新读秒数。
QVM_READ_TIMEOUT=3
countdown_read_line() {
    local prompt="$1" timeout="${2:-$QVM_READ_TIMEOUT}"
    local remaining="$timeout" line="" ok=0
    QVM_READ_INPUT=""
    while [ "$remaining" -gt 0 ]; do
        printf '\r\033[K%s（%2ds 后无输入将自动默认）' "$prompt" "$remaining"
        ok=0
        if [ -e /dev/tty ] && [ -w /dev/tty ]; then
            if IFS= read -r -t 1 line </dev/tty 2>/dev/null; then
                ok=1
            fi
        else
            if IFS= read -r -t 1 line; then
                ok=1
            fi
        fi
        if [ "$ok" = "1" ]; then
            QVM_READ_INPUT="$line"
            printf '\r\033[K'
            return 0
        fi
        remaining=$((remaining - 1))
    done
    printf '\r\033[K%s（等待超时）\n' "$prompt"
    return 1
}

# #M：普通交互式读取统一入口。与 read_user_input 相同，优先从控制终端 /dev/tty 读取，
# 隔离自动化/网页终端把脚本 stdout 回灌 stdin 导致的 `read` 读到自身输出问题。
# 用法与 `read` 完全一致（如 read_tty -rp "提示: " var），返回值恒为 0 以兼容 set -e。
# 3 秒倒计时超时无输入时变量保持原值，由调用方的 `${var:-默认}` 逻辑自动采用默认值。
read_tty() {
    local args=("$@") n="$#" i arg prev=""
    local prompt="" var_name=""
    # 解析 read 参数，提取提示文案与目标变量名（兼容 -rp "提示" var 与 -r -p "提示" var）
    for ((i=0; i<n; i++)); do
        arg="${args[$i]}"
        case "$arg" in
            -r|-p) prev="$arg" ;;
            -rp|-pr) prev="-p" ;;
            *)
                if [ "$prev" = "-p" ]; then
                    prompt="$arg"
                    prev=""
                elif [ "$prev" = "-r" ]; then
                    prev=""
                else
                    var_name="$arg"
                fi
                ;;
        esac
    done
    # CI / 非交互（stdin 不可读，如网页终端/管道）：跳过倒计时直接返回，
    # 变量保持原值，由调用方的 ${var:-默认} 立即采用默认值（与 read_user_input 一致，避免每个提示空等 5 秒）
    if [ "${CI:-}" = "1" ] || [ ! -t 0 ]; then
        return 0
    fi
    if [ -n "$var_name" ] && countdown_read_line "$prompt"; then
        printf -v "$var_name" '%s' "$QVM_READ_INPUT"
    else
        warn "等待超时（${QVM_READ_TIMEOUT}s），自动采用默认值"
    fi
    return 0
}

# detect_existing_install 检测是否有已安装的实例
# 返回: 0=存在, 1=不存在; 全局变量 LEGACY_INSTALL_DIR_PATH 写入检测到的旧路径（若有）
detect_existing_install() {
    LEGACY_INSTALL_DIR_PATH=""
    # 1. 检查当前默认路径
    if [ -x "${_DEFAULT_INSTALL_DIR}/kvm-console" ] || [ -f "${_DEFAULT_INSTALL_DIR}/.env" ]; then
        return 0
    fi
    # 2. 检查旧版路径 /opt/kvm-console
    if [ -x "${_LEGACY_INSTALL_DIR}/kvm-console" ] || [ -f "${_LEGACY_INSTALL_DIR}/.env" ]; then
        LEGACY_INSTALL_DIR_PATH="$_LEGACY_INSTALL_DIR"
        return 0
    fi
    # 3. 检查 systemd 服务文件
    if [ -f "$SERVICE_FILE" ]; then
        return 0
    fi
    return 1
}

choose_mode() {
    if detect_existing_install; then
        echo ""
        echo -e "${CYAN}检测到已安装的 ${APP_NAME}${NC}"
        echo -e "  ${CYAN}1.${NC} 更新"
        echo -e "  ${CYAN}2.${NC} 卸载"
        echo -e "  ${CYAN}3.${NC} 修复配置文件（重置 .env 为默认值）"
        echo -e "  ${CYAN}4.${NC} 回滚到历史发行版（从 .release_backup 恢复）"
        echo ""
        local choice
        # 非交互 / CI 模式：默认执行更新，不弹交互（#M）
        if [ "${CI:-}" = "1" ] || [ ! -t 0 ]; then
            choice="1"
            info "非交互模式，默认执行更新"
        else
            read_user_input "请选择操作 [1/2/3/4，默认 1]: " "1" "[1-4]"
            choice="$QVM_READ_RESULT"
        fi
        case "$choice" in
            1)
                MODE="update"
                info "将执行更新，并重新检测/修复运行地基"
                ;;
            2)
                MODE="uninstall"
                info "将执行卸载"
                ;;
            3)
                MODE="repair"
                info "将重置配置文件为默认值"
                ;;
            4)
                MODE="rollback"
                info "将回滚到上一发行版"
                ;;
            *)
                warn "无效的选择: $choice，已回退为默认更新"
                MODE="update"
                info "将执行更新，并重新检测/修复运行地基"
                ;;
        esac
    else
        MODE="install"
        info "未检测到已安装的 ${APP_NAME}，将执行首次安装"
    fi
}

# migrate_from_old_path 将旧版安装（/opt/kvm-console）迁移到新路径
# 参数: $1=旧路径, $2=新路径
migrate_from_old_path() {
    local old_dir="$1"
    local new_dir="$2"

    echo ""
    echo -e "${CYAN}========== 旧版安装迁移 ==========${NC}"
    echo -e "  旧路径: ${YELLOW}${old_dir}${NC}"
    echo -e "  新路径: ${GREEN}${new_dir}${NC}"
    echo ""

    # 读取旧版 .env 配置（迁移后保留）
    local old_env="${old_dir}/.env"
    if [ ! -f "$old_env" ]; then
        warn "旧路径 .env 不存在: $old_env"
        return 1
    fi

    # 停止旧服务
    info "停止旧版服务..."
    if systemctl is-active --quiet kvm-console.service 2>/dev/null; then
        systemctl stop kvm-console.service 2>/dev/null || true
        info "已停止 kvm-console.service"
    fi

    # 创建新目录
    mkdir -p "$new_dir"

    # 迁移二进制文件
    info "迁移二进制文件..."
    local bin_files=("kvm-console" "kvm-console-native" "kvm-console-compat" "kvm-console-compat-"*)
    for f in "${bin_files[@]}"; do
        if [ -f "${old_dir}/${f}" ] && [ "${f}" != "kvm-console-compat-"* ]; then
            cp -f "${old_dir}/${f}" "${new_dir}/${f}" 2>/dev/null && \
                info "  迁移: ${f}" || warn "  迁移失败: ${f}"
        fi
    done
    # 处理 compat-2.xx 文件
    for f in "${old_dir}"/kvm-console-compat-*; do
        if [ -f "$f" ]; then
            cp -f "$f" "${new_dir}/${f##*/}" 2>/dev/null && \
                info "  迁移: ${f##*/}" || warn "  迁移失败: ${f##*/}"
        fi
    done
    chmod +x "${new_dir}"/kvm-console* 2>/dev/null || true

    # 迁移前端资源
    if [ -d "${old_dir}/web-dist" ]; then
        info "迁移前端资源..."
        cp -rf "${old_dir}/web-dist" "${new_dir}/web-dist" 2>/dev/null && \
            info "  迁移: web-dist/" || warn "  迁移失败: web-dist/"
    fi

    # 迁移数据目录
    if [ -d "${old_dir}/data" ]; then
        info "迁移数据目录..."
        cp -rf "${old_dir}/data" "${new_dir}/data" 2>/dev/null && \
            info "  迁移: data/" || warn "  迁移失败: data/"
    fi

    # 迁移配置文件
    if [ -d "${old_dir}/.health" ]; then
        cp -rf "${old_dir}/.health" "${new_dir}/.health" 2>/dev/null || true
    fi
    if [ -d "${old_dir}/.install_state" ]; then
        cp -rf "${old_dir}/.install_state" "${new_dir}/.install_state" 2>/dev/null || true
    fi

    # 迁移 .env 并更新路径
    info "迁移配置文件..."
    cp -f "$old_env" "${new_dir}/.env"
    # 更新 .env 中的路径
    sed -i "s|^INSTALL_DIR=.*|INSTALL_DIR=${new_dir}|" "${new_dir}/.env" 2>/dev/null || \
        sed -i '' "s|^INSTALL_DIR=.*|INSTALL_DIR=${new_dir}|" "${new_dir}/.env" 2>/dev/null || true
    # 更新数据库路径（如果包含旧路径）
    sed -i "s|${old_dir}|${new_dir}|g" "${new_dir}/.env" 2>/dev/null || \
        sed -i '' "s|${old_dir}|${new_dir}|g" "${new_dir}/.env" 2>/dev/null || true

    # 迁移 /etc/kvm-console 配置
    if [ -d "/etc/kvm-console" ]; then
        info "配置目录 /etc/kvm-console 保持不变（全局共享）"
    fi

    # 迁移 systemd 服务文件
    if [ -f "$SERVICE_FILE" ]; then
        info "更新 systemd 服务文件..."
        sed -i "s|ExecStart=.*|ExecStart=${new_dir}/kvm-console|" "$SERVICE_FILE" 2>/dev/null || \
            sed -i '' "s|ExecStart=.*|ExecStart=${new_dir}/kvm-console|" "$SERVICE_FILE" 2>/dev/null || true
        systemctl daemon-reload 2>/dev/null || true
        info "已更新服务文件并重载 systemd"
    fi

    # 迁移 OVS/VPC 配置目录
    for dir in "/var/lib/kvm-console" "/var/lib/kvm-console/ovs" "/etc/kvm-console/ovs" "/etc/kvm-console/vpc"; do
        if [ -d "$dir" ]; then
            info "  保留: ${dir}（全局共享）"
        fi
    done

    echo ""
    success "迁移完成！"
    echo -e "  旧路径: ${YELLOW}${old_dir}${NC}"
    echo -e "  新路径: ${GREEN}${new_dir}${NC}"
    echo ""
    echo -e "${YELLOW}提示${NC}: 旧版安装保留在 ${old_dir}，确认迁移成功后可手动删除"
    echo ""

    return 0
}

# choose_install_dir 交互选择安装路径（仅首次安装时生效，更新/卸载/修复/回滚复用已有路径）
choose_install_dir() {
    # 非交互 / CI / 非首次安装：跳过
    if [ "${CI:-}" = "1" ] || [ ! -t 0 ]; then
        info "安装路径: ${INSTALL_DIR}（非交互模式）"
        return 0
    fi
    # 更新/卸载/修复/回滚模式：从已有 .env 读取或沿用默认值
    if [ "$MODE" != "install" ]; then
        if [ -f "${_DEFAULT_INSTALL_DIR}/.env" ]; then
            local saved_dir
            saved_dir=$(grep -m1 '^INSTALL_DIR=' "${_DEFAULT_INSTALL_DIR}/.env" 2>/dev/null | cut -d= -f2- | tr -d '"' | tr -d '[:space:]' || true)
            if [ -n "$saved_dir" ] && [ -d "$saved_dir" ]; then
                INSTALL_DIR="$saved_dir"
                ENV_FILE="${INSTALL_DIR}/.env"
                STATE_DIR="${INSTALL_DIR}/.install_state"
                STATE_FILE="${STATE_DIR}/state"
                info "安装路径: ${INSTALL_DIR}（从已有配置读取）"
                return 0
            fi
        fi
        info "安装路径: ${INSTALL_DIR}（使用默认值）"
        return 0
    fi

    # 检测旧版安装
    local has_legacy=0
    if [ -n "$LEGACY_INSTALL_DIR_PATH" ] && [ -d "$LEGACY_INSTALL_DIR_PATH" ]; then
        has_legacy=1
        echo ""
        echo -e "${YELLOW}检测到旧版安装: ${LEGACY_INSTALL_DIR_PATH}${NC}"
    fi

    echo ""
    echo -e "${CYAN}请选择安装路径${NC}"
    echo -e "  ${CYAN}直接回车${NC} 使用默认路径: ${GREEN}${_DEFAULT_INSTALL_DIR}${NC}"
    echo -e "  ${CYAN}输入自定义路径${NC} 安装到指定目录"
    if [ "$has_legacy" -eq 1 ]; then
        echo -e "  ${YELLOW}提示${NC}: 检测到旧版安装在 ${LEGACY_INSTALL_DIR_PATH}，可选择路径后自动迁移"
    fi
    echo ""
    local input_path
    read_tty -rp "安装路径 [${_DEFAULT_INSTALL_DIR}]: " input_path
    input_path=${input_path:-$_DEFAULT_INSTALL_DIR}

    # 去除首尾空格和尾部斜杠
    input_path=$(echo "$input_path" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//;s|/*$||')

    # 校验路径
    if [ -z "$input_path" ]; then
        input_path="$_DEFAULT_INSTALL_DIR"
    fi

    # 检查路径是否为绝对路径
    if [[ "$input_path" != /* ]]; then
        warn "请输入绝对路径（以 / 开头），当前输入: $input_path"
        echo -e "  ${CYAN}示例${NC}: /opt/QVMConsole, /data/kvm-console, /home/admin/kvm-console"
        return 1
    fi

    # 检查父目录是否存在
    local parent_dir
    parent_dir=$(dirname "$input_path")
    if [ ! -d "$parent_dir" ]; then
        warn "父目录不存在: $parent_dir"
        read_tty -rp "是否自动创建? [Y/n]: " create_parent
        create_parent=${create_parent:-Y}
        if [[ "$create_parent" =~ ^[Yy]$ ]]; then
            mkdir -p "$input_path" 2>/dev/null
            if [ $? -ne 0 ]; then
                warn "创建目录失败: $input_path"
                return 1
            fi
        else
            return 1
        fi
    fi

    # 检查磁盘空间（至少 1GB）
    local avail_kb
    avail_kb=$(df -k "$parent_dir" 2>/dev/null | awk 'NR==2{print $4}')
    if [ -n "$avail_kb" ] && [ "$avail_kb" -lt 1048576 ] 2>/dev/null; then
        local avail_gb=$((avail_kb / 1024 / 1024))
        warn "磁盘空间不足（当前可用 ${avail_gb}GB，建议至少 1GB）"
        read_tty -rp "是否继续安装? [y/N]: " continue_install
        continue_install=${continue_install:-N}
        if [[ ! "$continue_install" =~ ^[Yy]$ ]]; then
            return 1
        fi
    fi

    INSTALL_DIR="$input_path"
    ENV_FILE="${INSTALL_DIR}/.env"
    STATE_DIR="${INSTALL_DIR}/.install_state"
    STATE_FILE="${STATE_DIR}/state"
    info "安装路径: ${INSTALL_DIR}"

    # 旧版迁移提示
    if [ "$has_legacy" -eq 1 ] && [ "$INSTALL_DIR" != "$LEGACY_INSTALL_DIR_PATH" ]; then
        echo ""
        echo -e "${YELLOW}检测到旧版安装在 ${LEGACY_INSTALL_DIR_PATH}${NC}"
        read_tty -rp "是否自动迁移到新路径 ${INSTALL_DIR}? [Y/n]: " do_migrate
        do_migrate=${do_migrate:-Y}
        if [[ "$do_migrate" =~ ^[Yy]$ ]]; then
            migrate_from_old_path "$LEGACY_INSTALL_DIR_PATH" "$INSTALL_DIR"
            if [ $? -ne 0 ]; then
                warn "迁移失败，请检查错误信息后重试"
                return 1
            fi
        else
            info "跳过迁移，将全新安装到 ${INSTALL_DIR}"
        fi
    fi
}

install_optional_polkit() {
    if command -v pkaction >/dev/null 2>&1 || systemctl list-unit-files 2>/dev/null | grep -q '^polkit\.service'; then
        return
    fi
    info "补充安装 polkit 组件..."
    case "$PKG_MGR" in
        apt)
            if apt-cache show polkitd >/dev/null 2>&1; then
                pkg_install polkitd
            elif apt-cache show policykit-1 >/dev/null 2>&1; then
                pkg_install policykit-1
            else
                warn "未找到 polkitd / policykit-1 包，用户级 libvirt 授权可能需要手动检查"
            fi
            ;;
        dnf|yum)
            pkg_install polkit 2>/dev/null || warn "未找到 polkit 包，用户级 libvirt 授权可能需要手动检查"
            ;;
    esac
}

find_kvm_stat_binary() {
    if command -v kvm_stat >/dev/null 2>&1; then
        command -v kvm_stat
        return 0
    fi

    local found
    found=$(find /usr/lib/linux-tools -name kvm_stat -type f 2>/dev/null | sort -V | tail -n1 || true)
    if [ -n "$found" ]; then
        printf '%s\n' "$found"
        return 0
    fi
    return 1
}

check_optional_kvm_stat() {
    local kvm_stat_path
    if kvm_stat_path=$(find_kvm_stat_binary); then
        success "可选辅助指标 kvm_stat 已可用: $kvm_stat_path"
        return
    fi

    info "未检测到可用的 kvm_stat，跳过 kvm_page_fault 辅助指标；热迁移仍会使用 libvirt dirty-rate 判断"
}

# install_optional_virt_fw_vars 安装 UEFI 变量编辑工具。
# 该工具用于让 shim/fallback.efi 首次登记启动项后直接继续引导；缺失时不影响虚拟机创建。
install_optional_virt_fw_vars() {
    if command -v virt-fw-vars >/dev/null 2>&1 &&
       virt-fw-vars --help 2>&1 | grep -q -- '--set-fallback-no-reboot'; then
        success "virt-fw-vars 已可用"
        return
    fi

    info "尝试安装可选 UEFI 变量工具 virt-fw-vars..."
    case "$PKG_MGR" in
        apt)
            if pkg_is_available python3-virt-firmware 2>/dev/null; then
                pkg_install python3-virt-firmware >/dev/null 2>&1 || true
            fi
            ;;
        dnf|yum)
            pkg_install python3-virt-firmware >/dev/null 2>&1 ||
                pkg_install virt-firmware >/dev/null 2>&1 || true
            ;;
    esac

    if command -v virt-fw-vars >/dev/null 2>&1 &&
       virt-fw-vars --help 2>&1 | grep -q -- '--set-fallback-no-reboot'; then
        success "virt-fw-vars 安装成功"
    else
        warn "virt-fw-vars 不可用或版本过旧，UEFI 克隆首次启动时可能短暂显示启动项恢复界面"
    fi
}

check_and_install_deps() {
    info "检查宿主机依赖包..."
    # P3-12：依赖安装前先测速选择镜像源（offline 时仅扫描）
    test_mirror_speed
    # 应用镜像源到系统源（备份→修改→验证→失败回滚）
    apply_system_mirror
    # 镜像切换完成后探测关键包可用性（此时命中已切换的国内源，避免官方慢源卡顿）
    probe_critical_rpm_packages
    local missing=()
    local pkg

    # 根据架构动态确定依赖列表
    local deps=("${APT_DEPS[@]}")
    local qemu_pkg_rpm=""
    local qemu_pkg_rpm_fallback=""
    if [ "$ARCH" = "x86_64" ]; then
        if [ "$PKG_MGR" = "apt" ]; then
            deps+=("$QEMU_PKG_X86" "$EFI_PKG_X86")
            info "架构: x86_64，QEMU 包: ${QEMU_PKG_X86}，EFI 包: ${EFI_PKG_X86}"
        else
            qemu_pkg_rpm="$QEMU_PKG_X86_RPM"
            qemu_pkg_rpm_fallback="$QEMU_PKG_X86_RPM_FALLBACK"
            deps+=("$qemu_pkg_rpm" "$EFI_PKG_X86_RPM")
            info "架构: x86_64，QEMU 包: ${qemu_pkg_rpm}，EFI 包: ${EFI_PKG_X86_RPM}"
        fi
    elif [ "$ARCH" = "aarch64" ]; then
        if [ "$PKG_MGR" = "apt" ]; then
            deps+=("$QEMU_PKG_ARM" "$EFI_PKG_ARM")
            info "架构: aarch64，QEMU 包: ${QEMU_PKG_ARM}，EFI 包: ${EFI_PKG_ARM}"
        else
            qemu_pkg_rpm="$QEMU_PKG_ARM_RPM"
            qemu_pkg_rpm_fallback="$QEMU_PKG_ARM_RPM_FALLBACK"
            deps+=("$qemu_pkg_rpm" "$EFI_PKG_ARM_RPM")
            info "架构: aarch64，QEMU 包: ${qemu_pkg_rpm}，EFI 包: ${EFI_PKG_ARM_RPM}"
        fi
    fi

    for pkg in "${deps[@]}"; do
        local mapped_pkg
        mapped_pkg=$(pkg_name "$pkg") || continue  # RPM 系无映射的包跳过
        if is_pkg_installed "$mapped_pkg"; then
            success "$pkg 已安装"
        else
            # 检查是否为 RPM 可选软性包
            local is_soft=0
            if [ "$PKG_MGR" != "apt" ]; then
                for soft in "${RPM_PKG_SOFT[@]}"; do
                    if [ "$mapped_pkg" = "$soft" ]; then
                        is_soft=1
                        break
                    fi
                done
            fi
            if [ "$is_soft" -eq 1 ]; then
                # RPM 软性包：跳过主安装流程，交由 install_bundled_packages 后处理
                # （优先原生源，原生无该组件时才回退捆绑包，#A7c）
                info "可选包 $mapped_pkg 由捆绑包机制处理（优先原生源，缺失时才用捆绑包）"
                continue
            fi
            missing+=("$mapped_pkg")
        fi
    done

    # QEMU 包回退：如果主包名（qemu-kvm）不可用，尝试回退包名（qemu）
    if [ "$PKG_MGR" != "apt" ] && [ -n "$qemu_pkg_rpm" ] && [ -n "$qemu_pkg_rpm_fallback" ]; then
        local qemu_rpm_mapped
        qemu_rpm_mapped=$(pkg_name "$qemu_pkg_rpm" 2>/dev/null || true)
        local qemu_fb_mapped
        qemu_fb_mapped=$(pkg_name "$qemu_pkg_rpm_fallback" 2>/dev/null || true)
        if [ -n "$qemu_rpm_mapped" ] && [ -n "$qemu_fb_mapped" ]; then
            # 检查主包是否在 missing 列表中且未安装
            local qemu_in_missing=0
            local i
            for i in "${!missing[@]}"; do
                if [ "${missing[$i]}" = "$qemu_rpm_mapped" ]; then
                    qemu_in_missing=1
                    # 检查回退包是否已安装或可用
                    if is_pkg_installed "$qemu_fb_mapped" 2>/dev/null; then
                        unset 'missing[i]'
                        success "QEMU 包回退: $qemu_rpm_mapped → $qemu_fb_mapped（已安装）"
                    elif pkg_is_available "$qemu_fb_mapped" 2>/dev/null; then
                        unset 'missing[i]'
                        missing+=("$qemu_fb_mapped")
                        warn "QEMU 包回退: $qemu_rpm_mapped 不可用，改用 $qemu_fb_mapped"
                    fi
                    break
                fi
            done
        fi
    fi

    if [ ${#missing[@]} -gt 0 ]; then
        warn "发现缺失依赖: ${missing[*]}"
        # P3-12：offline 模式不自动安装，仅汇总到报告（专网/断网场景从内网源手动安装）
        if [ "$DEPS_MIRROR" = "offline" ]; then
            OFFLINE_MISSING_DEPS="${missing[*]}"
            info "offline 模式：跳过 apt/dnf install，缺失依赖将在安装报告列出（可从内网源手动安装）"
        else
            # 非交互 / CI 模式：直接自动安装缺失依赖，不弹交互（#M）
            local confirm="Y"
            if [ "${CI:-}" != "1" ] && [ -t 0 ]; then
                read_tty -rp "是否立即安装缺失依赖? [Y/n]: " confirm
                confirm=${confirm:-Y}
            fi
            if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
                error "缺少必要依赖，无法保证面板功能完整运行"
                exit 1
            fi
            info "更新包索引..."
            pkg_update_index
            info "安装缺失依赖..."
            pkg_install "${missing[@]}"
        fi
    fi

    # ISO 创建工具回退：genisoimage 不可用时尝试安装 xorriso
    if ! command -v genisoimage >/dev/null 2>&1 && \
       ! command -v xorriso >/dev/null 2>&1 && \
       ! command -v mkisofs >/dev/null 2>&1; then
        warn "未找到 genisoimage/xorriso/mkisofs，尝试安装替代工具..."
        pkg_install xorriso 2>/dev/null || pkg_install genisoimage 2>/dev/null || true
    fi

    install_optional_polkit
    check_optional_kvm_stat
# 安装捆绑的 RPM 包（为 Kylin/openEuler 等源中缺失的包提供，含 RPM 软性可选命令的原生源补齐）
    install_bundled_packages

    # 依赖/可选命令确认顺序：必须放在 install_bundled_packages 之后。
    # 否则 RPM 系软性命令（virt-*、growpart 等，provisioned by dnf native source）
    # 尚未补齐就被 ensure_required_commands 误判为缺失而打印 [WARN]。
    install_optional_virt_fw_vars
    ensure_required_commands
    ensure_core_services
}

# has_runnable_cmd 检测是否存在真实可运行的命令（绕过 PATH 遮蔽，
# 检查 /usr/bin 等系统目录中是否有能正常执行的同名原生二进制）。
# 目的：避免捆绑 RPM 提取的二进制（/usr/local/bin）遮蔽系统原生可用副本。
has_runnable_cmd() {
    local name="$1"
    local c
    for c in /usr/sbin /usr/bin /bin; do
        if [ -x "$c/$name" ]; then
            # 可执行且在系统目录中即视为原生可用（部分工具如 growpart 不支持 --version，容忍 --version 失败）
            if timeout 5s "$c/$name" --version >/dev/null 2>&1 || \
               timeout 5s "$c/$name" --help >/dev/null 2>&1; then
                return 0
            fi
        fi
    done
    return 1
}

# extract_rpm_cmd 从捆绑 RPM 中提取单个命令到 /usr/local/bin
# 前使用 rpm2cpio，回退 bsdtar；确保提取工具可用
extract_rpm_cmd() {
    local rpm_file="$1" cmd_name="$2"
    [ -f "$rpm_file" ] || return 1

    if ! command -v rpm2cpio >/dev/null 2>&1 && ! command -v bsdtar >/dev/null 2>&1; then
        dnf install -y rpm-build &>/dev/null 2>&1 || true
    fi

    local tmp
    tmp=$(mktemp -d)

    if command -v rpm2cpio >/dev/null 2>&1; then
        rpm2cpio "$rpm_file" 2>/dev/null | cpio -idm -D "$tmp" 2>/dev/null || { rm -rf "$tmp"; return 1; }
    elif command -v bsdtar >/dev/null 2>&1; then
        bsdtar xf "$rpm_file" -C "$tmp" 2>/dev/null || { rm -rf "$tmp"; return 1; }
    else
        rm -rf "$tmp"
        return 1
    fi

    if [ -f "$tmp/usr/bin/$cmd_name" ]; then
        # 若系统已有可运行的原生同名命令（/usr/bin 等），跳过提取避免 PATH 遮蔽
        if has_runnable_cmd "$cmd_name"; then
            rm -rf "$tmp"
            return 0
        fi
        cp "$tmp/usr/bin/$cmd_name" /usr/local/bin/
        chmod +x "/usr/local/bin/$cmd_name"
        # 校验提取物真实可运行（B7：仅文件存在不足以判定可用，动态链接缺失将导致后续 --version 探测失败）
        if ! command -v timeout >/dev/null 2>&1 || timeout 5s "$cmd_name" --version >/dev/null 2>&1; then
            rm -rf "$tmp"
            return 0
        fi
        # 可执行文件存在但运行失败（缺动态库等），移除避免误报已安装
        rm -f "/usr/local/bin/$cmd_name"
        warn "提取 ${cmd_name} 后无法运行（可能缺少动态库），已移除，将由系统源/dnf provides 回退处理"
        rm -rf "$tmp"
        return 1
    fi
    rm -rf "$tmp"
    return 1
}

# install_bundled_packages 安装 release 中捆绑的 RPM 包，以及处理软性安装包
# 用于 Kylin/openEuler 等系统默认源中缺少的包，或在主流程中跳过的软性包
# 执行顺序：发起由 dnf soft/retry 补齐系统原生包（含 dnf provides 回退）→ 捆绑包兜底。
# #A7c：优先使用系统原生源中的组件（解析完整依赖、原生链接），仅当原生库无该
# 组件时才回退取用捆绑包的离线二进制，避免用劣质提取副本遮蔽原生安装。
install_bundled_packages() {
    local script_dir
    script_dir="$(cd "$(dirname "$0")" && pwd)"
    local bundled_dir="${script_dir}/bundled"

    # === Phase 1: 优先补齐系统原生源组件（dnf soft retry + dnf provides fallback） ===
    if [ "$PKG_MGR" != "apt" ]; then
        local soft_pkg
        for soft_pkg in "${RPM_PKG_SOFT[@]}"; do
            if ! rpm -q "$soft_pkg" &>/dev/null 2>&1; then
                if dnf install -y "$soft_pkg" &>/dev/null 2>&1 || \
                   yum install -y "$soft_pkg" &>/dev/null 2>&1; then
                    success "$soft_pkg 安装成功（系统原生源）"
                fi
            fi
        done
        unset soft_pkg
        # dnf provides 回退仅覆盖核心命令（virt-filesystems/virt-customize/guestfish/virt-win-reg/growpart），
        # 其余 virt-* 工具由 Phase 2 捆绑包提取兜底，避免逐条 dnf provides 重复触发元数据下载拖慢安装。
        # timeout 10s 防止 dnf provides 在某些系统上挂死；offline 时跳过（无网络）。
        if [ "${DEPS_MIRROR:-}" != "offline" ]; then
        local soft_cmd
        for soft_cmd in virt-filesystems virt-customize guestfish virt-win-reg growpart; do
            if ! has_runnable_cmd "$soft_cmd"; then
                local providing_pkg
                # dnf provides 输出形如 "name-ver-rel.arch : 描述"，冒号前带尾随空格，
                # 需剥离空白，否则 dnf install 会因包名含空格而失败
                providing_pkg=$(timeout 10s dnf provides "$soft_cmd" 2>/dev/null | awk -F: '/^[^ ]+ :/ {gsub(/[[:space:]]/, "", $1); print $1; exit}' || true)
                if [ -n "$providing_pkg" ]; then
                    info "命令 $soft_cmd 由原生包 $providing_pkg 提供，尝试安装..."
                    if dnf install -y "$providing_pkg" &>/dev/null 2>&1 || \
                       yum install -y "$providing_pkg" &>/dev/null 2>&1; then
                        success "$soft_cmd 安装成功（系统原生源）"
                    else
                        warn "命令 $soft_cmd 的原生包 $providing_pkg 安装失败"
                    fi
                fi
            fi
        done
        unset soft_cmd
        fi
    fi

    # === Phase 2: 捆绑包兜底（仅当系统原生源无该组件时才回退离线包） ===
    if [ -d "$bundled_dir" ]; then
        info "检测到捆绑的 RPM 包目录，优先尝试本地包..."

        # arp-scan: 优先 dnf 安装（依赖少，可能成功）
        if ! command -v arp-scan >/dev/null 2>&1 && [ -f "$bundled_dir/arp-scan.rpm" ]; then
            if dnf install -y "$bundled_dir/arp-scan.rpm" 2>/dev/null || \
               yum install -y "$bundled_dir/arp-scan.rpm" 2>/dev/null; then
                success "arp-scan 安装成功"
            else
                warn "捆绑的 arp-scan 安装失败（可能缺少依赖），ARP 扫描功能将使用 nmap 替代"
            fi
        fi

        # libguestfs-tools-c: 直接提取二进制，跳过 dnf 依赖解析（libguestfs.so.0 已在系统）
        local -a lgft_bins=(virt-filesystems virt-customize guestfish guestmount \
            virt-sysprep virt-sparsify virt-builder virt-resize virt-inspector \
            virt-df virt-diff virt-edit virt-format virt-get-kernel virt-log \
            virt-ls virt-make-fs virt-rescue virt-tail virt-cat virt-alignment-scan)
        # 先清理历史坏副本（/usr/local/bin 中无法运行的同名二进制，会遮蔽 /usr/bin 原生版本），
        # 该项独立于下方提取块执行，确保已由系统包提供的工具不被坏副本抢占 PATH
        local cmd2
        for cmd2 in "${lgft_bins[@]}" virt-win-reg; do
            if [ -f "/usr/local/bin/$cmd2" ] && ! timeout 5s "/usr/local/bin/$cmd2" --version >/dev/null 2>&1; then
                rm -f "/usr/local/bin/$cmd2"
                info "移除无法运行的 ${cmd2} 残留（/usr/local/bin），避免遮蔽系统原生命令"
            fi
        done
        unset cmd2
        if ! has_runnable_cmd virt-filesystems && [ -f "$bundled_dir/libguestfs-tools-c.rpm" ]; then
            local extracted=0
            for cmd in "${lgft_bins[@]}"; do
                # 仅当系统无可用运行的原生命令时才提取（避开 PATH 遮蔽的坏副本）
                if ! has_runnable_cmd "$cmd" && \
                   extract_rpm_cmd "$bundled_dir/libguestfs-tools-c.rpm" "$cmd"; then
                    extracted=$((extracted + 1))
                fi
            done
            if has_runnable_cmd virt-filesystems; then
                success "libguestfs-tools-c 提取完成（${extracted} 个工具）"
            fi
        fi

        # virt-win-reg: 从 libguestfs-tools noarch RPM 提取
        if ! has_runnable_cmd virt-win-reg && [ -f "$bundled_dir/libguestfs-tools.rpm" ]; then
            if extract_rpm_cmd "$bundled_dir/libguestfs-tools.rpm" virt-win-reg; then
                success "virt-win-reg 已提取到 /usr/local/bin"
            fi
        fi
    fi
}

ensure_required_commands() {
    info "校验功能所需系统命令..."
    local missing_cmds=()
    local soft_missing_cmds=()
    local cmd
    # RPM 系上来自软性包的命令（缺失时仅警告不报错）
    local rpm_soft_cmds=("virt-customize" "guestfish" "virt-win-reg" "growpart" "virt-filesystems")
    for cmd in "${COMMAND_CHECKS[@]}"; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            # genisoimage 可由 xorriso 或 mkisofs 替代
            if [ "$cmd" = "genisoimage" ]; then
                if command -v xorriso >/dev/null 2>&1 || command -v mkisofs >/dev/null 2>&1; then
                    continue
                fi
                if [ "$PKG_MGR" != "apt" ]; then
                    soft_missing_cmds+=("genisoimage (或 xorriso/mkisofs)")
                    continue
                fi
                missing_cmds+=("genisoimage (或 xorriso/mkisofs)")
                continue
            fi
            # RPM 系软性命令：缺失时仅警告
            local is_soft=0
            if [ "$PKG_MGR" != "apt" ]; then
                for sc in "${rpm_soft_cmds[@]}"; do
                    if [ "$cmd" = "$sc" ]; then
                        is_soft=1
                        break
                    fi
                done
            fi
            if [ "$is_soft" -eq 1 ]; then
                soft_missing_cmds+=("$cmd")
            else
                missing_cmds+=("$cmd")
            fi
        fi
    done
    if [ ${#soft_missing_cmds[@]} -gt 0 ]; then
        warn "以下可选命令不可用（功能可能受限）: ${soft_missing_cmds[*]}"
    fi
    if [ ${#missing_cmds[@]} -gt 0 ]; then
        error "以下命令不可用: ${missing_cmds[*]}。请检查包管理器或依赖安装结果"
        exit 1
    fi
    success "系统命令校验完成"
}

ensure_core_services() {
    info "检查核心服务..."
    systemctl enable --now libvirtd 2>/dev/null || systemctl enable --now libvirt-daemon 2>/dev/null || \
        systemctl enable --now virtqemud 2>/dev/null || true
    systemctl enable --now openvswitch-switch 2>/dev/null || \
        systemctl enable --now openvswitch 2>/dev/null || true
    systemctl enable ssh 2>/dev/null || systemctl enable sshd 2>/dev/null || true

    if ! systemctl is-active --quiet libvirtd 2>/dev/null && \
       ! systemctl is-active --quiet libvirt-daemon 2>/dev/null && \
       ! systemctl is-active --quiet virtqemud 2>/dev/null; then
        # 尝试识别实际存在的服务名，给出更有用的错误信息
        local found_svc=""
        for svc in libvirtd libvirt-daemon virtqemud; do
            if systemctl list-unit-files 2>/dev/null | grep -q "^${svc}\.service"; then
                found_svc="$svc"
                break
            fi
        done
        if [ -n "$found_svc" ]; then
            error "libvirt 服务 ${found_svc} 已安装但未运行，请检查: systemctl status ${found_svc}"
        else
            error "未找到 libvirt 服务（libvirtd/libvirt-daemon/virtqemud），请检查 libvirt 安装状态"
        fi
        exit 1
    fi
    if ! systemctl is-active --quiet openvswitch-switch 2>/dev/null && \
       ! systemctl is-active --quiet openvswitch 2>/dev/null; then
        warn "openvswitch 当前未运行，面板会在网络修复时再次尝试启动"
    fi
    success "核心服务检查完成"
}

# print_ovs_dep_info 输出当前系统检测到的 OVS 依赖信息
print_ovs_dep_info() {
    local os_id="${ID:-unknown}"
    local os_pretty="${PRETTY_NAME:-$os_id}"
    local ovs_pkg="openvswitch-switch"
    local ovs_svc="openvswitch-switch"
    local install_cmd=""

    case "$PKG_MGR" in
        apt)
            ovs_pkg="openvswitch-switch"
            ovs_svc="openvswitch-switch"
            install_cmd="sudo apt install -y openvswitch-switch"
            ;;
        dnf)
            ovs_pkg="openvswitch"
            ovs_svc="openvswitch"
            install_cmd="sudo dnf install -y openvswitch"
            ;;
        yum)
            ovs_pkg="openvswitch"
            ovs_svc="openvswitch"
            install_cmd="sudo yum install -y openvswitch"
            ;;
    esac

    info "──────────────────────────────────────────"
    info "系统检测: ${os_pretty} (${PKG_MGR})"
    info "OVS 包名: ${ovs_pkg}"
    info "OVS 服务: ${ovs_svc}"
    if command -v ovs-vsctl &>/dev/null; then
        success "OVS 已安装 ($(ovs-vsctl --version 2>/dev/null | head -1))"
    else
        warn "OVS 未安装，安装命令: ${install_cmd}"
    fi
    info "──────────────────────────────────────────"
}

# configure_qemu_for_rpm 修复 openEuler/麒麟 上 QEMU 权限问题
# openEuler 默认 QEMU 以 qemu 用户运行，需确保 qemu.conf 配置允许访问虚拟机文件
configure_qemu_for_rpm() {
    [ "$PKG_MGR" = "apt" ] && return 0
    local qemu_conf="/etc/libvirt/qemu.conf"
    if [ ! -f "$qemu_conf" ]; then
        warn "未找到 $qemu_conf，跳过 QEMU 配置修复"
        return 0
    fi
    info "修复 openEuler/麒麟 QEMU 权限配置..."
    # 确保 user 和 group 设置为 root（面板以 root 运行，需要直接操控 QEMU 进程）
    if grep -qE '^#\s*user\s*=' "$qemu_conf"; then
        sed -i 's/^#\s*user\s*=.*/user = "root"/' "$qemu_conf"
    elif ! grep -qE '^user\s*=\s*"root"' "$qemu_conf"; then
        echo 'user = "root"' >> "$qemu_conf"
    fi
    if grep -qE '^#\s*group\s*=' "$qemu_conf"; then
        sed -i 's/^#\s*group\s*=.*/group = "root"/' "$qemu_conf"
    elif ! grep -qE '^group\s*=\s*"root"' "$qemu_conf"; then
        echo 'group = "root"' >> "$qemu_conf"
    fi
    # 重启 libvirtd 使配置生效
    systemctl restart libvirtd 2>/dev/null || systemctl restart libvirt-daemon 2>/dev/null || true
    success "QEMU 权限配置已修复（user=root, group=root）"
}

# configure_libvirt_nonroot 为非 root 用户配置 libvirt 访问权限
# openEuler 文档要求：用户加入 libvirt 组 + 设置 LIBVIRT_DEFAULT_URI 环境变量
# 注意：此函数在 write_env 之前调用；首次安装时 .env 不存在，直接跳过（默认 root）
#       更新时读取已有 .env 中的用户，符合预期（为当前运行用户配置）
configure_libvirt_nonroot() {
    [ "$PKG_MGR" = "apt" ] && return 0
    info "配置非 root 用户 libvirt 访问权限..."
    # 获取面板运行用户（从 .env 或默认 root）
    local panel_user="root"
    if [ -f "$ENV_FILE" ]; then
        local env_user
        env_user=$(grep -E '^KVM_USER=' "$ENV_FILE" 2>/dev/null | cut -d= -f2 | tr -d '"' || true)
        [ -n "$env_user" ] && panel_user="$env_user"
    fi
    if [ "$panel_user" = "root" ]; then
        info "面板以 root 运行，跳过非 root 用户配置"
        return 0
    fi
    # 将用户加入 libvirt 组
    if ! id -nG "$panel_user" 2>/dev/null | grep -qw libvirt; then
        usermod -a -G libvirt "$panel_user" 2>/dev/null && \
            info "用户 $panel_user 已加入 libvirt 组" || \
            warn "无法将用户 $panel_user 加入 libvirt 组"
    fi
    # 设置 LIBVIRT_DEFAULT_URI 环境变量
    local bashrc="/home/$panel_user/.bashrc"
    if [ -f "$bashrc" ] && ! grep -q 'LIBVIRT_DEFAULT_URI' "$bashrc"; then
        echo 'export LIBVIRT_DEFAULT_URI="qemu:///system"' >> "$bashrc"
        info "已为 $panel_user 设置 LIBVIRT_DEFAULT_URI 环境变量"
    fi
    success "libvirt 非 root 用户配置完成"
}

# apply_storage_selinux_label 对用户存储/镜像目录补建 fcontext 规则并 restorecon 打标。
# setup_selinux(STEP5) 运行时存储镜像尚未挂载/写入，restorecon 对后续新增文件无效，
# 故 setup_quota(STEP7) 挂载成功后再调用一次，确保 admin 等已有文件获得 svirt_image_t。
# 幂等：规则已存在时 semanage -d 后重建；无 semanage 时 chcon 兜底。
apply_storage_selinux_label() {
    [ "${SELINUX_MODE:-}" = "Enforcing" ] || return 0
    local dir
    for dir in "$@"; do
        [ -n "$dir" ] || continue
        if command -v semanage >/dev/null 2>&1 && command -v restorecon >/dev/null 2>&1; then
            semanage fcontext -d "${dir}(/.*)?" 2>/dev/null || true
            semanage fcontext -a -t svirt_image_t "${dir}(/.*)?" 2>/dev/null || true
            # 独立 loop 挂载的 ext4 镜像内容默认无 fcontext 匹配，restorecon 必须在此执行
            restorecon -RF "$dir" 2>/dev/null || true
        else
            chcon -R -t svirt_image_t "$dir" 2>/dev/null || true
        fi
    done
}

# setup_selinux 处理国产系统（openEuler/Kylin 等）默认 Enforcing 的 SELinux
# 仅在目标系统 SELinux 为 Enforcing 时执行打标操作；permissive/disabled 直接返回
# - 未启用 / Permissive：直接跳过
# - Enforcing：放行 libvirt/QEMU 相关布尔值，并提示需要时切换 permissive
setup_selinux() {
    if ! command -v getenforce >/dev/null 2>&1; then
        SELINUX_MODE="未安装"
        return 0
    fi
    local mode
    mode=$(getenforce 2>/dev/null || echo "Disabled")
    SELINUX_MODE="$mode"
    case "$mode" in
        Disabled|Permissive)
            info "SELinux 状态: ${mode}，无需额外配置"
            return 0
            ;;
        Enforcing)
            ;;
        *)
            warn "SELinux 状态未知: ${mode}，跳过配置"
            return 0
            ;;
    esac

    info "检测到 SELinux Enforcing，放行 libvirt/QEMU 相关布尔值..."
    local selinux_bools=(
        "virt_use_nfs"
        "virt_use_samba"
        "virt_use_fusefs"
        "virt_use_usb"
        "virt_manage_system"
    )
    local bool
    for bool in "${selinux_bools[@]}"; do
        setsebool -P "$bool" on 2>/dev/null && success "setsebool ${bool} on" || \
            warn "setsebool ${bool} 失败（可忽略，未安装 SELinux 策略包或布尔值不存在）"
    done

    # swtpm（软件 TPM）：UEFI 安全启动模版需要 libvirt 调用 swtpm。
    # openEuler/麒麟 SELinux Enforcing 下若 swtpm 二进制安全上下文缺失/错误会报
    # "applying Failed to execute binary /usr/bin/swtpm: Permission denied"，恢复其可执行标签。
    restorecon -R /usr/bin/swtpm 2>/dev/null || true

    # 面板自定义存储目录（模板/克隆/ISO/用户存储）需 QEMU 可读写
    load_env_file
    local selinux_dirs=(
        "${KVM_TEMPLATE_DIR:-/var/lib/libvirt/images/templates}"
        "${KVM_CLONE_DIR:-/var/lib/libvirt/images}"
        "${KVM_ISO_DIR:-/var/lib/libvirt/images/ISO}"
        "$STORAGE_MOUNT"
    )
    apply_storage_selinux_label "${selinux_dirs[@]}"

    warn "SELinux 保持 Enforcing。若后续 QEMU 虚拟机无法启动或无法访问磁盘，请在 /etc/selinux/config 将 SELINUX 改为 permissive 后重启。"
    success "SELinux 配置完成"
}

detect_storage_backing_size() {
    local backing_dir="$1"
    # 优先使用更稳健的 df --output 方式获取选中磁盘的文件系统大小
    local filesystem_size_gb
    filesystem_size_gb=$(df --output=size -BG "$backing_dir" 2>/dev/null | awk 'NR==2{gsub(/[^0-9]/,"",$1); print $1}')
    if [ -n "$filesystem_size_gb" ] && [ "$filesystem_size_gb" -gt 0 ] 2>/dev/null; then
        echo "${filesystem_size_gb}G"
        return
    fi
    # 回退方案：解析 df -k 输出
    local filesystem_size_kb
    filesystem_size_kb=$(df -k "$backing_dir" 2>/dev/null | awk 'NR==2{print $2}')
    if [ -n "$filesystem_size_kb" ] && [ "$filesystem_size_kb" -gt 0 ] 2>/dev/null; then
        echo "$((filesystem_size_kb / 1024 / 1024))G"
        return
    fi
    echo "100G"
}

load_existing_storage_image() {
    local configured_image
    local mounted_source

    # 优先从 fstab 读取已持久化的镜像位置，兼容此前的默认路径。
    configured_image=$(awk -v mount_point="$STORAGE_MOUNT" '$1 !~ /^#/ && $2 == mount_point { print $1; exit }' /etc/fstab 2>/dev/null || true)
    if [[ "$configured_image" = /* ]] && [ -f "$configured_image" ]; then
        STORAGE_IMG="$configured_image"
        return 0
    fi

    # fstab 尚未写入时，尝试从已挂载的 loop 设备恢复镜像路径。
    if mountpoint -q "$STORAGE_MOUNT"; then
        mounted_source=$(findmnt -rn -o SOURCE -T "$STORAGE_MOUNT" 2>/dev/null || true)
        if [[ "$mounted_source" == /dev/loop* ]]; then
            configured_image=$(losetup -n -O BACK-FILE "$mounted_source" 2>/dev/null | head -n 1 || true)
            if [[ "$configured_image" = /* ]] && [ -f "$configured_image" ]; then
                STORAGE_IMG="$configured_image"
                return 0
            fi
        fi
    fi

    [ -f "$STORAGE_IMG" ]
}

choose_storage_image_location() {
    local root_target
    local source
    local target
    local filesystem
    local available
    local total
    local choice
    local index
    declare -a storage_dirs
    declare -a storage_labels

    root_target=$(findmnt -rn -o TARGET -T "$STORAGE_DEFAULT_BACKING_DIR" 2>/dev/null || true)
    storage_dirs=("$STORAGE_DEFAULT_BACKING_DIR")
    storage_labels=("根目录（默认） 目录: ${STORAGE_DEFAULT_BACKING_DIR}")

    # 仅提供已挂载、可写的本地文件系统；未挂载磁盘不能安全地直接用于创建镜像。
    while read -r source target filesystem; do
        [ -n "$source" ] && [ -n "$target" ] || continue
        [ "$target" = "$root_target" ] && continue
        [ "$target" = "$STORAGE_MOUNT" ] && continue
        [[ "$source" == /dev/* ]] || continue
        if findmnt -rn -o OPTIONS -T "$target" 2>/dev/null | grep -qw ro; then
            continue
        fi
        available=$(df -hP "$target" 2>/dev/null | awk 'NR==2 {print $4}')
        total=$(df -hP "$target" 2>/dev/null | awk 'NR==2 {print $2}')
        storage_dirs+=("$target")
        storage_labels+=("设备: ${source}  挂载点: ${target}  文件系统: ${filesystem}  可用: ${available:-未知}/${total:-未知}")
    done < <(findmnt -rn -t ext4,xfs,btrfs -o SOURCE,TARGET,FSTYPE 2>/dev/null || true)

    echo ""
    info "请选择用户存储镜像所在磁盘（默认使用根目录）"
    for index in "${!storage_dirs[@]}"; do
        printf '  %s) %s\n' "$index" "${storage_labels[$index]}"
    done
    echo "  提示：未挂载或只读磁盘不会显示，请先在系统或存储池页面完成挂载。"

    while true; do
        read_tty -rp "请选择存储磁盘 [默认 0]: " choice
        choice=${choice:-0}
        if [[ "$choice" =~ ^[0-9]+$ ]] && [ "$choice" -lt "${#storage_dirs[@]}" ]; then
            STORAGE_IMG="${storage_dirs[$choice]%/}/${STORAGE_IMG_FILENAME}"
            success "用户存储镜像将创建在: $STORAGE_IMG"
            return
        fi
        warn "无效的选择，请输入 0 到 $((${#storage_dirs[@]} - 1)) 之间的数字"
    done
}

ensure_storage_fstab() {
    local expected_entry="${STORAGE_IMG} ${STORAGE_MOUNT} ext4 loop,prjquota 0 0"
    local temporary_fstab

    touch /etc/fstab
    if grep -Fxq "$expected_entry" /etc/fstab 2>/dev/null; then
        return
    fi

    # 挂载点由面板专用，替换旧条目可避免升级或重新选择磁盘后产生重复挂载。
    temporary_fstab=$(mktemp /etc/fstab.kvm-console.XXXXXX)
    awk -v mount_point="$STORAGE_MOUNT" '$2 != mount_point { print }' /etc/fstab > "$temporary_fstab"
    printf '%s\n' "$expected_entry" >> "$temporary_fstab"
    chmod --reference=/etc/fstab "$temporary_fstab" 2>/dev/null || chmod 644 "$temporary_fstab"
    mv "$temporary_fstab" /etc/fstab
    success "已更新用户存储挂载配置到 /etc/fstab"
}

setup_quota() {
    info "检查用户存储 Project Quota 文件系统..."
    mkdir -p "$STORAGE_MOUNT"
    touch /etc/projects /etc/projid

    if load_existing_storage_image; then
        info "检测到已有用户存储镜像: $STORAGE_IMG"
    else
        choose_storage_image_location
    fi

    if mountpoint -q "$STORAGE_MOUNT"; then
        quotaon -P "$STORAGE_MOUNT" 2>/dev/null || true
        ensure_storage_fstab
        # STEP5 SELinux 打标时存储未挂载，此处补一次，确保已有文件获得 svirt_image_t
        apply_storage_selinux_label "$STORAGE_MOUNT"
        success "用户存储文件系统已挂载"
        return
    fi

    if [ -f "$STORAGE_IMG" ]; then
        info "检测到已有用户存储镜像，正在挂载..."
        if mount -o loop,prjquota "$STORAGE_IMG" "$STORAGE_MOUNT" 2>/dev/null; then
            quotaon -P "$STORAGE_MOUNT" 2>/dev/null || true
            ensure_storage_fstab
            apply_storage_selinux_label "$STORAGE_MOUNT"
            success "用户存储文件系统已挂载"
            return
        fi
        # 挂载失败（可能是之前创建时损坏的镜像），允许重新创建
        warn "现有镜像挂载失败，可能为损坏文件。"
        local recreate="Y"
        if [ "${CI:-}" != "1" ] && [ -t 0 ]; then
            read_tty -rp "是否删除现有镜像并重新创建? [Y/n]: " recreate
            recreate=${recreate:-Y}
        fi
        if [[ "$recreate" =~ ^[Yy]$ ]]; then
            umount "$STORAGE_MOUNT" 2>/dev/null || true
            rm -f "$STORAGE_IMG"
            warn "已删除损坏的镜像，将重新创建"
        else
            error "无法挂载用户存储文件系统，请手动检查: $STORAGE_IMG"
            exit 1
        fi
    fi

    local storage_size
    local default_size
    default_size=$(detect_storage_backing_size "$(dirname "$STORAGE_IMG")")
    echo ""
    info "用户存储配额需要创建专用 ext4 project quota 稀疏镜像"
    if [ "${CI:-}" = "1" ] || [ ! -t 0 ]; then
        storage_size="$default_size"
        info "非交互模式，使用默认容量: ${storage_size}"
    else
        while true; do
            read_tty -rp "存储文件系统最大容量 [默认 ${default_size}]: " storage_size
            storage_size=${storage_size:-$default_size}
            # 校验格式：必须为数字+可选单位（K/M/G/T），不区分大小写
            if [[ "$storage_size" =~ ^[0-9]+[kKmMgGtT]?$ ]]; then
                # 确保有单位后缀（无后缀时默认当作 G）
                if [[ "$storage_size" =~ ^[0-9]+$ ]]; then
                    storage_size="${storage_size}G"
                fi
                break
            fi
            warn "无效的大小格式: ${storage_size}，请输入数字+单位，如 300G、1024M"
        done
    fi

    local confirm="Y"
    if [ "${CI:-}" != "1" ] && [ -t 0 ]; then
        read_tty -rp "是否创建用户存储文件系统? [Y/n]: " confirm
        confirm=${confirm:-Y}
    fi
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        error "已取消创建用户存储文件系统。该文件系统是"我的存储"配额的基础，请创建后再继续安装"
        exit 1
    fi

    info "创建用户存储镜像: $STORAGE_IMG ($storage_size)"
    # 使用 truncate 创建稀疏镜像文件，大小格式已在上方循环中校验
    truncate -s "$storage_size" "$STORAGE_IMG"
    mkfs.ext4 -q -O project,quota "$STORAGE_IMG"
    mount -o loop,prjquota "$STORAGE_IMG" "$STORAGE_MOUNT"
    quotaon -P "$STORAGE_MOUNT" 2>/dev/null || true
    ensure_storage_fstab
    # 新挂载的 ext4 内容默认 unlabeled，补 SELinux 打标（STEP5 时目录尚不存在）
    apply_storage_selinux_label "$STORAGE_MOUNT"
    success "用户存储 Project Quota 文件系统已创建"
}

env_get() {
    local key="$1"
    if [ -f "$ENV_FILE" ]; then
        awk -F= -v k="$key" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' "$ENV_FILE"
    fi
}

env_set() {
    local key="$1"
    local value="$2"
    mkdir -p "$(dirname "$ENV_FILE")"
    touch "$ENV_FILE"
    if grep -q "^${key}=" "$ENV_FILE"; then
        sed -i "s|^${key}=.*|${key}=${value}|" "$ENV_FILE"
    else
        echo "${key}=${value}" >> "$ENV_FILE"
    fi
}

env_default() {
    local key="$1"
    local value="$2"
    if [ -z "$(env_get "$key")" ] && ! grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
        env_set "$key" "$value"
    fi
}

random_secret() {
    local secret
    secret=$(tr -dc 'a-zA-Z0-9' </dev/urandom | head -c 48 || true)
    printf '%s' "$secret"
}

configure_port() {
    local default_port="8080"
    local existing_port
    existing_port=$(env_get "KVM_PORT")
    # 非交互 / CI 模式：直接沿用已存端口或默认端口，不弹交互（#M）
    if [ "${CI:-}" = "1" ] || [ ! -t 0 ]; then
        if [ -n "$existing_port" ]; then
            KVM_PORT="$existing_port"
        else
            KVM_PORT="$default_port"
        fi
        info "非交互模式，使用端口: ${KVM_PORT}"
    else
        if [ -n "$existing_port" ]; then
            read_tty -rp "请输入网页访问端口 [默认保持 ${existing_port}]: " input_port
            KVM_PORT=${input_port:-$existing_port}
        else
            read_tty -rp "请输入网页访问端口 [默认 ${default_port}]: " input_port
            KVM_PORT=${input_port:-$default_port}
        fi
    fi

    if ! [[ "$KVM_PORT" =~ ^[0-9]+$ ]] || [ "$KVM_PORT" -lt 1 ] || [ "$KVM_PORT" -gt 65535 ]; then
        error "无效的端口号: $KVM_PORT，请输入 1-65535 之间的数字"
        exit 1
    fi
    success "网页端口设置为: $KVM_PORT"
}

write_env() {
    info "写入并补齐环境配置..."
    mkdir -p "$INSTALL_DIR"
    touch "$ENV_FILE"
    chmod 600 "$ENV_FILE"

    # 安装路径持久化（供 qvmc-manage.sh / 更新模式读取）
    env_set "INSTALL_DIR" "$INSTALL_DIR"

    # === 关键配置：任何模式下都必须写入或补齐 ===
    # M3：KVM_PORT 为空（repair 模式不经过 configure_port）时保持 .env 已有值，
    # 避免修复配置把用户自定义端口清空。
    if [ -n "$KVM_PORT" ]; then
        env_set "KVM_PORT" "$KVM_PORT"
    else
        env_default "KVM_PORT" "8080"
    fi
    env_default "KVM_DB_PATH" "${INSTALL_DIR}/data/kvm_console.db"
    env_default "KVM_JWT_SECRET" "$(random_secret)"
    env_default "KVM_JWT_SECRET_ROTATE_HOURS" "24"

    # 二进制档位与防火墙后端持久化（§4.3/#M/§5.8：update 复用，白名单值）
    if [ -n "${KVM_BINARY_TIER:-}" ]; then
        case "$KVM_BINARY_TIER" in
            compat|native) env_set "KVM_BINARY_TIER" "$KVM_BINARY_TIER" ;;
            compat-*)
                if [ -n "$HIGH_COMPAT_VER" ] && [ "$KVM_BINARY_TIER" = "compat-${HIGH_COMPAT_VER}" ]; then
                    env_set "KVM_BINARY_TIER" "$KVM_BINARY_TIER"
                else
                    warn "KVM_BINARY_TIER 高兼容档版本不匹配: $KVM_BINARY_TIER，不写入 .env"
                fi
                ;;
            *) warn "KVM_BINARY_TIER 非法值: $KVM_BINARY_TIER，不写入 .env" ;;
        esac
    fi
    if [ -n "${FW_BACKEND:-}" ]; then
        case "$FW_BACKEND" in
            ufw|firewalld|none) env_set "FW_BACKEND" "$FW_BACKEND" ;;
            *) warn "FW_BACKEND 非法值: $FW_BACKEND，不写入 .env" ;;
        esac
    fi
    # CPU 厂商持久化（P0-1 / M8.1，§5.8）：precheck_domestic 已写入则保持，否则按当前探测回填
    if [ -n "${DOMESTIC_CPU_VENDOR:-}" ]; then
        env_set "DOMESTIC_CPU_VENDOR" "$DOMESTIC_CPU_VENDOR"
    elif [ -z "$(env_get "DOMESTIC_CPU_VENDOR")" ]; then
        env_set "DOMESTIC_CPU_VENDOR" "Unknown"
    fi

    if [ "$MODE" = "install" ] || [ "$MODE" = "repair" ]; then
        env_default "KVM_VM_CREDENTIAL_SECRET" "$(random_secret)"
        env_default "KVM_SECURITY_SECRET" "$(random_secret)"
    else
        # 旧版本升级时保持空值，让程序继续回退到 KVM_JWT_SECRET，避免历史加密数据无法解密。
        env_default "KVM_VM_CREDENTIAL_SECRET" ""
        env_default "KVM_SECURITY_SECRET" ""
    fi

    env_default "KVM_JWT_EXPIRE_HOURS" "24"
    env_default "KVM_PORTFORWARD_DIR" "$PORT_FORWARD_DIR"
    env_default "KVM_VM_ACCESS_DIR" "$VM_ACCESS_DIR"
    env_default "KVM_ADMIN_USER" "admin"
    env_default "KVM_ADMIN_PASS" "admin123"
    env_default "KVM_SERVICE_UNIT_NAME" "${SERVICE_NAME}.service"
    env_default "KVM_SMTP_PASSWORD_ENC" ""
    env_set "KVM_USER_STORAGE_IMAGE" "$STORAGE_IMG"

    # === 以下为可配置项：仅首次安装或修复时写入默认值 ===
    # 更新时跳过，保持 .env 现有内容不动，面板保存设置时会同步写 .env
    if [ "$MODE" = "install" ] || [ "$MODE" = "repair" ]; then
        env_default "KVM_TEMPLATE_DIR" "/var/lib/libvirt/images/templates"
        env_default "KVM_TEMPLATE_IMPORT_DIR" "/var/lib/libvirt/images/templates/_imports"
        env_default "KVM_TEMPLATE_EXPORT_DIR" "/var/lib/libvirt/images/templates/_exports"
        env_default "KVM_CLONE_DIR" "/var/lib/libvirt/images"
        env_default "KVM_ISO_DIR" "/var/lib/libvirt/images/ISO"
        env_default "KVM_DEFAULT_NETWORK" "default"
        env_default "KVM_NETWORK_BACKEND" "ovs"
        env_default "KVM_OVS_BRIDGE" "br-ovs"
        env_default "KVM_OVS_UPLINK" ""
        env_default "KVM_OVS_DHCP_START" ""
        env_default "KVM_OVS_DHCP_END" ""
        env_default "KVM_SUBNET_PREFIX" "192.168.122"
        env_default "KVM_AUTO_PORT_START" "10000"
        env_default "KVM_AUTO_PORT_END" "20000"
        env_default "KVM_HOST_IP" ""
        env_default "KVM_EXTERNAL_NIC" ""
        env_default "KVM_MAX_BURST_INBOUND" "0"
        env_default "KVM_MAX_BURST_OUTBOUND" "0"
        env_default "KVM_RESCUE_ISO" ""
        env_default "KVM_PUBLIC_BASE_URL" ""
        env_default "KVM_SITE_TITLE" "QVMConsole"
        env_default "KVM_DEVELOPMENT_MODE" "false"
        env_default "KVM_MAINTENANCE_MODE" "false"
        env_default "KVM_MAINTENANCE_SERVICE_UNITS" "kvm-console.service,libvirtd.service,libvirtd.socket,libvirtd-ro.socket,libvirtd-admin.socket"
        env_default "KVM_MAINTENANCE_VM_SHUTDOWN_TIMEOUT_SECONDS" "40"
        env_default "KVM_SMTP_HOST" ""
        env_default "KVM_SMTP_PORT" "587"
        env_default "KVM_SMTP_USERNAME" ""
        env_default "KVM_SMTP_FROM_NAME" "QVMConsole"
        env_default "KVM_SMTP_FROM_ADDRESS" ""
        env_default "KVM_SMTP_SECURITY" "starttls"
        env_default "KVM_SMTP_TIMEOUT_SECONDS" "15"
        env_default "KVM_DYNAMIC_MEMORY_SCHEDULER_ENABLED" "true"
        env_default "KVM_DYNAMIC_MEMORY_INTERVAL_SECONDS" "30"
        env_default "KVM_DYNAMIC_MEMORY_HOST_RESERVE_MB" "2048"
        env_default "KVM_DYNAMIC_MEMORY_HOST_RESERVE_PERCENT" "20"
        env_default "KVM_DYNAMIC_MEMORY_INCREASE_THRESHOLD_PERCENT" "15"
        env_default "KVM_DYNAMIC_MEMORY_RECLAIM_THRESHOLD_PERCENT" "35"
        env_default "KVM_DYNAMIC_MEMORY_COOLDOWN_SECONDS" "120"
        env_default "KVM_DYNAMIC_MEMORY_OBSERVATION_HOURS" "24"
        env_default "KVM_SCHEDULER_EVENT_RETENTION_HOURS" "168"
        env_default "KVM_VPC_SUBNET_PREFIX" "10.200"
        env_default "KVM_VPC_VLAN_START" "100"
        env_default "KVM_VPC_VLAN_END" "4094"
        env_default "KVM_VPC_DNS" "223.5.5.5,223.6.6.6"
        env_default "KVM_VPC_ACL_TABLE" "kvm_console_vpc_acl"
        env_default "KVM_DEFAULT_DISK_IOPS_TOTAL" "0"
        env_default "KVM_DEFAULT_DISK_IOPS_READ" "0"
        env_default "KVM_DEFAULT_DISK_IOPS_WRITE" "0"
        env_default "KVM_BATCH_CLONE_MAX_CONCURRENCY" "10"
    fi

    success "配置文件已准备: $ENV_FILE"
}

load_env_file() {
    if [ -f "$ENV_FILE" ]; then
        set -a
        # shellcheck disable=SC1090
        . "$ENV_FILE"
        set +a
    fi
}

ensure_directories() {
    info "补齐运行目录..."
    load_env_file

    local template_dir="${KVM_TEMPLATE_DIR:-/var/lib/libvirt/images/templates}"
    local import_dir="${KVM_TEMPLATE_IMPORT_DIR:-${template_dir}/_imports}"
    local export_dir="${KVM_TEMPLATE_EXPORT_DIR:-${template_dir}/_exports}"
    local clone_dir="${KVM_CLONE_DIR:-/var/lib/libvirt/images}"
    local iso_dir="${KVM_ISO_DIR:-/var/lib/libvirt/images/ISO}"

    mkdir -p \
        "${INSTALL_DIR}/data" \
        "$template_dir" \
        "$import_dir" \
        "$export_dir" \
        "$clone_dir" \
        "$iso_dir" \
        "$PORT_FORWARD_DIR/backups" \
        "$VM_ACCESS_DIR" \
        "$FIREWALL_DIR/backups" \
        "$VPC_CONFIG_DIR" \
        "$OVS_CONFIG_DIR" \
        "$OVS_STATE_DIR" \
        "$STORAGE_MOUNT" \
        "/etc/ssh/sshd_config.d"

    touch "$OVS_CONFIG_DIR/dhcp-hosts"
    touch /etc/projects /etc/projid

    # ARM 架构部署旧版 AAVMF 兼容固件（解决统信 UOS 等 OS 的 UEFI 引导兼容性问题）
    if [ "$ARCH" = "aarch64" ]; then
        local firmware_dir="${INSTALL_DIR}/firmware"
        mkdir -p "$firmware_dir"
        if [ ! -f "$firmware_dir/AAVMF_CODE_legacy.fd" ]; then
            info "部署 ARM UEFI 兼容固件..."
            # 优先从 Ubuntu 24.04 仓库下载旧版 EDK2
            local efi_deb_url="http://ports.ubuntu.com/pool/main/e/edk2/qemu-efi-aarch64_2024.02-2_all.deb"
            local efi_deb_file="/tmp/qemu-efi-legacy.deb"
            if wget -q "$efi_deb_url" -O "$efi_deb_file" 2>/dev/null || \
               wget -q "http://mirrors.aliyun.com/ubuntu-ports/pool/main/e/edk2/qemu-efi-aarch64_2024.02-2_all.deb" -O "$efi_deb_file" 2>/dev/null; then
                local efi_extract="/tmp/efi-legacy-extract"
                rm -rf "$efi_extract"
                mkdir -p "$efi_extract"
                dpkg-deb -x "$efi_deb_file" "$efi_extract" 2>/dev/null
                if [ -f "$efi_extract/usr/share/AAVMF/AAVMF_CODE.no-secboot.fd" ]; then
                    cp -f "$efi_extract/usr/share/AAVMF/AAVMF_CODE.no-secboot.fd" "$firmware_dir/AAVMF_CODE_legacy.fd"
                    cp -f "$efi_extract/usr/share/AAVMF/AAVMF_VARS.fd" "$firmware_dir/AAVMF_VARS_legacy.fd"
                    success "ARM UEFI 兼容固件部署完成"
                else
                    warn "旧版固件提取失败，跳过兼容固件部署"
                fi
                rm -rf "$efi_extract" "$efi_deb_file"
            else
                warn "下载旧版 AAVMF 固件失败，跳过兼容固件部署（可手动放置到 $firmware_dir）"
            fi
        else
            success "ARM UEFI 兼容固件已存在"
        fi
    fi

    if getent group vmoperator >/dev/null 2>&1; then
        true
    else
        groupadd -f vmoperator
    fi

    local qemu_user=""
    if id libvirt-qemu >/dev/null 2>&1; then
        qemu_user="libvirt-qemu"
    elif id qemu >/dev/null 2>&1; then
        qemu_user="qemu"
    fi
    if [ -n "$qemu_user" ] && getent group kvm >/dev/null 2>&1; then
        chown "$qemu_user:kvm" "$template_dir" "$import_dir" "$export_dir" "$clone_dir" "$iso_dir" 2>/dev/null || true
        chmod 775 "$template_dir" "$import_dir" "$export_dir" "$clone_dir" "$iso_dir" 2>/dev/null || true
        find "$template_dir" -type f \( -name '*.qcow2' -o -name '*.img' -o -name '*.raw' \) -exec chown "$qemu_user:kvm" {} + 2>/dev/null || true
        find "$template_dir" -type f \( -name '*.qcow2' -o -name '*.img' -o -name '*.raw' \) -exec chmod u+rw {} + 2>/dev/null || true
    fi

    # P1-4：敏感配置目录/文件权限加固（§5.8）：目录 700、含密钥/映射的 JSON 与规则文件 600
    chmod 700 /etc/kvm-console "$STATE_DIR" "$PORT_FORWARD_DIR" "$PORT_FORWARD_DIR/backups" "$VM_ACCESS_DIR" \
        "$FIREWALL_DIR" "$FIREWALL_DIR/backups" "$VPC_CONFIG_DIR" "$OVS_CONFIG_DIR" 2>/dev/null || true
    find "$FIREWALL_DIR" "$VPC_CONFIG_DIR" "$OVS_CONFIG_DIR" "$PORT_FORWARD_DIR" "$VM_ACCESS_DIR" -type f \
        \( -name '*.json' -o -name 'rules' -o -name 'policies' -o -name '*.conf' -o -name 'dhcp-hosts' \) \
        -exec chmod 600 {} + 2>/dev/null || true

    success "运行目录已补齐"
}

ensure_apparmor_storage_access() {
    if [ ! -d /sys/module/apparmor ] || [ ! -d /etc/apparmor.d ]; then
        return 0
    fi

    info "配置 libvirt 自定义存储 AppArmor 访问规则..."
    load_env_file
    mkdir -p /etc/apparmor.d/local /etc/apparmor.d/abstractions/libvirt-qemu.d

    local marker="# BEGIN kvm_console managed storage access"
    local marker_end="# END kvm_console managed storage access"
    local helper_file="/etc/apparmor.d/local/usr.lib.libvirt.virt-aa-helper"
    local qemu_file="/etc/apparmor.d/abstractions/libvirt-qemu.d/kvm-console-storage"
    local storage_root="/var/lib/kvm-storage"
    local template_dir="${KVM_TEMPLATE_DIR:-/var/lib/libvirt/images/templates}"
    local user_storage_root="$STORAGE_MOUNT"

    touch "$helper_file" "$qemu_file"

    write_managed_apparmor_block() {
        local file="$1"
        local permission="$2"
        local tmp
        tmp="$(mktemp)"
        awk -v begin="$marker" -v end="$marker_end" '
            $0 == begin { skip = 1; next }
            $0 == end { skip = 0; next }
            !skip { print }
        ' "$file" >"$tmp"

        {
            cat "$tmp"
            printf '\n%s\n' "$marker"
            for root in "$storage_root" "$user_storage_root" "$template_dir"; do
                root="${root%/}"
                [ -n "$root" ] || continue
                printf '%s/ r,\n' "$root"
                printf '%s/**/ r,\n' "$root"
                printf '%s/** %s,\n' "$root" "$permission"
            done
            printf '%s\n' "$marker_end"
        } >"$file"

        rm -f "$tmp"
    }

    write_managed_apparmor_block "$helper_file" "r"
    write_managed_apparmor_block "$qemu_file" "rwk"

    if command -v apparmor_parser >/dev/null 2>&1 && [ -f /etc/apparmor.d/usr.lib.libvirt.virt-aa-helper ]; then
        apparmor_parser -r /etc/apparmor.d/usr.lib.libvirt.virt-aa-helper 2>/dev/null || warn "virt-aa-helper AppArmor 规则重载失败，后续启动 VM 时会再次尝试修复"
    fi
}

detect_default_uplink() {
    ip route show default 2>/dev/null | awk '{print $5; exit}'
}

ensure_sysctl_network() {
    info "启用 IPv4 转发..."
    cat >/etc/sysctl.d/99-kvm-console-network.conf <<'EOF'
net.ipv4.ip_forward=1
EOF
    sysctl -p /etc/sysctl.d/99-kvm-console-network.conf >/dev/null || true
}

# #F：firewalld 后端时用 trusted zone 绑定网桥放行 dnsmasq 入站（UDP 67/53、TCP 53），
# 不再写 iptables INPUT 规则（nftables 后端下 firewalld 链先于 iptables 求值，直写会失效或冲突）。
firewalld_bind_bridge_trusted() {
    local iface="$1"
    [ -n "$iface" ] || return 0
    command -v firewall-cmd >/dev/null 2>&1 || return 0
    systemctl is-active --quiet firewalld 2>/dev/null || return 0
    if ! firewall-cmd --permanent --zone=trusted --list-interfaces 2>/dev/null | grep -qx "$iface"; then
        firewall-cmd --permanent --zone=trusted --add-interface "$iface" >/dev/null 2>&1 || true
    fi
    firewall-cmd --reload >/dev/null 2>&1 || true
}

# 存量环境迁移（#F）：后端为 firewalld 时清理面板早期直写的 dnsmasq iptables INPUT 规则，
# 避免与 trusted zone 绑定重复/冲突（install 模式幂等）。
cleanup_dnsmasq_iptables_rules() {
    [ "$FW_BACKEND" = "firewalld" ] || return 0
    command -v iptables >/dev/null 2>&1 || return 0

    local iface rule proto port
    for iface in br-ovs $(ovs-vsctl --format=csv --data=bare --no-heading --columns=name find Interface type=internal 2>/dev/null | grep '^vpcsw' || true); do
        [ -n "$iface" ] || continue
        for rule in "udp 67" "udp 53" "tcp 53"; do
            proto="${rule%% *}"
            port="${rule##* }"
            iptables -C INPUT -i "$iface" -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null && \
                iptables -D INPUT -i "$iface" -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || true
        done
    done
}

ensure_local_dnsmasq_input_rules() {
    local iface="$1"
    [ -n "$iface" ] || return 0

    # #F：firewalld 后端走 trusted zone 绑定，不写 iptables INPUT 规则
    if [ "${FW_BACKEND:-}" = "firewalld" ]; then
        firewalld_bind_bridge_trusted "$iface"
        return 0
    fi

    local rule proto port
    for rule in "udp 67" "udp 53" "tcp 53"; do
        proto="${rule%% *}"
        port="${rule##* }"
        iptables -C INPUT -i "$iface" -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || \
            iptables -I INPUT 1 -i "$iface" -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || true
    done
}

ensure_existing_vpc_dnsmasq_input_rules() {
    command -v ovs-vsctl >/dev/null 2>&1 || return 0

    local iface
    ovs-vsctl --format=csv --data=bare --no-heading --columns=name find Interface type=internal 2>/dev/null | while IFS= read -r iface; do
        case "$iface" in
            vpcsw*) ensure_local_dnsmasq_input_rules "$iface" ;;
        esac
    done
}

wait_unit_active() {
    local unit="$1"
    local max_wait="${2:-6}"
    local i
    for ((i = 0; i < max_wait; i++)); do
        if systemctl is-active --quiet "$unit" 2>/dev/null; then
            return 0
        fi
        sleep 1
    done
    return 1
}

# 释放 OVS 网桥 192.168.<prefix>.1 的 53/67 端口占用（多为 libvirt default dnsmasq），
# 避免 OVS dnsmasq 启动时 "Address already in use" 反复失败。
_free_ovs_dnsmasq_port() {
    local bridge="$1"
    local subnet="$2"
    local bind_ip="${subnet}.1"
    local pid
    # 仅清理监听该网关地址上的 dnsmasq（含 libvirt default 网络进程），不误杀其它服务
    pid=$(ss -tlnp 2>/dev/null | grep "${bind_ip}:53" | grep -oP 'pid=\K[0-9]+' | head -1 || true)
    if [ -n "$pid" ]; then
        kill "$pid" 2>/dev/null || true
        info "释放 ${bind_ip}:53 占用（pid $pid），等待 OVS dnsmasq 绑定"
        sleep 1
    fi
}

restart_ovs_dnsmasq_service() {
    if systemctl restart "$OVS_DNSMASQ_UNIT" >/dev/null 2>&1; then
        success "OVS DHCP 服务已启动"
        return 0
    fi

    # dnsmasq 首次启动可能因网桥/端口就绪时序失败一次，systemd 的 Restart=on-failure+RestartSec=5
    # 会自动重试；等待窗需覆盖一个重试周期，避免在 systemd 尚未完成恢复时误报「未启动成功」。
    if wait_unit_active "$OVS_DNSMASQ_UNIT" 15; then
        success "OVS DHCP 服务已在 systemd 自动重试后启动"
        return 0
    fi

    warn "OVS DHCP 服务暂未启动成功，可在面板 OVS 诊断中执行修复，或查看: journalctl -u ${OVS_DNSMASQ_UNIT} -n 80 --no-pager"
    return 0
}

setup_ovs_foundation() {
    info "准备 OVS 网络地基..."
    print_ovs_dep_info

    # 内部子函数，任何失败只警告不中断安装
    _setup_ovs_inner || warn "OVS 网络地基配置部分失败，可在面板 OVS 诊断中执行修复"
    success "OVS 网络地基已准备"
}

_setup_ovs_inner() {
    load_env_file
    local bridge="${KVM_OVS_BRIDGE:-br-ovs}"
    local subnet="${KVM_SUBNET_PREFIX:-192.168.122}"
    local gateway="${subnet}.1"
    local dhcp_start="${KVM_OVS_DHCP_START:-${subnet}.2}"
    local dhcp_end="${KVM_OVS_DHCP_END:-${subnet}.254}"
    local uplink="${KVM_OVS_UPLINK:-}"

    if [ -z "$uplink" ]; then
        uplink=$(detect_default_uplink)
    fi
    if [ -z "$uplink" ]; then
        warn "未检测到默认出口网卡，OVS NAT 将在面板网络修复时再次尝试。也可在 $ENV_FILE 配置 KVM_OVS_UPLINK"
    fi

    systemctl enable --now openvswitch-switch 2>/dev/null || \
        systemctl enable --now openvswitch 2>/dev/null || true
    # 等待 OVS 数据库就绪，避免 ovs-vsctl 挂起
    for _i in 1 2 3 4 5 6 7 8 9 10; do
        ovs-vsctl --no-wait show 2>/dev/null && break
        sleep 1
    done
    if ! ovs-vsctl --timeout=5 --may-exist add-br "$bridge" 2>/dev/null; then
        warn "创建 OVS 网桥失败，跳过 OVS 网络配置"
        return 0
    fi
    if ! ip link set "$bridge" up 2>/dev/null; then
        warn "启动 OVS 网桥失败，跳过 OVS 网络配置"
        return 0
    fi
    if ! ip -4 addr show dev "$bridge" | grep -q "${gateway}/24"; then
        ip addr flush dev "$bridge" 2>/dev/null || true
        ip addr add "${gateway}/24" dev "$bridge" 2>/dev/null || true
    fi
    ensure_local_dnsmasq_input_rules "$bridge" || true
    ensure_existing_vpc_dnsmasq_input_rules || true
    # #F：install 模式 + firewalld 后端时清理旧 iptables dnsmasq 规则（存量迁移，幂等）
    if [ "$MODE" = "install" ]; then
        cleanup_dnsmasq_iptables_rules || true
    fi

    cat >"${OVS_CONFIG_DIR}/dnsmasq.conf" <<EOF
interface=${bridge}
bind-interfaces
except-interface=lo
dhcp-authoritative
dhcp-range=${dhcp_start},${dhcp_end},255.255.255.0,12h
dhcp-option=option:router,${gateway}
dhcp-option=option:dns-server,223.5.5.5,223.6.6.6
dhcp-hostsfile=${OVS_CONFIG_DIR}/dhcp-hosts
dhcp-leasefile=${OVS_STATE_DIR}/dnsmasq.leases
pid-file=/run/kvm-console-ovs-dnsmasq.pid
log-dhcp
EOF

    cat >"${OVS_CONFIG_DIR}/prepare-bridge.sh" <<EOF
#!/bin/bash
set -e
BRIDGE="${bridge}"
GATEWAY="${gateway}/24"
ovs-vsctl --may-exist add-br "\$BRIDGE" 2>/dev/null || true
ip link set "\$BRIDGE" up 2>/dev/null || true
if ! ip -4 addr show dev "\$BRIDGE" | grep -q "\$GATEWAY"; then
  ip addr flush dev "\$BRIDGE" 2>/dev/null || true
  ip addr add "\$GATEWAY" dev "\$BRIDGE" 2>/dev/null || true
fi
# 释放端口，确保 libvirt dnsmasq 完全停止
for i in 1 2 3 4 5; do
  if ss -tlnp | grep -q "${gateway}:53"; then
    sleep 1
  else
    break
  fi
done
pkill -f "dnsmasq.*${subnet}" 2>/dev/null || true
sleep 0.5
EOF
    # M1：firewalld 后端下 dnsmasq 入站由 VM 桥绑定 trusted zone（ACCEPT）保证，
    # 不再写 iptables INPUT 规则（避免与 #F「firewalld 下不落 INPUT 链」设计冲突）。
    if [ "$FW_BACKEND" != "firewalld" ]; then
        cat >>"${OVS_CONFIG_DIR}/prepare-bridge.sh" <<'EOF'
for rule in "udp 67" "udp 53" "tcp 53"; do
  proto="${rule%% *}"
  port="${rule##* }"
  iptables -C INPUT -i "$BRIDGE" -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || \
    iptables -I INPUT 1 -i "$BRIDGE" -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || true
done
EOF
    fi
    chmod +x "${OVS_CONFIG_DIR}/prepare-bridge.sh"

    # 写 unit 前给 OVS 服务名兜底默认值，避免 heredoc 插值出空的 ".service" 依赖
    # （openEuler/RHEL 用 openvswitch，Debian 用 openvswitch-switch）
    if [ -z "${OVS_SERVICE_NAME:-}" ]; then
        case "$PKG_MGR" in
            apt) OVS_SERVICE_NAME="openvswitch-switch" ;;
            *) OVS_SERVICE_NAME="openvswitch" ;;
        esac
    fi
    cat >"$OVS_DNSMASQ_SERVICE_FILE" <<EOF
[Unit]
Description=KVM Console OVS DHCP/DNS service
After=network-online.target ${OVS_SERVICE_NAME}.service
Wants=network-online.target ${OVS_SERVICE_NAME}.service

[Service]
Type=forking
PIDFile=/run/kvm-console-ovs-dnsmasq.pid
ExecStart=/usr/sbin/dnsmasq --conf-file=${OVS_CONFIG_DIR}/dnsmasq.conf
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=5
# 网桥和端口释放由主服务 EnsureOVSNetworkReady() 统一处理

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable "$OVS_DNSMASQ_UNIT" >/dev/null 2>&1 || true

    # M4：仅 install 模式先销毁 libvirt default NAT 网络，释放其 dnsmasq 占用的
    # 192.168.122.1:53/67，再启动 OVS dnsmasq。顺序若反，OVS dnsmasq 会因
    # "Address already in use" 联败（openEuler 上 libvirt default 默认已监听该地址）。
    # update 模式保留 default 网络，避免已挂在其上的既有 VM 升级后立即断网。
    if [ "$MODE" = "install" ] && virsh net-info default >/dev/null 2>&1; then
        virsh net-destroy default >/dev/null 2>&1 || true
        virsh net-autostart default --disable >/dev/null 2>&1 || true
    fi
    # 兜底：若仍有进程占用 122.1:53（如 libvirt dnsmasq 未释放），先释放再启动
    _free_ovs_dnsmasq_port "$bridge" "$subnet"

    restart_ovs_dnsmasq_service

    if [ -n "$uplink" ]; then
        iptables -t nat -C POSTROUTING -s "${subnet}.0/24" -o "$uplink" -j MASQUERADE 2>/dev/null || \
            iptables -t nat -A POSTROUTING -s "${subnet}.0/24" -o "$uplink" -j MASQUERADE 2>/dev/null || true
        iptables -C FORWARD -i "$bridge" -o "$uplink" -j ACCEPT 2>/dev/null || \
            iptables -A FORWARD -i "$bridge" -o "$uplink" -j ACCEPT 2>/dev/null || true
        iptables -C FORWARD -i "$uplink" -o "$bridge" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || \
            iptables -A FORWARD -i "$uplink" -o "$bridge" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
    fi
}

setup_sshd_foundation() {
    if [ -f /etc/ssh/sshd_config ] && ! grep -q 'Include /etc/ssh/sshd_config.d/' /etc/ssh/sshd_config; then
        sed -i '1i Include /etc/ssh/sshd_config.d/*.conf' /etc/ssh/sshd_config
    fi
    systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || true
}

# detect_firewall_backend 探测宿主机防火墙后端（§4.4/§5.8，#M 支持环境变量覆盖）
# 必须放在 check_and_install_deps 之后（RPM 系 firewalld 由依赖步骤装上）。
detect_firewall_backend() {
    local backend="none"
    # 非交互 / 环境变量覆盖（#M）：前端已导出 FW_BACKEND 时跳过探测
    if [ -n "${FW_BACKEND:-}" ]; then
        case "$FW_BACKEND" in
            ufw|firewalld|none) backend="$FW_BACKEND"; info "FW_BACKEND 环境变量覆盖: ${backend}" ;;
            *) warn "FW_BACKEND 非法值: $FW_BACKEND，回退自动探测" ;;
        esac
    elif [ "$MODE" = "update" ] && [ -f "$ENV_FILE" ]; then
        # M2：update 模式复用 .env 已持久化的后端，避免检测顺序漂移导致静默切后端
        # （用户手动钉住 firewalld 后若系统又装了 ufw，自动探测会改选 ufw）。
        local persisted
        persisted=$(grep -E '^FW_BACKEND=' "$ENV_FILE" 2>/dev/null | tail -n1 | cut -d= -f2- | tr -d '"' | tr -d '[:space:]' || true)
        case "$persisted" in
            ufw|firewalld|none) backend="$persisted"; info "复用 .env 持久化 FW_BACKEND: ${backend}" ;;
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
        DEGRADED_NOTES="${DEGRADED_NOTES:+${DEGRADED_NOTES}；}未检测到 ufw / firewalld（宿主机防火墙功能不可用）"
        return 0
    fi
    info "检测到宿主机防火墙后端: ${backend}"
    # firewalld 版本探测（#Q：安装期 advice 判断 <0.9 缺 policy，与后端 /system-info 同口径）
    if [ "$backend" = "firewalld" ] && command -v firewall-cmd >/dev/null 2>&1; then
        DETECTED_FW_VER=$(firewall-cmd --version 2>/dev/null | head -n1 | tr -d '[:space:]' || true)
        [ -n "$DETECTED_FW_VER" ] && info "检测到 firewalld 版本: ${DETECTED_FW_VER}"
    fi
    # RPM 系 firewalld 未运行则询问是否启动（仅 install 模式；update 模式只展示不询问）
    if [ "$MODE" = "install" ] && [ "$backend" = "firewalld" ] && ! systemctl is-active --quiet firewalld 2>/dev/null; then
        if [ "${CI:-}" = "1" ] || [ ! -t 0 ]; then
            warn "firewalld 未运行（非交互模式跳过启动询问）"
            return 0
        fi
        read_tty -rp "检测到 firewalld 未运行，是否立即启动并设为开机自启? [y/N]: " ans
        if [[ "${ans:-N}" =~ ^[Yy]$ ]]; then
            systemctl enable --now firewalld
        fi
    fi
}

# open_frontend_port 检测防火墙状态并放行前端访问端口。
# 若防火墙已关闭（不拦截流量）则直接跳过；若开启则默认放开前端端口（KVM_PORT）后继续。
# 依赖 detect_firewall_backend 设置 FW_BACKEND；KVM_PORT 由 configure_port 设定。
# 支持 firewalld（RPM 系）与 ufw（Debian 系）两种后端；无后端或端口为空直接跳过。
open_frontend_port() {
    local port="${KVM_PORT:-$(env_get "KVM_PORT")}"
    [ -n "$port" ] || {
        warn "前端端口为空，跳过防火墙放行"
        return 0
    }

    case "${FW_BACKEND:-}" in
        firewalld)
            if ! command -v firewall-cmd >/dev/null 2>&1; then
                warn "未检测到 firewall-cmd，跳过防火墙放行"
                return 0
            fi
            # firewalld 未运行则视为防火墙关闭，直接继续
            if ! systemctl is-active --quiet firewalld 2>/dev/null; then
                info "firewalld 未运行（防火墙关闭），无需放行端口"
                return 0
            fi
            if firewall-cmd --query-port="${port}/tcp" >/dev/null 2>&1; then
                info "前端端口 ${port}/tcp 已在防火墙放行"
            else
                firewall-cmd --permanent --zone=public --add-port="${port}/tcp" >/dev/null 2>&1 && \
                    firewall-cmd --reload >/dev/null 2>&1 || true
                info "已放行前端端口 ${port}/tcp"
            fi
            ;;
        ufw)
            if ! command -v ufw >/dev/null 2>&1; then
                warn "未检测到 ufw，跳过防火墙放行"
                return 0
            fi
            # ufw 未激活则防火墙关闭，直接继续
            if ! ufw status | grep -q "Status: active"; then
                info "ufw 未启用（防火墙关闭），无需放行端口"
                return 0
            fi
            if ufw status | grep -q " ${port}/tcp "; then
                info "前端端口 ${port}/tcp 已在防火墙放行"
            else
                ufw allow "${port}/tcp" >/dev/null 2>&1 || true
                info "已放行前端端口 ${port}/tcp"
            fi
            ;;
        none|"")
            info "未检测到防火墙后端，跳过端口放行"
            ;;
        *)
            warn "未知防火墙后端: ${FW_BACKEND}，跳过端口放行"
            ;;
    esac
}

# check_component_versions 组件版本检测（§5.11.4 / M7.1）
# 在 get_release 之后、select_binary_tier 之前执行：需 RELEASE_SOURCE_DIR 提供 compat-manifest.json。
# 行为：critical（低于最低要求）→ 中止安装；warning（低于推荐但达最低）→ 交互确认；healthy → 通过。
# --skip-version-check 指定时仅跳过 critical 中止，报告照常输出。
check_component_versions() {
    info "检查关键组件版本..."
    COMP_VER_TOTAL=0
    COMP_VER_HEALTHY=0
    COMP_VER_WARN=0
    COMP_VER_CRIT=0
    local warnings=() criticals=()

    # 版本阈值默认值（§5.11.2 表格；versions.conf 存在时以配置为准。阈值唯一维护点在 build.sh COMPONENT_REQ_* 变量）
    local min_qemu="6.0" rec_qemu="8.0"
    local min_qemuimg="6.0" rec_qemuimg="8.0"
    local min_libvirt="7.0" rec_libvirt="8.0"
    local min_ovs="2.13" rec_ovs="2.15"
    local min_dnsmasq="2.80" rec_dnsmasq="2.86"
    local min_firewalld="0.4.0" rec_firewalld="0.9.0"
    local min_ufw="0.36"
    local min_virtinstall="3.0" rec_virtinstall="4.0"
    local min_virtcust="1.40" rec_virtcust="1.48"
    local min_guestfish="1.40" rec_guestfish="1.48"
    local min_growpart="0.30"
    local min_ntfsresize="2022.5"
    local min_tcpdump="4.9" rec_tcpdump="4.99"
    local min_tc="5.0" rec_tc="5.10"
    local min_glibc_amd64="2.2.5" min_glibc_arm64="2.17"

    # 读取构建产物 versions.conf（build.sh 依据 compat-manifest.json 同源生成，纯 shell 解析，无 python3 依赖）
    # 单循环直接覆盖阈值（§4.4 评审：合并原「读入 manifest_data + 二次解析」两段，避免中间态误改）
    local vconf="${RELEASE_SOURCE_DIR}/versions.conf"
    local loaded_count=0 line
    if [ -f "$vconf" ]; then
        while IFS= read -r line; do
            [ -n "$line" ] || continue
            case "$line" in
                \#*) continue ;;
                GLIBC_MIN_AMD64=*) min_glibc_amd64="${line#*=}"; loaded_count=$((loaded_count + 1)) ;;
                GLIBC_MIN_ARM64=*) min_glibc_arm64="${line#*=}"; loaded_count=$((loaded_count + 1)) ;;
                qemu-kvm=*)        min_qemu="${line#*=}"; min_qemu="${min_qemu%|*}"; rec_qemu="${line#*=}"; rec_qemu="${rec_qemu#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                qemu-img=*)        min_qemuimg="${line#*=}"; min_qemuimg="${min_qemuimg%|*}"; rec_qemuimg="${line#*=}"; rec_qemuimg="${rec_qemuimg#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                libvirt=*)         min_libvirt="${line#*=}"; min_libvirt="${min_libvirt%|*}"; rec_libvirt="${line#*=}"; rec_libvirt="${rec_libvirt#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                openvswitch=*)     min_ovs="${line#*=}"; min_ovs="${min_ovs%|*}"; rec_ovs="${line#*=}"; rec_ovs="${rec_ovs#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                dnsmasq=*)         min_dnsmasq="${line#*=}"; min_dnsmasq="${min_dnsmasq%|*}"; rec_dnsmasq="${line#*=}"; rec_dnsmasq="${rec_dnsmasq#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                firewalld=*)       min_firewalld="${line#*=}"; min_firewalld="${min_firewalld%|*}"; rec_firewalld="${line#*=}"; rec_firewalld="${rec_firewalld#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                ufw=*)             min_ufw="${line#*=}"; min_ufw="${min_ufw%|*}" ; loaded_count=$((loaded_count + 1)) ;;
                virt-install=*)    min_virtinstall="${line#*=}"; min_virtinstall="${min_virtinstall%|*}"; rec_virtinstall="${line#*=}"; rec_virtinstall="${rec_virtinstall#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                virt-customize=*)  min_virtcust="${line#*=}"; min_virtcust="${min_virtcust%|*}"; rec_virtcust="${line#*=}"; rec_virtcust="${rec_virtcust#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                guestfish=*)       min_guestfish="${line#*=}"; min_guestfish="${min_guestfish%|*}"; rec_guestfish="${line#*=}"; rec_guestfish="${rec_guestfish#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                growpart=*)        min_growpart="${line#*=}"; min_growpart="${min_growpart%|*}" ; loaded_count=$((loaded_count + 1)) ;;
                ntfsresize=*)      min_ntfsresize="${line#*=}"; min_ntfsresize="${min_ntfsresize%|*}" ; loaded_count=$((loaded_count + 1)) ;;
                tcpdump=*)         min_tcpdump="${line#*=}"; min_tcpdump="${min_tcpdump%|*}"; rec_tcpdump="${line#*=}"; rec_tcpdump="${rec_tcpdump#*|}" ; loaded_count=$((loaded_count + 1)) ;;
                tc=*)              min_tc="${line#*=}"; min_tc="${min_tc%|*}"; rec_tc="${line#*=}"; rec_tc="${rec_tc#*|}" ; loaded_count=$((loaded_count + 1)) ;;
            esac
        done < "$vconf"
        if [ "$loaded_count" -gt 0 ]; then
            success "已加载组件版本阈值: $vconf"
        else
            warn "versions.conf 存在但无有效内容，使用内置默认版本阈值（可能与构建档位不匹配）"
        fi
    else
        warn "未找到 versions.conf，使用内置默认版本阈值（可能与构建档位不匹配）"
    fi

    # 发行版基线覆盖：麒麟 V10 系统源锁版本（qemu 4.1/libvirt 6.2/ovs 2.12/virt-install 2.2），
    # 均低于官方统一门槛且无法升级（无更高版本包），原逻辑会导致关键中止、麒麟永远装不上。
    # 以麒麟系统源可达基线覆盖阈值（关键转健康/警告），其余发行版仍用 versions.conf / 默认统一门槛。
    if [ -f /etc/os-release ]; then
        local _os_id _os_vid
        # os-release 值可能带引号（如 ID="kylin"、VERSION_ID="V10"），必须剥掉引号否则 case 匹配失败
        _os_id=$(sed -n 's/^ID=//p' /etc/os-release 2>/dev/null | tr -d '"')
        _os_vid=$(sed -n 's/^VERSION_ID=//p' /etc/os-release 2>/dev/null | tr -d '"')
        case "$_os_id" in
            kylin|neokylin)
                case "$_os_vid" in
                    V10*|v10*|10*)
                        info "检测到麒麟 V10（系统源锁版本），应用麒麟基线版本阈值（qemu 4.0/libvirt 6.0/ovs 2.10/virt-install 2.0）"
                        min_qemu="4.0"          rec_qemu="4.1"
                        min_qemuimg="4.0"       rec_qemuimg="4.1"
                        min_libvirt="6.0"       rec_libvirt="6.2"
                        min_ovs="2.10"          rec_ovs="2.12"
                        min_virtinstall="2.0"   rec_virtinstall="2.2"
                        ;;
                esac
                ;;
        esac
    fi

    # ── 检测工具函数 ──
    version_lt() { [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" = "$1" ]; }

    # L2 批量缺失短路：一次循环用 command -v 统一探测所有检测命令，缺失命令不再逐个走 timeout 探针，
    # 避免串行检测在命令缺失/挂起时逼近 60×5s 上界。结果入 PRESENT_CMDS，cmd_version 优先查表、未覆盖再现场探测。
    declare -A PRESENT_CMDS=()
    local probe_c
    for probe_c in qemu-system-x86_64 qemu-system-aarch64 qemu-kvm qemu-img \
        libvirtd virsh ovs-vsctl dnsmasq virt-install virt-customize guestfish \
        growpart ntfsresize tcpdump tc firewall-cmd ufw genisoimage xorriso mkisofs; do
        if command -v "$probe_c" >/dev/null 2>&1; then
            PRESENT_CMDS["$probe_c"]=1
        fi
    done

    # cmd_version 执行命令并提取首个语义化版本 token；命令不可用/超时/解析失败输出空
    cmd_version() {
        local out
        if [ -z "${PRESENT_CMDS[$1]:-}" ]; then
            command -v "$1" >/dev/null 2>&1 || return 0
        fi
        if command -v timeout >/dev/null 2>&1; then
            out=$(timeout 5s "$@" 2>&1 || true)
        else
            out=$("$@" 2>&1 || true)
        fi
        grep -oE '[0-9]+(\.[0-9]+){1,2}' <<< "$out" | head -n1
    }

    # pkg_hint 生成安装/升级命令提示：dnf/yum 系自动映射为 RPM 包名（复用 RPM_PKG_MAP，§5.11.2 包名映射）
    pkg_hint() {
        local pkg="$1"
        if [ "$PKG_MGR" != "apt" ] && [ -n "${RPM_PKG_MAP[$pkg]+x}" ] && [ -n "${RPM_PKG_MAP[$pkg]}" ]; then
            pkg="${RPM_PKG_MAP[$pkg]}"
        fi
        echo "sudo ${PKG_MGR} install -y ${pkg}"
    }

    # report_comp 单组件版本比对并输出报告行
    # $1=组件名 $2=当前版本(空=未安装) $3=最低 $4=推荐 $5=可选(info级) $6=升级/安装命令提示
    report_comp() {
        local comp="$1" cur="$2" min="$3" rec="$4" optional="$5" hint="$6"
        COMP_VER_TOTAL=$((COMP_VER_TOTAL + 1))
        if [ -z "$cur" ]; then
            if [ "$optional" = "1" ]; then
                info "  ${comp}:（缺失，可选）— 不影响核心功能"
                return
            fi
            COMP_VER_WARN=$((COMP_VER_WARN + 1))
            warnings+=("${comp} 未安装（最低要求 ${min}）")
            warn "  ${comp}:（未安装）— 最低要求 ${min}，请安装后重试或接受功能降级（${hint}）"
            return
        fi
        if [ "$min" != "any" ] && version_lt "$cur" "$min"; then
            COMP_VER_CRIT=$((COMP_VER_CRIT + 1))
            criticals+=("${comp} ${cur} < 最低要求 ${min}（${hint}）")
            warn "  ${comp}: ${cur} < ${min}（关键不满足，${hint}）"
            return
        fi
        if [ -n "$rec" ] && [ "$rec" != "any" ] && version_lt "$cur" "$rec"; then
            COMP_VER_WARN=$((COMP_VER_WARN + 1))
            warnings+=("${comp} ${cur} 可运行，但推荐 >= ${rec}")
            info "  ${comp}: ${cur}（推荐 ≥ ${rec}）"
            return
        fi
        COMP_VER_HEALTHY=$((COMP_VER_HEALTHY + 1))
        success "  ${comp}: ${cur}（健康）"
    }

    # ── 逐项检测（§5.11.2 清单，19 key / 每架构 18 项） ──

    # glibc（必装且永不缺失，仅记录）
    local glibc_ver min_glibc
    glibc_ver=$(ldd --version 2>&1 | sed -n '1 s/.* //p') || true
    if [ -z "$glibc_ver" ] || ! echo "$glibc_ver" | grep -qE '^[0-9]+\.[0-9]+'; then
        glibc_ver=$(getconf GNU_LIBC_VERSION 2>/dev/null | awk '{print $2}' || echo "")
    fi
    if [ "$ARCH" = "aarch64" ]; then min_glibc="$min_glibc_arm64"; else min_glibc="$min_glibc_amd64"; fi
    COMP_VER_TOTAL=$((COMP_VER_TOTAL + 1))
    COMP_VER_HEALTHY=$((COMP_VER_HEALTHY + 1))
    info "  glibc: ${glibc_ver:-未知}（最低要求 ${min_glibc}，与二进制档位匹配见选优步骤）"

    # qemu-kvm
    local qemu_cmd
    if [ "$ARCH" = "aarch64" ]; then
        qemu_cmd="qemu-system-aarch64"
    elif command -v qemu-system-x86_64 >/dev/null 2>&1; then
        qemu_cmd="qemu-system-x86_64"
    else
        qemu_cmd="qemu-kvm"
    fi
    # 安装提示用真实包名（避免提示不存在的 qemu-system-x86_64）：apt 按架构、RPM 用 qemu-kvm
    local qemu_hint
    if [ "$PKG_MGR" = "apt" ]; then
        if [ "$ARCH" = "aarch64" ]; then
            qemu_hint="sudo apt install -y qemu-system-arm"
        else
            qemu_hint="sudo apt install -y qemu-system-x86"
        fi
    else
        qemu_hint=$(pkg_hint "qemu-kvm")
    fi
    report_comp "qemu-kvm" "$(cmd_version "$qemu_cmd" --version)" "$min_qemu" "$rec_qemu" 0 "$qemu_hint"

    report_comp "qemu-img" "$(cmd_version qemu-img --version)" "$min_qemuimg" "$rec_qemuimg" 0 "$(pkg_hint "qemu-utils")"

    # libvirt（libvirtd 优先，回退 virsh）
    local libvirt_ver
    libvirt_ver=$(cmd_version libvirtd --version)
    [ -z "$libvirt_ver" ] && libvirt_ver=$(cmd_version virsh --version)
    report_comp "libvirt" "$libvirt_ver" "$min_libvirt" "$rec_libvirt" 0 "$(pkg_hint "libvirt-daemon-system")"

    report_comp "openvswitch" "$(cmd_version ovs-vsctl --version)" "$min_ovs" "$rec_ovs" 0 "$(pkg_hint "openvswitch-switch")"

    report_comp "dnsmasq" "$(cmd_version dnsmasq --version)" "$min_dnsmasq" "$rec_dnsmasq" 0 "$(pkg_hint "dnsmasq-base")"

    # firewalld（RPM 系后端；命令不存在则视为不适用，不误报 critical）
    if command -v firewall-cmd >/dev/null 2>&1; then
        report_comp "firewalld" "$(cmd_version firewall-cmd --version)" "$min_firewalld" "$rec_firewalld" 0 "$(pkg_hint "firewalld")"
    else
        COMP_VER_TOTAL=$((COMP_VER_TOTAL + 1))
        info "  firewalld:（未安装，当前后端不使用 RPM 系 firewalld）"
    fi

    # ufw（Debian 系后端；命令不存在则视为不适用）
    if command -v ufw >/dev/null 2>&1; then
        report_comp "ufw" "$(cmd_version ufw --version)" "$min_ufw" "$min_ufw" 0 "$(pkg_hint "ufw")"
    else
        COMP_VER_TOTAL=$((COMP_VER_TOTAL + 1))
        info "  ufw:（未安装，当前后端不使用 Debian 系 ufw）"
    fi

    report_comp "virt-install" "$(cmd_version virt-install --version)" "$min_virtinstall" "$rec_virtinstall" 0 "$(pkg_hint "virtinst")"

    report_comp "virt-customize" "$(cmd_version virt-customize --version)" "$min_virtcust" "$rec_virtcust" 0 "$(pkg_hint "libguestfs-tools")"

    report_comp "guestfish" "$(cmd_version guestfish --version)" "$min_guestfish" "$rec_guestfish" 0 "$(pkg_hint "libguestfs-tools")"

    # genisoimage / xorriso / mkisofs（任一可用即可）
    COMP_VER_TOTAL=$((COMP_VER_TOTAL + 1))
    local iso_tool iso_ver
    iso_tool=""
    iso_ver=""
    for t in genisoimage xorriso mkisofs; do
        if command -v "$t" >/dev/null 2>&1; then
            iso_tool="$t"
            iso_ver=$(cmd_version "$t" --version)
            break
        fi
    done
    if [ -n "$iso_tool" ]; then
        COMP_VER_HEALTHY=$((COMP_VER_HEALTHY + 1))
        success "  genisoimage/xorriso/mkisofs: ${iso_tool} ${iso_ver:-可用}（任一可用即可）"
    else
        COMP_VER_WARN=$((COMP_VER_WARN + 1))
        warnings+=("genisoimage/xorriso/mkisofs 均未安装（Windows ConfigDrive ISO 生成不可用）")
        warn "  genisoimage/xorriso/mkisofs:（均未安装）— Windows ConfigDrive ISO 生成将不可用（$(pkg_hint "xorriso")）"
    fi

    # growpart 二进制不支持 --version，改用包版本探测（云上游包命名各异：cloud-utils-growpart / cloud-utils / cloud-guest-utils）
    local grow_ver grow_pkg
    grow_ver=$(cmd_version growpart --version)
    if [ -z "$grow_ver" ] && command -v growpart >/dev/null 2>&1; then
        grow_pkg=$(rpm -qf "$(command -v growpart)" 2>/dev/null | head -n1 || true)
        [ -n "$grow_pkg" ] && grow_ver=$(rpm -q --qf '%{VERSION}' "$grow_pkg" 2>/dev/null | head -n1)
    fi
    report_comp "growpart" "$grow_ver" "$min_growpart" "$min_growpart" 0 "$(pkg_hint "cloud-guest-utils")"

    report_comp "ntfsresize" "$(cmd_version ntfsresize --version)" "$min_ntfsresize" "$min_ntfsresize" 0 "$(pkg_hint "ntfs-3g")"

    # edk2 固件（架构专属：x86_64 查 OVMF，aarch64 查 AAVMF/edk2）
    # 路径覆盖 Debian/Ubuntu(/usr/share/OVMF) 与 openEuler/RHEL9+(/usr/share/edk2/ovmf) 两种布局，
    # 避免已安装 edk2-ovmf 却误报缺失。
    COMP_VER_TOTAL=$((COMP_VER_TOTAL + 1))
    if [ "$ARCH" = "x86_64" ]; then
        local ovmf=""
        for f in /usr/share/OVMF/OVMF_CODE_4M.fd /usr/share/OVMF/OVMF_CODE.fd /usr/share/edk2/ovmf/OVMF_CODE.fd; do
            [ -f "$f" ] && ovmf="$f" && break
        done
        if [ -n "$ovmf" ]; then
            COMP_VER_HEALTHY=$((COMP_VER_HEALTHY + 1))
            success "  edk2-ovmf: 已安装（${ovmf}）"
        else
            COMP_VER_WARN=$((COMP_VER_WARN + 1))
            warnings+=("edk2-ovmf 缺失（UEFI 引导类型 VM 创建不可用）")
            warn "  edk2-ovmf:（缺失）— UEFI 引导类型 VM 创建不可用（$(pkg_hint "edk2-ovmf")）"
        fi
    else
        local aavmf=""
        for f in /usr/share/AAVMF/AAVMF_CODE.fd /usr/share/edk2/aarch64/AAVMF_CODE.fd; do
            [ -f "$f" ] && aavmf="$f" && break
        done
        if [ -n "$aavmf" ]; then
            COMP_VER_HEALTHY=$((COMP_VER_HEALTHY + 1))
            success "  edk2-aarch64: 已安装（${aavmf}）"
        else
            COMP_VER_WARN=$((COMP_VER_WARN + 1))
            warnings+=("edk2-aarch64 缺失（UEFI 引导类型 VM 创建不可用）")
            warn "  edk2-aarch64:（缺失）— UEFI 引导类型 VM 创建不可用（$(pkg_hint "edk2-aarch64")）"
        fi
    fi

    report_comp "tcpdump" "$(cmd_version tcpdump --version)" "$min_tcpdump" "$rec_tcpdump" 0 "$(pkg_hint "tcpdump")"

    report_comp "tc" "$(cmd_version tc -V)" "$min_tc" "$rec_tc" 0 "$(pkg_hint "iproute2")"

    # kvm_stat（可选辅助指标，缺失仅提示）
    local kvm_stat_path=""
    if kvm_stat_path=$(find_kvm_stat_binary); then
        COMP_VER_TOTAL=$((COMP_VER_TOTAL + 1))
        COMP_VER_HEALTHY=$((COMP_VER_HEALTHY + 1))
        success "  kvm_stat: ${kvm_stat_path##*/}（可用，热迁移辅助指标正常）"
    else
        COMP_VER_TOTAL=$((COMP_VER_TOTAL + 1))
        info "  kvm_stat:（缺失，可选）— 热迁移辅助指标不可用，不影响核心功能"
    fi

    # ── 汇总输出（v0.8 #K 风格） ──
    COMP_VER_WARN=${#warnings[@]}
    COMP_VER_CRIT=${#criticals[@]}
    echo ""
    info "==================== 组件版本检测报告 ===================="
    info "  总计: ${COMP_VER_TOTAL} 项，健康: ${COMP_VER_HEALTHY} 项"
    if [ ${#criticals[@]} -gt 0 ]; then
        error "  关键不满足 (${#criticals[@]} 项):"
        local c
        for c in "${criticals[@]}"; do error "    - $c"; done
        error "========================================================="
        if [ "$SKIP_VERSION_CHECK" = "1" ]; then
            warn "已指定 --skip-version-check，跳过中止继续安装（不推荐）"
        else
            error "检测到 ${#criticals[@]} 个关键组件版本不满足最低要求，安装中止。"
            error "请按上述升级命令升级后重试，或使用 --skip-version-check 跳过（不推荐）。"
            return 1
        fi
    fi
    if [ ${#warnings[@]} -gt 0 ]; then
        warn "  警告 (${#warnings[@]} 项):"
        local w
        for w in "${warnings[@]}"; do warn "    - $w"; done
        warn "========================================================="
        if [ "${CI:-}" != "1" ] && [ -t 0 ] && [ "$SKIP_VERSION_CHECK" != "1" ]; then
            read_tty -rp "检测到 ${#warnings[@]} 个组件版本偏低，功能可能受限。是否继续安装? [Y/n]: " ans
            if [[ "${ans:-Y}" =~ ^[Nn]$ ]]; then
                info "已取消安装，请升级组件后重试"
                exit 0
            fi
        fi
    fi
    success "========================================================="
}

# select_binary_tier 选优二进制档位（§4.3/§5.8，#M 非交互覆盖 + 冒烟测试）
# 必须放在 get_release 之后：需确认发布包内实际存在哪些档位。
select_binary_tier() {
    local glibc_ver
    glibc_ver=$(ldd --version 2>&1 | sed -n '1 s/.* //p') || true
    if [ -z "$glibc_ver" ] || ! echo "$glibc_ver" | grep -qE '^[0-9]+\.[0-9]+$'; then
        glibc_ver=$(getconf GNU_LIBC_VERSION 2>/dev/null | awk '{print $2}' || echo "0")
    fi
    info "检测到宿主机 GLIBC 版本: ${glibc_ver}"
    DETECTED_GLIBC_VER="$glibc_ver"

    local avx2_supported=true
    AVX2_FLAG=""
    # AVX2 仅 x86_64 存在；aarch64 恒不涉及（避免报告误报「AVX2 支持」）
    if [ "$ARCH" = "x86_64" ] && grep -q 'avx2' /proc/cpuinfo 2>/dev/null; then
        AVX2_FLAG="1"
    elif [ "$ARCH" = "x86_64" ]; then
        avx2_supported=false
    fi

    # 发行包 native 需求版本（#G：供选优与 print_install_report glibc 升级提示复用；
    # 在早期 return 前读取，update 模式也能拿到）
    NATIVE_GLIBC_REQUIRED=""
    if [ -f "${RELEASE_SOURCE_DIR}/native-glibc.txt" ]; then
        NATIVE_GLIBC_REQUIRED=$(tr -d '[:space:]' < "${RELEASE_SOURCE_DIR}/native-glibc.txt")
        if ! echo "$NATIVE_GLIBC_REQUIRED" | grep -qE '^[0-9]+\.[0-9]+(\.[0-9]+)?$'; then
            NATIVE_GLIBC_REQUIRED=""
        fi
    fi

    # native 档可行判定（§2.5 评审）：在持久化/update 档位分支提前计算，
    # 供 print_install_report 输出「当前 glibc 已满足 native，可考虑重装启用」提示。
    NATIVE_FEASIBLE=""
    if [ -f "${RELEASE_SOURCE_DIR}/kvm-console-native" ] && [ -n "$NATIVE_GLIBC_REQUIRED" ] \
        && [ "$(printf '%s\n%s\n' "$NATIVE_GLIBC_REQUIRED" "$glibc_ver" | sort -V | tail -n 1)" = "$glibc_ver" ] \
        && { [ "$ARCH" != "x86_64" ] || [ "$avx2_supported" = true ]; }; then
        NATIVE_FEASIBLE="1"
    fi

    # H1 评审：高兼容档版本从发行包动态发现（build.sh --high-compat-glibc 可为任意值），
    # 无该档文件时 HIGH_COMPAT_VER 为空 → 推荐逻辑与落位逻辑均自然回落默认档。
    HIGH_COMPAT_VER=""
    if [ -d "$RELEASE_SOURCE_DIR" ]; then
        for f in "${RELEASE_SOURCE_DIR}"/kvm-console-compat-*; do
            [ -e "$f" ] || continue
            local ver
            ver="${f##*kvm-console-compat-}"
            if echo "$ver" | grep -qE '^[0-9]+\.[0-9]+(\.[0-9]+)?$'; then
                if [ -z "$HIGH_COMPAT_VER" ] \
                    || [ "$(printf '%s\n%s\n' "$HIGH_COMPAT_VER" "$ver" | sort -V | tail -n 1)" = "$ver" ]; then
                    HIGH_COMPAT_VER="$ver"
                fi
            fi
        done
    fi
    [ -n "$HIGH_COMPAT_VER" ] && info "检测到高兼容档: GLIBC ${HIGH_COMPAT_VER}（kvm-console-compat-${HIGH_COMPAT_VER}）"

    # 复用 .env 中的 KVM_BINARY_TIER（update 场景）：glibc 未变化时直接采用（#M 白名单校验）
    # update 模式不得重复弹交互（§5.8）：无持久值时静默采用默认档，不询问
    local persisted
    persisted=$(env_get "KVM_BINARY_TIER")
    if [ -n "$persisted" ]; then
        case "$persisted" in
            compat|native)
                info "复用上次选择 KVM_BINARY_TIER=${persisted}"
                KVM_BINARY_TIER="$persisted"
                return 0
                ;;
            compat-*)
                if [ -n "$HIGH_COMPAT_VER" ] && [ "$persisted" = "compat-${HIGH_COMPAT_VER}" ]; then
                    info "复用上次选择 KVM_BINARY_TIER=${persisted}"
                    KVM_BINARY_TIER="$persisted"
                    return 0
                fi
                warn "KVM_BINARY_TIER=${persisted} 与本包高兼容档 ${HIGH_COMPAT_VER:-无} 不匹配，回退重新选优"
                ;;
            *)
                warn "KVM_BINARY_TIER 非法值: $persisted，回退重新选优"
                ;;
        esac
    fi
    if [ "$MODE" = "update" ]; then
        info "update 模式无持久化档位，采用默认兼容档（不弹交互）"
        KVM_BINARY_TIER="compat"
        return 0
    fi

    # 选优规则（§4.3 步骤 3-5）
    local recommended="compat"
    if [ "$NATIVE_FEASIBLE" = "1" ]; then
        recommended="native"
    elif [ -n "$HIGH_COMPAT_VER" ] \
        && [ "$(printf '%s\n%s\n' "$HIGH_COMPAT_VER" "$glibc_ver" | sort -V | tail -n 1)" = "$glibc_ver" ]; then
        recommended="compat-${HIGH_COMPAT_VER}"
    fi

    # 非交互 / CI 模式自动采用推荐值
    if [ "${CI:-}" = "1" ] || [ ! -t 0 ]; then
        info "非交互模式，自动采用推荐档位: ${recommended}"
        KVM_BINARY_TIER="$recommended"
        return 0
    fi

    read_tty -rp "选择二进制档位 [默认 ${recommended}] (compat/native/compat-${HIGH_COMPAT_VER:-...}): " ans
    case "${ans:-$recommended}" in
        compat|native) KVM_BINARY_TIER="${ans:-$recommended}" ;;
        compat-*)
            if [ -n "$HIGH_COMPAT_VER" ] && [ "$ans" = "compat-${HIGH_COMPAT_VER}" ]; then
                KVM_BINARY_TIER="$ans"
            else
                warn "高兼容档版本应为 ${HIGH_COMPAT_VER:-无}，采用推荐档: ${recommended}"
                KVM_BINARY_TIER="$recommended"
            fi
            ;;
        *) warn "无效档位输入，采用推荐档: ${recommended}"; KVM_BINARY_TIER="$recommended" ;;
    esac
    info "二进制档位: ${KVM_BINARY_TIER}"
}

# select_binary_smoke_test 切换前对目标二进制做运行态验证（§4.3：#A 冒烟测试）
# 失败则保留原主程序并告警，避免 mv 后才发现新档不可运行。
# M8.3/P0-3：除 --version 外追加 --smoke-selfcheck（SQLite AutoMigrate 空结构体 + libvirt 空连），
# 验证 glibc 2.17 等低版本环境的真实运行面（CGO 符号可解析），防止「理论兼容 ≠ 实测通过」。
select_binary_smoke_test() {
    local target_bin="$1"
    if [ ! -x "$target_bin" ]; then
        warn "目标二进制不存在或不可执行: $target_bin，保留原主程序"
        return 1
    fi
    if ! "$target_bin" --version >/dev/null 2>&1; then
        warn "目标二进制冒烟测试失败（--version 无法运行）: $target_bin，保留原主程序"
        return 1
    fi
    if ! "$target_bin" --smoke-selfcheck >/dev/null 2>&1; then
        warn "目标二进制冒烟测试失败（--smoke-selfcheck 无法运行，可能缺少 GLIBC 符号）: $target_bin，保留原主程序"
        return 1
    fi
    return 0
}

# §14.5 候选④：minisign 离线签名验证（供应链防篡改，M8.7 SHA256 的增强）。
# SHA256 仅防传输损坏/偶发篡改；minisign 非对称签名可防有动机的替换攻击（攻击者无法重签）。
# 验证链：发行包旁 .minisig 签名文件 + 内嵌公钥 MINISIGN_PUBLIC_KEY（单一来源，不做文件探测），解压前验证。
# 降级策略（不阻断安装）：无 minisign 命令 / 无签名文件 / 无内嵌公钥 → 仅 SHA256 校验（已有）。
# 任一环节真实存在但校验失败 → exit 1 中止（有签名就该验证，不可静默放行）。
verify_minisign_signature() {
    local tarball_path="$1"
    local sig_file="${tarball_path}.minisig"
    local pub_file=""
    local tmp_key=""      # 内嵌公钥的临时文件（mktemp），仅此文件在函数返回时清理
    trap 'rm -f "$tmp_key"' RETURN

    # 无 minisign 命令 → 降级（提示，不阻断）
    if ! command -v minisign >/dev/null 2>&1; then
        info "未安装 minisign 命令，跳过签名验证（仅 SHA256 校验）。发行方签名机制见 docs/dependencies.md"
        return 0
    fi

    # 签名文件不存在 → 降级（提示）
    if [ ! -f "$sig_file" ]; then
        info "未找到 minisign 签名文件（${sig_file}），跳过签名验证（仅 SHA256 校验）"
        return 0
    fi

    # 公钥仅取内嵌值（与官方一致，单一来源）：写入临时文件供 minisign -V -p 使用，不探测任何公钥文件
    if [ -z "$MINISIGN_PUBLIC_KEY" ]; then
        warn "发行包带 minisign 签名但未配置内嵌公钥（MINISIGN_PUBLIC_KEY 为空），签名验证跳过（仅 SHA256 校验）"
        return 0
    fi
    tmp_key=$(mktemp)
    printf '%s\n' "$MINISIGN_PUBLIC_KEY" > "$tmp_key"
    pub_file="$tmp_key"

    if minisign -V -m "$tarball_path" -p "$pub_file" -x "$sig_file" >/dev/null 2>&1; then
        info "发行包 minisign 签名验证通过"
    else
        error "发行包 minisign 签名验证失败（可能被篡改或公钥不匹配），已中止安装"
        exit 1
    fi
}

extract_tarball() {
    local tarball_path="$1"

    # M8.7/P1-7 包校验：若发行包旁存在 .tar.gz.sha256，解压前校验完整性（防下载损坏/篡改）
    local expected_sha="" actual_sha=""
    expected_sha=$(awk '{print $1}' "${tarball_path}.sha256" 2>/dev/null) || true
    if [ -n "$expected_sha" ]; then
        actual_sha=$(sha256sum "$tarball_path" 2>/dev/null | awk '{print $1}') || true
        if [ -n "$actual_sha" ] && [ "$actual_sha" != "$expected_sha" ]; then
            error "发行包 SHA256 校验失败（期望 ${expected_sha}，实际 ${actual_sha}），已中止安装"
            exit 1
        fi
        info "发行包 SHA256 校验通过"
    fi

    # §14.5 候选④：minisign 离线签名验证（供应链防篡改）
    # 签名文件来自下载分支（.minisig 拉取）或本地安装（包旁同名文件）；公钥来自内嵌/INSTALL_DIR/发行包同目录
    verify_minisign_signature "$tarball_path"

    info "正在解压发行包: $tarball_path"
    TMP_RELEASE_DIR=$(mktemp -d)
    tar -xzf "$tarball_path" -C "$TMP_RELEASE_DIR"

    local found_bin
    found_bin=$(find "$TMP_RELEASE_DIR" -maxdepth 3 -name "kvm-console" -type f -perm /111 2>/dev/null | sed -n '1p') || true
    if [ -z "$found_bin" ]; then
        error "发行包中未找到 kvm-console 可执行文件"
        exit 1
    fi
    RELEASE_SOURCE_DIR=$(dirname "$found_bin")
    if [ ! -d "${RELEASE_SOURCE_DIR}/web-dist" ]; then
        error "发行包中未找到 web-dist 前端文件"
        exit 1
    fi
    success "发行包解压完成"
}

# backup_previous_release 在 update 覆盖前备份当前运行版本（M8.6/P1-6 发行版回滚）。
# 备份存放于 ${INSTALL_DIR}/.release_backup/{NN}/，保留最近 3 份，最早滚动删除。
# 仅备份可执行二进制与前端静态文件（数据/配置不动，回滚不影响数据库）。
backup_previous_release() {
    if [ ! -f "${INSTALL_DIR}/kvm-console" ] && [ ! -d "${INSTALL_DIR}/web-dist" ]; then
        info "未检测到已安装版本，跳过备份"
        return 0
    fi

    local backup_root="${INSTALL_DIR}/.release_backup"
    mkdir -p "$backup_root"
    chmod 700 "$backup_root"

    # 编号槽位：01/02/03 循环使用（保留最近 3 份）
    local newest slot
    newest=$(ls -1 "$backup_root" 2>/dev/null | sort | tail -n 1 | sed 's/^0*//' || true)
    newest=${newest:-0}
    slot=$(( (newest % 3) + 1 ))
    local target="${backup_root}/$(printf '%02d' "$slot")"

    rm -rf "$target"
    mkdir -p "$target"
    if [ -f "${INSTALL_DIR}/kvm-console" ]; then
        cp -a "${INSTALL_DIR}/kvm-console" "$target/kvm-console"
    fi
    if [ -f "${INSTALL_DIR}/kvm-console-native" ]; then
        cp -a "${INSTALL_DIR}/kvm-console-native" "$target/kvm-console-native"
    fi
    if [ -n "$HIGH_COMPAT_VER" ] && [ -f "${INSTALL_DIR}/kvm-console-compat-${HIGH_COMPAT_VER}" ]; then
        cp -a "${INSTALL_DIR}/kvm-console-compat-${HIGH_COMPAT_VER}" "$target/kvm-console-compat-${HIGH_COMPAT_VER}"
    fi
    if [ -d "${INSTALL_DIR}/web-dist" ]; then
        cp -a "${INSTALL_DIR}/web-dist" "$target/web-dist"
    fi
    echo "backup_version=\"$(cat "${INSTALL_DIR}/.version" 2>/dev/null || echo unknown)\"" > "$target/meta"
    success "已备份上一版本到 ${target}（保留最近 3 份）"
}

get_release() {
    local script_dir
    script_dir="$(cd "$(dirname "$0")" && pwd)"
    if [ -f "${script_dir}/kvm-console" ] && [ -d "${script_dir}/web-dist" ]; then
        info "检测到本地发行目录，使用本地文件"
        RELEASE_SOURCE_DIR="$script_dir"
        return
    fi

    # 按架构确定安装包名称与下载链接
    local local_tarball_name download_url
    if [ "$ARCH" = "x86_64" ]; then
        local_tarball_name="kvm-console-linux-amd64.tar.gz"
        download_url="$DOWNLOAD_URL_AMD64"
    elif [ "$ARCH" = "aarch64" ]; then
        local_tarball_name="kvm-console-linux-arm64.tar.gz"
        download_url="$DOWNLOAD_URL_ARM64"
    fi

    # 优先使用当前目录已有的本地发行包
    local local_tarball=""
    if [ -f "$(pwd)/${local_tarball_name}" ]; then
        local_tarball="$(pwd)/${local_tarball_name}"
        local use_local="Y"
        if [ "${CI:-}" != "1" ] && [ -t 0 ]; then
            read_tty -rp "检测到本地发行包 ${local_tarball}，是否使用? [Y/n]: " use_local
            use_local=${use_local:-Y}
        fi
        if [[ "$use_local" =~ ^[Yy]$ ]]; then
            # M8.7/P1-7：本地包旁若带 .tar.gz.sha256 校验文件，extract_tarball 会一并校验
            state_set "release_sha256" "$(sha256sum "$local_tarball" 2>/dev/null | awk '{print $1}')"
            extract_tarball "$local_tarball"
            return
        fi
    fi

    # 未检测到本地安装包，从官方下载源自动下载
    info "未检测到本地安装包，从官方下载源获取 ${local_tarball_name} (${ARCH})..."
    TMP_RELEASE_DIR=$(mktemp -d)
    local tarball_path="${TMP_RELEASE_DIR}/${local_tarball_name}"
    if command -v wget >/dev/null 2>&1; then
        if ! wget -O "$tarball_path" "$download_url"; then
            error "下载安装包失败，请检查网络或手动下载后放置于当前目录: ${local_tarball_name}"
            exit 1
        fi
        # M8.7/P1-7：尝试同时下载 .sha256 校验文件（不存在则跳过，extract_tarball 内仅在校验文件存在时校验）
        wget -q -O "${tarball_path}.sha256" "${download_url}.sha256" 2>/dev/null || rm -f "${tarball_path}.sha256"
        # §14.5 候选④：尝试下载 .minisig 签名（供 minisign 验证；不存在则跳过）；验签公钥用 install.sh 内嵌 MINISIGN_PUBLIC_KEY，不再下载/探测
        wget -q -O "${tarball_path}.minisig" "${download_url}.minisig" 2>/dev/null || rm -f "${tarball_path}.minisig"
    else
        if ! curl -fL --progress-bar -o "$tarball_path" "$download_url"; then
            error "下载安装包失败，请检查网络或手动下载后放置于当前目录: ${local_tarball_name}"
            exit 1
        fi
        curl -fsSL -o "${tarball_path}.sha256" "${download_url}.sha256" 2>/dev/null || rm -f "${tarball_path}.sha256"
        curl -fsSL -o "${tarball_path}.minisig" "${download_url}.minisig" 2>/dev/null || rm -f "${tarball_path}.minisig"
    fi
    extract_tarball "$tarball_path"
    state_set "release_sha256" "$(sha256sum "$tarball_path" 2>/dev/null | awk '{print $1}')"
}

install_files() {
    if [ "$MODE" = "update" ]; then
        info "停止 ${APP_NAME} 服务..."
        systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        # M8.6/P1-6：覆盖前备份上一版本（保留最近 3 份，供 qvmc-manage.sh rollback 回滚）
        backup_previous_release
        # 更新前先删除旧版前端所有资源，避免残留旧版静态文件
        info "清理旧版前端资源..."
        rm -rf "${INSTALL_DIR}/web-dist"
    fi

    mkdir -p "$INSTALL_DIR/data"
    info "安装后端程序..."
    cp -f "${RELEASE_SOURCE_DIR}/kvm-console" "${INSTALL_DIR}/kvm-console"
    chmod +x "${INSTALL_DIR}/kvm-console"

    # 发行包可能包含的附加档位：原生版 / 高兼容档（§4.3）
    local has_native=false
    local has_compat_high=false
    if [ -f "${RELEASE_SOURCE_DIR}/kvm-console-native" ]; then
        has_native=true
        cp -f "${RELEASE_SOURCE_DIR}/kvm-console-native" "${INSTALL_DIR}/kvm-console-native"
        chmod +x "${INSTALL_DIR}/kvm-console-native"
        info "已部署宿主机原生版二进制 kvm-console-native"
    fi
    # H1 评审：高兼容档版本动态发现（select_binary_tier 已填充 HIGH_COMPAT_VER）
    if [ -n "$HIGH_COMPAT_VER" ] && [ -f "${RELEASE_SOURCE_DIR}/kvm-console-compat-${HIGH_COMPAT_VER}" ]; then
        has_compat_high=true
        cp -f "${RELEASE_SOURCE_DIR}/kvm-console-compat-${HIGH_COMPAT_VER}" "${INSTALL_DIR}/kvm-console-compat-${HIGH_COMPAT_VER}"
        chmod +x "${INSTALL_DIR}/kvm-console-compat-${HIGH_COMPAT_VER}"
        info "已部署 GLIBC ${HIGH_COMPAT_VER} 高兼容档 kvm-console-compat-${HIGH_COMPAT_VER}"
    fi

    # 按 select_binary_tier 选择的档位落位主程序（§4.3，含冒烟测试）
    local chosen="$KVM_BINARY_TIER"
    [ -n "$chosen" ] || chosen="compat"
    case "$chosen" in
        native)
            if [ "$has_native" = true ] && select_binary_smoke_test "${INSTALL_DIR}/kvm-console-native"; then
                info "切换宿主机原生版为主程序（兼容版保留为 kvm-console-compat）"
                mv -f "${INSTALL_DIR}/kvm-console" "${INSTALL_DIR}/kvm-console-compat"
                mv -f "${INSTALL_DIR}/kvm-console-native" "${INSTALL_DIR}/kvm-console"
                success "已切换为宿主机原生版"
            else
                warn "native 档不可用或冒烟测试失败，保留 zig 兼容版作为主程序"
                DEGRADED_NOTES="${DEGRADED_NOTES:+${DEGRADED_NOTES}；}native 档冒烟测试失败，保留 zig 兼容版"
            fi
            ;;
        compat-*)
            if [ "$has_compat_high" = true ] && [ -n "$HIGH_COMPAT_VER" ] \
                && select_binary_smoke_test "${INSTALL_DIR}/kvm-console-compat-${HIGH_COMPAT_VER}"; then
                info "切换 GLIBC ${HIGH_COMPAT_VER} 高兼容档为主程序（默认兼容版保留为 kvm-console-compat）"
                mv -f "${INSTALL_DIR}/kvm-console" "${INSTALL_DIR}/kvm-console-compat"
                mv -f "${INSTALL_DIR}/kvm-console-compat-${HIGH_COMPAT_VER}" "${INSTALL_DIR}/kvm-console"
                success "已切换为 GLIBC ${HIGH_COMPAT_VER} 高兼容档"
            else
                warn "compat-${HIGH_COMPAT_VER:-?} 档不可用或冒烟测试失败，保留 zig 兼容版作为主程序"
                DEGRADED_NOTES="${DEGRADED_NOTES:+${DEGRADED_NOTES}；}compat 高兼容档冒烟测试失败，保留 zig 兼容版"
            fi
            ;;
        *)
            # compat（默认档）：保持 kvm-console 为主程序
            info "使用 zig 兼容版作为主程序（GLIBC 上限兼容面最广）"
            ;;
    esac

    info "安装前端静态文件..."
    rm -rf "${INSTALL_DIR}/web-dist"
    cp -r "${RELEASE_SOURCE_DIR}/web-dist" "${INSTALL_DIR}/web-dist"

    # 记录本次部署版本（供 .release_backup/meta 与回滚提示使用）
    if [ -f "${RELEASE_SOURCE_DIR}/versions.conf" ]; then
        grep -m1 -E "^(APP_VERSION|VERSION)=" "${RELEASE_SOURCE_DIR}/versions.conf" 2>/dev/null | cut -d= -f2- > "${INSTALL_DIR}/.version" 2>/dev/null || true
    fi
    [ -s "${INSTALL_DIR}/.version" ] || echo "unknown" > "${INSTALL_DIR}/.version"
    chmod 600 "${INSTALL_DIR}/.version"

    success "程序文件已安装"
}

setup_service() {
    info "配置 systemd 服务..."
    cat >"$SERVICE_FILE" <<EOF
[Unit]
Description=${APP_NAME} 虚拟机管理平台
After=network-online.target libvirtd.service ${OVS_SERVICE_NAME}.service
Wants=network-online.target libvirtd.service ${OVS_SERVICE_NAME}.service

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${ENV_FILE}
# 强制 C.UTF-8，确保 virsh/qemu-img 等命令输出保持英文，避免国产系统 zh_CN 环境下解析失败
Environment="LANG=C.UTF-8"
Environment="LC_ALL=C.UTF-8"
ExecStart=${INSTALL_DIR}/kvm-console
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
StandardOutput=journal
StandardError=journal
SyslogIdentifier=kvm-console

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"
    success "systemd 服务已配置"
}

start_service() {
    info "启动 ${APP_NAME} 服务..."
    systemctl restart "$SERVICE_NAME"
    sleep 2
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        success "${APP_NAME} 服务启动成功"
    else
        error "服务启动失败，请查看日志: journalctl -u $SERVICE_NAME -f"
        exit 1
    fi
}

uninstall_app() {
    echo ""
    warn "卸载不会删除已有虚拟机磁盘、模板、libvirt 定义和用户存储镜像，除非你手动清理。"
    # 非交互 / CI 模式：要求环境变量 UNINSTALL_CONFIRM=UNINSTALL 作为二次确认，避免误触发
    if [ "${CI:-}" = "1" ] || [ ! -t 0 ]; then
        if [ "${UNINSTALL_CONFIRM:-}" != "UNINSTALL" ]; then
            error "非交互卸载需设置 UNINSTALL_CONFIRM=UNINSTALL 以确认"
            exit 1
        fi
        confirm="UNINSTALL"
    else
        read_tty -rp "确认卸载 ${APP_NAME}? 请输入 UNINSTALL 确认: " confirm
    fi
    if [ "$confirm" != "UNINSTALL" ]; then
        warn "已取消卸载"
        return
    fi

    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    rm -f "$SERVICE_FILE"

    local stop_ovs="Y"
    if [ "${CI:-}" != "1" ] && [ -t 0 ]; then
        read_tty -rp "是否同时停用 OVS DHCP 辅助服务? [Y/n]: " stop_ovs
        stop_ovs=${stop_ovs:-Y}
    fi
    if [[ "$stop_ovs" =~ ^[Yy]$ ]]; then
        systemctl disable --now "$OVS_DNSMASQ_UNIT" 2>/dev/null || true
        rm -f "$OVS_DNSMASQ_SERVICE_FILE"
    fi

    systemctl daemon-reload

    local purge="N"
    if [ "${CI:-}" != "1" ] && [ -t 0 ]; then
        read_tty -rp "是否删除安装目录 ${INSTALL_DIR}（包含数据库和配置）? [y/N]: " purge
        purge=${purge:-N}
    fi
    if [[ "$purge" =~ ^[Yy]$ ]]; then
        rm -rf "$INSTALL_DIR"
        success "安装目录已删除"
    else
        rm -f "${INSTALL_DIR}/kvm-console"
        rm -rf "${INSTALL_DIR}/web-dist"
        warn "已保留 ${INSTALL_DIR}/data 与 ${ENV_FILE}"
    fi

    # #S2：卸载回滚 firewalld 面板自建 zone/policy（firewalld 未运行也正常，读操作降级）
    cleanup_firewalld_zones

    success "${APP_NAME} 已卸载"
}

# cleanup_firewalld_zones 卸载时删除面板自建 qvm-host zone 与 qvm-host-forward policy（#S2）
cleanup_firewalld_zones() {
    if ! command -v firewall-cmd >/dev/null 2>&1; then
        return 0
    fi
    if systemctl is-active --quiet firewalld 2>/dev/null; then
        firewall-cmd --permanent --delete-policy qvm-host-forward >/dev/null 2>&1 || true
        firewall-cmd --permanent --delete-zone qvm-host >/dev/null 2>&1 || true
        # 还原 trusted zone 中的 VM 桥绑定（br-ovs/vpcsw* 与 docker0），避免残留面板副作用
        for iface in $(firewall-cmd --permanent --zone=trusted --list-interfaces 2>/dev/null); do
            case "$iface" in
                br-ovs|vpcsw*|docker0)
                    firewall-cmd --permanent --zone=trusted --remove-interface "$iface" >/dev/null 2>&1 || true
                    ;;
            esac
        done
        firewall-cmd --reload >/dev/null 2>&1 || true
    fi
    rm -f /etc/firewalld/zones/qvm-host.xml /etc/firewalld/zones/qvm-host.xml.bak \
          /etc/firewalld/zones/qvm-host-forward.xml 2>/dev/null || true
}

BOX_INNER_WIDTH=64

# 测算可视化宽度，剔除所有ANSI转义序列
get_visual_width() {
    local txt="$1"
    local stripped=$(sed -E $'s/\x1b\[[0-9;]*[mKHF]//g' <<<"$txt")
    echo -n "$stripped" | wc -L
}

# 纯文本补齐空格，右填充到BOX_INNER_WIDTH
pad_plain() {
    local raw="$1"
    local w=$(get_visual_width "$raw")
    local pad=$(( BOX_INNER_WIDTH - w ))
    (( pad < 0 )) && pad=0
    local space_str
    space_str=$(printf "%${pad}s" "")
    printf '%s%s' "$raw" "$space_str"
}

# 新增：文本居中函数，左右自动分配空格
center_text() {
    local raw="$1"
    local w=$(get_visual_width "$raw")
    local total_pad=$(( BOX_INNER_WIDTH - w ))
    (( total_pad < 0 )) && total_pad=0
    local left_pad=$(( total_pad / 2 ))
    local right_pad=$(( total_pad - left_pad ))
    # 左侧空格 + 文字 + 右侧空格
    printf "%${left_pad}s%s%${right_pad}s" "" "$raw" ""
}

show_info() {
    local host_ip
    host_ip=$(hostname -I 2>/dev/null | awk '{print $1}')
    host_ip=${host_ip:-localhost}

    # 拼接固定边框字符串，边框统一使用青色
    top_border="${CYAN}╔$(printf '═%.0s' $(seq 1 $BOX_INNER_WIDTH))╗${NC}"
    mid_border="${CYAN}╠$(printf '═%.0s' $(seq 1 $BOX_INNER_WIDTH))╣${NC}"
    bot_border="${CYAN}╚$(printf '═%.0s' $(seq 1 $BOX_INNER_WIDTH))╝${NC}"

    echo ""
    echo -e "$top_border"

    # 标题居中，标题文字保持青色
    if [ "$MODE" = "install" ]; then
        title_raw="${APP_NAME} 安装完成！"
    else
        title_raw="${APP_NAME} 更新完成！"
    fi
    title_filled=$(center_text "$title_raw")
    title_line="${CYAN}║${title_filled}║${NC}"
    echo -e "$title_line"

    echo -e "$mid_border"

    # ========== 信息区块：标签普通白色，后面路径/地址部分单独绿色 ==========
    # 访问地址行
    label1="  访问地址:"
    val1=" http://${host_ip}:${KVM_PORT}"
    plain1="${label1}${val1}"
    pad1=$(pad_plain "$plain1")
    # 截取填充后的空白后缀
    suffix1="${pad1#"$plain1"}"
    line_info1="${CYAN}║${NC}${label1}${GREEN}${val1}${NC}${suffix1}${CYAN}║${NC}"

    # 安装目录行
    label2="  安装目录:"
    val2=" ${INSTALL_DIR}"
    plain2="${label2}${val2}"
    pad2=$(pad_plain "$plain2")
    suffix2="${pad2#"$plain2"}"
    line_info2="${CYAN}║${NC}${label2}${GREEN}${val2}${NC}${suffix2}${CYAN}║${NC}"

    # 配置文件行
    label3="  配置文件:"
    val3=" ${ENV_FILE}"
    plain3="${label3}${val3}"
    pad3=$(pad_plain "$plain3")
    suffix3="${pad3#"$plain3"}"
    line_info3="${CYAN}║${NC}${label3}${GREEN}${val3}${NC}${suffix3}${CYAN}║${NC}"

    echo -e "$line_info1"
    echo -e "$line_info2"
    echo -e "$line_info3"

    # 安装模式额外输出默认账号
    if [ "$MODE" = "install" ]; then
        label4="  默认账号:"
        val4=" admin / admin123"
        plain4="${label4}${val4}"
        pad4=$(pad_plain "$plain4")
        suffix4="${pad4#"$plain4"}"
        line_info4="${CYAN}║${NC}${label4}${GREEN}${val4}${NC}${suffix4}${CYAN}║${NC}"
        echo -e "$line_info4"
    fi

    echo -e "$mid_border"

    # ========== 命令区块：整行普通白色原色，不施加绿色 ==========
    c_raw1="  查看状态: systemctl status $SERVICE_NAME"
    c_fill1=$(pad_plain "$c_raw1")
    cmd_line1="${CYAN}║${NC}${c_fill1}${CYAN}║${NC}"

    c_raw2="  查看日志: journalctl -u $SERVICE_NAME -f"
    c_fill2=$(pad_plain "$c_raw2")
    cmd_line2="${CYAN}║${NC}${c_fill2}${CYAN}║${NC}"

    c_raw3="  重启服务: systemctl restart $SERVICE_NAME"
    c_fill3=$(pad_plain "$c_raw3")
    cmd_line3="${CYAN}║${NC}${c_fill3}${CYAN}║${NC}"

    echo -e "$cmd_line1"
    echo -e "$cmd_line2"
    echo -e "$cmd_line3"

    echo -e "$bot_border"
    echo ""
}

# #L：预检（§5.8）。端口占用 / 多防火墙共存 / NM 环境记录。仅做告警与记录，不弹交互，install/update 通用。
precheck_domestic() {
    # 端口占用检查
    local used_by
    used_by=$(ss -ltn 2>/dev/null | awk -v p=":${KVM_PORT} " 'index($0, p) {print $4}' | sed 's/.*://' | head -n 1) || true
    if [ -n "$used_by" ]; then
        warn "端口 ${KVM_PORT} 已被占用（${used_by}），若面板服务冲突请更换端口"
    fi

    # 多防火墙共存提示（firewalld 后端时检查非 firewalld 管理规则）
    if [ "$FW_BACKEND" = "firewalld" ]; then
        local foreign
        foreign=$(iptables -L -n 2>/dev/null | grep -vE '^(Chain|target|$)' | head -n 3) || true
        if [ -n "$foreign" ]; then
            warn "检测到既有防火墙规则（非 firewalld 管理），面板仅管理自建 zone，不会清理第三方规则"
        fi
    fi

    # NM 环境记录
    if command -v nmcli >/dev/null 2>&1 && nmcli -t --fields RUNNING general 2>/dev/null | grep -q running; then
        info "检测到 NetworkManager 运行中（物理接口 connection.zone 将同步 trusted 绑定）"
    else
        info "未检测到 NetworkManager，跳过 connection.zone 同步"
    fi

    # CPU 厂商探测（P0-1 / M8.1，§5.8）：读 /proc/cpuinfo 判定国产厂商，写入 .env DOMESTIC_CPU_VENDOR
    DOMESTIC_CPU_VENDOR="Unknown"
    local vendor_id cpu_impl
    vendor_id=$(awk -F: '/^vendor_id/{print $2; exit}' /proc/cpuinfo 2>/dev/null | xargs)
    cpu_impl=$(awk -F: '/^CPU implementer/{print $2; exit}' /proc/cpuinfo 2>/dev/null | xargs)
    case "$vendor_id" in
        *GenuineIntel*) DOMESTIC_CPU_VENDOR="Intel" ;;
        *AuthenticAMD*) DOMESTIC_CPU_VENDOR="AMD" ;;
        *HygonGenuine*|*Hygon*) DOMESTIC_CPU_VENDOR="Hygon" ;;
        *CentaurHauls*|*Zhaoxin*) DOMESTIC_CPU_VENDOR="Zhaoxin" ;;
        *) case "$cpu_impl" in
               0x70|0x71) DOMESTIC_CPU_VENDOR="Phytium" ;;
               0x41) DOMESTIC_CPU_VENDOR="Kunpeng" ;;
           esac ;;
    esac
    info "CPU 厂商: ${DOMESTIC_CPU_VENDOR}（${vendor_id:-${cpu_impl:-无}}）"
    if [ "$DOMESTIC_CPU_VENDOR" = "Hygon" ]; then
        info "海光 CPU 提示: 如遇嵌套页表异常可加内核参数 kvm_amd.npt=0"
    elif [ "$DOMESTIC_CPU_VENDOR" = "Phytium" ] || [ "$DOMESTIC_CPU_VENDOR" = "Kunpeng" ]; then
        info "飞腾/鲲鹏 CPU 提示: 请确认 kvm 模块加载顺序（kvm → kvm_arm → hyp/vhe）以启用嵌套虚拟化"
    fi
    env_set "DOMESTIC_CPU_VENDOR" "$DOMESTIC_CPU_VENDOR"

    # kdump 建议（P2-8 / M8.8）：裸金属无 crashkernel → warn + 记录供 print_install_report 输出
    check_kdump_suggestion

    # 发行版支持等级（P3-11 / M8.11）：S=官方全量回归 / A=核心功能回归 / B=社区自测 / C=理论兼容
    # support_level=C 的发行版（如 CentOS 7）安装时 warn「理论兼容，生产请升级到认证基线」
    check_support_level
}

# ── P3-11：发行版支持等级检测（M8.11，§5.11.3/§5.8）──
# 匹配当前发行版到 compat-manifest.json os_compat 的 key，读取 versions.conf 中的
# SUPPORT_LEVEL_<key>（与 manifest 同源，纯 shell 解析无 python3 依赖）。
# support_level=C → warn「本发行版为理论兼容，生产请升级到认证基线」。
check_support_level() {
    local os_key="" id="" version_id="" os_like=""
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        id="${ID:-}"
        version_id="${VERSION_ID:-}"
        os_like="${ID_LIKE:-}"
    fi
    case "$id" in
        kylin|neokylin) os_key="kylin-v10-server" ;;
        openEuler|openeuler)
            case "$version_id" in
                20.*) os_key="openEuler-20.03" ;;
                22.*) os_key="openEuler-22.03" ;;
                24.*) os_key="openEuler-24.03" ;;
            esac ;;
        uos) os_key="uos-1060" ;;
        ubuntu) os_key="ubuntu-22.04" ;;
        debian) os_key="debian-12" ;;
        centos) os_key="centos-7" ;;
    esac
    [ -n "$os_key" ] || return 0

    local level=""
    level=$(grep -m1 -E "^SUPPORT_LEVEL_${os_key}=" "${RELEASE_SOURCE_DIR}/versions.conf" 2>/dev/null | cut -d= -f2- || true)
    [ -n "$level" ] || return 0

    case "$level" in
        C)
            warn "当前发行版（${PRETTY_NAME:-$id}）为理论兼容（support_level=C），生产环境请升级到认证基线（支持等级 S/A）"
            SUPPORT_LEVEL_C="1"
            ;;
        *)
            info "当前发行版支持等级: ${level}（${os_key}）"
            ;;
    esac
}

# ── P2-8：kdump 建议（M8.8，§5.8）──
# 裸金属（非虚拟化）且 /proc/cmdline 无 crashkernel 参数时，向安装报告追加 kdump 建议。
check_kdump_suggestion() {
    # systemd-detect-virt 不可用 / 判定为裸金属时视为裸金属（none）
    local virt
    if command -v systemd-detect-virt >/dev/null 2>&1; then
        virt=$(systemd-detect-virt 2>/dev/null || true)
    else
        virt="none"
    fi
    if [ -n "$virt" ] && [ "$virt" != "none" ]; then
        return 0
    fi
    if grep -q "crashkernel" /proc/cmdline 2>/dev/null; then
        return 0
    fi
    warn "建议启用 kdump：当前裸金属未配置 crashkernel（建议 crashkernel=2048M,high；虚拟化环境 512M）"
    KDUMP_SUGGESTED="1"
}

# ── P3-12：国内镜像源测速（M8.12，§5.8）──
# apt 系对清华/阿里/163 临时源、dnf 系对 yum.repos.d 同法做 curl 计时，取最快写入 .env
# DEPS_MIRROR=tsinghua|aliyun|163|system|offline；offline 时 check_and_install_deps 跳过
# apt/dnf install 仅扫缺包（专网环境提示从内网源手动安装）。
DEPS_MIRROR="system"
_MIRROR_BACKUP_DIR=""
test_mirror_speed() {
    # 已显式配置则跳过测速
    local configured
    configured=$(env_get "DEPS_MIRROR")
    if [ -n "$configured" ]; then
        DEPS_MIRROR="$configured"
        info "镜像源: 沿用配置 ${DEPS_MIRROR}"
        return 0
    fi

    local best="system" best_time=999999.0
    local name url t probe_url reachable=0

    # 候选镜像（URL 探活计时；cn 网络环境偏好国内源）
    local candidates=""
    if [ "$PKG_MGR" = "apt" ]; then
        candidates="tsinghua|https://mirrors.tuna.tsinghua.edu.cn/ubuntu aliyun|https://mirrors.aliyun.com/ubuntu 163|http://mirrors.163.com/ubuntu"
        probe_url="dists/"
    elif [ "$OS_ID" = "openeuler" ]; then
        # openEuler：镜像仓库结构为 {version}/{arch}/os、{version}/everything/{arch}、
        # {version}/EPOL/main/{arch}，与官方 repo.openeuler.org 一致。
        # 推荐南京大学源（mirrors.nju.edu.cn，linuxmirrors.cn 高优先级教育网镜像）：
        # 若 NJU 可达则直接选用，避免阿里云等商业源偶发限流/404（见 install-20260802 日志），
        # 不可达时退化为清华/阿里测速取最快。
        if curl -o /dev/null -s -m 4 "https://mirrors.nju.edu.cn/openeuler/" 2>/dev/null; then
            DEPS_MIRROR="nju"
            info "镜像源: openEuler 推荐南京大学源（mirrors.nju.edu.cn）"
            env_set "DEPS_MIRROR" "$DEPS_MIRROR"
            return 0
        fi
        candidates="tsinghua|https://mirrors.tuna.tsinghua.edu.cn/openeuler aliyun|https://mirrors.aliyun.com/openeuler"
        probe_url=""
    elif [ "$OS_ID" = "kylin" ] || [ "$OS_ID" = "neokylin" ]; then
        # 麒麟（服务器版）：基于 CentOS 8 系但使用自有 archive.kylinos.cn 源，无公开国内镜像；
        # 对 centos 镜像测速无意义，且 apply_rpm_mirror 写入 centos-vault 源会拉取 CentOS 包污染麒麟。
        # 直接走 system（官方源），依赖安装依赖 dnf 超时兜底（--setopt=timeout/minrate/retries）。
        DEPS_MIRROR="system"
        info "镜像源: 麒麟使用系统默认源（archive.kylinos.cn，无公开镜像可测速）"
        env_set "DEPS_MIRROR" "$DEPS_MIRROR"
        return 0
    else
        candidates="tsinghua|https://mirrors.tuna.tsinghua.edu.cn/centos aliyun|https://mirrors.aliyun.com/centos-vault 163|http://mirrors.163.com/centos"
        probe_url=""
    fi

    local c pair
    for pair in $candidates; do
        name="${pair%%|*}"
        url="${pair#*|}"
        t=$(curl -o /dev/null -s -m 3 -w '%{time_total}' "${url}/${probe_url}" 2>/dev/null || true)
        if [[ "$t" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
            reachable=1
            # 浮点比较：t 快于 best_time 则更新
            if [ "$(awk -v a="$t" -v b="$best_time" 'BEGIN{print (a<b)?1:0}')" = "1" ]; then
                best_time="$t"
                best="$name"
            fi
        fi
    done

    # 候选全部不可达 → offline（专网/断网场景）
    if [ "$reachable" -eq 0 ]; then
        DEPS_MIRROR="offline"
        info "镜像源: 未检测到可达源，进入 offline 模式（依赖仅扫描不自动安装）"
    else
        DEPS_MIRROR="$best"
        info "镜像源: 测速选最快 ${best}（${best_time}s）"
    fi
    env_set "DEPS_MIRROR" "$DEPS_MIRROR"
}

# ── 系统源备份与回滚（参考宝塔 Set_Repo_Url / Check_And_Fix_Debian_Ubuntu_Source） ──
# 备份 → 修改 → 验证 → 失败回滚；确保安装过程不会因源问题破坏系统。
MIRROR_BACKUP_DIR="${TMPDIR:-/tmp}/kvm_console_mirror_backup"
backup_system_sources() {
    # 清理 7 天前的旧备份（幂等，每次安装时执行一次）
    find "$MIRROR_BACKUP_DIR" -maxdepth 1 -type d -mtime +7 -exec rm -rf {} + 2>/dev/null || true
    _MIRROR_BACKUP_DIR="${MIRROR_BACKUP_DIR}/$(date +%s)"
    mkdir -p "$_MIRROR_BACKUP_DIR"
    if [ "$PKG_MGR" = "apt" ]; then
        # apt: 备份 sources.list + sources.list.d 下所有 kvm-console 相关文件
        for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.sources /etc/apt/sources.list.d/*.list; do
            if [ -f "$f" ]; then
                cp -a "$f" "$_MIRROR_BACKUP_DIR/" 2>/dev/null || true
            fi
        done
    else
        # RPM: 备份 yum.repos.d 下所有 kvm-console / kvmconsole / local-mirror 相关文件
        for f in /etc/yum.repos.d/*kvm* /etc/yum.repos.d/*KVM* /etc/yum.repos.d/*local-mirror*; do
            if [ -f "$f" ]; then
                cp -a "$f" "$_MIRROR_BACKUP_DIR/" 2>/dev/null || true
            fi
        done
    fi
    info "系统源已备份到 ${_MIRROR_BACKUP_DIR}"
}

restore_system_sources() {
    if [ -z "${_MIRROR_BACKUP_DIR:-}" ] || [ ! -d "$_MIRROR_BACKUP_DIR" ]; then
        warn "无可用备份，跳过回滚"
        return 1
    fi
    if [ "$PKG_MGR" = "apt" ]; then
        # 清理自动生成的文件，恢复备份
        rm -f /etc/apt/sources.list.d/kvm-console* 2>/dev/null || true
        for f in "$_MIRROR_BACKUP_DIR"/sources.list "$_MIRROR_BACKUP_DIR"/*.sources "$_MIRROR_BACKUP_DIR"/*.list; do
            if [ -f "$f" ]; then
                local dest
                dest="/etc/apt/sources.list.d/$(basename "$f")"
                if [ "$(basename "$f")" = "sources.list" ]; then
                    dest="/etc/apt/sources.list"
                fi
                cp -a "$f" "$dest" 2>/dev/null || true
            fi
        done
    else
        for f in "$_MIRROR_BACKUP_DIR"/*; do
            if [ -f "$f" ]; then
                cp -a "$f" "/etc/yum.repos.d/$(basename "$f")" 2>/dev/null || true
            fi
        done
    fi
    warn "系统源已回滚到备份"
}

apply_apt_mirror() {
    local mirror_name="$1"
    local codename
    # 优先 lsb_release，回退 /etc/os-release VERSION_CODENAME，最终报错
    codename=$(lsb_release -cs 2>/dev/null || true)
    if [ -z "$codename" ] && [ -f /etc/os-release ]; then
        codename=$(grep -m1 '^VERSION_CODENAME=' /etc/os-release 2>/dev/null | cut -d= -f2 | tr -d '"' || true)
    fi
    if [ -z "$codename" ]; then
        warn "无法检测系统版本代号（lsb_release / VERSION_CODENAME），跳过 apt 源切换"
        return 1
    fi
    local src_file="/etc/apt/sources.list.d/kvm-console-mirror.sources"

    backup_system_sources

    # 清理旧的 kvm-console 源文件
    rm -f /etc/apt/sources.list.d/kvm-console* 2>/dev/null || true

    local base_url=""
    case "$mirror_name" in
        tsinghua) base_url="https://mirrors.tuna.tsinghua.edu.cn/ubuntu" ;;
        aliyun)   base_url="https://mirrors.aliyun.com/ubuntu" ;;
        163)      base_url="http://mirrors.163.com/ubuntu" ;;
        *)        info "使用系统默认 apt 源"; return 0 ;;
    esac

    cat > "$src_file" <<EOF
# KVM Console 自动生成镜像源（${mirror_name}），请勿手动编辑
# 回滚命令：sudo cp ${_MIRROR_BACKUP_DIR:-/tmp}/* /etc/apt/sources.list.d/ 2>/dev/null

deb ${base_url}/ ${codename} main restricted universe multiverse
deb ${base_url}/ ${codename}-security main restricted universe multiverse
deb ${base_url}/ ${codename}-updates main restricted universe multiverse
EOF

    # 验证源可用性
    wait_apt_dpkg_lock
    if apt-get update -o Dir::Etc::SourceList="$src_file" -o Dir::Etc::SourceParts="none" >/dev/null 2>&1; then
        info "apt 源已切换到 ${mirror_name}（${codename}）"
        return 0
    else
        warn "apt 源切换失败（${mirror_name}），回滚到原配置..."
        restore_system_sources
        return 1
    fi
}

apply_rpm_mirror() {
    local mirror_name="$1"
    local repo_file="/etc/yum.repos.d/kvm-console-local-mirror.repo"

    # openEuler 镜像结构与官方 repo.openeuler.org 一致（{version}/{OS|everything|EPOL/main|update}/{arch}），
    # 直接改写 openeuler-*.repo 的 baseurl 主机为所选镜像即可，无需引入 CentOS 风格镜像文件。
    # 南京大学源 mirrors.nju.edu.cn（linuxmirrors.cn 推荐）为 openEuler 高优先级镜像。
    if [ "$OS_ID" = "openeuler" ]; then
        local oe_base=""
        case "$mirror_name" in
            nju)      oe_base="https://mirrors.nju.edu.cn/openeuler" ;;
            tsinghua) oe_base="https://mirrors.tuna.tsinghua.edu.cn/openeuler" ;;
            aliyun)   oe_base="https://mirrors.aliyun.com/openeuler" ;;
            *)        info "openEuler 使用官方源（DEPS_MIRROR=${mirror_name} 无对应 openEuler 镜像）"; return 0 ;;
        esac
        # 清理历史遗留的 CentOS 风格 kvm-console 镜像源文件（repomd.xml 404 会拖垮 dnf）
        rm -f /etc/yum.repos.d/*kvm-console* /etc/yum.repos.d/*KVM-Console* \
            /etc/yum.repos.d/*local-mirror* 2>/dev/null || true
        # 重写所有 openeuler-*.repo / openEuler-*.repo：
        # ① baseurl 主机换为所选镜像（保留 /openEuler-{version}[-SPx]/ 路径结构）；
        # ② 注释掉 metalink（metalink 会优先于 baseurl 解析镜像列表，必须禁用才保证走所选镜像）；
        # ③ gpgkey 主机同步改写，避免 key 下载仍走官方/旧源。
        # 用 [^ ]*/openEuler-2 捕获版本目录前缀（兼容 BSD/GNU sed，且天然适配 SPx 路径）。
        local repo_f rewritten=0
        # 注意：openEuler 系统自带主源文件为无连字符的 openEuler.repo（24.03 实测），
        # glob 必须同时覆盖 无连字符/openEuler-*、以及补写的小写 openeuler-*，否则系统源
        # 的 metalink 未注释、baseurl 未切换，dnf makecache 仍打官方慢源导致安装前期卡顿。
        for repo_f in /etc/yum.repos.d/openEuler.repo /etc/yum.repos.d/OpenEuler.repo /etc/yum.repos.d/openEuler-*.repo /etc/yum.repos.d/openeuler-*.repo; do
            [ -f "$repo_f" ] || continue
            if grep -qE "^baseurl=" "$repo_f"; then
                sed -i -E "s|^baseurl=[^ ]*/openEuler-2|baseurl=${oe_base}/openEuler-2|" "$repo_f" 2>/dev/null || true
                rewritten=$((rewritten + 1))
            fi
            if grep -qE "^metalink=" "$repo_f"; then
                sed -i -E "s|^metalink=|#metalink=|" "$repo_f" 2>/dev/null || true
            fi
            if grep -qE "^gpgkey=[^ ]*/openEuler-2" "$repo_f"; then
                sed -i -E "s|^gpgkey=[^ ]*/openEuler-2|gpgkey=${oe_base}/openEuler-2|" "$repo_f" 2>/dev/null || true
            fi
        done
        if [ "$rewritten" -gt 0 ]; then
            info "openEuler 源已切换到 ${mirror_name}（${oe_base}）"
        else
            # 尚无 openeuler-*.repo 文件（enable_openeuler_repos 未执行过），按镜像结构补写 everything
            local ver_suffix=""
            local ver_id
            ver_id=$(. /etc/os-release; echo "${VERSION_ID:-}")
            case "$ver_id" in
                20.*) ver_suffix="openEuler-20.03-LTS" ;;
                22.*) ver_suffix="openEuler-22.03-LTS" ;;
                24.*) ver_suffix="openEuler-24.03-LTS" ;;
                *)    ver_suffix="openEuler-${ver_id}" ;;
            esac
            local harch repo_arch2
            harch=$(uname -m)
            case "$harch" in
                aarch64|arm64) repo_arch2="aarch64" ;;
                *)             repo_arch2="x86_64" ;;
            esac
            cat > /etc/yum.repos.d/openeuler-everything.repo <<EOF
[everything]
name=everything - ${ver_suffix}
baseurl=${oe_base}/${ver_suffix}/everything/${repo_arch2}/
enabled=1
gpgcheck=0
EOF
            info "openEuler everything 源已写入 ${mirror_name}（${oe_base}）"
        fi
        # makecache 不在此处执行：apply_rpm_mirror 之后 probe_critical_rpm_packages
        # 会统一做一次 dnf makecache，两处全量刷新完全冗余（实测前一次 106s、后一次 45s）。
        info "镜像切换完成，软件源元数据缓存将在关键包探测阶段统一刷新"
        return 0
    fi

    # 麒麟（服务器版）：无对应 centos 镜像（自有 archive.kylinos.cn 源），
    # 禁止落入下方 centos-vault 分支写入 CentOS 源（会拉取 CentOS 包污染麒麟）。
    if [ "$OS_ID" = "kylin" ] || [ "$OS_ID" = "neokylin" ]; then
        info "麒麟使用系统默认 rpm 源（未提供 centos 镜像映射）"
        return 0
    fi

    backup_system_sources

    # 清理旧的 kvm-console 源文件
    rm -f /etc/yum.repos.d/*kvm-console* /etc/yum.repos.d/*KVM-Console* 2>/dev/null || true

    local base_url=""
    case "$mirror_name" in
        tsinghua) base_url="https://mirrors.tuna.tsinghua.edu.cn/centos-vault" ;;
        aliyun)   base_url="https://mirrors.aliyun.com/centos-vault" ;;
        163)      base_url="http://mirrors.163.com/centos-vault" ;;
        *)        info "使用系统默认 rpm 源"; return 0 ;;
    esac

    cat > "$repo_file" <<EOF
# KVM Console 自动生成镜像源（${mirror_name}），请勿手动编辑
# 回滚命令：sudo cp ${_MIRROR_BACKUP_DIR:-/tmp}/* /etc/yum.repos.d/ 2>/dev/null

[kvm-console-local-mirror]
name=KVM Console Local Mirror (${mirror_name})
baseurl=${base_url}
gpgcheck=0
enabled=1
EOF

    # 验证源可用性
    if command -v dnf >/dev/null 2>&1; then
        if dnf repolist kvm-console-local-mirror >/dev/null 2>&1; then
            info "rpm 源已切换到 ${mirror_name}"
            return 0
        fi
    elif command -v yum >/dev/null 2>&1; then
        if yum repolist kvm-console-local-mirror >/dev/null 2>&1; then
            info "rpm 源已切换到 ${mirror_name}"
            return 0
        fi
    fi

    warn "rpm 源切换失败（${mirror_name}），回滚到原配置..."
    restore_system_sources
    return 1
}

apply_system_mirror() {
    local mirror="${1:-${DEPS_MIRROR:-system}}"
    if [ "$mirror" = "system" ] || [ "$mirror" = "offline" ]; then
        return 0
    fi
    if [ "$PKG_MGR" = "apt" ]; then
        apply_apt_mirror "$mirror" || true
    else
        apply_rpm_mirror "$mirror" || true
    fi
}

# ── P2-8：安装期命令审计（M8.8，§5.8；失败 warn 不阻断，等保操作审计合规） ──
setup_bash_audit() {
    local audit_log="/var/log/bash.log"
    local marker="# BEGIN kvm_console bash audit"
    local marker_end="# END kvm_console bash audit"
    local probe='PROMPT_COMMAND="${PROMPT_COMMAND:+$PROMPT_COMMAND;} history -a; echo \"\$(date +%F_%T) \$(whoami) \$(history 1 2>/dev/null | sed -n '\''1p'\'') rc=$?\" >> '"$audit_log"

    touch "$audit_log" 2>/dev/null || { warn "无法创建审计日志 ${audit_log}"; return 0; }

    # 追加-only 优先，失败降级 chmod 622（append-only 防篡改）
    if command -v chattr >/dev/null 2>&1 && chattr +a "$audit_log" 2>/dev/null; then
        info "审计日志已启用追加-only（chattr +a ${audit_log}）"
    else
        chmod 622 "$audit_log" 2>/dev/null || true
        warn "chattr 不可用或失败，审计日志降级 chmod 622（${audit_log}）"
    fi

    # 对 /root/.bashrc 与 /etc/skel/.bashrc 注入 PROMPT_COMMAND 审计（幂等：已有 marker 则跳过）
    local rc
    for rc in /root/.bashrc /etc/skel/.bashrc; do
        if [ -f "$rc" ] && ! grep -q "$marker" "$rc" 2>/dev/null; then
            {
                echo ""
                echo "$marker"
                echo "$probe"
                echo "$marker_end"
            } >> "$rc"
            info "已为 ${rc} 启用 bash 命令审计"
        fi
    done
    return 0
}

# #K：安装报告（§5.8），show_info 后调用，一次性总结关键信息
# 组件升级提示（#Q）：与运行期 /system-info → firewall.upgrade_advice 同口径，仅提示不改变选择
print_install_report() {
    echo ""
    info "==================== 安装报告 ===================="
    info "防火墙后端: ${FW_BACKEND:-auto（未探测）}"
    info "二进制档位: ${KVM_BINARY_TIER:-compat}（选择原因见上方日志）"
    info "GLIBC 版本: ${DETECTED_GLIBC_VER:-未探测}"
    info "镜像源: ${DEPS_MIRROR:-system}"
    # P3-12：offline 模式缺失依赖汇总（专网环境提示从内网源手动安装）
    if [ "$DEPS_MIRROR" = "offline" ] && [ -n "${OFFLINE_MISSING_DEPS:-}" ]; then
        info "offline 模式: 以下依赖未自动安装，请从内网源手动安装: ${OFFLINE_MISSING_DEPS}"
    fi
    info "CPU 指令集: ${ARCH}${AVX2_FLAG:+ (AVX2 支持)}"
    info "CPU 厂商: ${DOMESTIC_CPU_VENDOR:-Unknown}"
    info "SELinux 状态: ${SELINUX_MODE:-未探测}"
    if [ "${KYSEC_STATE:-}" = "enabled" ]; then
        info "KYSEC 状态: 启用（麒麟安全机制，请确保 KVM 模块与 libvirt 相关策略已放行）"
    fi
    # kdump 建议（P2-8 / M8.8）：precheck_domestic 已输出 warn，报告中复述一行便于定位
    if [ "${KDUMP_SUGGESTED:-}" = "1" ]; then
        info "kdump 建议: 裸金属未配置 crashkernel，建议 crashkernel=2048M,high（虚拟化环境 512M）"
    fi
    # 发行版支持等级（P3-11 / M8.11）：support_level=C 理论兼容 → 报告中复述一行
    if [ "${SUPPORT_LEVEL_C:-}" = "1" ]; then
        info "支持等级: 当前发行版为理论兼容（C 级），生产环境建议升级到认证基线（S/A 级）"
    fi
    # 组件升级提示（firewalld 三档 <0.6 不完整支持 / 0.6~0.9 缺 policy / ≥0.9 健康；glibc 未达 native 提示当前 compat 档；SELinux Enforcing 提示 restorecon 已处理）
    if [ "$FW_BACKEND" = "firewalld" ] && [ -n "${DETECTED_FW_VER:-}" ]; then
        local fw_major="${DETECTED_FW_VER%%.*}" fw_minor
        fw_minor="${DETECTED_FW_VER#*.}"
        fw_minor="${fw_minor%%.*}"
        if [[ "$fw_major" =~ ^[0-9]+$ ]] && [[ "$fw_minor" =~ ^[0-9]+$ ]] && [ "$fw_major" -eq 0 ]; then
            if [ "$fw_minor" -lt 6 ]; then
                info "升级提示: firewalld ${DETECTED_FW_VER} 低于 0.6（面板不启用宿主机防火墙统一管理），请升级至 0.6+ 或使用发行版 iptables-service"
            elif [ "$fw_minor" -lt 9 ]; then
                info "升级提示: firewalld ${DETECTED_FW_VER} 低于 0.9（缺少 policy 能力），建议升级至 0.9+"
            fi
        fi
    fi
    if [ -n "${NATIVE_GLIBC_REQUIRED:-}" ] && [ -n "${DETECTED_GLIBC_VER:-}" ] \
        && [ "${KVM_BINARY_TIER:-compat}" != "native" ] \
        && [ "$(printf '%s\n%s\n' "$DETECTED_GLIBC_VER" "$NATIVE_GLIBC_REQUIRED" | sort -V | head -n1)" = "$DETECTED_GLIBC_VER" ] \
        && [ "$DETECTED_GLIBC_VER" != "$NATIVE_GLIBC_REQUIRED" ]; then
        info "升级提示: glibc ${DETECTED_GLIBC_VER} 未达 native 需求 ${NATIVE_GLIBC_REQUIRED}，当前使用 ${KVM_BINARY_TIER} 档；升级 glibc 可启用 native 档"
    fi
    # §2.5 评审：update/持久化沿用 compat 档时，若当前 glibc 已满足 native 需求，提示可重装启用
    if [ "${NATIVE_FEASIBLE:-}" = "1" ] && [ "${KVM_BINARY_TIER:-compat}" != "native" ]; then
        info "优化提示: 当前 glibc ${DETECTED_GLIBC_VER} 已满足 native 档需求 ${NATIVE_GLIBC_REQUIRED}，本次沿用 ${KVM_BINARY_TIER} 档；如需性能最优可在下次重装时选择 native 档"
    fi
    if [ "${SELINUX_MODE:-}" = "Enforcing" ]; then
        info "升级提示: SELinux 为 Enforcing，restorecon 已处理，仍建议核对相关上下文标签"
    fi
    if [ "${COMP_VER_TOTAL:-0}" -gt 0 ]; then
        info "组件版本检测: 总计 ${COMP_VER_TOTAL} 项 / 健康 ${COMP_VER_HEALTHY} / 警告 ${COMP_VER_WARN} / 关键不满足 ${COMP_VER_CRIT}（详情见上方检测报告）"
    fi
    if [ -n "${DEGRADED_NOTES:-}" ]; then
        info "降级项: ${DEGRADED_NOTES}"
    fi
    if [ -n "$LOG_FILE" ]; then
        info "安装日志: ${LOG_FILE}"
    fi
    info "=================================================="
    echo ""
}

# #K：步骤耗时汇总（按耗时降序输出，用于定位耗时最久的环节）
print_step_timing_summary() {
    echo ""
    info "========== 步骤耗时汇总（按耗时降序） =========="
    if [ -z "$STEP_TIMES_SUMMARY" ]; then
        info "（无步骤耗时记录）"
        return 0
    fi
    printf '%b' "$STEP_TIMES_SUMMARY" | sort -t'|' -k3 -rn | while IFS='|' read -r num name sec; do
        [ -n "$num" ] || continue
        printf '  %7ss  [STEP %s] %s\n' "$sec" "$num" "$name"
        log_write "[TIMING] 步骤耗时 ${sec}s  [STEP ${num}] ${name}"
    done
    info "========== 汇总结束 =========="
    echo ""
}

run_install_or_update() {
    # 降级项清单（#K：detect_firewall_backend / install_files 冒烟回退时累积，print_install_report 输出）
    DEGRADED_NOTES=""
    # P3-12：offline 模式缺失依赖清单（test_mirror_speed 判定 DEPS_MIRROR=offline 时填充）
    OFFLINE_MISSING_DEPS=""
    # #K：step 包装器计数，本流程固定 19 步（v0.9.8：新增「组件版本检测」；含「OVS 网络地基」共 19 个 step 调用）
    STEP_TOTAL=19
    local t_all_start t_all_end t_all_cost
    t_all_start=$(date +%s.%N 2>/dev/null || date +%s)
    step "硬件虚拟化检测" check_kvm_hardware
    step "依赖检查与安装" check_and_install_deps
    step "QEMU 环境适配" configure_qemu_for_rpm
    step "libvirt 非 root 配置" configure_libvirt_nonroot
    step "SELinux 配置" setup_selinux
    step "KVM 运行环境" ensure_kvm_runtime
    step "用户存储配额" setup_quota
    step "服务端口配置" configure_port
    step "防火墙后端探测" detect_firewall_backend
    step "前端端口放行" open_frontend_port
    step "安装前预检" precheck_domestic
    step "发行包获取" get_release
    step "组件版本检测" check_component_versions
    step "二进制档位选优" select_binary_tier
    step "安装文件" install_files
    step "配置写入" write_env
    step "运行目录补齐" ensure_directories
    step "存储权限配置" ensure_apparmor_storage_access
    step "OVS 网络地基" setup_ovs_foundation
    # 以下为辅助配置（失败不阻断面板安装，仅告警）
    ensure_sysctl_network || warn "sysctl 网络优化配置失败"
    setup_sshd_foundation || warn "SSHD 地基配置失败"
    setup_bash_audit || warn "bash 命令审计配置失败"
    setup_service
    start_service
    show_info
    print_install_report
    # #K：汇总步骤耗时与本次流程总耗时（定位耗时最久的环节）
    t_all_end=$(date +%s.%N 2>/dev/null || date +%s)
    t_all_cost=$(awk -v s="$t_all_start" -v e="$t_all_end" 'BEGIN{if (s ~ /N/ || e ~ /N/) {print "0"} else {printf "%.2f", e-s}}')
    log_write "[TIMING] 本次安装/更新流程总耗时 ${t_all_cost}s"
    info "本次安装/更新流程总耗时: ${t_all_cost}s"
    print_step_timing_summary
    # P1-4：安装成功清理失败标记并记录关键状态（binary_tier / 降级项 / 组件版本汇总）
    state_set "last_error" ""
    state_set "binary_tier" "${KVM_BINARY_TIER:-compat}"
    state_set "degraded_notes" "${DEGRADED_NOTES:-}"
    state_set "component_summary" "${COMP_VER_TOTAL:-0}/${COMP_VER_HEALTHY:-0}/${COMP_VER_WARN:-0}/${COMP_VER_CRIT:-0}"
}

# 修复配置文件：将 .env 重置为默认值并重启服务
repair_config() {
    echo ""
    warn "修复配置文件将把 ${ENV_FILE} 重置为默认值，已有的自定义配置将被覆盖。"
    read_tty -rp "确认重置配置文件? [y/N]: " confirm
    confirm=${confirm:-N}
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        warn "已取消修复"
        return
    fi

    write_env
    success "配置文件已重置为默认值"
    info "重启面板服务使配置生效..."
    systemctl restart "$SERVICE_NAME"
    sleep 2
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        success "面板服务已重启，配置文件已修复"
    else
        warn "服务启动异常，请查看日志: journalctl -u $SERVICE_NAME -f"
    fi
}

# rollback_release 从 .release_backup 中选择历史版本恢复程序文件与前端（C2，与 qvmc-manage.sh 功能 6 对齐，不改数据库与配置）。
rollback_release() {
    echo ""
    local backup_root="${INSTALL_DIR}/.release_backup"
    if [ ! -d "$backup_root" ]; then
        warn "未找到备份目录（${backup_root}），可能尚无历史版本"
        return
    fi

    local backups=()
    while IFS= read -r slot; do
        [ -n "$slot" ] || continue
        if [ -f "$backup_root/$slot/kvm-console" ] || [ -d "$backup_root/$slot/web-dist" ]; then
            backups+=("$slot")
        fi
    done < <(ls -1 "$backup_root" 2>/dev/null | sort)

    if [ "${#backups[@]}" -eq 0 ]; then
        warn "备份目录为空，无可回滚版本"
        return
    fi

    echo -e "可用备份："
    local index=0
    for slot in "${backups[@]}"; do
        index=$((index + 1))
        local ver=""
        [ -f "$backup_root/$slot/meta" ] && ver=$(grep -m1 "^backup_version=" "$backup_root/$slot/meta" 2>/dev/null | cut -d= -f2- || true)
        [ -z "$ver" ] && ver="unknown"
        printf "  ${CYAN}%d${NC}. %s (版本: %s)\n" "$index" "$slot" "$ver"
    done

    echo ""
    read_tty -rp "请选择要回滚的备份编号 [回车取消]: " sel
    if [ -z "$sel" ]; then
        info "已取消回滚"
        return
    fi
    if ! [[ "$sel" =~ ^[0-9]+$ ]] || [ "$sel" -lt 1 ] || [ "$sel" -gt "${#backups[@]}" ]; then
        error "无效的选择: $sel"
        exit 1
    fi

    local slot="${backups[$((sel - 1))]}"
    local target="$backup_root/$slot"
    echo -e "将使用备份 ${CYAN}${slot}${NC} 回滚程序文件与前端（不影响数据库与配置）"
    local confirm
    read_tty -rp "确认回滚? [y/N]: " confirm
    confirm=${confirm:-N}
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        info "已取消回滚"
        return
    fi

    info "停止服务 ${SERVICE_NAME}..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true

    if [ -f "$target/kvm-console" ]; then
        cp -a "$target/kvm-console" "${INSTALL_DIR}/kvm-console"
        chmod +x "${INSTALL_DIR}/kvm-console"
    fi
    if [ -f "$target/kvm-console-native" ]; then
        cp -a "$target/kvm-console-native" "${INSTALL_DIR}/kvm-console-native"
        chmod +x "${INSTALL_DIR}/kvm-console-native"
    fi
    if ls "$target"/kvm-console-compat-* 1>/dev/null 2>&1; then
        cp -a "$target"/kvm-console-compat-* "${INSTALL_DIR}/" 2>/dev/null || true
    fi
    if [ -d "$target/web-dist" ]; then
        rm -rf "${INSTALL_DIR}/web-dist"
        cp -a "$target/web-dist" "${INSTALL_DIR}/web-dist"
    fi

    info "重启面板服务..."
    systemctl start "$SERVICE_NAME" 2>/dev/null || true
    sleep 2
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        success "回滚完成，服务已启动"
    else
        warn "回滚完成，但服务启动异常，请查看日志: journalctl -u $SERVICE_NAME -f"
    fi
}

main() {
    echo ""
    echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║         ${APP_NAME} 安装 / 更新 / 卸载脚本        ║${NC}"
    echo -e "${CYAN}║                     （开源版）                    ║${NC}"
    echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
    echo ""

    # M7.1：命令行参数解析（--skip-version-check 跳过组件版本检测的 critical 中止；--resume 从失败步骤续跑）
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --skip-version-check)
                SKIP_VERSION_CHECK="1"
                shift
                ;;
            --resume)
                if [ -f "$STATE_FILE" ] && state_get stage >/dev/null 2>&1; then
                    RESUME_FROM="$(state_get stage)"
                    info "检测到安装状态（stage=${RESUME_FROM}），--resume 将从下一步继续"
                else
                    warn "--resume 未找到安装状态文件（${STATE_FILE}），将从头开始完整安装"
                fi
                shift
                ;;
            -h|--help)
                echo "用法: $0 [--skip-version-check] [--resume]"
                echo "  --skip-version-check   组件版本不满足最低要求时跳过中止（不推荐），仅警告继续"
                echo "  --resume               从上次失败步骤继续安装（读取 ${STATE_FILE}）"
                exit 0
                ;;
            *)
                echo "未知参数: $1（使用 --help 查看用法）" >&2
                exit 1
                ;;
        esac
    done

    check_root
    check_os
    check_arch
    check_locale
    choose_mode
    choose_install_dir
    init_log_file
    info "安装日志: ${LOG_FILE}"

    case "$MODE" in
        install|update)
            run_install_or_update
            ;;
        repair)
            repair_config
            ;;
        rollback)
            rollback_release
            ;;
        uninstall)
            uninstall_app
            ;;
        *)
            error "未知模式: $MODE"
            exit 1
            ;;
    esac
}

main "$@"
