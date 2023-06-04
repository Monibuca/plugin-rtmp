package rtmp

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
	. "m7s.live/engine/v4"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

type RTMPConfig struct {
	config.HTTP
	config.Publish
	config.Subscribe
	config.TCP
	config.Pull
	config.Push
	ChunkSize int
	KeepAlive bool //保持rtmp连接，默认随着stream的close而主动断开
}

func pull(streamPath, url string) {
	if err := RTMPPlugin.Pull(streamPath, url, new(RTMPPuller), 0); err != nil {
		RTMPPlugin.Error("pull", zap.String("streamPath", streamPath), zap.String("url", url), zap.Error(err))
	}
}
func (c *RTMPConfig) OnEvent(event any) {
	switch v := event.(type) {
	case FirstConfig:
		for streamPath, url := range c.PullOnStart {
			pull(streamPath, url)
		}
	case config.Config:
		RTMPPlugin.CancelFunc()
		if c.TCP.ListenAddr != "" {
			RTMPPlugin.Context, RTMPPlugin.CancelFunc = context.WithCancel(Engine)
			RTMPPlugin.Info("server rtmp start at", zap.String("listen addr", c.TCP.ListenAddr))
			go c.ListenTCP(RTMPPlugin, c)
		}
	case SEpublish:
		if url, ok := c.PushList[v.Target.Path]; ok {
			if err := RTMPPlugin.Push(v.Target.Path, url, new(RTMPPusher), false); err != nil {
				RTMPPlugin.Error("push", zap.String("streamPath", v.Target.Path), zap.String("url", url), zap.Error(err))
			}
		}
	case *Stream: //按需拉流
		if url, ok := c.PullOnSub[v.Path]; ok {
			pull(v.Path, url)
		}
	}
}

var conf = &RTMPConfig{
	ChunkSize: 65536,
	TCP:       config.TCP{ListenAddr: ":1935"},
}

var RTMPPlugin = InstallPlugin(conf)

func filterStreams() (ss []*Stream) {
	Streams.Range(func(key string, s *Stream) {
		switch s.Publisher.(type) {
		case *RTMPReceiver, *RTMPPuller:
			ss = append(ss, s)
		}
	})
	return
}

func (*RTMPConfig) API_list(w http.ResponseWriter, r *http.Request) {
	util.ReturnJson(filterStreams, time.Second, w, r)
}

func (*RTMPConfig) API_Pull(rw http.ResponseWriter, r *http.Request) {
	save, _ := strconv.Atoi(r.URL.Query().Get("save"))
	err := RTMPPlugin.Pull(r.URL.Query().Get("streamPath"), r.URL.Query().Get("target"), new(RTMPPuller), save)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
	} else {
		rw.Write([]byte("ok"))
	}
}

func (*RTMPConfig) API_Push(rw http.ResponseWriter, r *http.Request) {
	err := RTMPPlugin.Push(r.URL.Query().Get("streamPath"), r.URL.Query().Get("target"), new(RTMPPusher), r.URL.Query().Has("save"))
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
	} else {
		rw.Write([]byte("ok"))
	}
}
