package rtmp

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"

	. "m7s.live/engine/v4"
	"m7s.live/engine/v4/util"
)

const (
	SEND_CHUNK_SIZE_MESSAGE         = "Send Chunk Size Message"
	SEND_ACK_MESSAGE                = "Send Acknowledgement Message"
	SEND_ACK_WINDOW_SIZE_MESSAGE    = "Send Window Acknowledgement Size Message"
	SEND_SET_PEER_BANDWIDTH_MESSAGE = "Send Set Peer Bandwidth Message"

	SEND_STREAM_BEGIN_MESSAGE       = "Send Stream Begin Message"
	SEND_SET_BUFFER_LENGTH_MESSAGE  = "Send Set Buffer Lengh Message"
	SEND_STREAM_IS_RECORDED_MESSAGE = "Send Stream Is Recorded Message"

	SEND_PING_REQUEST_MESSAGE  = "Send Ping Request Message"
	SEND_PING_RESPONSE_MESSAGE = "Send Ping Response Message"

	SEND_CONNECT_MESSAGE          = "Send Connect Message"
	SEND_CONNECT_RESPONSE_MESSAGE = "Send Connect Response Message"

	SEND_CREATE_STREAM_MESSAGE = "Send Create Stream Message"

	SEND_PLAY_MESSAGE          = "Send Play Message"
	SEND_PLAY_RESPONSE_MESSAGE = "Send Play Response Message"

	SEND_PUBLISH_RESPONSE_MESSAGE = "Send Publish Response Message"
	SEND_PUBLISH_START_MESSAGE    = "Send Publish Start Message"

	SEND_UNPUBLISH_RESPONSE_MESSAGE = "Send Unpublish Response Message"

	SEND_AUDIO_MESSAGE      = "Send Audio Message"
	SEND_FULL_AUDIO_MESSAGE = "Send Full Audio Message"
	SEND_VIDEO_MESSAGE      = "Send Video Message"
	SEND_FULL_VDIEO_MESSAGE = "Send Full Video Message"
)

func newConnectResponseMessageData(objectEncoding float64) (amfobj AMFObject) {
	amfobj = make(AMFObject)
	amfobj["fmsVer"] = "monibuca/" + Engine.Version
	amfobj["capabilities"] = 31
	amfobj["mode"] = 1
	amfobj["Author"] = "dexter"
	amfobj["level"] = Level_Status
	amfobj["code"] = NetConnection_Connect_Success
	amfobj["objectEncoding"] = uint64(objectEncoding)

	return
}

func newPublishResponseMessageData(streamid uint32, code, level string) (amfobj AMFObject) {
	amfobj = make(AMFObject)
	amfobj["code"] = code
	amfobj["level"] = level
	amfobj["streamid"] = streamid

	return
}

func newPlayResponseMessageData(streamid uint32, code, level string) (amfobj AMFObject) {
	amfobj = make(AMFObject)
	amfobj["code"] = code
	amfobj["level"] = level
	amfobj["streamid"] = streamid

	return
}

type NetConnection struct {
	*bufio.Reader      `json:"-"`
	*net.TCPConn       `json:"-"`
	bandwidth          uint32
	readSeqNum         uint32 // 当前读的字节
	writeSeqNum        uint32 // 当前写的字节
	totalWrite         uint32 // 总共写了多少字节
	totalRead          uint32 // 总共读了多少字节
	writeChunkSize     int
	readChunkSize      int
	incompleteRtmpBody map[uint32]*util.Buffer // 完整的RtmpBody,在网络上是被分成一块一块的,需要将其组装起来
	rtmpHeader         map[uint32]*ChunkHeader // RtmpHeader
	objectEncoding     float64
	appName            string
	tmpBuf             []byte //用来接收小数据，复用内存
}

func (conn *NetConnection) ReadFull(buf []byte) (n int, err error) {
	return io.ReadFull(conn.Reader, buf)
}
func (conn *NetConnection) SendStreamID(eventType uint16, streamID uint32) (err error) {
	return conn.SendMessage(RTMP_MSG_USER_CONTROL, &StreamIDMessage{UserControlMessage{EventType: eventType}, streamID})
}
func (conn *NetConnection) SendUserControl(eventType uint16) error {
	return conn.SendMessage(RTMP_MSG_USER_CONTROL, &UserControlMessage{EventType: eventType})
}

func (conn *NetConnection) ResponseCreateStream(tid uint64, streamID uint32) error {
	m := &ResponseCreateStreamMessage{}
	m.CommandName = Response_Result
	m.TransactionId = tid
	m.StreamId = streamID
	return conn.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
}

