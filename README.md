# c1daily — C1-Slim 每日一读

为 C1-Slim 墨水屏设备（296×152，1bpp）实现的「每日一读」客户端：从读书网
（dushu.com）抓取每日美文 / 随机美文，本地缓存后离线阅读。纯文本渲染（无图片）。

姊妹项目：c1weread（微信读书客户端）。UI / 渲染 / 字体 / 分页栈移植自 c1weread
与 reader-lab。

## 组成

| 路径 | 说明 |
|---|---|
| `cmd/c1daily` | 设备应用（同一源码在 Windows 用 host stub 编译可预览） |
| `cmd/c1daily/dushu.go` | 读书网抓取（60s 超时 + 2 次重试，内嵌 Certum 证书池做 TLS 校验）与 x/net/html 解析 |
| `cmd/c1daily/testdata/` | 真实页面 fixture（today.html / random.html） |
| `build.bat` | mipsle 交叉编译 + manifest + Windows 版（不改 ELF 头） |
| `deploy.bat` | adb 推送二进制 / manifest / 运行时字体（local-apps 本地安装） |
| `assets/fonts/` | MiSans 字体源文件（payload staging 用，构建时复制） |

## 功能与按键

- **进应用直接读**：启动即载入/抓取今日文章并进入阅读器（有今日缓存秒开，
  离线可读；否则联网抓取 `/meiwen/`，抓取中 Back 可取消）。
- 阅读器按 **R** 随机一篇：抓取 `/meiwen/random/`（每次返回不同文章）并直接翻到它。
- 阅读器按 **←/Back** 退到缓存列表：按时间倒序展示缓存文章（上限 30 篇，
  LRU 淘汰），↑↓ 选择、OK 离线阅读；缓存列表再按 Back 退出应用。
- 抓取失败显示错误视图（OK 重试，←/Back 返回缓存列表）。
- 阅读器：顶栏标题·作者 + 页码 n/m，正文分页，底栏倒显按键提示。
- 按键（与 c1weread 一致）：↑↓ 选择/翻页，OK 打开/全屏，←/Back 返回，S 设置
  （字体 / 字号 / 阈值），R 随机一篇，F 全屏，音量键 = 方向键（长按连续翻页）。

| 视图 | 按键 |
|---|---|
| 启动 | 直接进今日文章（抓取中 Back 取消） |
| 阅读器 | ↑↓ 翻页；OK/F 全屏；R 随机一篇；S 设置；←/Back 缓存列表 |
| 设置 | ↑↓ 选择；←→ 调整；S/Back 返回阅读器 |
| 缓存列表 | ↑↓ 选择；OK 打开；Back 退出应用 |
| 错误 / 取消 | OK 重试；←/Back 返回缓存列表 |

## 数据目录

`/storage/c1/local-apps/apps/c1daily/data/`（可用环境变量 `C1DAILY_DATA` 覆盖，
用于 PC 预览测试）。

- `articles/<key>.json`：文章（title / author / paragraphs / fetchedAt / source）。
  今日文章 key = 本地日期 YYYY-MM-DD；随机文章 key = sha1(标题+首段) 前 16 字符。
- `index.json`：缓存列表元数据（key / title / author / fetchedAt / source），
  按 fetchedAt 倒序，超过 30 条按 LRU 淘汰（连同文章文件一起删除）。
- `reader-settings.json`：阅读设置（字体 / 字号 / 阈值）。
- `c1daily.trace`：崩溃 / 信号留痕。

运行时字体目录：`/storage/c1/local-apps/apps/c1daily/assets/fonts/`。缺失
Fusion12 / 霞鹜文楷 / MiSans 时自动回退内嵌 Unifont。

## 构建与部署

```bat
build.bat    rem 产出 out\c1daily、out\21-c1daily.json、out\c1daily-win.exe
             rem 并整理 payload: payload\bin\c1daily + payload\assets\fonts\（运行时字体）
deploy.bat   rem adb push payload 到设备
```

`deploy.bat` 推送完成后，在设备的 local-apps 列表**按 R 刷新**，启动「每日一读」。

## PC 预览 / 测试

```bat
set C1DAILY_DATA=%CD%\out\preview-data
go vet ./... && go test ./...
out\c1daily-win.exe   rem host stub 下打开平台是 no-op，仅用于编译验证
```

测试说明：fixture 解析测试使用 `testdata/` 下的真实页面快照（联网测试用
`C1DAILY_LIVE_TEST=1 go test -run TestFetchLive` 单独启用）。测试断言信息均为
ASCII，避免 Windows GBK 控制台打印中文崩溃。

## 已知限制

- 依赖读书网页面结构，站点改版会导致解析失败（错误视图可重试）。
- 站点偶发慢/超时：HTTP client 超时 60 秒，失败自动重试 2 次（间隔 2 秒）。
- 设备的 `/etc/ssl/certs` 为空（无系统 CA 根证书），应用内嵌 Certum 根 +
  中间证书自建 x509 pool 做 TLS 校验；站点换证书链时需更新
  `cmd/c1daily/assets/tls/` 下的 PEM。
- 无图片渲染，仅纯文本。
- 商店上架需另行处理（terminal 启动模式的 SIGHUP 问题，见 c1weread README
  「上架应用商店」一节；本地 local-apps 安装无此问题）。

## 许可

GPL-3.0（与 c1weread / reader-lab 一致，栈派生自 reader-lab）。
字体许可见 `THIRD_PARTY_NOTICES.md`。
