#!/bin/bash
# ============================================================
# QVMConsole 本地打包脚本
# 构建前端 + 后端，自动检测宿主机架构，支持原生/交叉编译
# 产物: kvm-console-linux-{amd64|arm64}.tar.gz
# ============================================================

set -Eeuo pipefail

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }
success() { echo -e "${GREEN}[✓]${NC} $1"; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER_DIR="$SCRIPT_DIR/server"
WEB_DIR="$SCRIPT_DIR/web"
RELEASE_DIR="$SCRIPT_DIR/release"

# 自动检测宿主机架构
HOST_ARCH=$(uname -m)
case "$HOST_ARCH" in
    x86_64|amd64)   HOST_ARCH="amd64" ;;
    aarch64|arm64)  HOST_ARCH="arm64" ;;
    *)              HOST_ARCH="amd64" ;;  # 未知架构默认 amd64
esac

# 宿主机 OS：仅 Linux 同平台可「原生编译」，macOS/其他须走交叉编译（B1）
HOST_OS="$(uname -s)"
case "$HOST_OS" in
    Linux)  HOST_OS="linux" ;;
    Darwin) HOST_OS="darwin" ;;
    *)      HOST_OS="$(echo "$HOST_OS" | tr '[:upper:]' '[:lower:]')" ;;
esac

# 目标架构：默认与宿主机一致（原生编译）
TARGET_ARCH="$HOST_ARCH"

# ==================== 参数解析 ====================
VERSION=""
SKIP_FRONTEND=false
SKIP_BACKEND=false
BUILD_VARIANT=""  # 构建变体：空=全部, compat=zig兼容版, native=宿主机原生版
MINISIGN_KEY="${MINISIGN_KEY:-}"  # minisign 私钥文件路径（M8.7/§14.5 候选④）：指定时对发行包签名产出 .minisig；空则跳过仅 SHA256
# 公钥不在构建侧探测/分发（与官方一致，单一来源）：随 install.sh 内嵌为 MINISIGN_PUBLIC_KEY，不再维护独立公钥文件。
COMPAT_GLIBC_VERSION="${COMPAT_GLIBC_VERSION:-}"  # 兼容版 GLIBC 上限：未指定时按架构使用默认值
HIGH_COMPAT_GLIBC_VERSION=""  # 高兼容档 GLIBC 上限（如 2.28）：指定时额外构建 kvm-console-compat-{VER}（§4.3/M4）

get_compat_glibc_default() {
    case "$1" in
        amd64) echo "2.2.5" ;;
        arm64) echo "2.17" ;;
        *) error "无法为架构 $1 确定兼容版 GLIBC 版本" ;;
    esac
}

get_compat_zig_target() {
    local arch="$1"
    local glibc_version="$2"
    case "$arch" in
        amd64) echo "x86_64-linux-gnu.${glibc_version}" ;;
        arm64) echo "aarch64-linux-gnu.${glibc_version}" ;;
        *) error "无法为架构 $arch 确定 Zig 目标三元组" ;;
    esac
}

# ==================== 组件版本阈值（§5.11.2 表格唯一维护点，M7/H2） ====================
# compat-manifest.json 与 versions.conf 均由以下变量生成（同一来源），
# 修改阈值只允许改这里，禁止在 JSON heredoc / versions.conf / install.sh 内置默认值中单独改数字。
COMPONENT_REQ_GLIBC_AMD64="2.2.5"
COMPONENT_REQ_GLIBC_ARM64="2.17"
COMPONENT_REQ_QEMU="6.0|8.0"
COMPONENT_REQ_QEMUIMG="6.0|8.0"
COMPONENT_REQ_LIBVIRT="7.0|8.0"
COMPONENT_REQ_OVS="2.13|2.15"
COMPONENT_REQ_DNSMASQ="2.80|2.86"
COMPONENT_REQ_FIREWALLD="0.4.0|0.9.0"
COMPONENT_REQ_UFW="0.36|0.36"
COMPONENT_REQ_VIRTINSTALL="3.0|4.0"
COMPONENT_REQ_VIRTCUST="1.40|1.48"
COMPONENT_REQ_GUESTFISH="1.40|1.48"
COMPONENT_REQ_GROWPART="0.30|0.30"
COMPONENT_REQ_NTFSRESIZE="2022.5|2022.5"
COMPONENT_REQ_TCPDUMP="4.9|4.99"
COMPONENT_REQ_TC="5.0|5.10"

# ==================== 工具解析（B2：macOS/交叉环境 readelf 兼容） ====================
# 兼容版/原生版 GLIBC 校验依赖 GNU readelf。Linux 上为 binutils 的 readelf；
# macOS（brew binutils）为 greadelf；zig 交叉工具链可能提供 x86_64-linux-gnu-readelf / llvm-readelf。
# 解析出第一个可用的 readelf 命令，供 verify_compat_glibc / write_native_glibc 使用，避免硬编码 readelf 直接失败。
resolve_readelf() {
    if [ -n "${READELF_CMD:-}" ]; then
        echo "$READELF_CMD"
        return
    fi
    for c in readelf greadelf llvm-readelf x86_64-linux-gnu-readelf aarch64-linux-gnu-readelf; do
        if command -v "$c" &>/dev/null; then
            READELF_CMD="$c"
            echo "$c"
            return
        fi
    done
    echo ""
}

verify_compat_glibc() {
    local binary="$1"
    local expected_max="$2"
    local actual_max

    # v0.9.3（§5.9 #S）：readelf 缺失时必须构建失败，禁止静默漏检 GLIBC 上限
    local readelf_cmd
    readelf_cmd=$(resolve_readelf)
    if [ -z "$readelf_cmd" ]; then
        if command -v apt-get &>/dev/null; then
            error "缺少 readelf/binutils（兼容版 GLIBC 上限无法校验）。请安装: apt-get install -y binutils"
        elif command -v dnf &>/dev/null || command -v yum &>/dev/null; then
            error "缺少 readelf/binutils（兼容版 GLIBC 上限无法校验）。请安装: dnf install -y binutils"
        else
            error "缺少 readelf/binutils（兼容版 GLIBC 上限无法校验），构建中止"
        fi
    fi

    actual_max=$("$readelf_cmd" --version-info -W "$binary" 2>/dev/null \
        | grep -oE 'GLIBC_[0-9.]+' \
        | sed 's/^GLIBC_//' \
        | sort -Vu \
        | tail -n 1 || true)

    if [ -z "$actual_max" ]; then
        warn "未从兼容版二进制读取到 GLIBC 动态依赖，跳过版本上限校验"
        return
    fi

    if ! printf '%s\n%s\n' "$actual_max" "$expected_max" | sort -V -C; then
        error "兼容版 GLIBC 依赖校验失败：实际最高 GLIBC ${actual_max}，目标上限为 ${expected_max}"
    fi

    success "兼容版 GLIBC 依赖校验通过（最高 GLIBC ${actual_max}，目标上限 ${expected_max}）"
}