// func (conn *NetConnection) SendCommand(message string, args any) error {
// 	switch message {
// 	// case SEND_SET_BUFFER_LENGTH_MESSAGE:
// 	// 	if args != nil {
// 	// 		return errors.New(SEND_SET_BUFFER_LENGTH_MESSAGE + ", The parameter is nil")
// 	// 	}
// 	// 	m := new(SetBufferMessage)
// 	// 	m.EventType = RTMP_USER_SET_BUFFLEN
// 	// 	m.Millisecond = 100
// 	// 	m.StreamID = conn.streamID
// 	// 	return conn.writeMessage(RTMP_MSG_USER_CONTROL, m)
// 	}
// 	return errors.New("send message no exist")
// }

func (conn *NetConnection) readChunk() (msg *Chunk, err error) {
	head, err := conn.ReadByte()
	conn.readSeqNum++
	if err != nil {
		return nil, err
	}

	ChunkStreamID := uint32(head & 0x3f) // 0011 1111
	ChunkType := head >> 6               // 1100 0000

	// 如果块流ID为0,1的话,就需要计算.
	ChunkStreamID, err = conn.readChunkStreamID(ChunkStreamID)
	if err != nil {
		return nil, errors.New("get chunk stream id error :" + err.Error())
	}

	h, ok := conn.rtmpHeader[ChunkStreamID]
	if !ok {
		h = &ChunkHeader{
			ChunkBasicHeader: ChunkBasicHeader{
				ChunkStreamID,
				ChunkType,
			},
		}
		conn.rtmpHeader[ChunkStreamID] = h
	}
	currentBody, ok := conn.incompleteRtmpBody[ChunkStreamID]
	if ChunkType != 3 && ok && currentBody.Len() > 0 {
		// 如果块类型不为3,那么这个rtmp的body应该为空.
		return nil, errors.New("incompleteRtmpBody error")
	}
	if err = conn.readChunkType(h, ChunkType); err != nil {
		return nil, errors.New("get chunk type error :" + err.Error())
	}
	msgLen := int(h.MessageLength)

	if !ok {
		newBuffer := util.Buffer(make([]byte, 0, msgLen))
		currentBody = &newBuffer
		conn.incompleteRtmpBody[ChunkStreamID] = currentBody
	}

	needRead := conn.readChunkSize
	if unRead := msgLen - currentBody.Len(); unRead < needRead {
		needRead = unRead
	}
	if n, err := conn.ReadFull(currentBody.Malloc(needRead)); err != nil {
		return nil, err
	} else {
		conn.readSeqNum += uint32(n)
	}
	// 如果读完了一个完整的块,那么就返回这个消息,没读完继续递归读块.
	if currentBody.Len() == msgLen {
		msg = &Chunk{
			ChunkHeader: *h,
			Body:        currentBody.ReadN(msgLen),
		}
		err = GetRtmpMessage(msg)
	}
	return
}

func (conn *NetConnection) readChunkStreamID(csid uint32) (chunkStreamID uint32, err error) {
	chunkStreamID = csid

	switch csid {
	case 0:
		{
			u8, err := conn.ReadByte()
			conn.readSeqNum++
			if err != nil {
				return 0, err
			}

			chunkStreamID = 64 + uint32(u8)
		}
	case 1:
		{
			u16_0, err1 := conn.ReadByte()
			if err1 != nil {
				return 0, err1
			}
			u16_1, err1 := conn.ReadByte()
			if err1 != nil {
				return 0, err1
			}
			conn.readSeqNum += 2
			chunkStreamID = 64 + uint32(u16_0) + (uint32(u16_1) << 8)
		}
	}

	return chunkStreamID, nil
}

