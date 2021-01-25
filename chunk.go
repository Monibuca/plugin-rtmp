package rtmp

import (
	"errors"

	"github.com/Monibuca/utils/v3"
)

// RTMP协议中基本的数据单元称为消息(Message).
// 当RTMP协议在互联网中传输数据的时候,消息会被拆分成更小的单元,称为消息块(Chunk).
// 在网络上传输数据时,消息需要被拆分成较小的数据块,才适合在相应的网络环境上传输.

// 理论上Type 0, 1, 2的Chunk都可以使用Extended Timestamp来传递时间
// Type 3由于严禁携带Extened Timestamp字段.但实际上只有Type 0才需要带此字段.
// 这是因为,对Type 1, 2来说,其时间为一个差值,一般肯定小于0x00FFFFF

// 对于除Audio,Video以外的基它Message,其时间字段都可以是置为0的，似乎没有被用到.
// 只有在发送视频和音频数据时,才需要特别的考虑TimeStamp字段.基本依据是,要以HandShake时为起始点0来计算时间.
// 一般来说,建立一个相对时间,把一个视频帧的TimeStamp特意的在当前时间的基础上延迟3秒,则可以达到缓存的效果

const (
	RTMP_CHUNK_HEAD_12 = 0 << 6 // Chunk Basic Header = (Chunk Type << 6) | Chunk Stream ID.
	RTMP_CHUNK_HEAD_8  = 1 << 6
	RTMP_CHUNK_HEAD_4  = 2 << 6
	RTMP_CHUNK_HEAD_1  = 3 << 6
)

type Chunk struct {
	*ChunkHeader
	Body    []byte
	MsgData interface{}
}

func (c *Chunk) Encode(msg RtmpMessage) {
	c.MsgData = msg
	c.Body = msg.Encode()
	c.MessageLength = uint32(len(c.Body))
}
func (c *Chunk) Recycle() {
	chunkMsgPool.Put(c)
}

type ChunkHeader struct {
	ChunkBasicHeader
	ChunkMessageHeader
	ChunkExtendedTimestamp
}

// Basic Header (1 to 3 bytes) : This field encodes the chunk stream ID
// and the chunk type. Chunk type determines the format of the
// encoded message header. The length(Basic Header) depends entirely on the chunk
// stream ID, which is a variable-length field.
type ChunkBasicHeader struct {
	ChunkStreamID uint32 `json:""` // 6 bit. 3 ~ 65559, 0,1,2 reserved
	ChunkType     byte   `json:""` // 2 bit.
}

// Message Header (0, 3, 7, or 11 bytes): This field encodes
// information about the message being sent (whether in whole or in
// part). The length can be determined using the chunk type
// specified in the chunk header.
type ChunkMessageHeader struct {
	Timestamp       uint32 `json:""` // 3 byte
	MessageLength   uint32 `json:""` // 3 byte
	MessageTypeID   byte   `json:""` // 1 byte
	MessageStreamID uint32 `json:""` // 4 byte
}

// Extended Timestamp (0 or 4 bytes): This field is present in certain
// circumstances depending on the encoded timestamp or timestamp
// delta field in the Chunk Message header. See Section 5.3.1.3 for
// more information
type ChunkExtendedTimestamp struct {
	ExtendTimestamp uint32 `json:",omitempty"` // 标识该字段的数据可忽略
}

// ChunkBasicHeader会决定ChunkMessgaeHeader,ChunkMessgaeHeader有4种(0,3,7,11 Bytes),因此可能有4种头.

// 1  -> ChunkBasicHeader(1) + ChunkMessageHeader(0)
// 4  -> ChunkBasicHeader(1) + ChunkMessageHeader(3)
// 8  -> ChunkBasicHeader(1) + ChunkMessageHeader(7)
// 12 -> ChunkBasicHeader(1) + ChunkMessageHeader(11)