# write_native_glibc 探测原生版二进制实际最高 GLIBC 动态依赖符号，写入 native-glibc.txt（#A/M0.5）
# install.sh 的选优逻辑据此文件比较，取代 2.34 硬编码。
write_native_glibc() {
    local binary="$1"
    local out_file="$RELEASE_DIR/${OUTPUT_NAME}/native-glibc.txt"

    local readelf_cmd
    readelf_cmd=$(resolve_readelf)
    if [ -z "$readelf_cmd" ]; then
        warn "未检测到 readelf，无法探测原生版 GLIBC 依赖；跳过 native-glibc.txt 生成（install.sh 将判定 native 档不可用，回落 compat 档）"
        return
    fi

    local actual_max
    actual_max=$("$readelf_cmd" --version-info -W "$binary" 2>/dev/null \
        | grep -oE 'GLIBC_[0-9.]+' \
        | sed 's/^GLIBC_//' \
        | sort -Vu \
        | tail -n 1 || true)

    if [ -z "$actual_max" ]; then
        warn "未从原生版二进制读取到 GLIBC 动态依赖，跳过 native-glibc.txt 生成"
        return
    fi

    printf '%s\n' "$actual_max" > "$out_file"
    success "已写入 native-glibc.txt：原生版最高 GLIBC 依赖 ${actual_max}"
}