func (conn *NetConnection) readChunkType(h *ChunkHeader, chunkType byte) (err error) {
	b := conn.tmpBuf[:3]
	switch chunkType {
	case 0:
		{
			// Timestamp 3 bytes
			if _, err := conn.ReadFull(b); err != nil {
				return err
			}
			conn.readSeqNum += 3
			util.GetBE(b, &h.Timestamp) //type = 0的时间戳为绝对时间,其他的都为相对时间

			// Message Length 3 bytes
			if _, err = conn.ReadFull(b); err != nil { // 读取Message Length,这里的长度指的是一条信令或者一帧视频数据或音频数据的长度,而不是Chunk data的长度.
				return err
			}
			conn.readSeqNum += 3
			util.GetBE(b, &h.MessageLength)
			// Message Type ID 1 bytes
			v, err := conn.ReadByte() // 读取Message Type ID
			if err != nil {
				return err
			}
			conn.readSeqNum++
			h.MessageTypeID = v

			// Message Stream ID 4bytes
			b = conn.tmpBuf
			if _, err = conn.ReadFull(b); err != nil { // 读取Message Stream ID
				return err
			}
			conn.readSeqNum += 4
			h.MessageStreamID = binary.LittleEndian.Uint32(b)

			// ExtendTimestamp 4 bytes
			if h.Timestamp == 0xffffff { // 对于type 0的chunk,绝对时间戳在这里表示,如果时间戳值大于等于0xffffff(16777215),该值必须是0xffffff,且时间戳扩展字段必须发送,其他情况没有要求
				if _, err = conn.ReadFull(b); err != nil {
					return err
				}
				conn.readSeqNum += 4
				util.GetBE(b, &h.ExtendTimestamp)
			}
		}
	case 1:
		{
			// Timestamp 3 bytes
			if _, err = conn.ReadFull(b); err != nil {
				return err
			}
			conn.readSeqNum += 3
			h.ChunkType = chunkType
			util.GetBE(b, &h.Timestamp)
			// Message Length 3 bytes
			if _, err = conn.ReadFull(b); err != nil {
				return err
			}
			conn.readSeqNum += 3
			util.GetBE(b, &h.MessageLength)
			// Message Type ID 1 bytes
			v, err := conn.ReadByte()
			if err != nil {
				return err
			}
			conn.readSeqNum++
			h.MessageTypeID = v

			// ExtendTimestamp 4 bytes
			if h.Timestamp == 0xffffff {
				b = conn.tmpBuf
				if _, err := conn.ReadFull(b); err != nil {
					return err
				}
				conn.readSeqNum += 4
				util.GetBE(b, &h.ExtendTimestamp)
			}
		}
	case 2:
		{
			// Timestamp 3 bytes
			if _, err = conn.ReadFull(b); err != nil {
				return err
			}
			conn.readSeqNum += 3
			h.ChunkType = chunkType
			util.GetBE(b, &h.Timestamp)
			// ExtendTimestamp 4 bytes
			if h.Timestamp == 0xffffff {
				b = conn.tmpBuf
				if _, err := conn.ReadFull(b); err != nil {
					return err
				}
				conn.readSeqNum += 4
				util.GetBE(b, &h.ExtendTimestamp)
			}
		}
	case 3:
		{
			//h.ChunkType = chunkType
		}
	}

	return nil
}

func (conn *NetConnection) RecvMessage() (msg *Chunk, err error) {
	if conn.readSeqNum >= conn.bandwidth {
		conn.totalRead += conn.readSeqNum
		conn.readSeqNum = 0
		err = conn.SendMessage(RTMP_MSG_ACK, Uint32Message(conn.totalRead))
	}
	for msg == nil && err == nil {
		if msg, err = conn.readChunk(); msg != nil {
			switch msg.MessageTypeID {
			case RTMP_MSG_CHUNK_SIZE:
				conn.readChunkSize = int(msg.MsgData.(Uint32Message))
			case RTMP_MSG_ABORT:
				delete(conn.incompleteRtmpBody, uint32(msg.MsgData.(Uint32Message)))
			case RTMP_MSG_ACK, RTMP_MSG_EDGE:
			case RTMP_MSG_USER_CONTROL:
				if _, ok := msg.MsgData.(*PingRequestMessage); ok {
					conn.SendUserControl(RTMP_USER_PING_RESPONSE)
				}
			case RTMP_MSG_ACK_SIZE:
				conn.bandwidth = uint32(msg.MsgData.(Uint32Message))
			case RTMP_MSG_BANDWIDTH:
				conn.bandwidth = msg.MsgData.(*SetPeerBandwidthMessage).AcknowledgementWindowsize
			case RTMP_MSG_AMF0_COMMAND, RTMP_MSG_AUDIO, RTMP_MSG_VIDEO:
				return msg, err
			}
		}
	}
	return
}
func (conn *NetConnection) SendMessage(t byte, msg RtmpMessage) (err error) {
	body := msg.Encode()
	head := newChunkHeader(t)
	head.MessageLength = uint32(len(body))
	if sid, ok := msg.(HaveStreamID); ok {
		head.MessageStreamID = sid.GetStreamID()
	}
	if conn.writeSeqNum > conn.bandwidth {
		conn.totalWrite += conn.writeSeqNum
		conn.writeSeqNum = 0
		err = conn.SendMessage(RTMP_MSG_ACK, Uint32Message(conn.totalWrite))
		err = conn.SendStreamID(RTMP_USER_PING_REQUEST, 0)
	}
	var chunk = net.Buffers{conn.encodeChunk12(head)}
	if len(body) > conn.writeChunkSize {
		chunk = append(chunk, body[:conn.writeChunkSize])
		body = body[conn.writeChunkSize:]
		conn.writeSeqNum += uint32(util.SizeOfBuffers(chunk))
		_, err = chunk.WriteTo(conn)
		for len(body) > conn.writeChunkSize {
			chunk = append(chunk[:0], conn.encodeChunk12(head), body[:conn.writeChunkSize])
			body = body[conn.writeChunkSize:]
			conn.writeSeqNum += uint32(util.SizeOfBuffers(chunk))
			_, err = chunk.WriteTo(conn)
		}
		chunk = append(chunk[:0], conn.encodeChunk12(head), body)
		conn.writeSeqNum += uint32(util.SizeOfBuffers(chunk))
		_, err = chunk.WriteTo(conn)
	} else {
		chunk = append(chunk, body)
		conn.writeSeqNum += uint32(util.SizeOfBuffers(chunk))
		_, err = chunk.WriteTo(conn)
	}

	return nil
}
