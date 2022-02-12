package rtmp

import (
	"bufio"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/codec"
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
	var rec_audio, rec_video func(*Chunk)

	for {
		if msg, err := nc.RecvMessage(); err == nil {
			if msg.MessageLength <= 0 {
				continue
			}
			switch msg.MessageTypeID {
			case RTMP_MSG_CHUNK_SIZE:
				nc.readChunkSize = int(msg.MsgData.(Uint32Message))
			case RTMP_MSG_ABORT:
				delete(nc.incompleteRtmpBody, uint32(msg.MsgData.(Uint32Message)))
			case RTMP_MSG_ACK, RTMP_MSG_EDGE:
			case RTMP_MSG_USER_CONTROL:
				if _, ok := msg.MsgData.(*PingRequestMessage); ok {
					nc.SendUserControl(RTMP_USER_PING_RESPONSE)
				}
			case RTMP_MSG_ACK_SIZE:
				nc.bandwidth = uint32(msg.MsgData.(Uint32Message))
			case RTMP_MSG_BANDWIDTH:
				m := msg.MsgData.(*SetPeerBandwidthMessage)
				nc.bandwidth = m.AcknowledgementWindowsize
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
					if nc.Publish(nc.appName+"/"+pm.PublishingName, &nc, config.Publish) {
						absTs := make(map[uint32]uint32)
						vt := nc.Stream.NewVideoTrack()
						at := nc.Stream.NewAudioTrack()
						rec_audio = func(msg *Chunk) {
							plugin.Tracef("rec_audio chunkType:%d chunkStreamID:%d ts:%d", msg.ChunkType, msg.ChunkStreamID, msg.Timestamp)
							if msg.ChunkType == 0 {
								absTs[msg.ChunkStreamID] = 0
							}
							if msg.Timestamp == 0xffffff {
								absTs[msg.ChunkStreamID] += msg.ExtendTimestamp
							} else {
								absTs[msg.ChunkStreamID] += msg.Timestamp
							}
							at.WriteAVCC(absTs[msg.ChunkStreamID], msg.Body)
						}
						rec_video = func(msg *Chunk) {
							plugin.Tracef("rev_video chunkType:%d chunkStreamID:%d ts:%d", msg.ChunkType, msg.ChunkStreamID, msg.Timestamp)
							if msg.ChunkType == 0 {
								absTs[msg.ChunkStreamID] = 0
							}
							if msg.Timestamp == 0xffffff {
								absTs[msg.ChunkStreamID] += msg.ExtendTimestamp
							} else {
								absTs[msg.ChunkStreamID] += msg.Timestamp
							}
							vt.WriteAVCC(absTs[msg.ChunkStreamID], msg.Body)
						}
						nc.SendStreamID(RTMP_USER_STREAM_BEGIN)
						err = nc.SendCommand(SEND_PUBLISH_START_MESSAGE, newPublishResponseMessageData(nc.streamID, NetStream_Publish_Start, Level_Status))
					} else {
						err = nc.SendCommand(SEND_PUBLISH_RESPONSE_MESSAGE, newPublishResponseMessageData(nc.streamID, NetStream_Publish_BadName, Level_Error))
					}
				case "play":
					pm := msg.MsgData.(*PlayMessage)
					streamPath := nc.appName + "/" + pm.StreamName
					subscriber := engine.Subscriber{
						Type: "RTMP",
						ID:   fmt.Sprintf("%s|%d", conn.RemoteAddr().String(), nc.streamID),
					}
					if subscriber.Subscribe(streamPath, config.Subscribe) {
						nc.subscribers[nc.streamID] = &subscriber
						err = nc.SendStreamID(RTMP_USER_STREAM_IS_RECORDED)
						err = nc.SendStreamID(RTMP_USER_STREAM_BEGIN)
						err = nc.SendCommand(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Reset, Level_Status))
						err = nc.SendCommand(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Start, Level_Status))
						vt, at := subscriber.WaitVideoTrack(), subscriber.WaitAudioTrack()
						if vt != nil {
							frame := vt.DecoderConfiguration
							err = nc.sendAVMessage(0, net.Buffers(frame.AVCC), false, true)
							subscriber.OnVideo = func(frame *engine.VideoFrame) error {
								return nc.sendAVMessage(frame.DeltaTime, frame.AVCC, false, false)
							}
						}
						if at != nil {
							subscriber.OnAudio = func(frame *engine.AudioFrame) (err error) {
								if at.CodecID == codec.CodecID_AAC {
									frame := at.DecoderConfiguration
									err = nc.sendAVMessage(0, net.Buffers{frame.AVCC}, true, true)
								} else {
									err = nc.sendAVMessage(0, frame.AVCC, true, true)
								}
								subscriber.OnAudio = func(frame *engine.AudioFrame) error {
									return nc.sendAVMessage(frame.DeltaTime, frame.AVCC, true, false)
								}
								return
							}
						}
						go subscriber.Play(at, vt)
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
				rec_audio(msg)
			case RTMP_MSG_VIDEO:
				rec_video(msg)
			}
		} else {
			plugin.Error(err)
			return
		}
	}
}
