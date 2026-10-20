package main

import (
	"fmt"
	"io"
	"os"
)

// cmdHelp 打印全局或某个命令的中文帮助。
func cmdHelp(args []string) int {
	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "arcm: 帮助命令最多接收一个主题\n\n")
		printGlobalHelp(os.Stderr)
		return 2
	}
	topic := ""
	if len(args) == 1 {
		topic = args[0]
	}
	if topic == "" || topic == "help" {
		printGlobalHelp(os.Stdout)
		return 0
	}
	if printCommandHelp(topic, os.Stdout) {
		return 0
	}
	fmt.Fprintf(os.Stderr, "arcm: 未知帮助主题 %q\n\n", topic)
	printGlobalHelp(os.Stderr)
	return 2
}

func printGlobalHelp(w io.Writer) {
	fmt.Fprint(w, `arcm - Arc / Chrome / Edge 书签迁移工具

用法:
  arcm export <浏览器> [选项]          导出指定浏览器的书签到文件
  arcm import <浏览器> [文件] [选项]   从文件或另一个浏览器导入书签
  arcm <来源浏览器> <目标浏览器> [选项]  一步完成导出和导入
  arcm verify [chrome|edge] [选项]     校验 Chromium Bookmarks 文件的 checksum
  arcm doctor [选项]                   查看 Arc 数据文件状态
  arcm preview [选项]                  预览 Arc 侧边栏结构
  arcm help [命令]                     显示帮助（本命令）
  arcm version                         显示版本

浏览器:
  arc     Arc 侧边栏的固定标签、文件夹；Top Apps 导出为 top apps 文件夹
  chrome  Chrome 书签栏和其他书签
  edge    Edge 收藏夹栏和其他收藏夹

常用示例:
  arcm export arc                      导出 Arc 到 arc-bookmarks.html
  arcm export chrome --out chrome.html 导出 Chrome 书签
  arcm import arc edge-bookmarks.html  把文件导入 Arc
  arcm import arc --source edge        把 Edge 书签导入 Arc
  arcm edge arc                        一步：Edge -> Arc
  arcm arc chrome                      一步：Arc -> Chrome
  arcm verify edge                     校验 Edge 的 Bookmarks checksum
  arcm help export                     查看 export 的完整选项

说明:
  - Chrome/Edge 的导出使用 AppleScript，浏览器必须正在运行，并已授予终端“自动化”权限。
  - 写入 Chrome/Edge 前必须先退出浏览器；Arc 写入前建议使用 --restart。
  - 默认导入为增量模式，不会清空 Arc 已有固定标签。
  - 所有“来源”和“目标”都不能是同一个浏览器。
`)
}

