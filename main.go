package rtmp

import (
	"context"
	"log"

	. "github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/util"
	. "github.com/logrusorgru/aurora"
)

type RTMPConfig struct {
	Publish    PublishConfig
	Subscribe  SubscribeConfig
	ListenAddr string
	ChunkSize  int
	context.Context
	cancel context.CancelFunc
}

var config = &RTMPConfig{
	Publish:    DefaultPublishConfig,
	Subscribe:  DefaultSubscribeConfig,
	ChunkSize:  4096,
	ListenAddr: ":1935",
}

func (cfg *RTMPConfig) Update(override map[string]any) {
	if cfg.cancel == nil || (override != nil && override["ListenAddr"] != nil) {
		start()
	}
}

func init() {
	InstallPlugin(config)
}

func start() {
	if config.cancel == nil {
		util.Print(Green("server rtmp start at"), BrightBlue(config.ListenAddr))
	} else {
		config.cancel()
		util.Print(Green("server rtmp restart at"), BrightBlue(config.ListenAddr))
	}
	config.Context, config.cancel = context.WithCancel(Ctx)
	err := util.ListenTCP(config.ListenAddr, config)
	if err == context.Canceled {
		log.Println(err)
	} else {
		log.Fatal(err)
	}
}
