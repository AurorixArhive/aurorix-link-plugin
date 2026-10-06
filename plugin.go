// Package video 把聊天里出现的分享链接解析成视频/图文发回去。
//
// 这里只做装配: 请求客户端 → 解析内核 → 发送管线 → 事件订阅 → 注册插件。
// 加一个平台 = internal/parse 里加一个文件 + 在 parse.New 的注册表里加一行, 本文件不用动。
package video

import (
	"github.com/KasumiYuku/Aurorix/lib/constant"
	"github.com/KasumiYuku/Aurorix/lib/event"
	"github.com/KasumiYuku/Aurorix/lib/plugin"
	"github.com/KasumiYuku/Aurorix/lib/requests"

	"video-plugin/internal/parse"
	"video-plugin/internal/send"
	"video-plugin/internal/transport"
)

const PluginID = "video"

func init() {
	cfg := newSettings()
	client := requests.Init(20)
	pipeline := send.New(parse.New(transport.HTTP{Client: client}, cookieOf(cfg)), cfg.Get)

	// 无感: 群里任何人发链接就解析, 单聊同样。@ 机器人 发链接也认。
	handle := func(ctx *event.Context, e *event.MessageEvent) { pipeline.Handle(ctx, e) }
	event.OnFor(PluginID, constant.GROUP_MESSAGE_CREATE, handle)
	event.OnFor(PluginID, constant.GROUP_AT_MESSAGE_CREATE, handle)
	event.OnFor(PluginID, constant.C2C_MESSAGE_CREATE, handle)

	plugin.Register(&plugin.Plugin{
		Id:          PluginID,
		Name:        "视频解析",
		Description: "直接发抖音/快手/B站/小红书等 13 个平台的分享链接, 自动回视频与图文",
		TemplateFS:  templateFS,
		Config:      configFields(),
		ApplyConfig: cfg.Apply,
	})
}

// cookieOf 把配置里的登录态接给解析内核。做成函数而不是快照: 管理台改完, 下一次解析就用新的。
func cookieOf(cfg *settings) parse.Creds {
	return func(platform string) string { return cfg.Get().Cookie(platform) }
}
