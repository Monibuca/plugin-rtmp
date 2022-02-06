package rtmp

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"

	"github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/util"
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

	SEND_CREATE_STREAM_MESSAGE          = "Send Create Stream Message"
	SEND_CREATE_STREAM_RESPONSE_MESSAGE = "Send Create Stream Response Message"

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
	amfobj["fmsVer"] = "monibuca/" + engine.Version
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
	engine.Publisher
	subscribers map[uint32]*engine.Subscriber
	*bufio.Reader
	*net.TCPConn
	bandwidth          uint32
	readSeqNum         uint32 // 当前读的字节
	writeSeqNum        uint32 // 当前写的字节
	totalWrite         uint32 // 总共写了多少字节
	totalRead          uint32 // 总共读了多少字节
	writeChunkSize     int
	readChunkSize      int
	incompleteRtmpBody map[uint32]util.Buffer  // 完整的RtmpBody,在网络上是被分成一块一块的,需要将其组装起来
	streamID           uint32                  // 流ID
	rtmpHeader         map[uint32]*ChunkHeader // RtmpHeader
	objectEncoding     float64
	appName            string
	tmpBuf             []byte //用来接收小数据，复用内存
}

func (conn *NetConnection) Close() {
	conn.Publisher.Stream.UnPublish()
	conn.TCPConn.Close()
	for _, s := range conn.subscribers {
		s.Close()
	}
}

func (conn *NetConnection) ReadFull(buf []byte) (n int, err error) {
	return io.ReadFull(conn.Reader, buf)
}
func (conn *NetConnection) SendStreamID0(eventType uint16) (err error) {
	return conn.SendMessage(RTMP_MSG_USER_CONTROL, &StreamIDMessage{UserControlMessage{EventType: eventType}, 0})
}
func (conn *NetConnection) SendStreamID(eventType uint16) (err error) {
	return conn.SendMessage(RTMP_MSG_USER_CONTROL, &StreamIDMessage{UserControlMessage{EventType: eventType}, conn.streamID})
}
func (conn *NetConnection) SendUserControl(eventType uint16) error {
	return conn.SendMessage(RTMP_MSG_USER_CONTROL, &UserControlMessage{EventType: eventType})
}
func (conn *NetConnection) SendCommand(message string, args any) error {
	switch message {
	// case SEND_SET_BUFFER_LENGTH_MESSAGE:
	// 	if args != nil {
	// 		return errors.New(SEND_SET_BUFFER_LENGTH_MESSAGE + ", The parameter is nil")
	// 	}
	// 	m := new(SetBufferMessage)
	// 	m.EventType = RTMP_USER_SET_BUFFLEN
	// 	m.Millisecond = 100
	// 	m.StreamID = conn.streamID
	// 	return conn.writeMessage(RTMP_MSG_USER_CONTROL, m)
	case SEND_CREATE_STREAM_MESSAGE:
		if args != nil {
			return errors.New(SEND_CREATE_STREAM_MESSAGE + ", The parameter is nil")
		}

		m := &CreateStreamMessage{}
		m.CommandName = "createStream"
		m.TransactionId = 1
		return conn.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
	case SEND_CREATE_STREAM_RESPONSE_MESSAGE:
		tid, ok := args.(uint64)
		if !ok {
			return errors.New(SEND_CREATE_STREAM_RESPONSE_MESSAGE + ", The parameter only one(TransactionId uint64)!")
		}
		m := &ResponseCreateStreamMessage{}
		m.CommandName = Response_Result
		m.TransactionId = tid
		m.StreamId = conn.streamID
		return conn.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
	case SEND_PLAY_MESSAGE:
		data, ok := args.(map[interface{}]interface{})
		if !ok {
			errors.New(SEND_PLAY_MESSAGE + ", The parameter is map[interface{}]interface{}")
		}
		m := new(PlayMessage)
		m.CommandName = "play"
		m.TransactionId = 1
		for i, v := range data {
			if i == "StreamPath" {
				m.StreamName = v.(string)
			} else if i == "Start" {
				m.Start = v.(uint64)
			} else if i == "Duration" {
				m.Duration = v.(uint64)
			} else if i == "Rest" {
				m.Rest = v.(bool)
			}
		}
		return conn.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
	case SEND_PLAY_RESPONSE_MESSAGE:
		data, ok := args.(AMFObject)
		if !ok {
			errors.New(SEND_PLAY_RESPONSE_MESSAGE + ", The parameter is AMFObjects(map[string]interface{})")
		}

		obj := make(AMFObject)
		var streamID uint32

		for i, v := range data {
			switch i {
			case "code", "level":
				obj[i] = v
			case "streamid":
				if t, ok := v.(uint32); ok {
					streamID = t
				}
			}
		}

		obj["clientid"] = 1

		m := new(ResponsePlayMessage)
		m.CommandName = Response_OnStatus
		m.TransactionId = 0
		m.Object = obj
		m.StreamID = streamID
		return conn.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
	case SEND_CONNECT_RESPONSE_MESSAGE:
		//if !ok {
		//	errors.New(SEND_CONNECT_RESPONSE_MESSAGE + ", The parameter is AMFObjects(map[string]interface{})")
		//}

		pro := make(AMFObject)
		info := make(AMFObject)

		//for i, v := range data {
		//	switch i {
		//	case "fmsVer", "capabilities", "mode", "Author", "level", "code", "objectEncoding":
		//		pro[i] = v
		//	}
		//}

		pro["fmsVer"] = "monibuca/" + engine.Version
		pro["capabilities"] = 31
		pro["mode"] = 1
		pro["Author"] = "dexter"

		info["level"] = Level_Status
		info["code"] = NetConnection_Connect_Success
		info["objectEncoding"] = conn.objectEncoding
		m := new(ResponseConnectMessage)
		m.CommandName = Response_Result
		m.TransactionId = 1
		m.Properties = pro
		m.Infomation = info
		return conn.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
	case SEND_CONNECT_MESSAGE:
		data, ok := args.(AMFObject)
		if !ok {
			errors.New(SEND_CONNECT_MESSAGE + ", The parameter is AMFObjects(map[string]interface{})")
		}

		obj := make(AMFObject)
		info := make(AMFObject)

		for i, v := range data {
			switch i {
			case "videoFunction", "objectEncoding", "fpad", "flashVer", "capabilities", "pageUrl", "swfUrl", "tcUrl", "videoCodecs", "app", "audioCodecs":
				obj[i] = v
			}
		}

		m := new(CallMessage)
		m.CommandName = "connect"
		m.TransactionId = 1
		m.Object = obj
		m.Optional = info
		return conn.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
	case SEND_PUBLISH_RESPONSE_MESSAGE, SEND_PUBLISH_START_MESSAGE:
		info := args.(AMFObject)
		info["clientid"] = 1
		m := new(ResponsePublishMessage)
		m.CommandName = Response_OnStatus
		m.TransactionId = 0
		m.Infomation = info
		m.StreamID = info["streamid"].(uint32)
		delete(info, "streamid")
		return conn.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
	case SEND_UNPUBLISH_RESPONSE_MESSAGE:
		data, ok := args.(AMFObject)
		if !ok {
			errors.New(SEND_UNPUBLISH_RESPONSE_MESSAGE + ", The parameter is AMFObjects(map[string]interface{})")
		}
		m := new(CommandMessage)
		m.TransactionId = data["tid"].(uint64)
		m.CommandName = "releaseStream" + data["level"].(string)
		return conn.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
	}
	return errors.New("send message no exist")
}

