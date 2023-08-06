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
ffmpeg -i [视频源] -c:v h264 -c:a aac -f flv rtmp://localhost/live/test
```

会在m7s内部形成一个名为live/test的流


如果m7s中已经存在live/test流的话就可以用rtmp协议进行播放
```bash
ffplay -i rtmp://localhost/live/test
```


## 配置

```yaml
rtmp:
    publish: # 参考全局配置格式
    subscribe: # 参考全局配置格式
    tcp:
        listenaddr: :1935
        listenaddrtls: ""  # 用于RTMPS协议
        certfile: ""
        keyfile: ""
        listennum: 0
        nodelay: false
    pull: # 格式参考文档 https://m7s.live/guide/config.html#%E6%8F%92%E4%BB%B6%E9%85%8D%E7%BD%AE
    push: # 格式参考文档 https://m7s.live/guide/config.html#%E6%8F%92%E4%BB%B6%E9%85%8D%E7%BD%AE
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

### `rtmp/api/pull?target=[RTMP地址]&streamPath=[流标识]&save=[0|1|2]`
从远程拉取rtmp到m7s中
- save含义：0、不保存；1、保存到pullonstart；2、保存到pullonsub
- RTMP地址需要进行urlencode 防止其中的特殊字符影响解析
### `rtmp/api/push?target=[RTMP地址]&streamPath=[流标识]`
将本地的流推送到远端