# write_compat_manifest 生成 compat-manifest.json（§5.11.3 / M7.0）
# 组件版本阈值的「权威来源」：install.sh check_component_versions 与后端 component_health 均以此文件为准，
# 与 §5.11.2 表格保持一一对应（版本号在此集中维护）。
# 参数: $1 = 输出路径, $2 = native 档 min_glibc（缺失/非法时写 "pending" 并 warn）
# 两处调用：
#   1. 后端编译前写入 server/service/diagnostics/（go:embed 编译期读取，§5.11.5）
#   2. 构建完成后写入 release 目录（install.sh 部署期读取）
write_compat_manifest() {
    local out_file="$1"
    local native_min="${2:-pending}"
    local build_host
    build_host=$(hostname 2>/dev/null || echo "unknown")
    # JSON 字符串转义（§3.2 评审）：防 hostname 含引号/反斜杠破坏 heredoc JSON 结构（仅需处理这两个转义字符）
    build_host=${build_host//\\/\\\\}
    build_host=${build_host//\"/\\\"}

    if ! [[ "$native_min" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
        native_min="pending"
    fi

    # 高兼容档二进制条目（仅实际构建时写入，避免悬空逗号）
    local high_bin=""
    if [ "$BUILD_COMPAT" = true ] && [ -n "$HIGH_COMPAT_GLIBC_VERSION" ]; then
        high_bin=$(cat <<EOF2
    "kvm-console-compat-${HIGH_COMPAT_GLIBC_VERSION}": {
      "type": "compat-high",
      "max_glibc": "${HIGH_COMPAT_GLIBC_VERSION}",
      "notes": "国产系统服务器版推荐档"
    },
EOF2
)
    fi

    cat > "$out_file" <<EOF
{
  "manifest_version": "1.0",
  "build_time": "${BUILD_TIME}",
  "build_host": "${build_host}",
  "target_arch": "${TARGET_ARCH}",
  "binaries": {
    "kvm-console": {
      "type": "compat-default",
      "max_glibc": "${COMPAT_GLIBC_VERSION}",
      "notes": "兼容面最广，任何现代 Linux 可运行"
    },
${high_bin}    "kvm-console-native": {
      "type": "native",
      "min_glibc": "${native_min}",
      "notes": "性能最佳，需 glibc >= native 阈值"
    }
  },
  "system_requirements": {
    "glibc":           { "min_version_amd64": "${COMPONENT_REQ_GLIBC_AMD64}", "min_version_arm64": "${COMPONENT_REQ_GLIBC_ARM64}", "category": "core" },
    "qemu-kvm":        { "min_version": "${COMPONENT_REQ_QEMU%%|*}",  "recommended": "${COMPONENT_REQ_QEMU#*|}",  "category": "core" },
    "qemu-img":        { "min_version": "${COMPONENT_REQ_QEMUIMG%%|*}",  "recommended": "${COMPONENT_REQ_QEMUIMG#*|}",  "category": "core" },
    "libvirt":         { "min_version": "${COMPONENT_REQ_LIBVIRT%%|*}",  "recommended": "${COMPONENT_REQ_LIBVIRT#*|}",  "category": "core" },
    "openvswitch":     { "min_version": "${COMPONENT_REQ_OVS%%|*}", "recommended": "${COMPONENT_REQ_OVS#*|}", "category": "core" },
    "dnsmasq":         { "min_version": "${COMPONENT_REQ_DNSMASQ%%|*}", "recommended": "${COMPONENT_REQ_DNSMASQ#*|}", "category": "core" },
    "firewalld":       { "min_version": "${COMPONENT_REQ_FIREWALLD%%|*}", "recommended": "${COMPONENT_REQ_FIREWALLD#*|}", "category": "core", "os": "rpm" },
    "ufw":             { "min_version": "${COMPONENT_REQ_UFW%%|*}", "recommended": "${COMPONENT_REQ_UFW#*|}", "category": "core", "os": "debian" },
    "virt-install":    { "min_version": "${COMPONENT_REQ_VIRTINSTALL%%|*}",  "recommended": "${COMPONENT_REQ_VIRTINSTALL#*|}",  "category": "disk" },
    "virt-customize":  { "min_version": "${COMPONENT_REQ_VIRTCUST%%|*}",  "recommended": "${COMPONENT_REQ_VIRTCUST#*|}",  "category": "disk" },
    "guestfish":       { "min_version": "${COMPONENT_REQ_GUESTFISH%%|*}",  "recommended": "${COMPONENT_REQ_GUESTFISH#*|}",  "category": "disk" },
    "genisoimage":     { "min_version": "any",  "alternatives": ["xorriso", "mkisofs"], "category": "disk" },
    "growpart":        { "min_version": "${COMPONENT_REQ_GROWPART%%|*}", "recommended": "${COMPONENT_REQ_GROWPART#*|}", "category": "disk" },
    "ntfsresize":      { "min_version": "${COMPONENT_REQ_NTFSRESIZE%%|*}", "recommended": "${COMPONENT_REQ_NTFSRESIZE#*|}", "category": "disk" },
    "edk2-ovmf":       { "min_version": "any", "category": "disk", "arch": "x86_64" },
    "edk2-aarch64":    { "min_version": "any", "category": "disk", "arch": "aarch64" },
    "tcpdump":         { "min_version": "${COMPONENT_REQ_TCPDUMP%%|*}", "recommended": "${COMPONENT_REQ_TCPDUMP#*|}", "category": "diag" },
    "tc":              { "min_version": "${COMPONENT_REQ_TC%%|*}", "recommended": "${COMPONENT_REQ_TC#*|}", "category": "diag" },
    "kvm_stat":        { "min_version": "any", "category": "diag", "optional": true },
    "cpu_vendor":      { "whitelist": ["Intel", "AMD", "Hygon", "Phytium", "Zhaoxin", "Kunpeng"], "category": "core" }
  },
  "os_compat": {
    "kylin-v10-server":  { "firewall": "firewalld", "glibc": "2.28",  "recommended_tier": "compat-2.28", "support_level": "S", "certified_hardware": ["Kunpeng", "Phytium", "Hygon", "Zhaoxin", "Intel", "AMD"] },
    "openEuler-24.03":   { "firewall": "firewalld", "glibc": "2.38",  "recommended_tier": "compat-2.28", "support_level": "A", "certified_hardware": ["Kunpeng", "Hygon", "Intel", "AMD"] },
    "openEuler-22.03":   { "firewall": "firewalld", "glibc": "2.34",  "recommended_tier": "compat-2.28", "support_level": "A", "certified_hardware": ["Kunpeng", "Hygon", "Intel", "AMD"] },
    "openEuler-20.03":   { "firewall": "firewalld", "glibc": "2.28",  "recommended_tier": "compat-2.28", "support_level": "A", "certified_hardware": ["Kunpeng", "Hygon"] },
    "uos-1060":          { "firewall": "ufw",       "glibc": "2.28",  "recommended_tier": "compat-2.28", "support_level": "B", "certified_hardware": ["Kunpeng", "Intel", "AMD"] },
    "ubuntu-22.04":      { "firewall": "ufw",       "glibc": "2.35",  "recommended_tier": "native",      "support_level": "S", "certified_hardware": ["Intel", "AMD", "Hygon", "Kunpeng"] },
    "debian-12":         { "firewall": "ufw",       "glibc": "2.36",  "recommended_tier": "native",      "support_level": "A", "certified_hardware": ["Intel", "AMD"] },
    "centos-7":          { "firewall": "firewalld", "glibc": "2.17",  "recommended_tier": "compat-default", "support_level": "C", "certified_hardware": [] }
  }
}
EOF
    success "已生成兼容性清单: ${out_file}"
}

# write_versions_conf 生成 versions.conf（M7/H2）：install.sh 用纯 shell 读取的版本阈值
# key=min|rec 逐行与 compat-manifest.json 的 system_requirements 同源（共享 COMPONENT_REQ_* 变量），
# 消除 install.sh 对 python3 解析 JSON 的依赖与内置默认值漂移。
# 参数: $1 = 输出路径（随发行包放置于 compat-manifest.json 同目录）
write_versions_conf() {
    local out_file="$1"
    cat > "$out_file" <<EOF
# 组件版本阈值（build.sh 依据 §5.11.2 表格同源生成，install.sh 纯 shell 读取）
# 格式: <组件>=<最低>|<推荐>；GLIBC_* 为 glibc 最低要求（按架构）
# M8.6：APP_VERSION 供 install.sh 记录部署版本到 .version（回滚备份 meta 使用）
APP_VERSION=${VERSION}
GLIBC_MIN_AMD64=${COMPONENT_REQ_GLIBC_AMD64}
GLIBC_MIN_ARM64=${COMPONENT_REQ_GLIBC_ARM64}
qemu-kvm=${COMPONENT_REQ_QEMU}
qemu-img=${COMPONENT_REQ_QEMUIMG}
libvirt=${COMPONENT_REQ_LIBVIRT}
openvswitch=${COMPONENT_REQ_OVS}
dnsmasq=${COMPONENT_REQ_DNSMASQ}
firewalld=${COMPONENT_REQ_FIREWALLD}
ufw=${COMPONENT_REQ_UFW}
virt-install=${COMPONENT_REQ_VIRTINSTALL}
virt-customize=${COMPONENT_REQ_VIRTCUST}
guestfish=${COMPONENT_REQ_GUESTFISH}
growpart=${COMPONENT_REQ_GROWPART}
ntfsresize=${COMPONENT_REQ_NTFSRESIZE}
tcpdump=${COMPONENT_REQ_TCPDUMP}
tc=${COMPONENT_REQ_TC}
# M8.11/P3-11：发行版支持等级（S=官方全量回归 / A=核心功能回归 / B=社区自测 / C=理论兼容）
# install.sh 在 precheck 阶段按当前发行版匹配并 warn「理论兼容」；与 compat-manifest.json os_compat 同源
SUPPORT_LEVEL_kylin-v10-server=S
SUPPORT_LEVEL_openEuler-24.03=A
SUPPORT_LEVEL_openEuler-22.03=A
SUPPORT_LEVEL_openEuler-20.03=A
SUPPORT_LEVEL_uos-1060=B
SUPPORT_LEVEL_ubuntu-22.04=S
SUPPORT_LEVEL_debian-12=A
SUPPORT_LEVEL_centos-7=C
EOF
    success "已生成版本阈值配置: ${out_file}"
}

usage() {
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  -v, --version VERSION    指定版本号 (例如: 1.0.0)"
    echo "  --target-arch ARCH       目标架构: amd64 或 arm64 (默认: ${HOST_ARCH})"
    echo "  --variant VARIANT        构建变体: compat(兼容版) / native(原生版) (默认: 全部)"
    echo "  --compat-glibc VERSION   兼容版 GLIBC 上限（默认: amd64=2.2.5，arm64=2.17）"
    echo "  --high-compat-glibc VERSION  额外构建高兼容档（如 2.28，产 kvm-console-compat-2.28）"
    echo "  --minisign-key FILE      minisign 私钥文件（§14.5 候选④，对发行包签名产出 .minisig；缺省时用环境变量 MINISIGN_KEY，均无则跳过仅 SHA256；验签公钥随 install.sh 内嵌，不在此指定）"
    echo "  --skip-frontend          跳过前端构建"
    echo "  --skip-backend           跳过后端构建"
    echo "  -h, --help               显示帮助信息"
    echo ""
    echo "示例:"
    echo "  $0                       构建全部，版本号为 dev"
    echo "  $0 -v 1.0.0             指定版本号构建全部"
    echo "  $0 --variant compat      仅构建 zig 兼容版（amd64 默认最高 GLIBC 2.2.5）"
    echo "  $0 --compat-glibc 2.17  构建 GLIBC 2.17 兼容版"
    echo "  $0 --high-compat-glibc 2.28  额外构建 GLIBC 2.28 高兼容档（国产系统服务器版）"
    echo "  $0 --variant native      仅构建宿主机原生版"
    echo "  $0 --target-arch arm64   交叉编译 ARM64 版本"
    echo "  $0 --target-arch amd64   交叉编译 AMD64 版本"
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        -v|--version)
            VERSION="$2"
            shift 2
            ;;
        --target-arch)
            TARGET_ARCH="$2"
            if [[ "$TARGET_ARCH" != "amd64" && "$TARGET_ARCH" != "arm64" ]]; then
                error "不支持的架构: ${TARGET_ARCH}，仅支持 amd64 / arm64"
            fi
            shift 2
            ;;
        --variant)
            BUILD_VARIANT="$2"
            if [[ "$BUILD_VARIANT" != "compat" && "$BUILD_VARIANT" != "native" ]]; then
                error "不支持的构建变体: ${BUILD_VARIANT}，仅支持 compat / native"
            fi
            shift 2
            ;;
        --compat-glibc)
            COMPAT_GLIBC_VERSION="$2"
            if ! [[ "$COMPAT_GLIBC_VERSION" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
                error "无效的兼容版 GLIBC 版本: ${COMPAT_GLIBC_VERSION}"
            fi
            shift 2
            ;;
        --high-compat-glibc)
            HIGH_COMPAT_GLIBC_VERSION="$2"
            if ! [[ "$HIGH_COMPAT_GLIBC_VERSION" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
                error "无效的高兼容档 GLIBC 版本: ${HIGH_COMPAT_GLIBC_VERSION}"
            fi
            shift 2
            ;;
        --minisign-key)
            MINISIGN_KEY="$2"
            if [ ! -f "$MINISIGN_KEY" ]; then
                error "minisign 私钥文件不存在: ${MINISIGN_KEY}"
            fi
            # §14.5 候选④：转绝对路径（后续签名块会 cd 到 release 目录，相对路径会失效）
            MINISIGN_KEY="$(cd "$(dirname "$MINISIGN_KEY")" && pwd)/$(basename "$MINISIGN_KEY")"
            shift 2
            ;;
        --skip-frontend)
            SKIP_FRONTEND=true
            shift
            ;;
        --skip-backend)
            SKIP_BACKEND=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            error "未知参数: $1，使用 -h 查看帮助"
            ;;
    esac
done

# 版本号处理：去除可能的 v 前缀，构建时统一加 v
if [ -n "$VERSION" ]; then
    VERSION="${VERSION#v}"
else
    VERSION="dev"
fi

BUILD_VERSION="v${VERSION}"
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# 根据目标架构确定输出名和 Go 编译参数
OUTPUT_NAME="kvm-console-linux-${TARGET_ARCH}"
GOARCH_VALUE="$TARGET_ARCH"  # Go GOARCH 与我们的命名一致（amd64/arm64）
# 交叉编译判定：架构不同，或宿主 OS 非 Linux（macOS/其他必须交叉编译出 Linux ELF，B1）
IS_CROSS_COMPILE=false
if [ "$TARGET_ARCH" != "$HOST_ARCH" ] || [ "$HOST_OS" != "linux" ]; then
    IS_CROSS_COMPILE=true
fi

if [ -z "$COMPAT_GLIBC_VERSION" ]; then
    COMPAT_GLIBC_VERSION=$(get_compat_glibc_default "$TARGET_ARCH")
fi
COMPAT_ZIG_TARGET=$(get_compat_zig_target "$TARGET_ARCH" "$COMPAT_GLIBC_VERSION")

# 构建变体布尔位（write_compat_manifest 依据其决定 binaries 段条目）
# A1：定义统一判定函数，避免两处写同样 case（新增档位时只改一处）
set_build_variant_flags() {
    BUILD_COMPAT=false
    BUILD_NATIVE=false
    case "${BUILD_VARIANT}" in
        "")     BUILD_COMPAT=true; BUILD_NATIVE=true ;;
        compat)  BUILD_COMPAT=true ;;
        native)  BUILD_NATIVE=true ;;
    esac
    # macOS/非 Linux 宿主交叉编译时，原生版无法构建（缺 Linux 头文件），自动降级仅构建 compat
    if [ "$IS_CROSS_COMPILE" = true ] && [ "$HOST_OS" != "linux" ] && [ "$BUILD_NATIVE" = true ]; then
        warn "宿主机非 Linux（${HOST_OS}），原生版无法交叉编译，自动降级为仅构建 compat 档"
        BUILD_NATIVE=false
    fi
}
set_build_variant_flags

