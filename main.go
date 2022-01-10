package rtmp

import (
	"log"

	"github.com/Monibuca/engine/v3"
	. "github.com/Monibuca/utils/v3"
	. "github.com/logrusorgru/aurora"
)

var config = struct {
	ListenAddr string
	ChunkSize  int
}{":1935", 512}

func init() {
	pc := engine.PluginConfig{
		Name:   "RTMP",
		Config: &config,
	}
	pc.Install(run)
}
func run() {
	Print(Green("server rtmp start at"), BrightBlue(config.ListenAddr))
	log.Fatal(ListenTCP(config.ListenAddr, processRtmp))
}
