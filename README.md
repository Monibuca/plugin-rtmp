# RTMP插件
rtmp插件提供rtmp协议的推拉流能力，以及向远程服务器推拉rtmp协议的能力。

## 仓库地址

https://github.com/Monibuca/plugin-rtmp

## 引入
```go
import _ "m7s.live/plugin/rtmp/v4"
```

## 推拉地址形式
```
rtmp://localhost/live/test
```
- `localhost`是m7s的服务器域名或者IP地址，默认端口`1935`可以不写，否则需要写
- `live`代表`appName`
- `test`代表`streamName`
- m7s中`live/test`将作为`streamPath`为流的唯一标识


例如通过ffmpeg向m7s进行推流

```bash
ffmpeg -i [视频源] -f flv rtmp://localhost/live/test
```

会在m7s内部形成一个名为live/test的流


如果m7s中已经存在live/test流的话就可以用rtmp协议进行播放
```bash
ffplay -i rtmp://localhost/live/test
```


## 配置

```yaml
rtmp:
    publish:
        pubaudio: true
        pubvideo: true
        kickexist: false
        publishtimeout: 10
        waitclosetimeout: 0
    subscribe:
        subaudio: true
        subvideo: true
        iframeonly: false
        waittimeout: 10
    tcp:
        listenaddr: :1935
        listennum: 0
    pull:
        repull: 0 # 当断开后是否自动重新拉流，0代表不进行重新拉流，-1代表无限次重新拉流
        pullonstart: false # 是否在m7s启动的时候自动拉流
        pullonsubscribe: false  # 是否在有人订阅的时候自动拉流（按需拉流）
        pulllist: {} # 拉流列表，以 streamPath为key，远程地址为value
    push:
        repush: 0 # 当断开后是否自动重新推流，0代表不进行重新推流，-1代表无限次重新推流
        pushlist: {} # 推流列表，以 streamPath为key，远程地址为value
    chunksize: 4096
```
:::tip 配置覆盖
publish
subscribe
两项中未配置部分将使用全局配置
:::

## API
无