echo ""
echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║         QVMConsole 构建打包脚本                  ║${NC}"
echo -e "${CYAN}╠══════════════════════════════════════════════════╣${NC}"
echo -e "${CYAN}║${NC}  版本:   ${GREEN}${BUILD_VERSION}${NC}"
echo -e "${CYAN}║${NC}  时间:   ${GREEN}${BUILD_TIME}${NC}"
echo -e "${CYAN}║${NC}  宿主机: ${GREEN}${HOST_ARCH}${NC}"
echo -e "${CYAN}║${NC}  目标:   ${GREEN}${TARGET_ARCH}${NC}"
echo -e "${CYAN}║${NC}  兼容版: ${GREEN}${COMPAT_ZIG_TARGET}${NC}"
if [ "$IS_CROSS_COMPILE" = true ]; then
    echo -e "${CYAN}║${NC}  模式:   ${YELLOW}交叉编译${NC}"
else
    echo -e "${CYAN}║${NC}  模式:   ${GREEN}原生编译${NC}"
fi
echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
echo ""

# ==================== 清理旧产物 ====================
info "清理旧构建产物..."
rm -rf "$RELEASE_DIR"
mkdir -p "$RELEASE_DIR/${OUTPUT_NAME}"

# ==================== 构建前端 ====================
if [ "$SKIP_FRONTEND" = false ]; then
    info "检查前端环境..."
    if ! command -v npm &>/dev/null; then
        error "npm 未安装，请先安装 Node.js (推荐 v20+)"
    fi

    info "安装前端依赖..."
    cd "$WEB_DIR"
    NPM_CI_LOG=$(mktemp)
    if npm ci 2>&1 | tee "$NPM_CI_LOG"; then
        rm -f "$NPM_CI_LOG"
    elif grep -Eq 'npm error code EUSAGE|package\.json and package-lock\.json.*sync|Missing: .* from lock file' "$NPM_CI_LOG"; then
        warn "检测到前端锁文件与当前平台依赖元数据不同步，正在重新解析并修复锁文件..."
        npm install
        rm -f "$NPM_CI_LOG"
    else
        cat "$NPM_CI_LOG"
        rm -f "$NPM_CI_LOG"
        error "前端依赖安装失败"
    fi

    info "构建前端..."
    npm run build

    if [ ! -d "$WEB_DIR/dist" ]; then
        error "前端构建失败，未生成 dist 目录"
    fi
    success "前端构建完成"
