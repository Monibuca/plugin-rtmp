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
					stream = &engine.Stream{Type: "RTMP", StreamPath: streamPath}
					if stream.Publish() {
						absTs := make(map[uint32]uint32)
						vt := stream.NewVideoTrack(0)
						at := stream.NewAudioTrack(0)
						rec_audio = func(msg *Chunk) {
							if msg.ChunkType == 0 {
								absTs[msg.ChunkStreamID] = 0
							}
							if msg.Timestamp == 0xffffff {
								absTs[msg.ChunkStreamID] += msg.ExtendTimestamp
							} else {
								absTs[msg.ChunkStreamID] += msg.Timestamp
							}
							at.PushByteStream(engine.AudioPack{Timestamp: absTs[msg.ChunkStreamID], Payload: msg.Body})
						}
						rec_video = func(msg *Chunk) {
							if msg.ChunkType == 0 {
								absTs[msg.ChunkStreamID] = 0
							}
							if msg.Timestamp == 0xffffff {
								absTs[msg.ChunkStreamID] += msg.ExtendTimestamp
							} else {
								absTs[msg.ChunkStreamID] += msg.Timestamp
							}
							vt.PushByteStream(engine.VideoPack{Timestamp: absTs[msg.ChunkStreamID], Payload: msg.Body})
						}
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
						Type:             "RTMP",
						ID:               fmt.Sprintf("%s|%d", conn.RemoteAddr().String(), nc.streamID),
						ByteStreamFormat: true,
					}
					if err = subscriber.Subscribe(streamPath); err == nil {
						streams[nc.streamID] = &subscriber
						err = nc.SendMessage(SEND_CHUNK_SIZE_MESSAGE, uint32(nc.writeChunkSize))
						err = nc.SendMessage(SEND_STREAM_IS_RECORDED_MESSAGE, nil)
						err = nc.SendMessage(SEND_STREAM_BEGIN_MESSAGE, nil)
						err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Reset, Level_Status))
						err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Start, Level_Status))
						vt, at := subscriber.WaitVideoTrack(), subscriber.WaitAudioTrack()
						if vt != nil {
							var lastTimeStamp uint32
							var getDeltaTime func(uint32) uint32
							getDeltaTime = func(ts uint32) (t uint32) {
								lastTimeStamp = ts
								getDeltaTime = func(ts uint32) (t uint32) {
									t = ts - lastTimeStamp
									lastTimeStamp = ts
									return
								}
								return
							}
							err = nc.SendMessage(SEND_FULL_VDIEO_MESSAGE, &AVPack{Payload: vt.ExtraData.Payload})
							subscriber.OnVideo = func(pack engine.VideoPack) {
								err = nc.SendMessage(SEND_FULL_VDIEO_MESSAGE, &AVPack{Timestamp: 0, Payload: pack.Payload})
								subscriber.OnVideo = func(pack engine.VideoPack) {
									err = nc.SendMessage(SEND_VIDEO_MESSAGE, &AVPack{Timestamp: getDeltaTime(pack.Timestamp), Payload: pack.Payload})
								}
							}
						}
						if at != nil {
							var lastTimeStamp uint32
							var getDeltaTime func(uint32) uint32
							getDeltaTime = func(ts uint32) (t uint32) {
								lastTimeStamp = ts
								getDeltaTime = func(ts uint32) (t uint32) {
									t = ts - lastTimeStamp
									lastTimeStamp = ts
									return
								}
								return
							}
							subscriber.OnAudio = func(pack engine.AudioPack) {
								if at.CodecID == 10 {
									err = nc.SendMessage(SEND_FULL_AUDIO_MESSAGE, &AVPack{Payload: at.ExtraData})
								}
								subscriber.OnAudio = func(pack engine.AudioPack) {
									err = nc.SendMessage(SEND_AUDIO_MESSAGE, &AVPack{Timestamp: getDeltaTime(pack.Timestamp), Payload: pack.Payload})
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
