package rtmp

import (
	"context"

	. "github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/config"
	"go.uber.org/zap"
)

type RTMPConfig struct {
	config.Publish
	config.Subscribe
	config.TCP
	config.Pull
	config.Push
	ChunkSize int
}

func (c *RTMPConfig) OnEvent(event any) {
	switch v := event.(type) {
	case FirstConfig:
		if c.ListenAddr != "" {
			plugin.Info("server rtmp start at", zap.String("listen addr", c.ListenAddr))
			go c.Listen(plugin, c)
		}
	case config.Config:
		plugin.CancelFunc()
		if c.ListenAddr != "" {
			plugin.Context, plugin.CancelFunc = context.WithCancel(Engine)
			plugin.Info("server rtmp start at", zap.String("listen addr", c.ListenAddr))
			go c.Listen(plugin, c)
		}
	case Puller:
		client := RTMPPuller{
			Puller: v,
		}
		client.OnEvent(PullEvent(0))
	case Pusher:
		client := RTMPPusher{
			Pusher: v,
		}
		client.OnEvent(PushEvent(0))
	}
}

var plugin = InstallPlugin(&RTMPConfig{
	ChunkSize: 4096,
	TCP:       config.TCP{ListenAddr: ":1935"},
})