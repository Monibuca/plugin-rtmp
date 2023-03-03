package rtmp

import (
	"errors"
	"runtime"

	"go.uber.org/zap"
	. "m7s.live/engine/v4"
	"m7s.live/engine/v4/common"
)

type AVSender struct {
	*RTMPSender
	ChunkHeader
	firstSent bool
}

func (av *AVSender) sendSequenceHead(seqHead []byte) {
	av.SetTimestamp(0)
	av.MessageLength = uint32(len(seqHead))
	for !av.writing.CompareAndSwap(false, true) {
		runtime.Gosched()
	}
	defer av.writing.Store(false)
	av.WriteTo(RTMP_CHUNK_HEAD_12, &av.chunkHeader)
	av.sendChunk(seqHead)
}

func (av *AVSender) sendFrame(frame *common.AVFrame, absTime uint32) (err error) {
	payloadLen := frame.AVCC.ByteLength
	if payloadLen == 0 {
		err := errors.New("payload is empty")
		av.Error("payload is empty", zap.Error(err))
		return err
	}
	if av.writeSeqNum > av.bandwidth {
		av.totalWrite += av.writeSeqNum
		av.writeSeqNum = 0
		av.SendMessage(RTMP_MSG_ACK, Uint32Message(av.totalWrite))
		av.SendStreamID(RTMP_USER_PING_REQUEST, 0)
	}
	av.MessageLength = uint32(payloadLen)
	for !av.writing.CompareAndSwap(false, true) {
		runtime.Gosched()
	}
	defer av.writing.Store(false)
	// 第一次是发送关键帧,需要完整的消息头(Chunk Basic Header(1) + Chunk Message Header(11) + Extended Timestamp(4)(可能会要包括))
	// 后面开始,就是直接发送音视频数据,那么直接发送,不需要完整的块(Chunk Basic Header(1) + Chunk Message Header(7))
	// 当Chunk Type为0时(即Chunk12),
	if !av.firstSent {
		av.firstSent = true
		av.SetTimestamp(absTime)
		av.WriteTo(RTMP_CHUNK_HEAD_12, &av.chunkHeader)
	} else {
		av.SetTimestamp(frame.DeltaTime)
		av.WriteTo(RTMP_CHUNK_HEAD_8, &av.chunkHeader)
	}
	r := frame.AVCC.NewReader()
	chunk := r.ReadN(av.writeChunkSize)
	// payloadLen -= util.SizeOfBuffers(chunk)
	av.sendChunk(chunk...)
	if r.CanRead() {
		// 如果在音视频数据太大,一次发送不完,那么这里进行分割(data + Chunk Basic Header(1))
		for av.WriteTo(RTMP_CHUNK_HEAD_1, &av.chunkHeader); r.CanRead(); av.sendChunk(chunk...) {
			chunk = r.ReadN(av.writeChunkSize)
			// payloadLen -= util.SizeOfBuffers(chunk)
		}
	}
	return nil
}

type RTMPSender struct {
	Subscriber
	NetStream
	audio, video AVSender
}

func (rtmp *RTMPSender) OnEvent(event any) {
	switch v := event.(type) {
	case SEwaitPublish:
		rtmp.Response(1, NetStream_Play_UnpublishNotify, Response_OnStatus)
	case SEpublish:
		rtmp.Response(1, NetStream_Play_PublishNotify, Response_OnStatus)
	case ISubscriber:
		rtmp.audio.RTMPSender = rtmp
		rtmp.video.RTMPSender = rtmp
		rtmp.audio.ChunkStreamID = RTMP_CSID_AUDIO
		rtmp.video.ChunkStreamID = RTMP_CSID_VIDEO
		rtmp.audio.MessageTypeID = RTMP_MSG_AUDIO
		rtmp.video.MessageTypeID = RTMP_MSG_VIDEO
		rtmp.audio.MessageStreamID = rtmp.StreamID
		rtmp.video.MessageStreamID = rtmp.StreamID
	case AudioDeConf:
		rtmp.audio.sendSequenceHead(v)
	case VideoDeConf:
		rtmp.video.sendSequenceHead(v)
	case AudioFrame:
		rtmp.audio.sendFrame(v.AVFrame, v.AbsTime)
	case VideoFrame:
		rtmp.video.sendFrame(v.AVFrame, v.AbsTime)
	default:
		rtmp.Subscriber.OnEvent(event)
	}
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
		r.WriteAVCCAudio(0, &msg.AVData, r.bytePool)
		return
	}
	r.AudioTrack.WriteAVCC(msg.ExtendTimestamp, &msg.AVData)
}

func (r *RTMPReceiver) ReceiveVideo(msg *Chunk) {
	if r.VideoTrack == nil {
		r.WriteAVCCVideo(0, &msg.AVData, r.bytePool)
		return
	}
	r.VideoTrack.WriteAVCC(msg.ExtendTimestamp, &msg.AVData)
}
