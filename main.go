package rtmp

import (
	"log"

	. "github.com/Monibuca/engine"
	. "github.com/logrusorgru/aurora"
)

var config = new(struct {
	ListenAddr string
})

func init() {
	InstallPlugin(&PluginConfig{
		Name:   "RTMP",
		Type:   PLUGIN_SUBSCRIBER | PLUGIN_PUBLISHER,
		Config: config,
		Run:    run,
	})
}
func run() {
	Print(Green("server rtmp start at"), BrightBlue(config.ListenAddr))
	log.Fatal(ListenRtmp(config.ListenAddr))
}
