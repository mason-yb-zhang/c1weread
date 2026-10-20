# c1weread — C1-Slim 微信读书客户端

为 C1-Slim 墨水屏设备（296×152，1bpp）实现的微信读书阅读器。
书架/目录/阅读三段式，纯文本渲染（无图片），本地缓存整本书后离线阅读。

## 组成

| 路径 | 说明 |
|---|---|
| `weread/` | 纯协议包（无 UI，可在 Windows 上 `go test`）：`_e` 哈希、签名、分片解码、XHTML→文本、客户端、续期、扫码登录、官方 gateway |
| `cmd/c1weread` | 设备应用（同一源码在 Windows 用 host stub 编译可预览） |
| `cmd/weread-login` | PC 扫码登录工具（仅此工具依赖 qrterminal） |
| `build.bat` | mipsle 交叉编译 + manifest + Windows 版（不改 ELF 头） |
| `deploy.bat` | adb 推送二进制 / manifest / 凭证（local-apps 本地安装） |
| `PUBLISHED.md` | 商店版本、令牌归属、设备侧修复状态 |

## 一、设备 admit（一次性）

需要管理员 PowerShell：

```powershell
D:\dev\c1-slim\workspace\run-admit.ps1
```

然后在设备的「关于设备」页面 **5 秒内按 Enter 10 次**开启 adb。
验证：

```bat
adb shell id   rem 应输出 uid=0(root)
```

## 二、PC 登录生成凭证

设备端 v1 不支持扫码，凭证在 PC 上生成后 push 到设备：

```bat
out\weread-login-win.exe -out creds.json
```

1. 终端打印二维码，用**微信扫码**（微信读书公众号/App 内确认登录）。
2. 若手机提示 4 位验证码，在终端输入。
3. 成功后终端打印账号名，并写出 `creds.json`（含 cookie、官方 apikey、账号名、wr_fp 设备指纹）。

> 前提：微信读书 App 里已开通「我 → 设置 → 微信读书技能 → 获取 API Key」，否则拿不到 apikey。

## 三、构建与部署

```bat
build.bat    rem 产出 out\c1weread、out\20-weread.json、out\c1weread-win.exe、out\weread-login-win.exe
             rem 并整理 payload: payload\bin\c1weread + payload\assets\fonts\（运行时字体）
deploy.bat   rem adb push payload 到设备；本机有 creds.json 时一并推送
```

`deploy.bat` 推送 `payload/bin/c1weread` 与 `payload/assets/fonts/` 完成后，在设备的 local-apps 列表**按 R 刷新**，启动「微信读书」。

运行时字体目录：`/storage/c1/local-apps/apps/weread/assets/fonts/`。若缺失 Fusion12 / 霞鹜文楷，程序自动回退到内嵌 Unifont。

也可手动 push 凭证：

```bat
adb push creds.json /storage/c1/local-apps/apps/weread/data/creds.json
```

## 三·B、上架应用商店（必读）

商店安装路径与 local-apps **不是同一套启动链路**。`c1pkg` 按应用 ID 决定启动模式：

| 模式 | 来源 | 行为 |
|---|---|---|
| `direct` | `c1pkg_app_uses_direct_io(id)` 白名单，或包内 `.c1pkg-mode` | 启动器持有硬件租约后 exec，C1ancher **让出屏幕与输入** |
| `terminal` | 其余应用的默认值 | **不**拿独占租约；C1ancher 继续画终端页，并经 `terminal.c` 的 `PR_SET_PDEATHSIG, SIGHUP` 拉起 |

`weread` 不在核心白名单（`book-reader` / `pic` / `music-player` 等在）。不指定 mode 时商店会以 **terminal** 启动：
Go 默认收到 SIGHUP 即退 → 表现为「运行一会就闪退」；UI 还在刷屏，观感也像崩溃。
local-apps 启动器自己持租约、走薄 exec，没有这条 SIGHUP 路径，所以本地正常。

**发布时必须显式指定 `-mode direct`**（与 book-reader 相同），让安装目录写入 `.c1pkg-mode`：

```bat
c1publish.exe -token-file .\masonzhang.token -allow-insecure-http ^
  -id weread -version 0.1.x -name "微信读书" ^
  -entry bin/c1weread -payload .\payload -mode direct
```

payload 布局：`payload/bin/c1weread`（`out\c1weread` 拷入）+ 可选 `payload/assets/`。
同版本内容不可变：改了程序必须升版本号再发。不要把 `creds.json` 打进包。

