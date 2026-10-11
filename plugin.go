// Package link 把聊天里出现的分享链接解析成音频/视频/图片/文本发回去。
package link

import (
	"github.com/KasumiYuku/Aurorix/lib/constant"
	"github.com/KasumiYuku/Aurorix/lib/event"
	"github.com/KasumiYuku/Aurorix/lib/plugin"

	"github.com/AurorixArhive/aurorix-link-plugin/internal/parse"
	_ "github.com/AurorixArhive/aurorix-link-plugin/internal/platform"
	"github.com/AurorixArhive/aurorix-link-plugin/internal/send"
)

const PluginID = "link"

func init() {
	engine := parse.NewEngine()
	cfg := newSettings(engine)
	pipeline := send.New(engine, cfg.Get)

	handle := func(ctx *event.Context, e *event.MessageEvent) { pipeline.Handle(ctx, e) }
	event.OnFor(PluginID, constant.GROUP_MESSAGE_CREATE, handle)
	event.OnFor(PluginID, constant.GROUP_AT_MESSAGE_CREATE, handle)
	event.OnFor(PluginID, constant.C2C_MESSAGE_CREATE, handle)

	plugin.Register(&plugin.Plugin{
		Id:          PluginID,
		Name:        "链接解析",
		Description: "直接发作品链接, 自动回音频/视频/图片/文本",
		TemplateFS:  templateFS,
		Config:      cfg.configFields(),
		ApplyConfig: cfg.Apply,
	})
}