else
    warn "跳过前端构建"
    if [ ! -d "$WEB_DIR/dist" ]; then
        error "前端 dist 目录不存在，无法跳过构建"
    fi
fi

# ==================== 构建后端 ====================
if [ "$SKIP_BACKEND" = false ]; then
    info "检查后端环境..."
    if ! command -v go &>/dev/null; then
        error "Go 未安装，请先安装 Go (参考 server/go.mod 中的版本要求)"
    fi

    cd "$SERVER_DIR"

    # 确定需要构建哪些变体（A1：复用顶部统一判定，避免两处漂移）
    set_build_variant_flags

    # M7.0/§5.11.3：编译前将 compat-manifest.json 写入 server/service/diagnostics/
    # （go:embed 在编译期读取该文件，§5.11.5 后端 component_health 据此加载版本阈值）
    # native 档 min_glibc 此时尚不可知（release 目录刚被清理），先写 "pending"，打包段再回写实际值
    write_compat_manifest "$SERVER_DIR/service/diagnostics/compat-manifest.json" "pending"

    # ========== 构建 zig 兼容版（显式锁定 GLIBC 目标） ==========
    # build_compat_tier 构建单个 GLIBC 上限的兼容档（§4.3：默认档 + 可选高兼容档 M4）
    build_compat_tier() {
        local tier_glibc="$1"
        local tier_target="$2"
        local tier_output="$3"

        info "构建 zig 兼容版（目标: ${tier_target}）..."

        # 兼容版必须通过 Zig 的目标三元组锁定 GLIBC 版本；系统 GCC 会继承构建机 GLIBC。
        # 注意：必须显式设置 CGO_CFLAGS 禁止 FMA/AVX2 指令生成，否则新版 GCC 可能在
        #       浮点运算中自动使用 vfmadd 等 FMA3 指令，导致 Ivy Bridge 等旧 CPU 上 SIGILL
        compat_cgo_cflags="-O2"
        if [ "$TARGET_ARCH" = "amd64" ]; then
            compat_cgo_cflags="-O2 -mno-avx2 -mno-fma -mno-avx"
        fi

        if [ "${CGO_ENABLED:-1}" = "1" ]; then
            if command -v zig &>/dev/null; then
                export CC="zig cc -target ${tier_target}"
                export CXX="zig cxx -target ${tier_target}"
                info "使用 zig 作为 C 编译器: ${CC}"
            else
                error "构建兼容版需要 Zig，以确保 GLIBC 依赖不高于 ${tier_glibc}"
            fi
        fi

        # 清理 Go build cache 以确保 CC/CGO_CFLAGS 变更生效（防止复用 native 构建的缓存对象）
        # A2：不再全清默认缓存（会拖慢 native 档），改用独立的临时 GOCACHE 隔离兼容档编译产物
        tier_cache="$(mktemp -d)"
        export GOCACHE="$tier_cache"
        info "兼容版使用独立缓存（避免与 native 档互污染）: ${GOCACHE}"

        CGO_ENABLED=${CGO_ENABLED:-1} CGO_CFLAGS="$compat_cgo_cflags" GOOS=linux GOARCH="$GOARCH_VALUE" \
            go build \
            -ldflags="-s -w \
                -X main.Version=${BUILD_VERSION} \
                -X kvm_console/handler.Version=${BUILD_VERSION} \
                -X kvm_console/handler.BuildTime=${BUILD_TIME}" \
            -o "$RELEASE_DIR/${OUTPUT_NAME}/${tier_output}" \
            .

        rm -rf "$tier_cache"
        unset GOCACHE

        if [ ! -f "$RELEASE_DIR/${OUTPUT_NAME}/${tier_output}" ]; then
            error "zig 兼容版构建失败，未生成二进制文件 ${tier_output}"
        fi
        verify_compat_glibc "$RELEASE_DIR/${OUTPUT_NAME}/${tier_output}" "$tier_glibc"
        success "zig 兼容版构建完成（GLIBC 上限 ${tier_glibc}，产物 ${tier_output}）"
    }

    if [ "$BUILD_COMPAT" = true ]; then
        build_compat_tier "$COMPAT_GLIBC_VERSION" "$COMPAT_ZIG_TARGET" "kvm-console"

        # M4：--high-compat-glibc 指定时额外构建高兼容档（§4.3，产 kvm-console-compat-{VER}）
        if [ -n "$HIGH_COMPAT_GLIBC_VERSION" ]; then
            HIGH_COMPAT_ZIG_TARGET=$(get_compat_zig_target "$TARGET_ARCH" "$HIGH_COMPAT_GLIBC_VERSION")
            build_compat_tier "$HIGH_COMPAT_GLIBC_VERSION" "$HIGH_COMPAT_ZIG_TARGET" "kvm-console-compat-${HIGH_COMPAT_GLIBC_VERSION}"
        fi
    fi

    # ========== 构建宿主机原生版 ==========
    if [ "$BUILD_NATIVE" = true ]; then
        info "构建宿主机原生版..."

        # 清除 zig 编译器环境，使用系统默认编译器
        saved_cc="${CC:-}"
        saved_cxx="${CXX:-}"
        unset CC CXX

        native_output="kvm-console"
        if [ "$BUILD_COMPAT" = true ]; then
            native_output="kvm-console-native"  # 双构建时加后缀区分
        fi

        # 交叉编译且无 zig 时，检测 gcc 交叉编译器
        if [ "${CGO_ENABLED:-1}" = "1" ] && [ "$IS_CROSS_COMPILE" = true ]; then
            cross_cc=$(GOOS=linux GOARCH="$GOARCH_VALUE" go env CC 2>/dev/null || true)
            if [ -z "$cross_cc" ] || ! command -v "$cross_cc" >/dev/null 2>&1; then
                warn "CGO 交叉编译需要安装交叉编译器"
                if [ "$TARGET_ARCH" = "amd64" ]; then
                    warn "  请执行: apt-get install gcc-x86-64-linux-gnu"
                elif [ "$TARGET_ARCH" = "arm64" ]; then
                    warn "  请执行: apt-get install gcc-aarch64-linux-gnu"
                fi
                error "缺少交叉编译器 ${cross_cc:-gcc-${TARGET_ARCH}-linux-gnu}，无法完成 CGO 交叉编译"
            fi
            info "检测到交叉编译器: ${cross_cc}"
        fi

        CGO_ENABLED=${CGO_ENABLED:-1} GOOS=linux GOARCH="$GOARCH_VALUE" \
            go build \
            -ldflags="-s -w \
                -X main.Version=${BUILD_VERSION} \
                -X kvm_console/handler.Version=${BUILD_VERSION} \
                -X kvm_console/handler.BuildTime=${BUILD_TIME}" \
            -o "$RELEASE_DIR/${OUTPUT_NAME}/${native_output}" \
            .

        export CC="$saved_cc"
        export CXX="$saved_cxx"

        if [ ! -f "$RELEASE_DIR/${OUTPUT_NAME}/${native_output}" ]; then
            error "宿主机原生版构建失败，未生成二进制文件"
        fi
        write_native_glibc "$RELEASE_DIR/${OUTPUT_NAME}/${native_output}"
        success "宿主机原生版构建完成"
    fi
