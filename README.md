# 链接解析

![Go](https://img.shields.io/badge/Go-1.26-blue)
![License](https://img.shields.io/badge/License-MIT-green)

> Aurorix 框架的链接解析插件
>
> 群里或私聊直接发作品链接，机器人自己认出来、解析、把音频 / 视频 / 图片 / 文案发回来。**不需要任何指令。**

## 快速开始

本插件运行在 [Aurorix](https://github.com/KasumiYuku/Aurorix) 实例中，请先按框架 README 完成安装：

```bash
git clone https://github.com/KasumiYuku/Aurorix.git
cd Aurorix
make install          # 安装脚手架 aurx（Windows 用 go install ./tools/aurx）
cd ..
aurx new mybot --framework Aurorix
```

然后在实例中导入本插件：

```bash
cd mybot
aurx add github.com/AurorixArhive/aurorix-link-plugin
```

本地开发时把本目录放进实例的 `plugins/`，再 `aurx add <实例module>/plugins/link-plugin`。

把机器人的 `appid` / `secret` 填入 `config.json` 后启动：

| 平台 | 链接样子 |
|---|---|
| 抖音 | `v.douyin.com/xxx`、`douyin.com/video/xxx` |
| 快手 | `v.kuaishou.com/xxx` |
| 豆瓣 | `douban.com/group/topic/...` |
| 堆糖 | `duitang.com/blog?id=...`、`duitang.com/atlas?id=...` |
| Lofter | `xxx.lofter.com/post/...` |
| 网易云音乐 | `music.163.com/#/song?id=...` |
| 酷狗音乐 | `t1.kugou.com/xxx`、`kugou.com/share/...` |
| 酷我音乐 | `kuwo.cn/play_detail/...` |
| 汽水音乐 | `qishui.douyin.com/s/...` |

待移植：B站、小红书、微博、知乎、贴吧、米游社、虎扑、小黑盒、QQ音乐等。

需要登录态才看得到内容的平台，在管理台填对应的 Cookie。
| 小黑盒 | `xiaoheihe.cn/...` |
| 网易云音乐 | `music.163.com/#/song?id=...` |
| QQ音乐 | `y.qq.com/n/ryqq/songDetail/...` |
| 酷狗音乐 | `t1.kugou.com/xxx`、`kugou.com/share/...` |
| 酷我音乐 | `kuwo.cn/play_detail/...` |
| 汽水音乐 | `qishui.douyin.com/s/...` |

需要登录态才看得到内容的平台，在管理台填对应的 Cookie。

## 平台

B站、抖音、快手、小红书、微博、知乎、百度贴吧、豆瓣、堆糖、LOFTER、米游社、虎扑、小黑盒、酷安、TapTap、5EPlay、完美世界电竞、大神、DS、ZLB、壁吧专楼吧、LINUX DO、BUFF、ILLU、豆包、网易云音乐、酷狗音乐、酷我音乐、汽水音乐

小红书、微博、知乎的部分内容要登录态才看得到，在管理台填对应的 Cookie 即可。

## 发送方式

- **音频**：3 分钟以内发语音，超过发文件。
- **视频**：优先把直链交给 QQ 自己去拉；拉不动就下到本机再上传，并记住这个平台一段时间内不再试直链。
- **图片**：按图发。
- **文案**：跟着卡片一起发；没有媒体时就是一条文本。

## 配置

管理台 → 插件 → 链接解析，改完立刻生效，不用重启。

| 配置项 | 默认 | 作用 |
|---|---|---|
| 关闭的平台 | 空 | 逗号分隔，中英文名都认。留空表示全开 |
| 群内冷却（秒） | 0 | 同一个群两次解析之间的最小间隔，0 表示不限制 |
| 同一链接去重（分钟） | 0 | 同一条链接这段时间内只解析一次，0 表示每次都解析 |
| 视频时长上限（分钟） | 10 | 超了只发卡片 |
| 视频体积上限（MB） | 30 | 超了只发卡片 |
| 发送视频本体 | 开 | 关掉就只发卡片和图片 |
| 各平台 Cookie | 空 | 需要登录态的平台才有这一项 |

## 结构

```
link-plugin/
├── plugin.go                装配: 解析引擎 → 发送管线 → 事件订阅 → 注册插件
├── config.go                配置项与运行期快照
├── templates/markdown/      用户可见文案(卡片 / 提示)
└── internal/
    ├── platform/<名字>/     每个平台一个包, 加平台 = 加一个包 + platform.go 一行导入
    ├── parse/               URL 匹配、路由注册表、短链/卡片/裸 BV 提取
    ├── model/               统一模型: 文本 / 图片 / 视频 / 音频 / 实况照片
    ├── netx/                HTTP 客户端与 JSON 解码
    ├── htmlx/               HTML 取文本与配图、数字与时间格式化
    ├── send/                投递与防刷
    ├── media/               视频下载与直链探测
    └── guard/               每平台限速、熔断、并发合并
```

## 开发

源码依赖框架，按 `go.mod` 里的相对引用检出：

```text
Projects/
└── Okamikko/            # go.work 工作区
    ├── Aurorix/         # github.com/KasumiYuku/Aurorix
    └── link-plugin/     # 本仓
```

```bash
go vet ./...
go test -short ./...        # 离线
go test -run TestLive -v ./ # 联网用例: 每个平台拉一次真实链接
```

`TestLivePlatforms` 里的链接是这套解析器的冒烟样本：上游改字段、加风控、换域名，它是最先响的那一个。

## 更新

本插件是标准 Go module，发新版靠 git tag。使用方一行升级：

```bash
aurx update aurorix-link-plugin    # 模块名可以只写一段
```

`aurx update` = `go get -u` + `go mod tidy` + 编译校验；跑完 `aurx run` 重启实例即可。

## 许可

代码采用 [MIT](LICENSE) 协议。

本插件为非官方插件，与各平台无关；内容版权归各权利人所有。
