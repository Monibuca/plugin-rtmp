package rtmp

import (
	"context"

	"go.uber.org/zap"
	. "m7s.live/engine/v4"
	"m7s.live/engine/v4/config"
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
		if c.PullOnStart {
			for streamPath, url := range c.PullList {
				if err := plugin.Pull(streamPath, url, new(RTMPPuller), false); err != nil {
					plugin.Error("pull", zap.String("streamPath", streamPath), zap.String("url", url), zap.Error(err))
				}
			}
		}
	case config.Config:
		plugin.CancelFunc()
		if c.ListenAddr != "" {
			plugin.Context, plugin.CancelFunc = context.WithCancel(Engine)
			plugin.Info("server rtmp start at", zap.String("listen addr", c.ListenAddr))
			go c.Listen(plugin, c)
		}
	case SEpublish:
		for streamPath, url := range c.PushList {
			if streamPath == v.Stream.Path {
				if err := plugin.Push(streamPath, url, new(RTMPPusher), false); err != nil {
					plugin.Error("push", zap.String("streamPath", streamPath), zap.String("url", url), zap.Error(err))
				}
			}
		}
	case *Stream: //按需拉流
		if c.PullOnSubscribe {
			for streamPath, url := range c.PullList {
				if streamPath == v.Path {
					if err := plugin.Pull(streamPath, url, new(RTMPPuller), false); err != nil {
						plugin.Error("pull", zap.String("streamPath", streamPath), zap.String("url", url), zap.Error(err))
					}
					break
				}
			}
		}
	}
}
var conf = &RTMPConfig{
	ChunkSize: 4096,
	TCP:       config.TCP{ListenAddr: ":1935"},
}
var plugin = InstallPlugin(conf)
