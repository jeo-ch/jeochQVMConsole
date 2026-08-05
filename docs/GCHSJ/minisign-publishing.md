# minisign 离线签名与发布流程

> 对应设计文档 §14.5 候选④（M8.7 包校验的供应链安全增强）。

## 为什么需要

- `SHA256SUMS` / `.tar.gz.sha256` 只能防**传输损坏 / 偶发篡改**（校验值随包发布，攻击者可同时替换包与校验值）。
- minisign 是**非对称签名**（Ed25519）：私钥离线保管，公钥随包分发；攻击者无法在无私钥的情况下伪造签名，可防**有动机的替换攻击**。

## 当前状态

- ✅ 构建侧：`build.sh` 已支持签名（`--minisign-key FILE` 或环境变量 `MINISIGN_KEY`），签名产出 `release/<包名>.tar.gz.minisig`，公钥 `minisign.pub` 随包分发。
- ✅ 安装侧：`install.sh` `extract_tarball` 在 SHA256 校验后执行 `verify_minisign_signature()`：
  - 优先用**内嵌公钥** `MINISIGN_PUBLIC_KEY`（发行方随 install.sh 分发），其次 `INSTALL_DIR/minisign.pub`、包内/同目录 `minisign.pub`。
  - 有签名+公钥但验证失败 → **exit 1 中止**；无 minisign 命令 / 无签名 / 无公钥 → 降级仅 SHA256（不阻断）。
- ✅ 依赖登记：`docs/dependencies.md`「构建期依赖」节。
- ⏳ 待发行方完成：**密钥生成与公钥提交**（下述流程）。

## 一次性：生成密钥对（离线环境执行）

> 私钥必须**离线、不可恢复地妥善保管**（建议两台及以上可信机器各存一份、U 盘冷备），公钥入库公开。

```bash
# 在离线/隔离环境安装 minisign（apt install minisign / dnf install minisign）
minisign -G -s "$HOME/.qvmc-signing/minisign.key" -p "$HOME/.qvmc-signing/minisign.pub"
# 输入注释（如 "QVMConsole Release Signing"）+ 强密码（≥16 位，含大小写+数字+符号）
```

## 每次发布

```bash
# 1. 构建 + 签名（私钥路径用参数或环境变量）
MINISIGN_KEY="$HOME/.qvmc-signing/minisign.key" bash build.sh -v 1.0.0
# 或: bash build.sh -v 1.0.0 --minisign-key "$HOME/.qvmc-signing/minisign.key"

# 2. 将公钥随发布目录一并上传（构建已自动拷贝到包内 + 发布目录）
ls release/*.tar.gz.minisig release/minisign.pub   # 确认存在

# 3. 上传到下载源：
#    - <包名>.tar.gz           发行包
#    - <包名>.tar.gz.sha256    完整性校验（M8.7）
#    - <包名>.tar.gz.minisig   签名（候选④）
#    - minisign.pub            公钥（与包同目录，或由发行方发布到官网/仓库根）
#    - <包名>.run              单文件自解压安装器（tar.gz+签名+sha256 内嵌，用户只需下载此一个文件）
#    - <包名>.run.sha256       .run 自身完整性校验（供下载后人工核验）
```

## 公钥分发原则

- 公钥放置于**与发布包不同源、可独立核对**的位置（仓库根 / 官网），避免攻击者同时篡改发布包与公钥。
- 公钥一经发布不可更换（更换会失效所有旧包验签），密钥轮换需走独立公告。
- 可将公钥写入 `install.sh` 的 `MINISIGN_PUBLIC_KEY` 变量随安装脚本内嵌分发（最稳），或由 `install.sh` 自动下载 `minisign.pub`。

## 安装端验证（用户侧，自动执行）

```bash
sudo ./install.sh   # extract_tarball 自动执行 SHA256 + minisign 双重校验
# 无 minisign 命令时自动降级 SHA256（提示，不阻断）
```

## 密钥泄露应急

- 立即生成新密钥对 → 公告新公钥 → 下架旧签名包 → 用新私钥重新签名并重新发布。
- 保留旧公钥一段时间的"过渡验证"（可选），引导用户在安装端指定新旧公钥任一生效。
