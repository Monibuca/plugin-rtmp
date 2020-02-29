# rtmpplugin
the rtmp protocol plugin for monibuca

实现了RTMP Server的基本功能，即接收来自OBS、ffmpeg等推流器的rtmp协议推流。
实现了RTMP协议的播放，可供rtmp协议播放器拉流播放。

## 插件名称

RTMP

## 配置
目前仅有的配置是监听的端口号

```toml
[Plugins.RTMP]
ListenAddr = ":1935"
```