package rtmp

import (
	"bufio"
	"errors"
	"net"
	"net/url"
	"strings"

	"go.uber.org/zap"
	"m7s.live/engine/v4"
	"m7s.live/engine/v4/util"
)

func NewRTMPClient(addr string) (client *NetConnection, err error) {
	u, err := url.Parse(addr)
	if err != nil {
		RTMPPlugin.Error("connect url parse", zap.Error(err))
		return nil, err
	}
	if strings.Count(u.Host, ":") == 0 {
		u.Host += ":1935"
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		RTMPPlugin.Error("dial tcp", zap.String("host", u.Host), zap.Error(err))
		return nil, err
	}
	client = &NetConnection{
		TCPConn:            conn.(*net.TCPConn),
		Reader:             bufio.NewReader(conn),
		writeChunkSize:     conf.ChunkSize,
		readChunkSize:      RTMP_DEFAULT_CHUNK_SIZE,
		rtmpHeader:         make(map[uint32]*ChunkHeader),
		incompleteRtmpBody: make(map[uint32]*util.Buffer),
		bandwidth:          RTMP_MAX_CHUNK_SIZE << 3,
		tmpBuf:             make([]byte, 4),
	}
	err = client.ClientHandshake()
	if err != nil {
		RTMPPlugin.Error("handshake", zap.Error(err))
		return nil, err
	}
	ps := strings.Split(u.Path, "/")
	client.appName = ps[1]
	err = client.SendMessage(RTMP_MSG_CHUNK_SIZE, Uint32Message(conf.ChunkSize))
	client.SendMessage(RTMP_MSG_AMF0_COMMAND, &CallMessage{
		CommandMessage{"connect", 1},
		AMFObject{
			"app":      client.appName,
			"flashVer": "monibuca/" + engine.Engine.Version,
			"swfUrl":   addr,
			"tcUrl":    addr,
		},
		nil,
	})
	for {
		msg, err := client.RecvMessage()
		if err != nil {
			return nil, err
		}
		switch msg.MessageTypeID {
		case RTMP_MSG_AMF0_COMMAND:
			cmd := msg.MsgData.(Commander).GetCommand()
			switch cmd.CommandName {
			case "_result":
				response := msg.MsgData.(*ResponseMessage)
				if response.Infomation["code"] == NetConnection_Connect_Success {
					return client, nil
				} else {
					return nil, err
				}
			}
		}
	}
}

type RTMPPusher struct {
	RTMPSender
	engine.Pusher
}

func (pusher *RTMPPusher) Connect() (err error) {
	pusher.NetConnection, err = NewRTMPClient(pusher.RemoteURL)
	RTMPPlugin.Info("connect", zap.String("remoteURL", pusher.RemoteURL))
	return
}

func (pusher *RTMPPusher) Push() error {
	pusher.SendMessage(RTMP_MSG_AMF0_COMMAND, &CommandMessage{"createStream", 2})
	defer pusher.Stop()
	for {
		msg, err := pusher.RecvMessage()
		if err != nil {
			return err
		}
		switch msg.MessageTypeID {
		case RTMP_MSG_AMF0_COMMAND:
			cmd := msg.MsgData.(Commander).GetCommand()
			switch cmd.CommandName {
			case Response_Result, Response_OnStatus:
				if response, ok := msg.MsgData.(*ResponseCreateStreamMessage); ok {
					pusher.StreamID = response.StreamId
					URL, _ := url.Parse(pusher.RemoteURL)
					ps := strings.Split(URL.Path, "/")
					pusher.Args = URL.Query()
					pusher.SendMessage(RTMP_MSG_AMF0_COMMAND, &PublishMessage{
						CURDStreamMessage{
							CommandMessage{
								"publish",
								1,
							},
							response.StreamId,
						},
						ps[len(ps)-1],
						"live",
					})
				} else if response, ok := msg.MsgData.(*ResponsePublishMessage); ok {
					if response.Infomation["code"] == NetStream_Publish_Start {
						go pusher.PlayRaw()
					} else {
						return errors.New(response.Infomation["code"].(string))
					}
				}
			}
		}
	}
}

type RTMPPuller struct {
	RTMPReceiver
	engine.Puller
}

func (puller *RTMPPuller) Connect() (err error) {
	puller.NetConnection, err = NewRTMPClient(puller.RemoteURL)
	RTMPPlugin.Info("connect", zap.String("remoteURL", puller.RemoteURL))
	return
}

func (puller *RTMPPuller) Pull() {
	puller.absTs = make(map[uint32]uint32)
	puller.SendMessage(RTMP_MSG_AMF0_COMMAND, &CommandMessage{"createStream", 2})
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
					m.StreamId = response.StreamId
					m.TransactionId = 1
					m.CommandMessage.CommandName = "play"
					URL, _ := url.Parse(puller.RemoteURL)
					ps := strings.Split(URL.Path, "/")
					puller.Args = URL.Query()
					m.StreamName = ps[len(ps)-1]
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
