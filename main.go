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

var _ PullPlugin = (*RTMPConfig)(nil)

func (config *RTMPConfig) Update(override config.Config) {
	plugin.Info("server rtmp start at", zap.String("listen addr", config.ListenAddr))
	err := config.Listen(plugin, config)
	if err == context.Canceled {
		plugin.Info("rtmp listen shutdown")
	} else {
		plugin.Fatal("rtmp server", zap.Error(err))
	}
}

var plugin = InstallPlugin(&RTMPConfig{
	ChunkSize: 4096,
	TCP:       config.TCP{ListenAddr: ":1935"},
})

func (config *RTMPConfig) PullStream(puller Puller) {
	client := RTMPPuller{
		Puller: puller,
	}
	client.OnEvent(PullEvent(0))
}

func (config *RTMPConfig) PushStream(pusher Pusher) {
	client := RTMPPusher{
		Pusher: pusher,
	}
	client.OnEvent(PushEvent(0))
}
