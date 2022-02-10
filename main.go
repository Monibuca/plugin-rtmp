package rtmp

import (
	"context"
	"log"

	. "github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/config"
	. "github.com/logrusorgru/aurora"
)

type RTMPConfig struct {
	config.Publish
	config.Subscribe
	config.TCP
	ChunkSize int
}

func (config *RTMPConfig) Update(override config.Config) {
	plugin.Infoln(Green("server rtmp start at"), BrightBlue(config.ListenAddr))
	err := config.Listen(plugin, config)
	if err == context.Canceled {
		log.Println(err)
	} else {
		log.Fatal(err)
	}
}

var plugin = InstallPlugin(&RTMPConfig{
	ChunkSize: 4096,
	TCP:       config.TCP{ListenAddr: ":1935"},
})