func (nc *NetConnection) encodeChunk12(head *ChunkHeader, payload []byte, size int) (need []byte, err error) {
	if size > RTMP_MAX_CHUNK_SIZE || payload == nil || len(payload) == 0 {
		return nil, errors.New("chunk error")
	}
	b := utils.GetSlice(12)
	//chunkBasicHead
	b[0] = byte(RTMP_CHUNK_HEAD_12 + head.ChunkBasicHeader.ChunkStreamID)
	utils.BigEndian.PutUint24(b[1:], head.ChunkMessageHeader.Timestamp)
	utils.BigEndian.PutUint24(b[4:], head.ChunkMessageHeader.MessageLength)
	b[7] = head.ChunkMessageHeader.MessageTypeID
	utils.LittleEndian.PutUint32(b[8:], uint32(head.ChunkMessageHeader.MessageStreamID))
	nc.Write(b)
	utils.RecycleSlice(b)
	nc.writeSeqNum += 12
	if head.ChunkMessageHeader.Timestamp == 0xffffff {
		b := utils.GetSlice(4)
		utils.LittleEndian.PutUint32(b, head.ChunkExtendedTimestamp.ExtendTimestamp)
		nc.Write(b)
		utils.RecycleSlice(b)
		nc.writeSeqNum += 4
	}
	if len(payload) > size {
		nc.Write(payload[0:size])
		nc.writeSeqNum += uint32(size)
		need = payload[size:]
	} else {
		nc.Write(payload)
		nc.writeSeqNum += uint32(len(payload))
	}
	return
}

func (nc *NetConnection) encodeChunk8(head *ChunkHeader, payload []byte, size int) (need []byte, err error) {
	if size > RTMP_MAX_CHUNK_SIZE || payload == nil || len(payload) == 0 {
		return nil, errors.New("chunk error")
	}
	b := utils.GetSlice(8)
	//chunkBasicHead
	b[0] = byte(RTMP_CHUNK_HEAD_8 + head.ChunkBasicHeader.ChunkStreamID)
	utils.BigEndian.PutUint24(b[1:], head.ChunkMessageHeader.Timestamp)
	utils.BigEndian.PutUint24(b[4:], head.ChunkMessageHeader.MessageLength)
	b[7] = head.ChunkMessageHeader.MessageTypeID
	nc.Write(b)
	utils.RecycleSlice(b)
	nc.writeSeqNum += 8
	if len(payload) > size {
		nc.Write(payload[0:size])
		nc.writeSeqNum += uint32(size)
		need = payload[size:]
	} else {
		nc.Write(payload)
		nc.writeSeqNum += uint32(len(payload))
	}
	return
}

func (nc *NetConnection) encodeChunk4(head *ChunkHeader, payload []byte, size int) (need []byte, err error) {
	if size > RTMP_MAX_CHUNK_SIZE || payload == nil || len(payload) == 0 {
		return nil, errors.New("chunk error")
	}
	b := utils.GetSlice(4)
	//chunkBasicHead
	b[0] = byte(RTMP_CHUNK_HEAD_4 + head.ChunkBasicHeader.ChunkStreamID)
	utils.BigEndian.PutUint24(b[1:], head.ChunkMessageHeader.Timestamp)
	nc.Write(b)
	utils.RecycleSlice(b)
	nc.writeSeqNum += 4
	if len(payload) > size {
		nc.Write(payload[0:size])
		nc.writeSeqNum += uint32(size)
		need = payload[size:]
	} else {
		nc.Write(payload)
		nc.writeSeqNum += uint32(len(payload))
	}
	return
}

func (nc *NetConnection) encodeChunk1(head *ChunkHeader, payload []byte, size int) (need []byte, err error) {
	if size > RTMP_MAX_CHUNK_SIZE || payload == nil || len(payload) == 0 {
		return nil, errors.New("chunk error")
	}
	chunkBasicHead := byte(RTMP_CHUNK_HEAD_1 + head.ChunkBasicHeader.ChunkStreamID)
	nc.WriteByte(chunkBasicHead)
	nc.writeSeqNum++
	if len(payload) > size {
		nc.Write(payload[0:size])
		nc.writeSeqNum += uint32(size)
		need = payload[size:]
	} else {
		nc.Write(payload)
		nc.writeSeqNum += uint32(len(payload))
	}
	return
}