else
    warn "跳过后端构建"
fi

# ==================== 打包发行文件 ====================
info "打包发行文件..."

# 安全校验：后端二进制必须至少存在一个
if [ ! -f "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console" ] && [ ! -f "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console-native" ]; then
    error "后端二进制不存在于 ${RELEASE_DIR}/${OUTPUT_NAME}/。\n  若使用 --skip-backend，请确保之前已成功构建且未清空 release 目录。\n  建议：不带 --skip-backend 重新构建。"
fi

# 复制前端静态文件
cp -r "$WEB_DIR/dist" "$RELEASE_DIR/${OUTPUT_NAME}/web-dist"

# 复制安装脚本
cp "$SCRIPT_DIR/install.sh" "$RELEASE_DIR/${OUTPUT_NAME}/"
chmod +x "$RELEASE_DIR/${OUTPUT_NAME}/install.sh"

# 复制首次安装兼容性实机测试脚本到发行包根目录
cp "$SCRIPT_DIR/scripts/check-system-compatibility.sh" "$RELEASE_DIR/${OUTPUT_NAME}/"
chmod +x "$RELEASE_DIR/${OUTPUT_NAME}/check-system-compatibility.sh"

# 设置后端二进制可执行权限
if [ -f "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console" ]; then
    chmod +x "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console"
fi
if [ -f "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console-native" ]; then
    chmod +x "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console-native"
fi
if [ -n "$HIGH_COMPAT_GLIBC_VERSION" ] && [ -f "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console-compat-${HIGH_COMPAT_GLIBC_VERSION}" ]; then
    chmod +x "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console-compat-${HIGH_COMPAT_GLIBC_VERSION}"
fi

# ==================== 生成 compat-manifest.json（§5.11.3 / M7.0，install.sh 部署期版本比对用）====================
if [ -f "$RELEASE_DIR/${OUTPUT_NAME}/native-glibc.txt" ]; then
    NATIVE_MIN=$(tr -d '[:space:]' < "$RELEASE_DIR/${OUTPUT_NAME}/native-glibc.txt")
else
    NATIVE_MIN="pending"
    warn "未检测到 native-glibc.txt，compat-manifest.json 的 native 档 min_glibc 写入 pending（不影响版本检测，仅记录档位口径）"
fi
write_compat_manifest "$RELEASE_DIR/${OUTPUT_NAME}/compat-manifest.json" "$NATIVE_MIN"
write_versions_conf "$RELEASE_DIR/${OUTPUT_NAME}/versions.conf"

# ==================== 下载捆绑的 RPM 包（用于 Kylin/openEuler 等缺少的包）====================
# A3：捆绑源集中在变量管理，国产系统 el7→el9 迁移时仅需改此处地址
BUNDLE_EPEL_BASE_AMD64="https://dl.fedoraproject.org/pub/epel/8/Everything/x86_64/Packages"
BUNDLE_EPEL_BASE_ARM64="https://dl.fedoraproject.org/pub/epel/8/Everything/aarch64/Packages"
BUNDLE_ALMA_APPSTREAM_AMD64="https://repo.almalinux.org/almalinux/8/AppStream/x86_64/os/Packages"
BUNDLE_ALMA_APPSTREAM_ARM64="https://repo.almalinux.org/almalinux/8/AppStream/aarch64/os/Packages"

info "下载捆绑的 RPM 包..."
mkdir -p "$RELEASE_DIR/${OUTPUT_NAME}/bundled"

# arp-scan: 在 Kylin/openEuler 默认源中不存在，从 EPEL 获取
ARP_SCAN_RPM_URL=""
if [ "$TARGET_ARCH" = "amd64" ]; then
    ARP_SCAN_RPM_URL="${BUNDLE_EPEL_BASE_AMD64}/a/arp-scan-1.10.0-1.el8.x86_64.rpm"
elif [ "$TARGET_ARCH" = "arm64" ]; then
    ARP_SCAN_RPM_URL="${BUNDLE_EPEL_BASE_ARM64}/a/arp-scan-1.10.0-1.el8.aarch64.rpm"
fi

if [ -n "$ARP_SCAN_RPM_URL" ]; then
    if curl -fL --connect-timeout 10 "$ARP_SCAN_RPM_URL" -o "$RELEASE_DIR/${OUTPUT_NAME}/bundled/arp-scan.rpm" 2>/dev/null; then
        success "arp-scan RPM 下载完成"
    else
        warn "arp-scan RPM 下载失败，该功能将在系统中不可用时跳过（不影响核心功能）"
    fi
fi

# libguestfs-tools-c: 包含 virt-filesystems、virt-customize 等 C 工具
# 在 Kylin 上 guestfs-tools 包可能因依赖不足安装失败，从 AlmaLinux 8 AppStream 预取
LIBGUESTFS_TOOLS_C_URL=""
if [ "$TARGET_ARCH" = "amd64" ]; then
    LIBGUESTFS_TOOLS_C_URL="${BUNDLE_ALMA_APPSTREAM_AMD64}/libguestfs-tools-c-1.44.0-9.module_el8.7.0+3493+5ed0bd1c.alma.x86_64.rpm"
elif [ "$TARGET_ARCH" = "arm64" ]; then
    LIBGUESTFS_TOOLS_C_URL="${BUNDLE_ALMA_APPSTREAM_ARM64}/libguestfs-tools-c-1.44.0-9.module_el8.7.0+3493+5ed0bd1c.alma.aarch64.rpm"
fi

if [ -n "$LIBGUESTFS_TOOLS_C_URL" ]; then
    if curl -fL --connect-timeout 10 "$LIBGUESTFS_TOOLS_C_URL" -o "$RELEASE_DIR/${OUTPUT_NAME}/bundled/libguestfs-tools-c.rpm" 2>/dev/null; then
        success "libguestfs-tools-c RPM 下载完成"
    else
        warn "libguestfs-tools-c RPM 下载失败，virt-filesystems/virt-customize 将尝试通过系统源安装"
    fi
fi

