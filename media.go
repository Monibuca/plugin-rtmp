package rtmp

import (
	"errors"

	"go.uber.org/zap"
	. "m7s.live/engine/v4"
	"m7s.live/engine/v4/common"
)

type RTMPSender struct {
	Subscriber
	NetStream
	firstAudioSent   bool
	firstVideoSent   bool
	audioChunkHeader ChunkHeader
	videoChunkHeader ChunkHeader
}

func (rtmp *RTMPSender) OnEvent(event any) {
	switch v := event.(type) {
	case SEwaitPublish:
		rtmp.Response(1, NetStream_Play_UnpublishNotify, Response_OnStatus)
	case SEpublish:
		rtmp.Response(1, NetStream_Play_PublishNotify, Response_OnStatus)
	case ISubscriber:
		rtmp.audioChunkHeader.ChunkStreamID = RTMP_CSID_AUDIO
		rtmp.videoChunkHeader.ChunkStreamID = RTMP_CSID_VIDEO
		rtmp.audioChunkHeader.MessageTypeID = RTMP_MSG_AUDIO
		rtmp.videoChunkHeader.MessageTypeID = RTMP_MSG_VIDEO
		rtmp.audioChunkHeader.MessageStreamID = rtmp.StreamID
		rtmp.videoChunkHeader.MessageStreamID = rtmp.StreamID
	case AudioDeConf:
		rtmp.audioChunkHeader.SetTimestamp(0)
		rtmp.audioChunkHeader.MessageLength = uint32(len(v))
		rtmp.audioChunkHeader.WriteTo(RTMP_CHUNK_HEAD_12, &rtmp.chunkHeader)
		rtmp.sendChunk(v)
	case VideoDeConf:
		rtmp.videoChunkHeader.SetTimestamp(0)
		rtmp.videoChunkHeader.MessageLength = uint32(len(v))
		rtmp.videoChunkHeader.WriteTo(RTMP_CHUNK_HEAD_12, &rtmp.chunkHeader)
		rtmp.sendChunk(v)
	case AudioFrame:
		rtmp.sendAVMessage(v.AVFrame, v.AbsTime, true)
	case VideoFrame:
		rtmp.sendAVMessage(v.AVFrame, v.AbsTime, false)
	default:
		rtmp.Subscriber.OnEvent(event)
	}
}

// 当发送音视频数据的时候,当块类型为12的时候,Chunk Message Header有一个字段TimeStamp,指明一个时间
// 当块类型为4,8的时候,Chunk Message Header有一个字段TimeStamp Delta,记录与上一个Chunk的时间差值
// 当块类型为0的时候,Chunk Message Header没有时间字段,与上一个Chunk时间值相同
func (sender *RTMPSender) sendAVMessage(frame *common.AVFrame, absTime uint32, isAudio bool) (err error) {
	payloadLen := frame.AVCC.ByteLength
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

	var isFirst = false
	if isAudio {
		head = &sender.audioChunkHeader
		if isFirst = !sender.firstAudioSent; isFirst {
			sender.firstAudioSent = true
		}
	} else {
		head = &sender.videoChunkHeader
		if isFirst = !sender.firstVideoSent; isFirst {
			sender.firstVideoSent = true
		}
	}
	head.MessageLength = uint32(payloadLen)
	// 第一次是发送关键帧,需要完整的消息头(Chunk Basic Header(1) + Chunk Message Header(11) + Extended Timestamp(4)(可能会要包括))
	// 后面开始,就是直接发送音视频数据,那么直接发送,不需要完整的块(Chunk Basic Header(1) + Chunk Message Header(7))
	// 当Chunk Type为0时(即Chunk12),
	if isFirst {
		head.SetTimestamp(absTime)
		head.WriteTo(RTMP_CHUNK_HEAD_12, &sender.chunkHeader)
	} else {
		head.SetTimestamp(frame.DeltaTime)
		head.WriteTo(RTMP_CHUNK_HEAD_8, &sender.chunkHeader)
	}
	r := frame.AVCC.NewReader()
	chunk := r.ReadN(sender.writeChunkSize)
	// payloadLen -= util.SizeOfBuffers(chunk)
	sender.sendChunk(chunk...)
	if r.CanRead() {
		// 如果在音视频数据太大,一次发送不完,那么这里进行分割(data + Chunk Basic Header(1))
		for head.WriteTo(RTMP_CHUNK_HEAD_1, &sender.chunkHeader); r.CanRead(); sender.sendChunk(chunk...) {
			chunk = r.ReadN(sender.writeChunkSize)
			// payloadLen -= util.SizeOfBuffers(chunk)
		}
	}
	return nil
}

func (r *RTMPSender) Response(tid uint64, code, level string) error {
	m := new(ResponsePlayMessage)
	m.CommandName = Response_OnStatus
	m.TransactionId = tid
	m.Infomation = map[string]any{
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
}

func (r *RTMPReceiver) Response(tid uint64, code, level string) error {
	m := new(ResponsePublishMessage)
	m.CommandName = Response_OnStatus
	m.TransactionId = tid
	m.Infomation = map[string]any{
		"code":        code,
		"level":       level,
		"description": "",
	}
	m.StreamID = r.StreamID
	return r.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
}

func (r *RTMPReceiver) ReceiveAudio(msg *Chunk) {
	if r.AudioTrack == nil {
		if r.WriteAVCCAudio(0, msg.AVData); r.AudioTrack != nil {
			r.AudioTrack.SetStuff(r.bytePool)
		}
		return
	}
	r.AudioTrack.WriteAVCC(msg.ExtendTimestamp, msg.AVData)
}

func (r *RTMPReceiver) ReceiveVideo(msg *Chunk) {
	if r.VideoTrack == nil {
		if r.WriteAVCCVideo(0, msg.AVData); r.VideoTrack != nil {
			r.VideoTrack.SetStuff(r.bytePool)
		}
		return
	}
	r.VideoTrack.WriteAVCC(msg.ExtendTimestamp, msg.AVData)
}
