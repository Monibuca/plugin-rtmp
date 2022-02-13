package rtmp

import (
	"bufio"
	"net"
	"net/url"
	"strings"

	"github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/util"
)

type RTMPClient struct {
	NetConnection
}

func (client *RTMPClient) Connect(addr string) bool {
	u, err := url.Parse(addr)
	if err != nil {
		plugin.Error(err)
		return false
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		plugin.Error(err)
		return false
	}
	client.NetConnection = NetConnection{
		TCPConn:            conn.(*net.TCPConn),
		Reader:             bufio.NewReader(conn),
		writeChunkSize:     RTMP_DEFAULT_CHUNK_SIZE,
		readChunkSize:      RTMP_DEFAULT_CHUNK_SIZE,
		rtmpHeader:         make(map[uint32]*ChunkHeader),
		incompleteRtmpBody: make(map[uint32]util.Buffer),
		bandwidth:          RTMP_MAX_CHUNK_SIZE << 3,
		tmpBuf:             make([]byte, 4),
		// subscribers:        make(map[uint32]*engine.Subscriber),
	}
	err = client.Handshake()
	if err != nil {
		plugin.Error(err)
		return false
	}
	connectArg := make(AMFObject)
	connectArg["swfUrl"] = addr
	connectArg["tcUrl"] = addr
	connectArg["flashVer"] = "monibuca/" + engine.Engine.Version
	ps := strings.Split(u.Path, "/")
	connectArg["app"] = ps[0]
	client.SendCommand(SEND_CONNECT_MESSAGE, connectArg)
	for {
		msg, err := client.RecvMessage()
		if err != nil {
			return false
		}
		switch msg.MessageTypeID {
		case RTMP_MSG_AMF0_COMMAND:
			cmd := msg.MsgData.(Commander).GetCommand()
			switch cmd.CommandName {
			case "_result":
				response := msg.MsgData.(*ResponseMessage)
				if response.Infomation["code"] == NetConnection_Connect_Success {
					return true
				} else {
					return false
				}
			}
		}
	}
}

var _ engine.IPusher = (*RTMPPusher)(nil)
var _ engine.IPuller = (*RTMPPuller)(nil)

type RTMPPusher struct {
	engine.Pusher
	RTMPClient
}

func (pusher *RTMPPusher) push() {
	SendMedia(&pusher.NetConnection, &pusher.Subscriber)
	pusher.NetConnection.Close()
}

func (pusher *RTMPPusher) Push(count int) {
	if pusher.Connect(pusher.RemoteURL) {
		pusher.SendCommand(SEND_CREATE_STREAM_MESSAGE, nil)
		for {
			msg, err := pusher.RecvMessage()
			if err != nil {
				return
			}
			switch msg.MessageTypeID {
			case RTMP_MSG_AMF0_COMMAND:
				cmd := msg.MsgData.(Commander).GetCommand()
				switch cmd.CommandName {
				case "_result":
					if response, ok := msg.MsgData.(*ResponseCreateStreamMessage); ok {
						arg := make(AMFObject)
						arg["streamid"] = response.StreamId
						pusher.SendCommand(SEND_PUBLISH_START_MESSAGE, arg)
					} else if response, ok := msg.MsgData.(*ResponsePublishMessage); ok {
						if response.Infomation["code"] == "NetStream.Publish.Start" {
							go pusher.push()
						} else {
							return
						}
					}
				}
			}
		}
	}
}

type RTMPPuller struct {
	engine.Puller
	RTMPClient
}

func (puller *RTMPPuller) pull() {
	for {
		msg, err := puller.RecvMessage()
		if err != nil {
			return
		}
		switch msg.MessageTypeID {
		case RTMP_MSG_AUDIO:
			puller.ReceiveAudio(msg)
		case RTMP_MSG_VIDEO:
			puller.ReceiveVideo(msg)
			// case RTMP_MSG_AMF0_COMMAND:
			// 	cmd := msg.MsgData.(Commander).GetCommand()
			// 	switch cmd.CommandName {
			// 	case "_result":
			// 		if response, ok := msg.MsgData.(*ResponsePlayMessage); ok {
			// 			if response.Object["code"] == "NetStream.Play.Start" {

			// 			} else if response.Object["level"] == Level_Error {
			// 				return errors.New(response.Object["code"].(string))
			// 			}
			// 		} else {
			// 			return errors.New("pull faild")
			// 		}
			// 	}
			// }
		}
	}
}
func (puller *RTMPPuller) Pull(count int) {
	if puller.Connect(puller.RemoteURL) {
		puller.SendCommand(SEND_PLAY_MESSAGE, AMFObject{"StreamPath": puller.StreamName})
		puller.MediaReceiver = NewMediaReceiver(&puller.Publisher)
		go puller.pull()
	}
}
