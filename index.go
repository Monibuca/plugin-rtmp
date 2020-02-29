package rtmpplugin

import (
	"log"

	. "github.com/Monibuca/engine"
)

var config = new(ListenerConfig)

func init() {
	InstallPlugin(&PluginConfig{
		Name:    "RTMP",
		Type:    PLUGIN_SUBSCRIBER | PLUGIN_PUBLISHER,
		Config:  config,
		Version: "1.0.0",
		Run:     run,
	})
}
func run() {
	log.Printf("server rtmp start at %s", config.ListenAddr)
	log.Fatal(ListenRtmp(config.ListenAddr))
}
