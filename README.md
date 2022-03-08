# RTMP插件

## 插件地址

github.com/Monibuca/plugin-rtmp

## 插件引入
```go
import (
    _ "m7s.live/plugin/rtmp/v4"
)
```

## 默认插件配置

```yaml
rtmp
  tcp:
    listenaddr: :1935
    listennum: 0
  chunksize: 512
```

- listenaddr是监听的地址
- chunksize是输出分块大小

## 插件功能

### 接收RTMP协议的推流

例如通过ffmpeg向m7s进行推流

```bash
ffmpeg -i **** -f flv rtmp://localhost/live/test
```

会在m7s内部形成一个名为live/test的流

### 从m7s拉取rtmp协议流
如果m7s中已经存在live/test流的话就可以用rtmp协议进行播放
```bash
ffplay -i rtmp://localhost/live/test
```

### 从远端拉流到m7s
