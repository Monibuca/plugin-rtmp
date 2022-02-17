package rtmp

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/util"
	"go.uber.org/zap"
)

type NetStream struct {
	*NetConnection
	StreamID uint32
}

func (ns *NetStream) Begin() {
	ns.SendStreamID(RTMP_USER_STREAM_BEGIN, ns.StreamID)
}

var gstreamid = uint32(64)

func (config *RTMPConfig) ServeTCP(conn *net.TCPConn) {
	senders := make(map[uint32]*RTMPSender)
	receivers := make(map[uint32]*RTMPReceiver)
	nc := NetConnection{
		TCPConn:            conn,
		Reader:             bufio.NewReader(conn),
		writeChunkSize:     RTMP_DEFAULT_CHUNK_SIZE,
		readChunkSize:      RTMP_DEFAULT_CHUNK_SIZE,
		rtmpHeader:         make(map[uint32]*ChunkHeader),
		incompleteRtmpBody: make(map[uint32]util.Buffer),
		bandwidth:          RTMP_MAX_CHUNK_SIZE << 3,
		tmpBuf:             make([]byte, 4),
	}
	ctx, cancel := context.WithCancel(engine.Engine)
	defer nc.Close()
	defer cancel()
	/* Handshake */
	if err := nc.Handshake(); err != nil {
		plugin.Error("handshake", zap.Error(err))
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
				plugin.Debug("recv cmd", zap.String("commandName", cmd.CommandName), zap.Uint32("streamID", msg.MessageStreamID))
				switch cmd.CommandName {
				case "connect":
					connect := msg.MsgData.(*CallMessage)
					app := connect.Object["app"]                       // 客户端要连接到的服务应用名
					objectEncoding := connect.Object["objectEncoding"] // AMF编码方法
					if objectEncoding != nil {
						nc.objectEncoding = objectEncoding.(float64)
					}
					nc.appName = app.(string)
					plugin.Info("connect", zap.String("appName", nc.appName), zap.Float64("objectEncoding", nc.objectEncoding))
					err = nc.SendMessage(RTMP_MSG_ACK_SIZE, Uint32Message(512<<10))
					nc.writeChunkSize = config.ChunkSize
					err = nc.SendMessage(RTMP_MSG_CHUNK_SIZE, Uint32Message(config.ChunkSize))
					err = nc.SendMessage(RTMP_MSG_BANDWIDTH, &SetPeerBandwidthMessage{
						AcknowledgementWindowsize: uint32(512 << 10),
						LimitType:                 byte(2),
					})
					err = nc.SendStreamID(RTMP_USER_STREAM_BEGIN, 0)
					err = nc.SendCommand(SEND_CONNECT_RESPONSE_MESSAGE, nc.objectEncoding)
				case "createStream":
					streamId := atomic.AddUint32(&gstreamid, 1)
					plugin.Info("createStream:", zap.Uint32("streamId", streamId))
					nc.ResponseCreateStream(cmd.TransactionId, streamId)
				case "publish":
					pm := msg.MsgData.(*PublishMessage)
					receiver := &RTMPReceiver{
						NetStream: NetStream{
							NetConnection: &nc,
							StreamID:      pm.StreamId,
						},
					}
					receiver.OnEvent(ctx)
					if plugin.Publish(nc.appName+"/"+pm.PublishingName, receiver) {
						receiver.absTs = make(map[uint32]uint32)
						receiver.Begin()
						err = receiver.Response(NetStream_Publish_Start, Level_Status)
					} else {
						err = receiver.Response(NetStream_Publish_BadName, Level_Error)
					}
				case "play":
					pm := msg.MsgData.(*PlayMessage)
					streamPath := nc.appName + "/" + pm.StreamName
					sender := &RTMPSender{
						NetStream: NetStream{
							NetConnection: &nc,
							StreamID:      msg.MessageStreamID,
						},
					}
					sender.OnEvent(ctx)
					sender.ID = fmt.Sprintf("%s|%d", conn.RemoteAddr().String(), sender.StreamID)
					if plugin.Subscribe(streamPath, sender) {
						senders[msg.MessageStreamID] = sender
						err = nc.SendStreamID(RTMP_USER_STREAM_IS_RECORDED, msg.MessageStreamID)
						sender.Begin()
						sender.Response(NetStream_Play_Reset, Level_Status)
						sender.Response(NetStream_Play_Start, Level_Status)
					} else {
						sender.Response(NetStream_Play_Failed, Level_Error)
					}
				case "closeStream":
					cm := msg.MsgData.(*CURDStreamMessage)
					if stream, ok := senders[cm.StreamId]; ok {
						stream.Unsubscribe()
						delete(senders, cm.StreamId)
					}
				case "releaseStream":
					cm := msg.MsgData.(*ReleaseStreamMessage)
					amfobj := make(AMFObject)
					p, ok := receivers[msg.MessageStreamID]
					if ok {
						amfobj["level"] = "_result"
						p.Unpublish()
					} else {
						amfobj["level"] = "_error"
					}
					amfobj["tid"] = cm.TransactionId
					err = nc.SendCommand(SEND_UNPUBLISH_RESPONSE_MESSAGE, amfobj)
				}
			case RTMP_MSG_AUDIO:
				if r, ok := receivers[msg.MessageStreamID]; ok {
					r.ReceiveAudio(msg)
				}
			case RTMP_MSG_VIDEO:
				if r, ok := receivers[msg.MessageStreamID]; ok {
					r.ReceiveVideo(msg)
				}
			}
		} else {
			plugin.Error("receive", zap.Error(err))
			return
		}
	}
}
