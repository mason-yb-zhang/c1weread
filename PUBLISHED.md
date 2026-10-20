# weread 商店发布与设备状态

## 商店（c1pkg / fwz233 索引）

| 项 | 值 |
|---|---|
| 应用 ID | `weread` |
| 显示名 | `微信读书` |
| 商店当前版本 | **0.1.1** |
| 建议下一版本 | 0.1.2 |
| 商店作者 | `masonzhang` |
| 启动模式（0.1.1） | 无 `.c1pkg-mode` → 默认 **terminal** → 闪退根因 |

索引片段（设备 `/usr/data/c1/pkg/cache/verified.v1`）：

```
P	weread	0.1.1	…	c1weread	masonzhang
```

### 待发 0.1.2（未上传）

- 内容：`signal.Ignore(SIGHUP)` + trace 留痕 + 不改 ELF 头（`e_flags=0x50001004`）
- **必须** `-mode direct`
- 二进制 SHA256：`879742745bdae30e96596249f9aab6d461f50c97dc677831829bf7f6cc1aa644`
- 阻塞：本机只有 `mason-yb-zhang` 令牌，更新 `weread` 返回 **HTTP 403**；须 `masonzhang` 令牌

```bat
c1publish.exe -token-file <masonzhang-token> -allow-insecure-http ^
  -id weread -version 0.1.2 -name "微信读书" ^
  -entry bin/c1weread -payload .\payload -mode direct
```

## 令牌归属（勿混用）

| 令牌 | 作者 | 能做什么 |
|---|---|---|
| `~/.config/c1-reader-lab/publisher.token` | `mason-yb-zhang` | 新 ID 首发（如 `local-apps`）；**不能**更新 `weread`/`reader-lab` |
| `masonzhang` 令牌（本机暂无） | `masonzhang` | 更新 `weread`、`reader-lab` |

令牌文件绝不入库、不进 payload、不发聊天。

## 设备侧就地修复（2026-10-20，MagicPen-b898d0）

商店装的 0.1.1 已在设备上修好，不必等 0.1.2：

1. `versions/0.1.1/.c1pkg-mode` = 6 字节 `direct`（`0444`，`nlink=1`）
2. 二进制换成上表 SHA 的构建（含 SIGHUP 忽略）
3. 目录重封 `0555`（满足 `trusted_directory`）
4. local-apps 同步同一二进制，`20-weread.json` 的 `sha256` 已更新

备份：

- `versions/0.1.1/c1weread.before-20261020`（旧 SHA `dc579dd8…`）
- `/storage/c1/local-apps/manifests/20-weread.json.before-20261020`

崩溃留痕：`/storage/c1/local-apps/apps/weread/data/c1weread.trace`

## 同日相关：local-apps 上架

| 项 | 值 |
|---|---|
| ID | `local-apps` |
| 显示名 | **本地应用** |
| 版本 | 0.1.0（首发） |
| 入口 | `bin/local-launcher` |
| 模式 | `direct` |
| 作者 | `mason-yb-zhang` |
| 服务端包 | 1,816,979 字节 |

与 `weread` 是不同应用、不同作者身份。

## 2026-10-20 晚：0.1.2 与 -mode direct 补发（本机 masonzhang 令牌）

阻塞清除，三个应用全部改为 `-mode direct` 重新上架：

| 应用 | 新版本 | 二进制 | 说明 |
|---|---|---|---|
| weread | **0.1.2** | SHA `87974274…`（设备就地修复版，原样拉回） | `-mode direct`，服务端已确认（next=0.1.3） |
| radio | **0.1.1** | 本机重建 r1 未改头（`radio-store`） | `-mode direct`（next=0.1.2） |
| reader-lab | **0.1.1** | 设备原版 r1 二进制（换回未改头版） | `-mode direct`（next=0.1.2） |

发布命令模板（weread 用 payload，radio 用单文件）：

```bat
c1publish.exe -token-file D:\dev\c1-slim\tokens\masonzhang.token -allow-insecure-http ^
  -id weread -version <v> -name "微信读书" -entry bin/c1weread -payload .\payload -mode direct
```

教训：`-mode direct` 必须在**每次发布**时显式带上（服务端不记忆模式）；二进制保持 r1 原头，禁止改 e_flags。

## 2026-10-21：0.1.3 上架（本机 masonzhang 令牌）

| 项 | 值 |
|---|---|
| 版本 | **0.1.3** |
| 内容 | 9b0cb4a + 9ff41a5：reader-lab 字体栈/阅读设置移植 + 界面骨架/音量键翻页/MiSans/新晰黑正文字体 |
| 二进制 SHA256 | `31324800742292e9aa88dbd53d90831a19693a64de5d6116b1537ea502e1bd18`（payload/bin/c1weread） |
| 模式 | `-mode direct`（显式携带） |
| 服务端 | 已确认（next=0.1.4） |
