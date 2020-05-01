package rtmp

import (
	"log"

	. "github.com/Monibuca/engine/v2/v2"
	. "github.com/logrusorgru/aurora"
)

var config = struct {
	ListenAddr string
	ChunkSize  int
}{":1935", 512}

func init() {
	InstallPlugin(&PluginConfig{
		Name:   "RTMP",
		Type:   PLUGIN_SUBSCRIBER | PLUGIN_PUBLISHER,
		Config: &config,
		Run:    run,
	})
}
func run() {
	Print(Green("server rtmp start at"), BrightBlue(config.ListenAddr))
	log.Fatal(ListenRtmp(config.ListenAddr))
}