### 商店 Publisher 身份（与 Git 作者不是一回事）

| 项 | 值 | 说明 |
|---|---|---|
| 应用 ID | `weread` | 首次发布后归作者独占，更新必须同 ID |
| 显示名 | `微信读书` | |
| Publisher 作者名 | `masonzhang` | 商店索引作者。**不是** Git 提交名 `mason-yb-zhang`，也不是参考应用的 `fwz233` |
| 令牌文件 | `masonzhang.token`（本地私密目录） | **绝不入库、不进 payload、不发聊天**。丢失无法找回 |
| 启动模式 | `direct` | 必选，否则 terminal 模式闪退（见上） |

**令牌 ≠ 目录名**：`~/.config/c1-reader-lab/publisher.token` 绑定的是 `mason-yb-zhang`，只能发新 ID，**不能**更新 `weread`（HTTP 403）。更新须用 `masonzhang` 令牌。

发布记录与设备侧修复状态见 [PUBLISHED.md](PUBLISHED.md)。

程序侧已 `signal.Ignore(SIGHUP)`，但那只是兜底；**商店包必须是带该修复的二进制，且 mode 为 direct**。

崩溃留痕：`/storage/c1/local-apps/apps/weread/data/c1weread.trace`（panic / TERM / INT）。

## 四、设备上操作

| 按键 | 作用 |
|---|---|
| ↑ / ↓ | 选择（书架/目录/设置/最近阅读）；阅读时翻页（跨章自动衔接） |
| ← / → | 调整设置项数值 |
| OK | 打开书 / 进入章节；阅读时切换全屏；同步失败时重试 |
| ← / Back | 返回目录 / 书架；设置屏返回阅读器；错误屏返回书架 |
| R | 书架进入/退出最近阅读 |
| S | 阅读器中进入/退出字体设置 |
| F | 阅读器中切换全屏（同 OK） |
| 音量+ / 音量- | 等同 ↓ / ↑：阅读时翻页，列表中选择（同 reader-lab） |

首次打开一本书会自动下载全部章节（每章间隔 200ms，屏上显示进度），
之后离线可读。下载中按 Back 取消。

## 四·B、界面骨架（移植自 reader-lab）

书架/目录/最近阅读三个列表视图共用一套骨架（对应 reader-lab `renderList`）：
顶栏为标题 + 数量与分隔线，底栏为倒显按键提示（有状态/消息时替换为消息文本）。
阅读器对应 reader-lab `renderReader`：

- **顶栏**：章节标题（书名·章名）左对齐，页码 `n/m` 与全书百分比右对齐，下加分隔线。
  百分比整本下载完成后按字节精确计算（`manifest.json` 记录 `totalBytes`），
  未整本下载的书退回按章节序号比例估算。
- **底栏**：倒显按键提示 `←目录 ↑↓翻页 OK全屏 S设置`。
- **全屏**：OK / F 切换，只画正文（顶部留 4px 边距，同 reader-lab），
  切换时重分页并保持当前页首字节偏移。
- **最近阅读**：书架按 R 进入，按最近阅读时间倒序（`progress.json` 记录 `updated`），
  OK 续读——章节齐全直达阅读器，缺章先补全再恢复；打开书时若有进度同样自动续读。

## 五、数据目录

`/storage/c1/local-apps/apps/weread/data/`

- `creds.json` — 凭证（PC 生成；应用 renewal 成功后会自动原子更新）
- `books/<bookId>/manifest.json` — 书名 + 章节表 + 全书总字节（下载完成时记录，顶栏百分比用）
- `books/<bookId>/<chapterIdx>_<chapterUid>.txt` — 章节纯文本
- `progress.json` — 每本书 `{chapterIdx, page, updated}` 阅读进度（updated 为最近阅读时间戳）
- `reader-settings.json` — 阅读字体/字号/阈值设置

环境变量 `C1WEREAD_DATA` 可覆盖数据目录（PC 预览测试用）。
环境变量 `C1WEREAD_FONTS` 可覆盖运行时字体目录（默认从 `/proc/self/exe` 相邻的 `../assets/fonts` 或 `./assets/fonts` 查找）。

## 六、字体与许可

阅读正文支持三种字体：

