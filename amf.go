package rtmp

import (

	"github.com/Monibuca/engine/v4/util"
)

// Action Message Format -- AMF 0
// Action Message Format -- AMF 3
// http://download.macromedia.com/pub/labs/amf/amf0_spec_121207.pdf
// http://wwwimages.adobe.com/www.adobe.com/content/dam/Adobe/en/devnet/amf/pdf/amf-file-format-spec.pdf

// AMF Object == AMF Object Type(1 byte) + AMF Object Value
//
// AMF Object Value :
// AMF0_STRING : 2 bytes(datasize,记录string的长度) + data(string)
// AMF0_OBJECT : AMF0_STRING + AMF Object
// AMF0_NULL : 0 byte
// AMF0_NUMBER : 8 bytes
// AMF0_DATE : 10 bytes
// AMF0_BOOLEAN : 1 byte
// AMF0_ECMA_ARRAY : 4 bytes(arraysize,记录数组的长度) + AMF0_OBJECT
// AMF0_STRICT_ARRAY : 4 bytes(arraysize,记录数组的长度) + AMF Object

// 实际测试时,AMF0_ECMA_ARRAY数据如下:
// 8 0 0 0 13 0 8 100 117 114 97 116 105 111 110 0 0 0 0 0 0 0 0 0 0 5 119 105 100 116 104 0 64 158 0 0 0 0 0 0 0 6 104 101 105 103 104 116 0 64 144 224 0 0 0 0 0
// 8 0 0 0 13 | { 0 8 100 117 114 97 116 105 111 110 --- 0 0 0 0 0 0 0 0 0 } | { 0 5 119 105 100 116 104 --- 0 64 158 0 0 0 0 0 0 } | { 0 6 104 101 105 103 104 116 --- 0 64 144 224 0 0 0 0 0 } |...
// 13 | {AMF0_STRING --- AMF0_NUMBER} | {AMF0_STRING --- AMF0_NUMBER} | {AMF0_STRING --- AMF0_NUMBER} | ...
// 13 | {AMF0_OBJECT} | {AMF0_OBJECT} | {AMF0_OBJECT} | ...
// 13 | {duration --- 0} | {width --- 1920} | {height --- 1080} | ...

const (
	AMF0_NUMBER         = 0x00 // 浮点数
	AMF0_BOOLEAN        = 0x01 // 布尔型
	AMF0_STRING         = 0x02 // 字符串
	AMF0_OBJECT         = 0x03 // 对象,开始
	AMF0_MOVIECLIP      = 0x04
	AMF0_NULL           = 0x05 // null
	AMF0_UNDEFINED      = 0x06
	AMF0_REFERENCE      = 0x07
	AMF0_ECMA_ARRAY     = 0x08
	AMF0_END_OBJECT     = 0x09 // 对象,结束
	AMF0_STRICT_ARRAY   = 0x0A
	AMF0_DATE           = 0x0B // 日期
	AMF0_LONG_STRING    = 0x0C // 字符串
	AMF0_UNSUPPORTED    = 0x0D
	AMF0_RECORDSET      = 0x0E
	AMF0_XML_DOCUMENT   = 0x0F
	AMF0_TYPED_OBJECT   = 0x10
	AMF0_AVMPLUS_OBJECT = 0x11

	AMF3_UNDEFINED     = 0x00
	AMF3_NULL          = 0x01
	AMF3_FALSE         = 0x02
	AMF3_TRUE          = 0x03
	AMF3_INTEGER       = 0x04
	AMF3_DOUBLE        = 0x05
	AMF3_STRING        = 0x06
	AMF3_XML_DOC       = 0x07
	AMF3_DATE          = 0x08
	AMF3_ARRAY         = 0x09
	AMF3_OBJECT        = 0x0A
	AMF3_XML           = 0x0B
	AMF3_BYTE_ARRAY    = 0x0C
	AMF3_VECTOR_INT    = 0x0D
	AMF3_VECTOR_UINT   = 0x0E
	AMF3_VECTOR_DOUBLE = 0x0F
	AMF3_VECTOR_OBJECT = 0x10
	AMF3_DICTIONARY    = 0x11
)

var END_OBJ = []byte{0, 0, AMF0_END_OBJECT}

type AMFValue any

type AMFObject map[string]AMFValue

type AMF struct {
	util.Buffer
}

var ObjectEnd = &struct{}{}
var Undefined = &struct{}{}

