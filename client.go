package rtmp

import (
	"bufio"
	"net"
	"net/url"
	"strings"

	"github.com/Monibuca/engine/v4"
	"github.com/Monibuca/engine/v4/util"
	"go.uber.org/zap"
)

func NewRTMPClient(addr string) (client *NetConnection) {
	u, err := url.Parse(addr)
	if err != nil {
		plugin.Error("connect url parse", zap.Error(err))
		return
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		plugin.Error("dial tcp", zap.String("host", u.Host), zap.Error(err))
		return
	}
	client = &NetConnection{
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
	err = client.ClientHandshake()
	if err != nil {
		plugin.Error("handshake", zap.Error(err))
		return nil
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
			return nil
		}
		switch msg.MessageTypeID {
		case RTMP_MSG_AMF0_COMMAND:
			cmd := msg.MsgData.(Commander).GetCommand()
			switch cmd.CommandName {
			case "_result":
				response := msg.MsgData.(*ResponseMessage)
				if response.Infomation["code"] == NetConnection_Connect_Success {
					return
				} else {
					return nil
				}
			}
		}
	}
}

type RTMPPusher struct {
	RTMPSender
	engine.Pusher
}

func (pusher *RTMPPusher) OnEvent(event any) {
	pusher.RTMPSender.OnEvent(event)
	switch event.(type) {
	case *engine.Stream:
		pusher.NetConnection = NewRTMPClient(pusher.RemoteURL)
		if pusher.NetConnection != nil {
			pusher.SendCommand(SEND_CREATE_STREAM_MESSAGE, nil)
			go pusher.push()
		}
	case engine.PushEvent:
		pusher.ReConnectCount++
		if pusher.Stream == nil {
			plugin.Subscribe(pusher.StreamPath, pusher)
		}
	}
}

func (pusher *RTMPPusher) push() {
	defer pusher.Stop()
	for {
		msg, err := pusher.RecvMessage()
		if err != nil {
			break
		}
		switch msg.MessageTypeID {
		case RTMP_MSG_AMF0_COMMAND:
			cmd := msg.MsgData.(Commander).GetCommand()
			switch cmd.CommandName {
			case "_result":
				if response, ok := msg.MsgData.(*ResponseCreateStreamMessage); ok {
					pusher.StreamID = response.StreamId
					m := &PublishMessage{
						CURDStreamMessage{
							CommandMessage{
								"publish",
								0,
							},
							response.StreamId,
						},
						pusher.Stream.StreamName,
						"live",
					}
					pusher.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
				} else if response, ok := msg.MsgData.(*ResponsePublishMessage); ok {
					if response.Infomation["code"] == "NetStream.Publish.Start" {

					} else {
						return
					}
				}
			}
		}
	}
	if !pusher.Stream.IsClosed() && pusher.Reconnect() {
		pusher.OnEvent(engine.PullEvent(pusher.ReConnectCount))
	}
}

type RTMPPuller struct {
	RTMPReceiver
	engine.Puller
}

func (puller *RTMPPuller) OnEvent(event any) {
	puller.RTMPReceiver.OnEvent(event)
	switch event.(type) {
	case *engine.Stream:
		puller.NetConnection = NewRTMPClient(puller.RemoteURL)
		if puller.NetConnection != nil {
			puller.absTs = make(map[uint32]uint32)
			puller.SendCommand(SEND_CREATE_STREAM_MESSAGE, nil)
			go puller.pull()
			break
		}
	case engine.PullEvent:
		puller.ReConnectCount++
		if puller.Stream == nil {
			plugin.Publish(puller.StreamPath, puller)
		}
	}
}

func (puller *RTMPPuller) pull() {
	defer puller.Stop()
	for {
		msg, err := puller.RecvMessage()
		if err != nil {
			break
		}
		switch msg.MessageTypeID {
		case RTMP_MSG_AUDIO:
			puller.ReceiveAudio(msg)
		case RTMP_MSG_VIDEO:
			puller.ReceiveVideo(msg)
		case RTMP_MSG_AMF0_COMMAND:
			cmd := msg.MsgData.(Commander).GetCommand()
			switch cmd.CommandName {
			case "_result":
				if response, ok := msg.MsgData.(*ResponseCreateStreamMessage); ok {
					puller.StreamID = response.StreamId
					m := &PlayMessage{}
					m.CommandMessage.CommandName = "play"
					m.StreamName = puller.Stream.StreamName
					puller.SendMessage(RTMP_MSG_AMF0_COMMAND, m)
					// if response, ok := msg.MsgData.(*ResponsePlayMessage); ok {
					// 	if response.Object["code"] == "NetStream.Play.Start" {

					// 	} else if response.Object["level"] == Level_Error {
					// 		return errors.New(response.Object["code"].(string))
					// 	}
					// } else {
					// 	return errors.New("pull faild")
					// }
				}
			}
		}
	}
}
