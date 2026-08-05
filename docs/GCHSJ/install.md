# QVMConsole 安装指南

> 三种安装方式，均只需下载**一个文件**即可完成安装，无额外旁文件依赖。

## 安装方式概览

| 方式 | 下载文件 | 命令 | 适用场景 |
|------|---------|------|---------|
| ① 单文件自解压（推荐） | `kvm-console-linux-<arch>.run` | `bash 文件` 或 `./文件` | 最简单，一条命令全自动 |
| ② tar.gz 传统包 | `kvm-console-linux-<arch>.tar.gz` | `tar -xzf` + `./install.sh` | 手动部署/离线拷贝 |
| ③ 仅 install.sh | `install.sh` | `./install.sh` | 联网机器，自动下载安装包 |

`<arch>` 为 `amd64`（x86_64）或 `arm64`（aarch64），按目标机器架构选择。

> 安装包本身不区分发行版，支持 Debian/Ubuntu、openEuler/麒麟/CentOS、Rocky/Alma 等主流 Linux。
> 首次安装后服务默认监听 `8080` 端口，防火墙开启时安装脚本会自动放行该端口。

---

## 方式 ①：单文件自解压安装器（推荐）

> 将安装包 + minisign 签名 + SHA256 校验**全量内嵌**在一个文件里，无任何旁文件。

```bash
# 下载
curl -L -O https://<下载源>/kvm-console-linux-amd64.run

# 执行（两种写法等价）
sudo bash kvm-console-linux-amd64.run
# 或
chmod +x kvm-console-linux-amd64.run && sudo ./kvm-console-linux-amd64.run
```

执行后引导头自动完成：**SHA256 自校验 → 解压 → 调用包内 install.sh**。
校验失败（文件损坏/被篡改）会明确报错并中止，绝不带病安装。

可选核验：下载 `.run.sha256` 后比对（`sha256sum -c`）。

---

## 方式 ②：tar.gz 传统包

> 发行包为自包含完整目录，解压后运行包内 install.sh 即可，**不需要**额外下载
> `.sha256` / `.minisig` / `.SHA256SUMS`——这些旁文件全部可选，缺失时自动降级。

```bash
# 下载
curl -L -O https://<下载源>/kvm-console-linux-amd64.tar.gz

# 解压
tar -xzf kvm-console-linux-amd64.tar.gz
cd kvm-console-linux-amd64

# 安装
sudo ./install.sh
```

说明：

- 包内已含后端二进制、前端静态文件、安装脚本、捆绑 RPM、公钥等全部内容，**离线可装**。
- 若手动核验，可下载 `.tar.gz.sha256` / `.tar.gz.minisig` 置于包旁，install.sh 会自动校验：
  - `.sha256` 存在则校验完整性（不符 exit 1 中止）；
  - `.minisig` 存在且系统装有 minisign 则验签（失败 exit 1 中止）；
  - 均缺失时正常安装（仅提示降级，不阻断）。

---

## 方式 ③：仅 install.sh（联网自动下载）

> 机器有网络时，只需 install.sh 一个脚本，它会自动从官方下载源拉取对应架构的安装包。

```bash
curl -L -O https://<下载源>/install.sh
sudo ./install.sh
```

`get_release` 自动完成：检测本地发行包 → 未找到则按架构下载 → 校验 → 解压 → 安装。

---

## 共用说明

### 参数

```bash
sudo ./install.sh --skip-version-check   # 跳过组件版本最低要求的中止（不推荐）
sudo ./install.sh --resume               # 从上次失败步骤继续安装
```

### 更新 / 卸载 / 修复

已安装过时再次执行安装脚本，会进入交互菜单（或 `CI=1` 非交互直接更新）：

1. 更新（重新检测/修复运行地基，回滚机制见 `qvmc-manage.sh` 功能 6）
2. 卸载
3. 修复配置文件（重置 `.env` 为默认值）
4. 回滚到历史发行版

### 验证安装

```bash
systemctl status kvm-console      # 服务状态
curl -sI http://localhost:8080    # 前端 HTTP 200
```

### 常见问题

- **防火墙开启**：安装脚本自动放行前端端口（firewalld `--add-port` / ufw `allow`）；防火墙关闭则跳过。
- **SELinux Enforcing（openEuler/麒麟）**：安装脚本自动放行 libvirt/QEMU 布尔值并为存储目录打标。
- **下载源不通**：可手动下载对应安装包后放置于当前目录再执行，install.sh 优先使用本地包。
