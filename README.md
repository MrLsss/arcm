# arcm

`arcm` 是一个 Go 编写的小型命令行工具，用于在 Arc、Chrome、Edge 之间迁移书签。

支持三个浏览器作为来源或目标：

- **Arc**：读取/写入 `StorableSidebar.json` 的 pinned 侧边栏；多个 Space 时用 `--space` 选择；Top Apps 导出为名为 `top apps` 的文件夹。
- **Chrome**：通过 AppleScript 读取书签栏和其他书签；通过 Chromium `Bookmarks` 文件写入。
- **Edge**：通过 AppleScript 读取收藏夹栏和其他收藏夹；通过 Chromium `Bookmarks` 文件写入（实验支持）。

导入 Chrome/Edge 时按目标书签栏现有 URL 去重，并保持“文件夹在前、书签在后”；从 Arc 导出到 Chrome/Edge 时，Top Apps 会保留为一个 `top apps` 文件夹；把这份数据导回 Arc 时，该文件夹会被 Top Apps 去重规则拦截。

## 构建

仓库内自带便携 Go 工具链（`.toolchain/`，已被 gitignore），也可以使用系统 Go。解析 Netscape 书签 HTML 依赖系统 `xmllint`（macOS 自带 libxml2）：

```bash
export PATH="$PWD/.toolchain/go/bin:$PATH"
export GOCACHE="$PWD/.tmp/gocache"
export GOPATH="$PWD/.tmp/gopath"
go build -o arcm ./cmd/arcm
go test ./...
```

## 命令总览

```bash
arcm export <browser> [options]       # 导出到文件
arcm import <browser> [FILE] [options] # 从文件或浏览器导入
arcm <from-browser> <to-browser> [options] # 一步完成导出+导入
arcm verify [chrome|edge] [--profile-dir DIR]
arcm doctor
arcm preview
arcm help [命令]
arcm version
```

`<browser>` 可以是 `arc`、`chrome`、`edge`，大小写不敏感。

### 帮助

所有命令都有中文帮助：

```bash
./arcm help
./arcm help export
./arcm help import
./arcm help transfer
./arcm export --help
./arcm edge --help
```

### 导出

```bash
# Arc：pinned 固定标签 + 文件夹；Top Apps 放入名为 top apps 的文件夹
./arcm export arc
./arcm export arc --out my-arc.html --format html
./arcm export arc --no-topapps
./arcm export arc --space thebrowser.company.defaultPersonalSpaceID

# Chrome / Edge：书签栏 + 其他书签（浏览器必须在运行）
./arcm export chrome
./arcm export edge --out my-edge.html
./arcm export chrome --format json
```

默认输出：

- `arcm export arc` → `arc-bookmarks.html`
- `arcm export chrome` → `chrome-bookmarks.html`
- `arcm export edge` → `edge-bookmarks.html`

`--out -` 输出到标准输出。`--arc-dir`、`--space`、`--no-topapps` 只对 Arc 有效。输出到文件时先写入同目录临时文件，再原子替换目标文件。

Arc 导出结构：

- Arc 的 pinned 固定标签直接位于顶层，不额外创建 `Arc` 文件夹；
- Top Apps 放在单独的 `top apps` 文件夹，导入 Chrome/Edge 后保持为一个文件夹，不展开；
- `--no-topapps` 只导出 pinned 数据；
- 存在多个 Space 时必须用 `--space` 指定，不会默认合并所有 Space。

### 导入

`import` 必须指定目标浏览器，并且必须提供数据源：位置参数 `FILE` 或 `--source FILE|BROWSER`，二选一。

```bash
# 从文件导入
./arcm import chrome arc-bookmarks.html
./arcm import arc my-edge.html --restart

# 从另一个浏览器导入
./arcm import arc --source edge
./arcm import chrome --source arc

# 先 dry-run
./arcm import arc --source edge --dry-run
```

目标为 Arc 时：

- 默认增量：只添加 Arc 中尚不存在的 URL；现有 pinned 与 Top Apps 任一命中都会跳过，保留已有 pinned 项目；
- 空文件夹在目标不存在时创建；同名顶层文件夹按目标现有结构逐个合并，不会合并成一个；
- 写入前自动备份 `StorableSidebar.json`；
- `--append` 原始追加（可能重复）；
- `--restart` 先退出 Arc，写完再打开；
- `--force` 在 Arc 运行时写入（不安全，仅测试）。

目标为 Chrome/Edge 时：

- 写入 profile 的 `Bookmarks` 文件，放入书签栏；
- 默认按书签栏现有内容去重：URL 已存在（任意层级）就跳过，只导入新 URL；
- 只含重复 URL 的文件夹不创建，空文件夹保留；源数据中的 `top apps` 文件夹作为普通文件夹保留，不展开；
- 导入后文件夹排在书签前，所有层级都递归按此顺序排列；
- 写入前备份，备份文件名带纳秒时间戳，连续导入不会覆盖旧备份；
- 目标浏览器必须退出，`--force` 可跳过检查（仅测试）；
- `--profile-dir` 指定 profile 目录；
- 源和目标不能是同一个浏览器，同源导入会直接报错。

