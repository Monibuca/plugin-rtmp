package rtmp

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync/atomic"

	"go.uber.org/zap"
	"m7s.live/engine/v4"
	"m7s.live/engine/v4/util"
)

type NetStream struct {
	*NetConnection
	StreamID uint32
}

func (ns *NetStream) Begin() {
	ns.SendStreamID(RTMP_USER_STREAM_BEGIN, ns.StreamID)
}

var gstreamid = uint32(64)

type RTMPSubscriber struct {
	RTMPSender
}

func (s *RTMPSubscriber) OnEvent(event any) {
	switch event.(type) {
	case engine.SEclose:
		s.Response(NetStream_Play_Stop, Level_Status)
	}
	s.RTMPSender.OnEvent(event)
}
func (config *RTMPConfig) ServeTCP(conn *net.TCPConn) {
	senders := make(map[uint32]*RTMPSubscriber)
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
	defer func() {
		nc.Close()
		cancel() //终止所有发布者和订阅者
	}()
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
					m := new(ResponseConnectMessage)
					m.CommandName = Response_Result
					m.TransactionId = 1
					m.Properties = AMFObject{
						"fmsVer":       "monibuca/" + engine.Engine.Version,
						"capabilities": 31,
						"mode":         1,
						"Author":       "dexter",
					}
					m.Infomation = AMFObject{
						"level":          Level_Status,
						"code":           NetConnection_Connect_Success,
						"objectEncoding": nc.objectEncoding,
					}
					err = nc.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
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
					receiver.SetParentCtx(ctx)
					if plugin.Publish(nc.appName+"/"+pm.PublishingName, receiver) == nil {
						receivers[receiver.StreamID] = receiver
						receiver.absTs = make(map[uint32]uint32)
						receiver.Begin()
						err = receiver.Response(NetStream_Publish_Start, Level_Status)
					} else {
						err = receiver.Response(NetStream_Publish_BadName, Level_Error)
					}
				case "play":
					pm := msg.MsgData.(*PlayMessage)
					streamPath := nc.appName + "/" + pm.StreamName
					sender := &RTMPSubscriber{}
					sender.NetStream = NetStream{
						&nc,
						msg.MessageStreamID,
					}
					sender.SetParentCtx(ctx)
					sender.ID = fmt.Sprintf("%s|%d", conn.RemoteAddr().String(), sender.StreamID)
					if plugin.Subscribe(streamPath, sender) == nil {
						senders[sender.StreamID] = sender
						err = nc.SendStreamID(RTMP_USER_STREAM_IS_RECORDED, msg.MessageStreamID)
						sender.Begin()
						sender.Response(NetStream_Play_Reset, Level_Status)
						sender.Response(NetStream_Play_Start, Level_Status)
						go sender.PlayBlock(sender)
					} else {
						sender.Response(NetStream_Play_Failed, Level_Error)
					}
				case "closeStream":
					cm := msg.MsgData.(*CURDStreamMessage)
					if stream, ok := senders[cm.StreamId]; ok {
						stream.Stop()
						delete(senders, cm.StreamId)
					}
				case "releaseStream":
					cm := msg.MsgData.(*ReleaseStreamMessage)
					m := &CommandMessage{
						CommandName:   "releaseStream",
						TransactionId: cm.TransactionId,
					}
					if p, ok := receivers[msg.MessageStreamID]; ok {
						m.CommandName += "_result"
						p.Stop()
					} else {
						m.CommandName += "_error"
					}
					err = nc.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
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