// 当发送音视频数据的时候,当块类型为12的时候,Chunk Message Header有一个字段TimeStamp,指明一个时间
// 当块类型为4,8的时候,Chunk Message Header有一个字段TimeStamp Delta,记录与上一个Chunk的时间差值
// 当块类型为0的时候,Chunk Message Header没有时间字段,与上一个Chunk时间值相同
func (conn *NetConnection) sendAVMessage(ts uint32, payload net.Buffers, isAudio bool, isFirst bool) (err error) {
	if conn.writeSeqNum > conn.bandwidth {
		conn.totalWrite += conn.writeSeqNum
		conn.writeSeqNum = 0
		conn.SendMessage(RTMP_MSG_ACK, Uint32Message(conn.totalWrite))
		conn.SendStreamID0(RTMP_USER_PING_REQUEST)
	}

	var head *ChunkHeader
	if isAudio {
		head = newRtmpHeader(RTMP_CSID_AUDIO, ts, uint32(util.SizeOfBuffers(payload)), RTMP_MSG_AUDIO, conn.streamID, 0)
	} else {
		head = newRtmpHeader(RTMP_CSID_VIDEO, ts, uint32(util.SizeOfBuffers(payload)), RTMP_MSG_VIDEO, conn.streamID, 0)
	}

	// 第一次是发送关键帧,需要完整的消息头(Chunk Basic Header(1) + Chunk Message Header(11) + Extended Timestamp(4)(可能会要包括))
	// 后面开始,就是直接发送音视频数据,那么直接发送,不需要完整的块(Chunk Basic Header(1) + Chunk Message Header(7))
	// 当Chunk Type为0时(即Chunk12),
	var chunk1 net.Buffers
	if isFirst {
		chunk1 = append(chunk1, conn.encodeChunk12(head))
	} else {
		chunk1 = append(chunk1, conn.encodeChunk8(head))
	}
	chunks := util.SplitBuffers(payload, conn.writeChunkSize)
	chunk1 = append(chunk1, chunks[0]...)
	conn.writeSeqNum += uint32(util.SizeOfBuffers(chunk1))
	_, err = chunk1.WriteTo(conn)
	// 如果在音视频数据太大,一次发送不完,那么这里进行分割(data + Chunk Basic Header(1))
	for _, chunk := range chunks[1:] {
		chunk1 = net.Buffers{conn.encodeChunk1(head)}
		chunk1 = append(chunk1, chunk...)
		conn.writeSeqNum += uint32(util.SizeOfBuffers(chunk1))
		_, err = chunk1.WriteTo(conn)
	}

	return nil
}

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

	if !ok || currentBody.Len() == 0 {
		currentBody = util.Buffer(make([]byte, 0, msgLen))
		conn.incompleteRtmpBody[ChunkStreamID] = currentBody
	}

	needRead := conn.readChunkSize
	if unRead := msgLen - currentBody.Len(); unRead < needRead {
		needRead = unRead
	}
	if n, err := conn.ReadFull(currentBody.Malloc(needRead)); err != nil {
		util.Println(err)
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
	conn.incompleteRtmpBody[ChunkStreamID] = currentBody
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
		msg, err = conn.readChunk()
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
		err = conn.SendStreamID0(RTMP_USER_PING_REQUEST)
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
