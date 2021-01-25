package rtmp

import (
	. "github.com/Monibuca/engine/v3"
	. "github.com/Monibuca/utils/v3"
	. "github.com/logrusorgru/aurora"
	"log"
)

var config = struct {
	ListenAddr string
	ChunkSize  int
}{":1935", 512}

func init() {
	InstallPlugin(&PluginConfig{
		Name:   "RTMP",
		Config: &config,
		Run:    run,
	})
}
func run() {
	Print(Green("server rtmp start at"), BrightBlue(config.ListenAddr))
	log.Fatal(ListenRtmp(config.ListenAddr))
}