# libguestfs-tools (noarch): 包含 virt-win-reg 等 Perl 脚本
# 在 openEuler 上可能为独立子包，安装失败时从捆绑包提取
LIBGUESTFS_TOOLS_NOARCH_URL="${BUNDLE_ALMA_APPSTREAM_AMD64}/libguestfs-tools-1.44.0-9.module_el8.7.0+3493+5ed0bd1c.alma.noarch.rpm"
if curl -fL --connect-timeout 10 "$LIBGUESTFS_TOOLS_NOARCH_URL" -o "$RELEASE_DIR/${OUTPUT_NAME}/bundled/libguestfs-tools.rpm" 2>/dev/null; then
    success "libguestfs-tools (noarch) RPM 下载完成"
else
    warn "libguestfs-tools (noarch) RPM 下载失败，virt-win-reg 将尝试通过系统源安装"
fi

# ==================== 生成 tar.gz ====================
# §14.5 候选④：公钥不在此分发（与官方一致，单一来源）。验签公钥随 install.sh 内嵌为
# MINISIGN_PUBLIC_KEY，构建侧仅需私钥签名产出 .minisig，不做任何公钥文件探测/拷贝，避免漂移。

cd "$RELEASE_DIR"
tar -czf "${OUTPUT_NAME}.tar.gz" "${OUTPUT_NAME}/"

# M8.7/P1-7 包校验：为发行包生成 SHA256SUMS（install.sh extract_tarball 下载后校验完整性）
# 同时为 release 目录内各二进制生成 sha256（供交付侧人工核对/防篡改）
cd "$RELEASE_DIR"
(
    sha256sum "${OUTPUT_NAME}.tar.gz" > "${OUTPUT_NAME}.tar.gz.sha256"
    cd "${OUTPUT_NAME}"
    find . -maxdepth 2 -type f \( -name 'kvm-console*' -o -name 'compat-manifest.json' -o -name 'native-glibc.txt' \) -exec sha256sum {} + 2>/dev/null > "../${OUTPUT_NAME}.SHA256SUMS"
) 2>/dev/null
success "已生成包校验文件: ${OUTPUT_NAME}.tar.gz.sha256 / ${OUTPUT_NAME}.SHA256SUMS"

