package rhub

import (
	"encoding/json"
)

type Handler func(msg *MessageIn)
type Filter func(msg *MessageIn, next func())
type HandlerWs func(message *ClientMessage)

//IHub like chat room
type IHub interface {
	//Get hub's id
	Id() interface{}
	//before new client join
	BeforeJoin(callback func(client IClient) error)
	AfterJoin(callback func(client IClient))
	//On client leave
	BeforeLeave(callback func(client IClient))
	AfterLeave(callback func(client IClient))
	BeforeWsMsg(callback func(msg *ClientMessage) bool)
	//Add filter
	// Use(filter Filter)
	// Attach an event handler function
	On(subject string, handler Handler)
	OnWs(subject string, handler HandlerWs)
	// OnLocal(subject string, handler HandlerLocal)
	//simulate client send msg
	// Emit(msg *ClientMessage)
	//Dettach an event handler function
	Off(subject string, handler Handler)
	OffWs(subject string, handler HandlerWs)
	SendRedisRaw(msg *MessageIn)
	SendRedis(subject string, data interface{})
	//Send message to all clients
	SendWsAll(subject string, message interface{})
	SendWs(subject string, message interface{}, receivers []IClient)
	EchoWs(msg *ClientMessage)
	Close()
	// CloseMessageLoop()
	// SetSelf(self IHub)
	Run()
	UnregisterChan() chan IClient
	RegisterChan() chan IClient
	MessageChan() chan *ClientMessage
	// MessageLocalChan() chan<- MessageLocal
	CloseChan() chan struct{}
	Clients() map[IClient]bool

	OnTick(func(int))
	GetSeconds() int
	SendWsClient(client IClient, subject string, message interface{})
	SendWsBytes(client IClient, bs []byte)
}
type IClient interface {
	Close()
	// Send() chan []byte
	SendChan() chan []byte
	// Send(subject string, msg interface{})
	Hub() IHub
	WritePump()
	ReadPump()
	GetProps() map[string]interface{}
	// Get(key interface{}) interface{}
	// Set(key, value interface{})
	NewClientMessage(data []byte) (*ClientMessage, error)
	GetClient() *Client
}

type IFilters interface {
	Do(fn Handler)
}

//find callbacks in hub
type IRoute interface {
	Route(subject string) []Handler
	// Attach an event handler function
	On(subject string, handler Handler)
	//Dettach an event handler function
	Off(subject string, handler Handler)
}
type MessageLocal struct {
	Subject string
	Data    interface{}
	Error   error
}

//send Message should have this format
type MessageOut struct {
	Subject string
	Data    interface{}
}
type MessageIn struct {
	Subject string
	Data    *json.RawMessage
}

// type RedisHubMessage struct {
// 	*MessageIn
// }
type ClientMessage struct {
	*MessageIn
	Client IClient
}

func NewMessageIn(subject string, data interface{}) *MessageIn {
	j, _ := json.Marshal(data)
	r := json.RawMessage(j)
	return &MessageIn{Subject: subject, Data: &r}
}

func NewMessage(subject string, data interface{}, client IClient) *ClientMessage {
	r := &ClientMessage{Client: client}
	r.MessageIn = &MessageIn{Subject: subject}
	r.MessageIn.SetData(data)
	return r
}
func (m *MessageIn) SetData(data interface{}) error {
	bs, err := json.Marshal(data)
	if err != nil {
		return err
	}
	r := json.RawMessage(bs)
	m.Data = &r
	return nil
}
func (m *MessageIn) Decode(obj interface{}) error {
	if nil != m.Data {
		bs, err := m.Data.MarshalJSON()
		err = json.Unmarshal(bs, &obj)
		return err
	}
	return nil
}

func (m MessageOut) Encode() ([]byte, error) {
	return json.Marshal(m)
}
