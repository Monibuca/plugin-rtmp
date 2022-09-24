package rtmp

import (
	"errors"
	"net"

	"go.uber.org/zap"
	. "m7s.live/engine/v4"
	"m7s.live/engine/v4/util"
)

type RTMPSender struct {
	Subscriber
	NetStream
}

func (rtmp *RTMPSender) OnEvent(event any) {
	switch v := event.(type) {
	case SEwaitPublish:
		rtmp.Response(1, NetStream_Play_UnpublishNotify, Response_OnStatus)
	case SEpublish:
		rtmp.Response(1, NetStream_Play_PublishNotify, Response_OnStatus)
	case AudioDeConf:
		rtmp.sendAVMessage(0, v.AVCC, true, true)
	case VideoDeConf:
		rtmp.sendAVMessage(0, v.AVCC, false, true)
	case *AudioFrame:
		rtmp.sendAVMessage(v.DeltaTime, v.AVCC, true, false)
	case *VideoFrame:
		rtmp.sendAVMessage(v.DeltaTime, v.AVCC, false, false)
	default:
		rtmp.Subscriber.OnEvent(event)
	}
}

// 当发送音视频数据的时候,当块类型为12的时候,Chunk Message Header有一个字段TimeStamp,指明一个时间
// 当块类型为4,8的时候,Chunk Message Header有一个字段TimeStamp Delta,记录与上一个Chunk的时间差值
// 当块类型为0的时候,Chunk Message Header没有时间字段,与上一个Chunk时间值相同
func (sender *RTMPSender) sendAVMessage(ts uint32, payload net.Buffers, isAudio bool, isFirst bool) (err error) {
	payloadLen := util.SizeOfBuffers(payload)
	if payloadLen == 0 {
		err := errors.New("payload is empty")
		sender.Error("payload is empty", zap.Error(err))
		return err
	}
	if sender.writeSeqNum > sender.bandwidth {
		sender.totalWrite += sender.writeSeqNum
		sender.writeSeqNum = 0
		sender.SendMessage(RTMP_MSG_ACK, Uint32Message(sender.totalWrite))
		sender.SendStreamID(RTMP_USER_PING_REQUEST, 0)
	}

	var head *ChunkHeader
	if isAudio {
		head = newRtmpHeader(RTMP_CSID_AUDIO, ts, uint32(payloadLen), RTMP_MSG_AUDIO, sender.StreamID, 0)
	} else {
		head = newRtmpHeader(RTMP_CSID_VIDEO, ts, uint32(payloadLen), RTMP_MSG_VIDEO, sender.StreamID, 0)
	}

	// 第一次是发送关键帧,需要完整的消息头(Chunk Basic Header(1) + Chunk Message Header(11) + Extended Timestamp(4)(可能会要包括))
	// 后面开始,就是直接发送音视频数据,那么直接发送,不需要完整的块(Chunk Basic Header(1) + Chunk Message Header(7))
	// 当Chunk Type为0时(即Chunk12),
	var chunk1 net.Buffers
	if isFirst {
		chunk1 = append(chunk1, sender.encodeChunk12(head))
	} else {
		chunk1 = append(chunk1, sender.encodeChunk8(head))
	}
	chunks := util.SplitBuffers(payload, sender.writeChunkSize)
	chunk1 = append(chunk1, chunks[0]...)
	sender.writeSeqNum += uint32(util.SizeOfBuffers(chunk1))
	_, err = chunk1.WriteTo(sender.NetConnection)
	// 如果在音视频数据太大,一次发送不完,那么这里进行分割(data + Chunk Basic Header(1))
	for _, chunk := range chunks[1:] {
		chunk1 = net.Buffers{sender.encodeChunk1(head)}
		chunk1 = append(chunk1, chunk...)
		sender.writeSeqNum += uint32(util.SizeOfBuffers(chunk1))
		_, err = chunk1.WriteTo(sender.NetConnection)
	}

	return nil
}

func (r *RTMPSender) Response(tid uint64, code, level string) error {
	m := new(ResponsePlayMessage)
	m.CommandName = Response_OnStatus
	m.TransactionId = tid
	m.Object = AMFObject{
		"code":        code,
		"level":       level,
		"description": "",
	}
	m.StreamID = r.StreamID
	return r.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
}

type RTMPReceiver struct {
	Publisher
	NetStream
	absTs map[uint32]uint32
}

func (r *RTMPReceiver) Response(tid uint64, code, level string) error {
	m := new(ResponsePublishMessage)
	m.CommandName = Response_OnStatus
	m.TransactionId = tid
	m.Infomation = AMFObject{
		"code":        code,
		"level":       level,
		"description": "",
	}
	m.StreamID = r.StreamID
	return r.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
}

func (r *RTMPReceiver) ReceiveAudio(msg *Chunk) {
	if r.AudioTrack == nil {
		r.absTs[msg.ChunkStreamID] = 0
		r.WriteAVCCAudio(0, msg.Body)
		return
	}
	ts := msg.Timestamp
	if ts == 0xffffff {
		ts = msg.ExtendTimestamp
	}
	if msg.ChunkType == 0 {
		if r.AudioTrack.GetBase().Name == "" {
			r.absTs[msg.ChunkStreamID] = 0
		} else {
			r.absTs[msg.ChunkStreamID] = ts
		}
	} else {
		r.absTs[msg.ChunkStreamID] += ts
	}
	r.AudioTrack.WriteAVCC(r.absTs[msg.ChunkStreamID], msg.Body)
}
func (r *RTMPReceiver) ReceiveVideo(msg *Chunk) {
	if r.VideoTrack == nil {
		r.absTs[msg.ChunkStreamID] = 0
		r.WriteAVCCVideo(0, msg.Body)
		return
	}
	ts := msg.Timestamp
	if ts == 0xffffff {
		ts = msg.ExtendTimestamp
	}
	if msg.ChunkType == 0 {
		if r.VideoTrack.GetBase().Name == "" {
			r.absTs[msg.ChunkStreamID] = 0
		} else {
			r.absTs[msg.ChunkStreamID] = ts
		}
	} else {
		r.absTs[msg.ChunkStreamID] += ts
	}
	r.VideoTrack.WriteAVCC(r.absTs[msg.ChunkStreamID], msg.Body)
}
