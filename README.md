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
        pubaudio: true # 是否发布音频流
        pubvideo: true # 是否发布视频流
        kickexist: false # 剔出已经存在的发布者，用于顶替原有发布者
        publishtimeout: 10s # 发布流默认过期时间，超过该时间发布者没有恢复流将被删除
        delayclosetimeout: 0 # 自动关闭触发后延迟的时间(期间内如果有新的订阅则取消触发关闭)，0为关闭该功能，保持连接。
        waitclosetimeout: 0 # 发布者断开后等待时间，超过该时间发布者没有恢复流将被删除，0为关闭该功能，由订阅者决定是否删除
        buffertime: 0 # 缓存时间，用于时光回溯，0为关闭缓存
    subscribe:
        subaudio: true # 是否订阅音频流
        subvideo: true # 是否订阅视频流
        subaudioargname: ats # 订阅音频轨道参数名
        subvideoargname: vts # 订阅视频轨道参数名
        subdataargname: dts # 订阅数据轨道参数名
        subaudiotracks: [] # 订阅音频轨道名称列表
        subvideotracks: [] # 订阅视频轨道名称列表
        submode: 0 # 订阅模式，0为跳帧追赶模式，1为不追赶（多用于录制），2为时光回溯模式
        iframeonly: false # 只订阅关键帧
        waittimeout: 10s # 等待发布者的超时时间，用于订阅尚未发布的流
    tcp:
        listenaddr: :1935
        listennum: 0
    pull:
        repull: 0 # 当断开后是否自动重新拉流，0代表不进行重新拉流，-1代表无限次重新拉流
        pullonstart: {} # 是否在m7s启动的时候自动拉流
        pullonsub: {}  # 是否在有人订阅的时候自动拉流（按需拉流）
    push:
        repush: 0 # 当断开后是否自动重新推流，0代表不进行重新推流，-1代表无限次重新推流
        pushlist: {} # 推流列表，以 streamPath为key，远程地址为value
    chunksize: 65536 # rtmp chunk size
    keepalive: false #保持rtmp连接，默认随着stream的close而主动断开
```
:::tip 配置覆盖
publish
subscribe
两项中未配置部分将使用全局配置
:::

## API
### `rtmp/api/list`
获取所有rtmp流

### `rtmp/api/pull?target=[RTMP地址]&streamPath=[流标识]`
从远程拉取rtmp到m7s中

### `rtmp/api/push?target=[RTMP地址]&streamPath=[流标识]`
将本地的流推送到远端