func (amf *AMF) decodeObject() (obj AMFValue) {
	if !amf.CanRead() {
		return nil
	}
	t := amf.Buffer[0]
	switch t {
	case AMF0_NUMBER:
		return amf.readNumber()
	case AMF0_BOOLEAN:
		return amf.readBool()
	case AMF0_STRING:
		return amf.readString()
	case AMF0_OBJECT:
		return amf.readObject()
	case AMF0_MOVIECLIP:
		plugin.Println("This type is not supported and is reserved for future use.(AMF0_MOVIECLIP)")
	case AMF0_NULL:
		return amf.readNull()
	case AMF0_UNDEFINED:
		amf.ReadByte()
		return Undefined
	case AMF0_REFERENCE:
		plugin.Println("reference-type.(AMF0_REFERENCE)")
	case AMF0_ECMA_ARRAY:
		return amf.readECMAArray()
	case AMF0_END_OBJECT:
		amf.ReadByte()
		return ObjectEnd
	case AMF0_STRICT_ARRAY:
		return amf.readStrictArray()
	case AMF0_DATE:
		return amf.readDate()
	case AMF0_LONG_STRING,
		AMF0_XML_DOCUMENT:
		return amf.readLongString()
	case AMF0_UNSUPPORTED:
		plugin.Println("If a type cannot be serialized a special unsupported marker can be used in place of the type.(AMF0_UNSUPPORTED)")
	case AMF0_RECORDSET:
		plugin.Println("This type is not supported and is reserved for future use.(AMF0_RECORDSET)")
	case AMF0_TYPED_OBJECT:
		plugin.Println("If a strongly typed object has an alias registered for its class then the type name will also be serialized. Typed objects are considered complex types and reoccurring instances can be sent by reference.(AMF0_TYPED_OBJECT)")
	case AMF0_AVMPLUS_OBJECT:
		plugin.Println("AMF0_AVMPLUS_OBJECT")
	default:
		plugin.Warnf("Unsupported type %v", t)
	}
	return nil
}

func (amf *AMF) writeObject(t AMFObject) {
	if t == nil {
		return
	}
	amf.Malloc(1)[0] = AMF0_OBJECT
	defer amf.Write(END_OBJ)
	for k, vv := range t {
		switch vvv := vv.(type) {
		case string:
			amf.writeObjectString(k, vvv)
		case float64, uint, float32, int, int16, int32, int64, uint16, uint32, uint64, uint8, int8:
			amf.writeObjectNumber(k, util.ToFloat64(vv))
		case bool:
			amf.writeObjectBool(k, vvv)
		}
	}
	return
}

func (amf *AMF) readDate() uint64 {
	amf.ReadByte()
	ret := amf.ReadUint64()
	amf.ReadN(2) // timezone
	return ret
}

func (amf *AMF) readStrictArray() (list []AMFValue) {
	amf.ReadByte()
	size := amf.ReadUint16()
	for i := uint16(0); i < size; i++ {
		list = append(list, amf.decodeObject())
	}
	return
}

func (amf *AMF) readECMAArray() (m AMFObject) {
	m = make(AMFObject, 0)
	amf.ReadByte()
	size := amf.ReadUint16()
	for i := uint16(0); i < size; i++ {
		k := amf.readString1()
		v := amf.decodeObject()
		if k == "" && v == ObjectEnd {
			return
		}
		m[k] = v
	}
	return
}

func (amf *AMF) readString() string {
	if amf.Len() == 0 {
		return ""
	}
	amf.ReadByte()
	return amf.readString1()
}

func (amf *AMF) readString1() string {
	return string(amf.ReadN(int(amf.ReadUint16())))
}

func (amf *AMF) readLongString() string {
	amf.ReadByte()
	return string(amf.ReadN(int(amf.ReadUint32())))
}

func (amf *AMF) readNull() any {
	amf.ReadByte()
	return nil
}

func (amf *AMF) readNumber() (num float64) {
	if amf.Len() == 0 {
		return 0
	}
	amf.ReadByte()
	return amf.ReadFloat64()
}

func (amf *AMF) readBool() bool {
	if amf.Len() == 0 {
		return false
	}
	amf.ReadByte()
	return amf.ReadByte() == 1
}

func (amf *AMF) readObject() (m AMFObject) {
	if amf.Len() == 0 {
		return nil
	}
	amf.ReadByte()
	m = make(AMFObject, 0)
	for {
		k := amf.readString1()
		v := amf.decodeObject()
		if k == "" && v == ObjectEnd {
			return
		}
		m[k] = v
	}
}

func (amf *AMF) writeSize16(l int) {
	util.PutBE(amf.Malloc(2), l)
}

func (amf *AMF) writeString(value string) {
	amf.WriteByte(AMF0_STRING)
	amf.writeSize16(len(value))
	amf.WriteString(value)
}

func (amf *AMF) writeNull() {
	amf.WriteByte(AMF0_NULL)
}

func (amf *AMF) writeBool(b bool) {
	amf.WriteByte(AMF0_BOOLEAN)
	if b {
		amf.WriteByte(1)
	}
	amf.WriteByte(0)
}

func (amf *AMF) writeNumber(b float64) {
	amf.WriteByte(AMF0_NUMBER)
	amf.WriteFloat64(b)
}

func (amf *AMF) writeKey(key string) {
	amf.writeSize16(len(key))
	amf.WriteString(key)
}

func (amf *AMF) writeObjectString(key, value string) {
	amf.writeKey(key)
	amf.writeString(value)
}

func (amf *AMF) writeObjectBool(key string, f bool) {
	amf.writeKey(key)
	amf.writeBool(f)
}

func (amf *AMF) writeObjectNumber(key string, value float64) {
	amf.writeKey(key)
	amf.writeNumber(value)
}