| 字体 | 来源 | 加载方式 | 字号支持 |
|---|---|---|---|
| MiSans | 小米澎湃 OS 系统字体（免费商用+允许嵌入） | `payload/assets/fonts/MiSans-Regular.ttf` | 12/14/16/18/20/24 px（默认） |
| Unifont 16 | GNU Unifont 子集 | `go:embed` 内嵌 | 12/14/16/18/20/24 px（缩放） |
| Fusion Pixel 12px | https://github.com/TakWolf/fusion-pixel-font | `payload/assets/fonts/fusion12.bin` | 原生 12 px |
| 霞鹜文楷 LXGW WenKai | https://github.com/lxgw/LxgwWenKai | `payload/assets/fonts/LXGWWenKaiLite-Regular.ttf` | 12/14/16/18/20/24 px |
| 霞鹜新晰黑 LXGW Neo XiHei | https://github.com/lxgw/LxgwNeoXiHei（IPA 许可） | `payload/assets/fonts/LXGWNeoXiHei.ttf` | 12/14/16/18/20/24 px |

- 字号档位：12 / 14 / 16 / 18 / 20 / 24 px；行高随字体真实 metrics。
- 黑白阈值 32–224（默认 128）仅影响 TTF（MiSans / 文楷 / 新晰黑）栅格化；点阵字体不受阈值影响。
- Fusion 原生点阵固定 12 px，选择该字体时字号锁定为 12。
- 运行时字体缺失时自动回退到内嵌 Unifont，并提示。
- **默认 MiSans**：小字号 1bpp 屏显实测最清晰（样张对比见开发记录 `out/fontcmp/`）。
  若部署包未带 MiSans 文件，首次启动回退 Unifont，设置里仍可手动切换。

许可证：

- 代码：`LICENSE`（GPL-3.0）；字体渲染栈与分页逻辑移植自 `reader-lab`（同许可）。
- 字体许可详见 `THIRD_PARTY_NOTICES.md` 与 `payload/assets/fonts/` 下的 `OFL*.txt`/`pkg-font-LICENSE.txt`。

## 七、PC 预览 / 开发

```bat
set GOPATH=D:\dev\c1-slim\workspace\gopath
set GOCACHE=D:\dev\c1-slim\workspace\gocache
set PATH=D:\dev\c1-slim\tools\go1.27.1\bin;%PATH%
set GOPROXY=https://goproxy.cn,direct

go vet ./... && go test ./...                    rem 协议单测 + 预览帧渲染
set C1WEREAD_DATA=%CD%\out\preview-data
go run ./cmd/c1weread                            rem host stub 下跑首屏（引导视图）
go test ./cmd/c1weread -run TestPreview          rem 生成 out\preview-*.frame（各 5624 字节）
```

预览帧转 PNG（需 Python）：

```bat
py -3 D:\dev\c1-slim\workspace\c1slim-toolkit\tools\frame2png.py out\preview-shelf.frame out\preview-shelf.png
```

## 八、协议说明（给维护者）

算法严格对照 `finlater/weread.koplugin` 参考实现（`scripts/fetch_weread_epub.py`），
单测向量由该 Python 实现批量生成对拍（`weread/testdata/vectors.json`）：

- `WereadE` — reader URL 与请求字段的 `_e` 哈希（MD5 前缀 + 数字/非数字分支编码）。
- `WereadSign` / `SortedQuery` — 内容请求签名：key 字典序、JS 值拼写、
  encodeURIComponent 式百分号编码（禁用 `url.QueryEscape`）。
- 分片解码 — 剥 32 字符大写 MD5 前缀校验 → e0+e1+e3（或 TXT 的 t0+t1）拼接 →
  丢首字符 → 尾字符推导换位表 → 成对逆交换 → base64url → base64 → 字节即 UTF-8 文本。
- 格式探测 — `e_0` 响应以 `{` 开头且含 `"bookId"` 即为 TXT 书，走 `t_0`/`t_1`。
- 会话 — `POST /web/login/renewal` 10 分钟冷却；`-2012` / `{}` 自动 renewal 重试一次。
- 书架/搜索/进度 — 官方 gateway `i.weread.qq.com/api/agent/gateway`，
  `Authorization: Bearer <apikey>`，`skill_version=1.0.5`。

## 九、已知限制

- 纯文本，无图片、无排版样式（表格/代码块退化为纯文本行）。
- 有声书（albums）只标注「不支持」，无音频能力。
- 阅读进度仅本地保存，不回传微信读书服务端。
- 依赖微信读书网页版接口与签名算法，官方改版后可能需要跟进更新。
- 下载整本书需要网络；逐章下载大书（几百章网文）需要数分钟。
