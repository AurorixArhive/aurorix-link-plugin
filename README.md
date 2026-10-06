# video-plugin

给 [Aurorix](https://github.com/KasumiYuku/Aurorix) 用的视频解析插件:群里或私聊直接发分享链接,机器人自己认出来、解析、把视频/图集/文案发回来。**不需要任何指令。**

## 支持哪些链接

| 平台 | 认得的链接 | 发回来的东西 |
|---|---|---|
| 抖音 | `v.douyin.com/xxx`、`douyin.com/video/xxx`、`iesdouyin.com/share/video/xxx` | 无水印视频,或图集 + 文案 |
| 快手 | `v.kuaishou.com/xxx`、`kuaishou.com/short-video/xxx` | 视频,或图集 + 文案 |
| B站 | `bilibili.com/video/BV...`、`b23.tv/xxx`、`av123`、直接发裸 BV 号 | 视频 + 标题 / UP主 / 时长 / 播放量 |
| 小红书 | `xhslink.com/xxx`、`xiaohongshu.com/explore/xxx` | 笔记图集或视频 + 正文 |
| 小黑盒 | `xiaoheihe.cn` 的分享链接与站内链接 | 帖子图文 + 正文 |

从 App 里点"分享到 QQ"发出来的卡片也认 —— 链接藏在卡片里,插件会把它取出来。

## 安装

```bash
cd 你的机器人实例
aurx add github.com/KasumiYuku/video-plugin
aurx run
```

本地开发时把本目录放进实例的 `plugins/` 再 `aurx add <实例module>/plugins/video-plugin`。

## 配置

管理台 → 插件 → 视频解析,改完立刻生效,不用重启。

| 配置项 | 默认 | 作用 |
|---|---|---|
| 群内冷却(秒) | 10 | 同一个群/单聊两次解析之间的最小间隔 |
| 同一链接去重(分钟) | 10 | 同一条链接在这段时间内只解析一次,别人再发不会重复刷屏 |
| 视频时长上限(分钟) | 10 | 超过就只发卡片,不发视频本体 |
| 视频体积上限(MB) | 50 | 同上;QQ 上传大文件容易超时,所以比平台上限更保守 |
| 发送视频本体 | 开 | 关掉后只发卡片和标题,省流量 |
| 关闭的平台 | 空 | 填平台名,逗号分隔,例如 `快手,小黑盒`;留空表示五个平台全开 |

## 行为细节

- 群里任何人发链接就解析,**不带前缀、不用 @ 机器人**;@ 机器人发链接也认
- 回复引用原消息,方便对上是谁发的那条链接
- 认不出链接的消息**完全静默** —— 聊什么都能触发的机器人只会被踢
- 解析失败会回一句能看懂的话;平台报的原始错误码不会甩给用户
- 视频超限、下载失败都降级成"卡片 + 标题链接",不会让用户等半天然后没反应

## 已知限制

- 小红书的分享链接里带 `xsec_token`,token 过期或笔记私密时会解析不到,这时重新复制一次分享链接即可
- 小黑盒的接口有风控,个别帖子会取不到正文
- B站取的是 `platform=html5` 的渐进式 MP4:单文件、免合流、体积小,代价是清晰度不高
- 卡片消息的解析依赖 Aurorix 透出 `ark_data`(见框架 `lib/structers`);框架版本过旧时只能认粘贴出来的文本链接
- 直播间、付费内容、需要登录才能看的作品不在支持范围内

## 结构

```
video-plugin/
├── plugin.go                    装配: 请求客户端 → 解析内核 → 发送管线 → 事件订阅
├── config.go                    管理台配置项与生效逻辑
├── templates/markdown/          所有用户可见文案
└── internal/
    ├── detect/                  从一段文字或卡片里认出平台与链接
    ├── model/                   解析产物(视频 / 图集 / 文案)
    ├── parse/                   五个平台各一个文件, 共用一套 HTML 取值工具
    ├── media/                   视频落盘与体积探测
    ├── send/                    防刷、降级与发送
    └── transport/               把框架的 HTTP 客户端接到解析内核上
```

解析内核(`detect` / `model` / `parse`)不 import 框架,换框架、写测试都不用动它。加一个平台 = `internal/parse` 里加一个文件 + 在 `parse.New` 的注册表里加一行。

## 测试

```bash
go test ./...          # 全部离线用例: 链接识别、各平台样本解析、防刷、配置生效
VIDEO_LIVE=1 go test ./internal/transport/ -v   # 真实网络用例, 验"现在到底通不通"
```

## 许可

MIT
