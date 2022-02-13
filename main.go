package rtmp

import (
	"context"
	"errors"

	. "github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/config"
	. "github.com/logrusorgru/aurora"
)

type RTMPConfig struct {
	config.Publish
	config.Subscribe
	config.TCP
	config.Pull
	config.Push
	ChunkSize int
}

func (config *RTMPConfig) Update(override config.Config) {
	plugin.Info(Green("server rtmp start at"), BrightBlue(config.ListenAddr))
	err := config.Listen(plugin, config)
	if err == context.Canceled {
		plugin.Println(err)
	} else {
		plugin.Fatal(err)
	}
}

var plugin = InstallPlugin(&RTMPConfig{
	ChunkSize: 4096,
	TCP:       config.TCP{ListenAddr: ":1935"},
})

func (config *RTMPConfig) PullStream(streamPath string, puller Puller) error {
	var client RTMPPuller
	client.Puller = puller
	if client.Publish(streamPath, &client, config.Publish) {
		return nil
	} else {
		return errors.New("publish faild")
	}
}

func (config *RTMPConfig) PushStream(stream *Stream, pusher Pusher) error {
	var client RTMPPusher
	client.ID = "RTMPPusher"
	client.Pusher = pusher
	if client.Subscribe(stream.Path,config.Subscribe) {
		client.Pusher.Push(&client, config.Push)
	}
	return nil
}