func printCommandHelp(name string, w io.Writer) bool {
	switch name {
	case "export":
		fmt.Fprint(w, `arcm export <浏览器> [选项]

把指定浏览器的书签导出为 Netscape Bookmark HTML 或 arcm JSON。

浏览器:
  arc     导出 Arc 的固定标签、文件夹；Top Apps 放入名为 top apps 的文件夹
  chrome  导出 Chrome 书签栏（以及其他书签，如果存在）；Chrome 需运行
  edge    导出 Edge 收藏夹栏（以及其他收藏夹，如果存在）；Edge 需运行

选项:
  --out FILE        输出文件；- 表示标准输出；默认 <浏览器>-bookmarks.html
  --format FORMAT   输出格式：html（默认）或 json
  --arc-dir DIR     Arc 数据目录，仅 arc 可用
  --space ID        Arc Space ID；存在多个 Space 时必须指定
  --no-topapps      不导出 Arc 的 top apps 文件夹，仅 arc 可用

示例:
  arcm export arc
  arcm export arc --out my-arc.html
  arcm export arc --no-topapps
  arcm export arc --space thebrowser.company.defaultPersonalSpaceID
  arcm export chrome --out chrome.html
  arcm export edge --format json --out edge.json
  arcm export chrome --out - > chrome.html
`)
		return true
	case "import":
		fmt.Fprint(w, `arcm import <目标浏览器> [文件] [选项]

把书签导入到目标浏览器。数据源必须是以下二选一：
  1. 位置参数 [文件]：Netscape HTML 或 arcm JSON
  2. --source FILE|BROWSER：文件或来源浏览器（arc、chrome、edge）

目标浏览器:
  arc     写入 Arc 侧边栏；默认增量，跳过现有 pinned/Top Apps 中已有的 URL
  chrome  写入 Chrome 的 Bookmarks 文件，放入书签栏；按现有书签去重
  edge    写入 Edge 的 Bookmarks 文件，放入收藏夹栏；按现有书签去重（实验支持）

选项:
  --source SOURCE       书签文件或来源浏览器（arc|chrome|edge）
  --arc-dir DIR         Arc 数据目录；来源或目标为 arc 时可用
  --space ID            Arc Space ID；来源或目标为 Arc 且存在多个 Space 时必须指定
  --profile-dir DIR     目标 Chromium profile 目录（含 Bookmarks）
  --no-topapps          来源为 Arc 时不导入 top apps 文件夹
  --append              Arc：保留已有项并追加，可能产生重复
  --dry-run             只显示将要做什么，不写文件
  --force               目标浏览器运行时仍强制写入（仅测试）
  --restart             Arc：先退出，写入后重新打开

说明:
  - 目标为 Chrome/Edge 时必须先退出浏览器；否则默认拒绝写入。
  - 目标为 Arc 时默认增量；现有 pinned 或 Top Apps 中已有的 URL 不会重复添加。
  - 目标为 Chrome/Edge 时按书签栏现有 URL（任意层级）去重，只导入新 URL；
    只含重复 URL 的文件夹不创建，空文件夹保留；top apps 文件夹整体保留，不展开。
  - 所有目标导入后都先放文件夹，再放书签。
  - 导入 Chrome/Edge 时只管理 arcm 自己写入的节点，不会删除其他书签。

示例:
  arcm import arc edge.html
  arcm import arc --source edge
  arcm import chrome arc-bookmarks.html
  arcm import chrome --source arc
  arcm import arc --source edge --dry-run
  arcm import arc --source edge --restart
`)
		return true
	case "transfer":
		fmt.Fprint(w, `arcm <来源浏览器> <目标浏览器> [选项]

一步完成“导出 + 导入”，不生成中间文件。

支持的方向:
  arcm edge arc
  arcm arc edge
  arcm arc chrome
  arcm chrome arc
  arcm chrome edge
  arcm edge chrome

选项与 import 相同，例如:
  --arc-dir DIR          Arc 数据目录
  --space ID             Arc Space ID；Arc 参与且存在多个 Space 时必须指定
  --profile-dir DIR      目标 Chromium profile 目录
  --dry-run              只预览
  --force                目标浏览器运行时仍写入（仅测试）
  --restart              Arc：退出后写入，再重新打开
  --append               Arc：追加而不是增量合并

说明:
  - 从 Arc 导出时，pinned 数据直接位于顶层，Top Apps 放在 top apps 文件夹。
  - 导入 Chrome/Edge 时保留 top apps 文件夹；导入 Arc 时该文件夹会被去重规则拦截。

示例:
  arcm edge arc
  arcm arc chrome --dry-run
  arcm edge arc --restart
  arcm chrome edge --profile-dir ~/tmp/edge-profile --force
`)
		return true
	case "verify":
		fmt.Fprint(w, `arcm verify [chrome|edge] [选项]

校验 Chromium Bookmarks 文件里的 checksum。校验不匹配时以状态码 1 退出。

Chromium 系浏览器（Chrome、Edge、Brave、Vivaldi、Opera 等）使用同一套
BookmarkCodec checksum 算法，因此都可以用本命令校验；用 --profile-dir
指向对应 profile 即可。Arc 不使用 Chromium Bookmarks，不支持此命令。

选项:
  --profile-dir DIR   Chromium profile 目录（含 Bookmarks）；默认取浏览器默认目录

示例:
  arcm verify chrome
  arcm verify edge
  arcm verify chrome --profile-dir /path/to/Chromium/Default
`)
		return true
	case "doctor":
		fmt.Fprint(w, `arcm doctor [选项]

检查 Arc 的 StorableSidebar.json 是否存在、版本、路径、大小，以及每个
Space 的固定标签、文件夹数量和全局 Top Apps、Easel 的数量。

选项:
  --arc-dir DIR   Arc 数据目录
`)
		return true
	case "preview":
		fmt.Fprint(w, `arcm preview [选项]

以文本形式预览 Arc 侧边栏的标签和文件夹结构。

选项:
  --arc-dir DIR   Arc 数据目录
  --no-topapps    不显示 Top Apps
`)
		return true
	case "version":
		fmt.Fprint(w, "arcm version\n\n显示 arcm 版本。\n")
		return true
	default:
		return false
	}
}
