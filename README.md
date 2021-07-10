# RTMP插件

## 插件地址

github.com/Monibuca/plugin-rtmp

## 插件引入
```go
import (
    _ "github.com/Monibuca/plugin-rtmp"
)
```

## 默认插件配置

```toml
[RTMP]
ListenAddr = ":1935"
ChunkSize = 512
```

- ListenAddr是监听的地址
- ChunkSize是分块大小

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