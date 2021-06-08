package rtmp

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	. "github.com/Monibuca/engine/v2"
	"github.com/Monibuca/engine/v2/avformat"
)

type RTMP struct {
	Publisher
}

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
	var stream *Stream
	streams := make(map[uint32]*Subscriber)
	defer func() {
		conn.Close()
		if stream != nil {
			stream.Cancel()
		}
		for _, s := range streams {
			s.Close()
		}
	}()
	var totalDuration uint32
	nc := &NetConnection{
		ReadWriter:         bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
		writeChunkSize:     RTMP_DEFAULT_CHUNK_SIZE,
		readChunkSize:      RTMP_DEFAULT_CHUNK_SIZE,
		rtmpHeader:         make(map[uint32]*ChunkHeader),
		incompleteRtmpBody: make(map[uint32][]byte),
		bandwidth:          RTMP_MAX_CHUNK_SIZE << 3,
		nextStreamID: func(u uint32) uint32 {
			gstreamid++
			return gstreamid
		},
	}
	/* Handshake */
	if MayBeError(Handshake(nc.ReadWriter)) {
		return
	}
	if MayBeError(nc.OnConnect()) {
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
				switch cmd.CommandName {
				case "createStream":
					nc.streamID = nc.nextStreamID(msg.ChunkStreamID)
					log.Println("createStream:", nc.streamID)
					err = nc.SendMessage(SEND_CREATE_STREAM_RESPONSE_MESSAGE, cmd.TransactionId)
					if MayBeError(err) {
						return
					}
				case "publish":
					pm := msg.MsgData.(*PublishMessage)
					streamPath := nc.appName + "/" + strings.Split(pm.PublishingName, "?")[0]
					if pub := new(RTMP); pub.Publish(streamPath) {
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
					var lastAudioTime uint32 = 0
					var lastVideoTime uint32 = 0
					// followAVCSequence := false
					stream := &Subscriber{OnData: func(packet *avformat.SendPacket) (err error) {
						switch true {
						// case packet.IsADTS:
						// 	tagPacket := avformat.NewAVPacket(RTMP_MSG_AUDIO)
						// 	tagPacket.Payload = avformat.ADTSToAudioSpecificConfig(packet.Payload)
						// 	err = nc.SendMessage(SEND_FULL_AUDIO_MESSAGE, tagPacket)
						// 	ADTSLength := 7 + (int(packet.Payload[1]&1) << 1)
						// 	if len(packet.Payload) > ADTSLength {
						// 		contentPacket := avformat.NewAVPacket(RTMP_MSG_AUDIO)
						// 		contentPacket.Timestamp = packet.Timestamp
						// 		contentPacket.Payload = make([]byte, len(packet.Payload)-ADTSLength+2)
						// 		contentPacket.Payload[0] = 0xAF
						// 		contentPacket.Payload[1] = 0x01 //raw AAC
						// 		copy(contentPacket.Payload[2:], packet.Payload[ADTSLength:])
						// 		err = nc.SendMessage(SEND_AUDIO_MESSAGE, contentPacket)
						// 	}
						case packet.Type == RTMP_MSG_VIDEO:
							if packet.IsSequence {
								err = nc.SendMessage(SEND_FULL_VDIEO_MESSAGE, packet)
							} else {
								t := packet.Timestamp - lastVideoTime
								lastVideoTime = packet.Timestamp
								packet.Timestamp = t
								err = nc.SendMessage(SEND_VIDEO_MESSAGE, packet)
							}
						case packet.Type == RTMP_MSG_AUDIO:
							if packet.IsSequence {
								err = nc.SendMessage(SEND_FULL_AUDIO_MESSAGE, packet)
							} else {
								t := packet.Timestamp - lastAudioTime
								lastAudioTime = packet.Timestamp
								packet.Timestamp = t
								err = nc.SendMessage(SEND_AUDIO_MESSAGE, packet)
							}
						}
						return
					}}
					stream.Type = "RTMP"
					stream.ID = fmt.Sprintf("%s|%d", conn.RemoteAddr().String(), nc.streamID)
					err = nc.SendMessage(SEND_CHUNK_SIZE_MESSAGE, uint32(nc.writeChunkSize))
					err = nc.SendMessage(SEND_STREAM_IS_RECORDED_MESSAGE, nil)
					err = nc.SendMessage(SEND_STREAM_BEGIN_MESSAGE, nil)
					err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Reset, Level_Status))
					err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Start, Level_Status))
					if err == nil {
						streams[nc.streamID] = stream
						go stream.Subscribe(streamPath)
					} else {
						return
					}
				case "closeStream":
					cm := msg.MsgData.(*CURDStreamMessage)
					if stream, ok := streams[cm.StreamId]; ok {
						stream.Cancel()
						delete(streams, cm.StreamId)
					}
				case "releaseStream":
					cm := msg.MsgData.(*ReleaseStreamMessage)
					streamPath := nc.appName + "/" + strings.Split(cm.StreamName, "?")[0]
					amfobj := newAMFObjects()
					if s := FindStream(streamPath); s != nil {
						amfobj["level"] = "_result"
						if s.Publisher != nil {
							s.Close()
						}
					} else {
						amfobj["level"] = "_error"
					}
					amfobj["tid"] = cm.TransactionId
					err = nc.SendMessage(SEND_UNPUBLISH_RESPONSE_MESSAGE, amfobj)
				}
			case RTMP_MSG_AUDIO:
				// pkt := avformat.NewAVPacket(RTMP_MSG_AUDIO)
				if msg.Timestamp == 0xffffff {
					totalDuration += msg.ExtendTimestamp
				} else {
					totalDuration += msg.Timestamp // 绝对时间戳
				}
				stream.PushAudio(totalDuration, msg.Body)
			case RTMP_MSG_VIDEO:
				// pkt := avformat.NewAVPacket(RTMP_MSG_VIDEO)
				if msg.Timestamp == 0xffffff {
					totalDuration += msg.ExtendTimestamp
				} else {
					totalDuration += msg.Timestamp // 绝对时间戳
				}
				stream.PushVideo(totalDuration, msg.Body)
			}
			msg.Recycle()
		} else {
			return
		}
	}
}
