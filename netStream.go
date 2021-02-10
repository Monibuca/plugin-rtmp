package rtmp

import (
	"bufio"
	"fmt"
	"github.com/Monibuca/engine/v3"
	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"
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
			if stream.Publisher != nil {
				stream.Publisher.Dispose()
			}
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
	rec_audio = func(msg *Chunk) {
		var ts_audio uint32
		va := stream.AudioTracks[0]
		tmp := msg.Body[0]
		if va.SoundFormat = tmp >> 4; va.SoundFormat == 10 {
			if msg.Body[1] != 0 {
				return
			}
			config1, config2 := msg.Body[2], msg.Body[3]
			//audioObjectType = (config1 & 0xF8) >> 3
			// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
			// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
			// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
			// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
			va.SoundRate = codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)]
			va.SoundType = (config2 >> 3) & 0x0F //声道
			//frameLengthFlag = (config2 >> 2) & 0x01
			//dependsOnCoreCoder = (config2 >> 1) & 0x01
			//extensionFlag = config2 & 0x01
		} else {
			va.SoundRate = codec.SoundRate[(tmp&0x0c)>>2] // 采样率 0 = 5.5 kHz or 1 = 11 kHz or 2 = 22 kHz or 3 = 44 kHz
			va.SoundSize = (tmp & 0x02) >> 1              // 采样精度 0 = 8-bit samples or 1 = 16-bit samples
			va.SoundType = tmp & 0x01                     // 0 单声道，1立体声
		}
		va.RtmpTag = msg.Body
		rec_audio = func(msg *Chunk) {
			if msg.Timestamp == 0xffffff {
				ts_audio += msg.ExtendTimestamp
			} else {
				ts_audio += msg.Timestamp // 绝对时间戳
			}
			stream.PushAudio(ts_audio, msg.Body[2:])
		}
	}
	rec_video = func(msg *Chunk) {
		// 等待AVC序列帧
		if msg.Body[1] != 0 {
			return
		}
		vt := stream.VideoTracks[0]
		var ts_video uint32
		var info codec.AVCDecoderConfigurationRecord
		//0:codec,1:IsAVCSequence,2~4:compositionTime
		if _, err := info.Unmarshal(msg.Body[5:]); err == nil {
			vt.SPSInfo, err = codec.ParseSPS(info.SequenceParameterSetNALUnit)
			vt.SPS = info.SequenceParameterSetNALUnit
			vt.PPS = info.PictureParameterSetNALUnit
		}
		vt.RtmpTag = msg.Body
		nalulenSize := int(info.LengthSizeMinusOne&3 + 1)
		rec_video = func(msg *Chunk) {
			nalus := msg.Body[5:]
			if msg.Timestamp == 0xffffff {
				ts_video += msg.ExtendTimestamp
			} else {
				ts_video += msg.Timestamp // 绝对时间戳
			}
			for len(nalus) > nalulenSize {
				nalulen := 0
				for i := 0; i < nalulenSize; i++ {
					nalulen += int(nalus[i]) << (8 * (nalulenSize - i - 1))
				}
				vt.Push(ts_video, nalus[nalulenSize:nalulen+nalulenSize])
				nalus = nalus[nalulen+nalulenSize:]
			}
		}
		close(vt.WaitFirst)
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
					var subscriber engine.Subscriber
					subscriber.Type = "RTMP"
					subscriber.ID = fmt.Sprintf("%s|%d", conn.RemoteAddr().String(), nc.streamID)
					if err = subscriber.Subscribe(streamPath); err == nil {
						streams[nc.streamID] = &subscriber
						err = nc.SendMessage(SEND_CHUNK_SIZE_MESSAGE, uint32(nc.writeChunkSize))
						err = nc.SendMessage(SEND_STREAM_IS_RECORDED_MESSAGE, nil)
						err = nc.SendMessage(SEND_STREAM_BEGIN_MESSAGE, nil)
						err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Reset, Level_Status))
						err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Start, Level_Status))
						vt, at := subscriber.VideoTracks[0], subscriber.AudioTracks[0]
						err = nc.SendMessage(SEND_FULL_VDIEO_MESSAGE, &AVPack{Payload: vt.RtmpTag})
						if at.SoundFormat == 10 {
							err = nc.SendMessage(SEND_FULL_AUDIO_MESSAGE, &AVPack{Payload: at.RtmpTag})
						}
						var lastAudioTime, lastVideoTime uint32
						go (&engine.TrackCP{at, vt}).Play(subscriber.Context, func(pack engine.AudioPack) {
							if lastAudioTime == 0 {
								lastAudioTime = pack.Timestamp
							}
							t := pack.Timestamp - lastAudioTime
							lastAudioTime = pack.Timestamp
							l := len(pack.Payload) + 1
							if at.SoundFormat == 10 {
								l++
							}
							payload := utils.GetSlice(l)
							defer utils.RecycleSlice(payload)
							payload[0] = at.RtmpTag[0]
							if at.SoundFormat == 10 {
								payload[1] = 1
							}
							copy(payload[2:], pack.Payload)
							err = nc.SendMessage(SEND_AUDIO_MESSAGE, &AVPack{Timestamp: t, Payload: payload})
						}, func(pack engine.VideoPack) {
							if lastVideoTime == 0 {
								lastVideoTime = pack.Timestamp
							}
							t := pack.Timestamp - lastVideoTime
							lastVideoTime = pack.Timestamp
							payload := utils.GetSlice(9 + len(pack.Payload))
							defer utils.RecycleSlice(payload)
							if pack.NalType == codec.NALU_IDR_Picture {
								payload[0] = 0x17
							} else {
								payload[0] = 0x27
							}
							payload[1] = 0x01
							utils.BigEndian.PutUint32(payload[5:], uint32(len(pack.Payload)))
							copy(payload[9:], pack.Payload)
							err = nc.SendMessage(SEND_VIDEO_MESSAGE, &AVPack{Timestamp: t, Payload: payload})
						})
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
