# rtmpplugin
the rtmp protocol plugin for monibuca

实现了RTMP Server的基本功能，即接收来自OBS、ffmpeg等推流器的rtmp协议推流。
实现了RTMP协议的播放，可供rtmp协议播放器拉流播放。

## 插件名称

RTMP

## 配置

```toml
[RTMP]
FirstScreen = false
ListenAddr = ":1935"
```

- FirstScreen 代表是否打开首屏秒开
- ListenAddr 代表监听的端口号