Arc 相关选项在来源或目标为 Arc 时可用：

- `--arc-dir DIR` 指定 Arc 数据目录；
- `--space ID` 指定 Arc Space；存在多个 Space 时必须指定；
- `--no-topapps` 来源为 Arc 时不导入 Top Apps。

`--dry-run` 对 Arc、Chrome、Edge 都可用，只打印将要执行的操作，不写入任何文件。

### 一步完成

```bash
./arcm edge arc              # Edge -> Arc
./arcm arc edge              # Arc -> Edge
./arcm arc chrome            # Arc -> Chrome
./arcm chrome arc            # Chrome -> Arc
./arcm edge chrome           # Edge -> Chrome
./arcm chrome edge           # Chrome -> Edge
```

一步命令不生成中间 HTML，复用同一套导出/导入逻辑。参数按来源/目标校验，例如 `--space` 和 `--no-topapps` 作用于 Arc 来源或目标，`--restart` 只在目标为 Arc 时可用。

## 校验

`arcm verify` 校验的是 Chromium `Bookmarks` 文件的 `checksum`，算法来自 Chromium `BookmarkCodec`：

- 顺序：`bookmark_bar`、`other`、`synced` 三个根，先序遍历；
- 每个节点：`id`（十进制字符串）+ `name`（UTF-16LE）+ `"url"`/`"folder"` + URL（仅链接）；
- MD5，小写十六进制。

该算法适用于所有使用 `BookmarkCodec` 的 Chromium 系浏览器。`verify` 的浏览器参数只接受 `chrome` 和 `edge`，用于选择默认 profile；校验其他 Chromium 浏览器时，传 `chrome` 或 `edge`，再用 `--profile-dir` 指向对应 profile。Arc 没有 Chromium `Bookmarks`，`arcm verify arc` 会报错。

```bash
./arcm verify chrome
./arcm verify edge
./arcm verify chrome --profile-dir /path/to/Brave/Default
```

`arcm verify` 在 checksum 不匹配时打印 `MISMATCH` 并以状态码 1 退出，脚本可以直接用退出码判定。

`TestChecksumMatchesChromiumFile` 用 Arc 自带的 Chromium `Bookmarks` 文件验证该实现。

## Edge 读写

macOS 上 Edge 的 profile 目录可能受系统保护，直接读 `Bookmarks` 会 `EPERM`。因此：

- **导出 Edge** 通过 Edge 自带的 AppleScript 书签接口（Edge 必须运行，首次会弹“自动化”权限）；
- **导入 Edge** 需要写 profile 的 `Bookmarks` 文件，必须退出 Edge，并给终端 Full Disk Access（或由有权限的终端执行）。已用临时 profile 验证写入、去重、排序和 checksum；当前环境直接读取真实 Edge profile 的 `Bookmarks` 文件仍被 TCC 拒绝（`operation not permitted`），`arcm arc edge` 的真实 profile 验收需要在有权限的环境完成。

如果 `osascript` 被拒绝，在“系统设置 → 隐私与安全性 → 自动化”中授权终端控制 Chrome/Edge。

## 目录

```
cmd/arcm/            CLI
internal/arc/        Arc StorableSidebar.json 读取/写入
internal/browser/    Chrome/Edge 共用的 AppleScript 书签读取
internal/netscape/   Netscape 书签 HTML 读写
internal/edge/       Edge 导出适配
internal/chrome/     Chrome 导出适配、Chromium Bookmarks JSON/checksum/导入
internal/model/      浏览器无关的书签树
```

## Chrome 155+ 的 `EncryptedBookmarks2`

Chrome 155 启动后可能在同目录生成 `EncryptedBookmarks2`（OSCrypt 加密镜像）。实测 macOS Chrome 155.0.8059.85：退出 Chrome 后写入 `Bookmarks`，Chrome 启动时会读取新内容并重建加密镜像；`EncryptedBookmarks2` 不需要手动删除或改写。不要只在 Chrome 运行时改文件（会被覆盖）。

## 已知限制

- 写 Chrome/Edge 前必须退出目标浏览器；否则文件会被运行中的浏览器覆盖。工具默认检测到目标浏览器运行会拒绝写入（`--force` 可跳过）。
- Chrome/Edge 导入只删除由 `arcm` 自己创建/记录的节点（`.arcm-state.json`）；用户自己创建的 `Arc`/`Arc Top Apps` 等同名文件夹不会被删除或展开。目标书签栏中已有的 URL（任意层级）会被视为已存在，不会重复创建。不要手动删除状态文件，否则下次导入可能重复。
- 目前只处理固定标签 + 文件夹 + Top Apps；Easel、归档不处理。
- 已验证 macOS Chrome 155 与 Edge 155 的导出；Edge 写入是新增实验能力，需在具备 profile 读写权限的环境验收。
- Arc 写回必须退出/重启 Arc；写入的是明文 `StorableSidebar.json`。
- Chrome/Edge 导出是只读 AppleScript，不包含“移动设备书签”；AppleScript 的写接口不提供书签创建。
