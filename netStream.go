package rtmp

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Monibuca/engine/v3"
	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
)

func ListenRtmp(addr string) error {
	defer log.Println("rtmp server start!")
	// defer fmt.Println("server start!")
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	var tempDelay time.Duration
	for {
		conn, err := listener.Accept()
		conn.(*net.TCPConn).SetNoDelay(false)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				fmt.Printf("rtmp: Accept error: %v; retrying in %v", err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return err
		}

		tempDelay = 0
		go processRtmp(conn)
	}
	return nil
}

var gstreamid = uint32(64)

func processRtmp(conn net.Conn) {
	var stream *engine.Stream
	streams := make(map[uint32]*engine.Subscriber)
	defer func() {
		conn.Close()
		if stream != nil {
			stream.Close()
		}
		for _, s := range streams {
			s.Close()
		}
	}()
	nc := NetConnection{
		ReadWriter:         bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
		writeChunkSize:     RTMP_DEFAULT_CHUNK_SIZE,
		readChunkSize:      RTMP_DEFAULT_CHUNK_SIZE,
		rtmpHeader:         make(map[uint32]*ChunkHeader),
		incompleteRtmpBody: make(map[uint32][]byte),
		bandwidth:          RTMP_MAX_CHUNK_SIZE << 3,
		nextStreamID: func() uint32 {
			return atomic.AddUint32(&gstreamid, 1)
		},
	}
	/* Handshake */
	if utils.MayBeError(Handshake(nc.ReadWriter)) {
		return
	}
	if utils.MayBeError(nc.OnConnect()) {
		return
	}
	var rec_audio, rec_video func(*Chunk)
	var abslouteTs uint32
	rec_audio = func(msg *Chunk) {
		va := engine.NewAudioTrack()
		tmp := msg.Body[0]
		soundFormat := tmp >> 4
		switch soundFormat {
		case 10:
			if msg.Body[1] != 0 {
				return
			}
			va.SoundFormat = soundFormat
			config1, config2 := msg.Body[2], msg.Body[3]
			//audioObjectType = (config1 & 0xF8) >> 3
			// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
			// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
			// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
			// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
			va.SoundRate = codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)]
			va.Channels = ((config2 >> 3) & 0x0F) //声道
			//frameLengthFlag = (config2 >> 2) & 0x01
			//dependsOnCoreCoder = (config2 >> 1) & 0x01
			//extensionFlag = config2 & 0x01
			va.RtmpTag = msg.Body
			rec_audio = func(msg *Chunk) {
				if msg.Timestamp == 0xffffff {
					abslouteTs += msg.ExtendTimestamp
				} else {
					abslouteTs += msg.Timestamp // 绝对时间戳
				}
				va.Push(abslouteTs, msg.Body[2:])
			}
			stream.SetOriginAT(va)
			return
		case 7, 8:
			va.RtmpTag = msg.Body
			va.SoundFormat = soundFormat
			va.SoundRate = codec.SoundRate[(tmp&0x0c)>>2] // 采样率 0 = 5.5 kHz or 1 = 11 kHz or 2 = 22 kHz or 3 = 44 kHz
			va.SoundSize = (tmp & 0x02) >> 1              // 采样精度 0 = 8-bit samples or 1 = 16-bit samples
			va.Channels = tmp&0x01 + 1                    // 0 单声道，1立体声
			rec_audio = func(msg *Chunk) {
				if msg.Timestamp == 0xffffff {
					abslouteTs += msg.ExtendTimestamp
				} else {
					abslouteTs += msg.Timestamp // 绝对时间戳
				}
				va.Push(abslouteTs, msg.Body[1:])
			}
			stream.SetOriginAT(va)
		}
	}
	rec_video = func(msg *Chunk) {
		codecId := msg.Body[0] & 0x0F
		// 等待AVC序列帧
		if codecId != 7 && codecId != 12 || msg.Body[1] != 0 {
			return
		}
		vt := engine.NewVideoTrack()
		vt.CodecID = codecId
		vt.RtmpTag = msg.Body
		var info codec.AVCDecoderConfigurationRecord
		//0:codec,1:IsAVCSequence,2~4:compositionTime
		if _, err := info.Unmarshal(msg.Body[5:]); err == nil {
			var pack engine.VideoPack
			pack.Payload = info.SequenceParameterSetNALUnit
			vt.Push(pack)
			pack.Payload = info.PictureParameterSetNALUnit
			vt.Push(pack)
		}
		nalulenSize := int(info.LengthSizeMinusOne&3 + 1)
		stream.SetOriginVT(vt)
		rec_video = func(msg *Chunk) {
			var pack engine.VideoPack
			pack.CompositionTime = utils.BigEndian.Uint24(msg.Body[2:5])
			nalus := msg.Body[5:]
			if msg.Timestamp == 0xffffff {
				abslouteTs += msg.ExtendTimestamp
			} else {
				abslouteTs += msg.Timestamp // 绝对时间戳
			}
			pack.Timestamp = abslouteTs
			for len(nalus) > nalulenSize {
				nalulen := 0
				for i := 0; i < nalulenSize; i++ {
					nalulen += int(nalus[i]) << (8 * (nalulenSize - i - 1))
				}
				pack.Payload = nalus[nalulenSize : nalulen+nalulenSize]
				vt.Push(pack)
				nalus = nalus[nalulen+nalulenSize:]
			}
		}
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
				switch cmd.CommandName {
				case "createStream":
					nc.streamID = nc.nextStreamID()
					log.Println("createStream:", nc.streamID)
					err = nc.SendMessage(SEND_CREATE_STREAM_RESPONSE_MESSAGE, cmd.TransactionId)
					if utils.MayBeError(err) {
						return
					}
				case "publish":
					pm := msg.MsgData.(*PublishMessage)
					streamPath := nc.appName + "/" + strings.Split(pm.PublishingName, "?")[0]
					if pub := new(engine.Publisher); pub.Publish(streamPath) {
						pub.Type = "RTMP"
						stream = pub.Stream
						err = nc.SendMessage(SEND_STREAM_BEGIN_MESSAGE, nil)
						err = nc.SendMessage(SEND_PUBLISH_START_MESSAGE, newPublishResponseMessageData(nc.streamID, NetStream_Publish_Start, Level_Status))
					} else {
						err = nc.SendMessage(SEND_PUBLISH_RESPONSE_MESSAGE, newPublishResponseMessageData(nc.streamID, NetStream_Publish_BadName, Level_Error))
					}
				case "play":
					pm := msg.MsgData.(*PlayMessage)
					streamPath := nc.appName + "/" + strings.Split(pm.StreamName, "?")[0]
					nc.writeChunkSize = config.ChunkSize
					subscriber := engine.Subscriber{
						Type: "RTMP",
						ID:   fmt.Sprintf("%s|%d", conn.RemoteAddr().String(), nc.streamID),
					}
					if err = subscriber.Subscribe(streamPath); err == nil {
						streams[nc.streamID] = &subscriber
						err = nc.SendMessage(SEND_CHUNK_SIZE_MESSAGE, uint32(nc.writeChunkSize))
						err = nc.SendMessage(SEND_STREAM_IS_RECORDED_MESSAGE, nil)
						err = nc.SendMessage(SEND_STREAM_BEGIN_MESSAGE, nil)
						err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Reset, Level_Status))
						err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Start, Level_Status))
						vt, at := subscriber.OriginVideoTrack, subscriber.OriginAudioTrack
						var lastTimeStamp uint32
						getDeltaTime := func(ts uint32) (t uint32) {
							t = ts - lastTimeStamp
							lastTimeStamp = ts
							return
						}
						if vt != nil {
							err = nc.SendMessage(SEND_FULL_VDIEO_MESSAGE, &AVPack{Payload: vt.RtmpTag})
							subscriber.OnVideo = func(pack engine.VideoPack) {
								payload := pack.ToRTMPTag()
								defer utils.RecycleSlice(payload)
								lastTimeStamp = pack.Timestamp
								err = nc.SendMessage(SEND_FULL_VDIEO_MESSAGE, &AVPack{Timestamp: 0, Payload: payload})
								subscriber.OnVideo = func(pack engine.VideoPack) {
									payload := pack.ToRTMPTag()
									defer utils.RecycleSlice(payload)
									err = nc.SendMessage(SEND_VIDEO_MESSAGE, &AVPack{Timestamp: getDeltaTime(pack.Timestamp), Payload: payload})
								}
							}
						}
						if at != nil {
							subscriber.OnAudio = func(pack engine.AudioPack) {
								payload := pack.ToRTMPTag(at.RtmpTag[0])
								defer utils.RecycleSlice(payload)
								if at.SoundFormat == 10 {
									err = nc.SendMessage(SEND_FULL_AUDIO_MESSAGE, &AVPack{Payload: at.RtmpTag})
								}
								subscriber.OnAudio = func(pack engine.AudioPack) {
									payload := pack.ToRTMPTag(at.RtmpTag[0])
									defer utils.RecycleSlice(payload)
									err = nc.SendMessage(SEND_AUDIO_MESSAGE, &AVPack{Timestamp: getDeltaTime(pack.Timestamp), Payload: payload})
								}
								subscriber.OnAudio(pack)
							}
						}
						go subscriber.Play(at, vt)
					}
				case "closeStream":
					cm := msg.MsgData.(*CURDStreamMessage)
					if stream, ok := streams[cm.StreamId]; ok {
						stream.Close()
						delete(streams, cm.StreamId)
					}
				case "releaseStream":
					cm := msg.MsgData.(*ReleaseStreamMessage)
					streamPath := nc.appName + "/" + strings.Split(cm.StreamName, "?")[0]
					amfobj := newAMFObjects()
					if s := engine.FindStream(streamPath); s != nil {
						amfobj["level"] = "_result"
						s.Close()
					} else {
						amfobj["level"] = "_error"
					}
					amfobj["tid"] = cm.TransactionId
					err = nc.SendMessage(SEND_UNPUBLISH_RESPONSE_MESSAGE, amfobj)
				}
			case RTMP_MSG_AUDIO:
				rec_audio(msg)
			case RTMP_MSG_VIDEO:
				rec_video(msg)
			}
			msg.Recycle()
		} else {
			return
		}
	}
}
