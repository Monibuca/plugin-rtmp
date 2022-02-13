package rtmp

import (
	"bufio"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/util"
)

var gstreamid = uint32(64)

func (config *RTMPConfig) ServeTCP(conn *net.TCPConn) {
	nc := NetConnection{
		TCPConn:            conn,
		Reader:             bufio.NewReader(conn),
		writeChunkSize:     RTMP_DEFAULT_CHUNK_SIZE,
		readChunkSize:      RTMP_DEFAULT_CHUNK_SIZE,
		rtmpHeader:         make(map[uint32]*ChunkHeader),
		incompleteRtmpBody: make(map[uint32]util.Buffer),
		bandwidth:          RTMP_MAX_CHUNK_SIZE << 3,
		tmpBuf:             make([]byte, 4),
		subscribers:        make(map[uint32]*engine.Subscriber),
	}
	defer nc.Close()
	/* Handshake */
	if err := nc.Handshake(); err != nil {
		plugin.Error("handshake", err)
		return
	}
	for {
		if msg, err := nc.RecvMessage(); err == nil {
			if msg.MessageLength <= 0 {
				continue
			}
			switch msg.MessageTypeID {
			case RTMP_MSG_AMF0_COMMAND:
				if msg.MsgData == nil {
					break
				}
				cmd := msg.MsgData.(Commander).GetCommand()
				plugin.Debugf("recv cmd '%s'", cmd.CommandName)
				switch cmd.CommandName {
				case "connect":
					connect := msg.MsgData.(*CallMessage)
					app := connect.Object["app"]                       // 客户端要连接到的服务应用名
					objectEncoding := connect.Object["objectEncoding"] // AMF编码方法
					if objectEncoding != nil {
						nc.objectEncoding = objectEncoding.(float64)
					}
					nc.appName = app.(string)
					plugin.Infof("connect app:'%s',objectEncoding:%v", nc.appName, objectEncoding)
					err = nc.SendMessage(RTMP_MSG_ACK_SIZE, Uint32Message(512<<10))
					nc.writeChunkSize = config.ChunkSize
					err = nc.SendMessage(RTMP_MSG_CHUNK_SIZE, Uint32Message(config.ChunkSize))
					err = nc.SendMessage(RTMP_MSG_BANDWIDTH, &SetPeerBandwidthMessage{
						AcknowledgementWindowsize: uint32(512 << 10),
						LimitType:                 byte(2),
					})
					err = nc.SendStreamID(RTMP_USER_STREAM_BEGIN)
					err = nc.SendCommand(SEND_CONNECT_RESPONSE_MESSAGE, nc.objectEncoding)
				case "createStream":
					nc.streamID = atomic.AddUint32(&gstreamid, 1)
					plugin.Info("createStream:", nc.streamID)
					err = nc.SendCommand(SEND_CREATE_STREAM_RESPONSE_MESSAGE, cmd.TransactionId)
					if err != nil {
						plugin.Error(err)
						return
					}
				case "publish":
					pm := msg.MsgData.(*PublishMessage)
					var puber engine.Publisher
					if puber.Publish(nc.appName+"/"+pm.PublishingName, &nc, config.Publish) {
						nc.MediaReceiver = NewMediaReceiver(&puber)
						nc.SendStreamID(RTMP_USER_STREAM_BEGIN)
						err = nc.SendCommand(SEND_PUBLISH_START_MESSAGE, newPublishResponseMessageData(nc.streamID, NetStream_Publish_Start, Level_Status))
					} else {
						err = nc.SendCommand(SEND_PUBLISH_RESPONSE_MESSAGE, newPublishResponseMessageData(nc.streamID, NetStream_Publish_BadName, Level_Error))
					}
				case "play":
					pm := msg.MsgData.(*PlayMessage)
					streamPath := nc.appName + "/" + pm.StreamName
					subscriber := &engine.Subscriber{
						Type: "RTMP",
						ID:   fmt.Sprintf("%s|%d", conn.RemoteAddr().String(), nc.streamID),
					}
					if subscriber.Subscribe(streamPath, config.Subscribe) {
						nc.subscribers[nc.streamID] = subscriber
						err = nc.SendStreamID(RTMP_USER_STREAM_IS_RECORDED)
						err = nc.SendStreamID(RTMP_USER_STREAM_BEGIN)
						err = nc.SendCommand(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Reset, Level_Status))
						err = nc.SendCommand(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Start, Level_Status))
						go func() {
							SendMedia(&nc, subscriber)
							err = nc.SendCommand(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Stop, Level_Status))
							err = nc.SendCommand(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Complete, Level_Status))
						}()
					} else {
						err = nc.SendCommand(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Failed, Level_Error))
					}
				case "closeStream":
					cm := msg.MsgData.(*CURDStreamMessage)
					if stream, ok := nc.subscribers[cm.StreamId]; ok {
						stream.Close()
						delete(nc.subscribers, cm.StreamId)
					}
				case "releaseStream":
					cm := msg.MsgData.(*ReleaseStreamMessage)
					amfobj := make(AMFObject)
					if nc.Stream != nil && nc.Stream.AppName == nc.appName && nc.Stream.StreamName == cm.StreamName {
						amfobj["level"] = "_result"
						nc.Stream.UnPublish()
					} else {
						amfobj["level"] = "_error"
					}
					amfobj["tid"] = cm.TransactionId
					err = nc.SendCommand(SEND_UNPUBLISH_RESPONSE_MESSAGE, amfobj)
				}
			case RTMP_MSG_AUDIO:
				nc.ReceiveAudio(msg)
			case RTMP_MSG_VIDEO:
				nc.ReceiveVideo(msg)
			}
		} else {
			plugin.Error(err)
			return
		}
	}
}
