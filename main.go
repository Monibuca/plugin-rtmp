package rtmp

import (
	"context"
	"log"

	. "github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/util"
	. "github.com/logrusorgru/aurora"
)

type RTMPConfig struct {
	Publish   PublishConfig
	Subscribe SubscribeConfig
	TCPConfig
	ChunkSize int
	context.Context
	cancel context.CancelFunc
}

var config = &RTMPConfig{
	Publish:   DefaultPublishConfig,
	Subscribe: DefaultSubscribeConfig,
	ChunkSize: 4096,
	TCPConfig: TCPConfig{ListenAddr: ":1935"},
}

func (cfg *RTMPConfig) Update(override Config) {
	override.Unmarshal(cfg)
	if config.cancel == nil {
		util.Print(Green("server rtmp start at"), BrightBlue(config.ListenAddr))
	} else if override.Has("ListenAddr") {
		config.cancel()
		util.Print(Green("server rtmp restart at"), BrightBlue(config.ListenAddr))
	} else {
		return
	}
	config.Context, config.cancel = context.WithCancel(Ctx)
	err := cfg.Listen(cfg)
	if err == context.Canceled {
		log.Println(err)
	} else {
		log.Fatal(err)
	}
}

func init() {
	InstallPlugin(config)
}
