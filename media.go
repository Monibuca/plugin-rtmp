package rtmp

import (
	"net"

	"github.com/Monibuca/engine/v4"
	. "github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/codec"
	"github.com/Monibuca/engine/v4/track"
)

func SendMedia(nc *NetConnection, sub *Subscriber) (err error) {
	vt, at := sub.WaitVideoTrack(), sub.WaitAudioTrack()
	if vt != nil {
		frame := vt.DecoderConfiguration
		err = nc.sendAVMessage(0, net.Buffers(frame.AVCC), false, true)
		sub.OnVideo = func(frame *engine.VideoFrame) error {
			return nc.sendAVMessage(frame.DeltaTime, frame.AVCC, false, false)
		}
	}
	if at != nil {
		sub.OnAudio = func(frame *engine.AudioFrame) (err error) {
			if at.CodecID == codec.CodecID_AAC {
				frame := at.DecoderConfiguration
				err = nc.sendAVMessage(0, net.Buffers{frame.AVCC}, true, true)
			} else {
				err = nc.sendAVMessage(0, frame.AVCC, true, true)
			}
			sub.OnAudio = func(frame *engine.AudioFrame) error {
				return nc.sendAVMessage(frame.DeltaTime, frame.AVCC, true, false)
			}
			return
		}
	}
	sub.Play(at, vt)
	return nil
}

func NewMediaReceiver(pub *Publisher) *MediaReceiver {
	return &MediaReceiver{
		pub,
		make(map[uint32]uint32), pub.NewVideoTrack(), pub.NewAudioTrack(),
	}
}

type MediaReceiver struct {
	*Publisher
	absTs map[uint32]uint32
	vt    *track.UnknowVideo
	at    *track.UnknowAudio
}

func (r *MediaReceiver) ReceiveAudio(msg *Chunk) {
	plugin.Tracef("rec_audio chunkType:%d chunkStreamID:%d ts:%d", msg.ChunkType, msg.ChunkStreamID, msg.Timestamp)
	if msg.ChunkType == 0 {
		r.absTs[msg.ChunkStreamID] = 0
	}
	if msg.Timestamp == 0xffffff {
		r.absTs[msg.ChunkStreamID] += msg.ExtendTimestamp
	} else {
		r.absTs[msg.ChunkStreamID] += msg.Timestamp
	}
	r.at.WriteAVCC(r.absTs[msg.ChunkStreamID], msg.Body)
}
func (r *MediaReceiver) ReceiveVideo(msg *Chunk) {
	plugin.Tracef("rev_video chunkType:%d chunkStreamID:%d ts:%d", msg.ChunkType, msg.ChunkStreamID, msg.Timestamp)
	if msg.ChunkType == 0 {
		r.absTs[msg.ChunkStreamID] = 0
	}
	if msg.Timestamp == 0xffffff {
		r.absTs[msg.ChunkStreamID] += msg.ExtendTimestamp
	} else {
		r.absTs[msg.ChunkStreamID] += msg.Timestamp
	}
	r.vt.WriteAVCC(r.absTs[msg.ChunkStreamID], msg.Body)
}