# §14.5 候选④ / M8.7 增强：minisign 离线签名（供应链防篡改）
# SHA256 仅防传输损坏/偶发篡改；minisign 非对称签名可防有动机的替换攻击。
# 私钥来源优先级：--minisign-key 参数 > 环境变量 MINISIGN_KEY。均无则跳过（仅 SHA256，不阻断构建）。
# 公钥随 install.sh 内嵌（MINISIGN_PUBLIC_KEY），install.sh 安装期用内嵌公钥 minisign -V -m 验证，
# 构建侧不再分发公钥文件（与官方一致）。
cd "$RELEASE_DIR"
# §14.5 候选④：环境变量 MINISIGN_KEY 传入的相对路径在 cd 到 release 后失效，兜底转绝对（参数方式已在解析时转换）
if [ -n "$MINISIGN_KEY" ] && [[ "$MINISIGN_KEY" != /* ]]; then
    MINISIGN_KEY="$SCRIPT_DIR/$MINISIGN_KEY"
fi
if [ -n "$MINISIGN_KEY" ]; then
    if command -v minisign >/dev/null 2>&1; then
        if minisign -S -s "$MINISIGN_KEY" -m "${OUTPUT_NAME}.tar.gz" -x "${OUTPUT_NAME}.tar.gz.minisig" >/dev/null 2>&1; then
            success "已生成 minisign 签名: ${OUTPUT_NAME}.tar.gz.minisig"
        else
            warn "minisign 签名失败（检查私钥密码/路径），本次发行包仅 SHA256 校验"
        fi
    else
        warn "未安装 minisign 命令，跳过签名（仅 SHA256 校验）。安装: apt install minisign / dnf install minisign"
    fi
else
    info "未指定 minisign 私钥（--minisign-key 或 MINISIGN_KEY），跳过签名，仅 SHA256 校验"
fi

# ==================== 生成自解压 .run 单文件安装器 ====================
# §15.0 / 单文件方案：将 tar.gz + .minisig + .sha256 全部内嵌进一个自解压 shell 脚本，
# 用户只需下载并执行一个文件，引导头自动解压并调用包内 install.sh，
# 无需额外下载 .minisig / .sha256 / 公钥等旁文件，避免漏下载导致安装失败。
# 结构：<引导shell> + <PAYLOAD_MARKER 行> + <base64 编码的 tar.gz[;minisig;sha256]>
generate_selfextract_installer() {
    local tarball="${OUTPUT_NAME}.tar.gz"
    local installer="${OUTPUT_NAME}.run"
    local tarb64 sig_b64 sha_b64 payload marker

    tarb64=$(base64 < "$tarball" | tr -d '\n')
    sig_b64=""
    if [ -f "$tarball.minisig" ]; then
        sig_b64=$(base64 < "$tarball.minisig" | tr -d '\n')
    fi
    sha_b64=""
    if [ -f "$tarball.sha256" ]; then
        sha_b64=$(base64 < "$tarball.sha256" | tr -d '\n')
    fi
    payload="${tarb64};${sig_b64};${sha_b64}"

    marker="__QVM_PAYLOAD_MARKER__"

    # 引导头模板：单引号 heredoc 不做任何展开，占位符 @ARCHIVE_BASENAME@ 由下方 sed 替换
    cat > "$installer" <<'QVM_SELFEXT_TMPL'
#!/usr/bin/env bash
# ============================================================
#  QVMConsole 自解压安装器（单文件）
#  用法:   bash ./本文件 [安装脚本参数...]
#  或:     chmod +x ./本文件 && ./本文件 [参数...]
#  说明:   安装包 tar.gz + .minisig 签名 + .sha256 校验均已内嵌，
#          运行本文件即自动解压并调用包内 install.sh，无需额外下载旁文件。
# ============================================================
PAYLOAD_MARKER="__QVM_PAYLOAD_MARKER__"
ARCHIVE_NAME="@ARCHIVE_BASENAME@.tar.gz"
TMPD=""

err()  { echo -e "\033[0;31m[ERROR]\033[0m $*"; exit 1; }
info() { echo -e "\033[0;32m[INFO]\033[0m $*"; }

# 提前解析本脚本绝对路径（后续 cd 后相对 $0 会失效，导致跨目录以相对路径调用时报
# “未找到 payload 标记”）；dirname/basename 先用命令解析，兼容各种调用方式
SCRIPT_PATH="$(cd "$(dirname "$0")" 2>/dev/null && pwd)/$(basename "$0")"
CUR_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$CUR_DIR" || err "无法进入当前目录"

# 保存原始调用参数（后续 set -- 分割 payload 会覆盖 $@，必须提前备份）
CALL_ARGS=("$@")

# 定位 payload 标记行，取其下一行为 payload 起始
line=$(grep -n -m1 "^${PAYLOAD_MARKER}$" "$SCRIPT_PATH" | awk -F: '{print $1}')
[ -n "$line" ] || err "自解压脚本结构异常（未找到 payload 标记）"
line=$((line + 1))

TMPD="$(mktemp -d)" || err "无法创建临时目录"
trap 'rm -rf "$TMPD" 2>/dev/null' EXIT

# 提取 marker 之后全部行并合并为单串 base64 payload
payload=$(sed -n "${line},\$ p" "$SCRIPT_PATH" | tr -d '\n')
[ -n "$payload" ] || err "payload 为空"

# 分割: tar.gz_base64 ; minisig_base64 ; sha256_base64
OIFS="$IFS"; IFS=";"; set -- $payload; IFS="$OIFS"
pkg_b64="${1:-}"; sig64="${2:-}"; sha64="${3:-}"
[ -n "$pkg_b64" ] || err "缺少安装包数据"

echo "$pkg_b64" | base64 -d > "$TMPD/$ARCHIVE_NAME" || err "安装包 base64 解码失败"
[ -f "$TMPD/$ARCHIVE_NAME" ] || err "安装包提取失败"

# 写入校验旁文件（install.sh 会主动读取；缺则跳过、由包内逻辑降级）
if [ -n "$sha64" ]; then
    echo "$sha64" | base64 -d > "$TMPD/${ARCHIVE_NAME}.sha256" 2>/dev/null || rm -f "$TMPD/${ARCHIVE_NAME}.sha256"
    exp_sha="$(awk '{print $1}' "$TMPD/${ARCHIVE_NAME}.sha256" 2>/dev/null || true)"
    if [ -n "$exp_sha" ]; then
        act_sha="$(sha256sum "$TMPD/$ARCHIVE_NAME" | awk '{print $1}')"
        if [ "$exp_sha" != "$act_sha" ]; then
            err "内部安装包 SHA256 校验失败（可能已损坏或被篡改），已中止"
        fi
        info "内部安装包 SHA256 校验通过"
    fi
fi
if [ -n "$sig64" ]; then
    echo "$sig64" | base64 -d > "$TMPD/${ARCHIVE_NAME}.minisig" 2>/dev/null || rm -f "$TMPD/${ARCHIVE_NAME}.minisig"
fi

# 解压安装包
cd "$TMPD" || err "无法进入临时目录"
tar -xzf "$ARCHIVE_NAME" || err "安装包解压失败"

local_install="$(find "$TMPD" -maxdepth 3 -name 'install.sh' -type f | sed -n '1p')"
[ -n "$local_install" ] || err "解压文件中未找到 install.sh"

chmod +x "$local_install" 2>/dev/null
info "自解压完成，启动安装脚本。"
cd "$(dirname "$local_install")" || exit 1
exec bash "$(basename "$local_install")" "${CALL_ARGS[@]}"
QVM_SELFEXT_TMPL

    # 替换引导头中的占位符（ARCHIVE_BASENAME 由 OUTPUT_NAME 按架构替换）
    # 兼容 macOS BSD sed（-i 需后缀）与 GNU/Linux sed（-i 不带后缀）：统一 -i.bak 并清理备份
    sed -i.bak "s#@ARCHIVE_BASENAME@#${OUTPUT_NAME}#" "$installer" 2>/dev/null || true
    rm -f "$installer.bak"

    # 追加 payload 标记行与 base64 payload
    printf '%s\n' "$marker" >> "$installer"
    printf '%s\n' "$payload" >> "$installer"

    # 保证全部写入并设可执行
    chmod +x "$installer"
    # 为单文件安装器自身生成 SHA256（供下载后人工核验）
    sha256sum "$installer" > "$installer.sha256"
    success "已生成自解压单文件安装器: ${installer}（$(du -h "$installer" | cut -f1)）"
}

generate_selfextract_installer

PACKAGE_SIZE=$(du -sh "$RELEASE_DIR/${OUTPUT_NAME}.tar.gz" | cut -f1)

echo ""
echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║         构建完成！                               ║${NC}"
echo -e "${CYAN}╠══════════════════════════════════════════════════╣${NC}"
echo -e "${CYAN}║${NC}  产物:   ${GREEN}release/${OUTPUT_NAME}.tar.gz${NC}"
echo -e "${CYAN}║${NC}          ${GREEN}release/${OUTPUT_NAME}.run（单文件自解压安装器）${NC}"
echo -e "${CYAN}║${NC}  大小:   ${GREEN}${PACKAGE_SIZE}${NC}"
echo -e "${CYAN}║${NC}  版本:   ${GREEN}${BUILD_VERSION}${NC}"
echo -e "${CYAN}║${NC}  架构:   ${GREEN}${TARGET_ARCH}${NC}"
echo -e "${CYAN}╠══════════════════════════════════════════════════╣${NC}"
echo -e "${CYAN}║${NC}  内容:"
if [ -f "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console" ]; then
    echo -e "${CYAN}║${NC}    - kvm-console        后端二进制（zig 兼容版，GLIBC 上限 ${COMPAT_GLIBC_VERSION}）"
fi
if [ -n "$HIGH_COMPAT_GLIBC_VERSION" ] && [ -f "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console-compat-${HIGH_COMPAT_GLIBC_VERSION}" ]; then
    echo -e "${CYAN}║${NC}    - kvm-console-compat-${HIGH_COMPAT_GLIBC_VERSION}  后端二进制（zig 高兼容档，GLIBC 上限 ${HIGH_COMPAT_GLIBC_VERSION}）"
fi
if [ -f "$RELEASE_DIR/${OUTPUT_NAME}/kvm-console-native" ]; then
    echo -e "${CYAN}║${NC}    - kvm-console-native  后端二进制（宿主机原生版）"
fi
if [ -f "$RELEASE_DIR/${OUTPUT_NAME}/native-glibc.txt" ]; then
    echo -e "${CYAN}║${NC}    - native-glibc.txt  native 版 GLIBC 需求记录（install.sh 选优用）"
fi
if [ -f "$RELEASE_DIR/${OUTPUT_NAME}/compat-manifest.json" ]; then
    echo -e "${CYAN}║${NC}    - compat-manifest.json  组件版本兼容性清单（install.sh 检测与面板展示用）"
fi
echo -e "${CYAN}║${NC}    - web-dist/          前端静态文件"
echo -e "${CYAN}║${NC}    - install.sh         安装脚本"
echo -e "${CYAN}║${NC}    - check-system-compatibility.sh  首次安装兼容性实机测试"
echo -e "${CYAN}║${NC}    - bundled/           捆绑的 RPM 包（用于缺失的系统包）"
if [ -f "$RELEASE_DIR/${OUTPUT_NAME}.tar.gz.minisig" ]; then
    echo -e "${CYAN}║${NC}    - ${OUTPUT_NAME}.tar.gz.minisig  minisign 签名（§14.5 候选④，安装期验证）"
fi
echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
echo ""
