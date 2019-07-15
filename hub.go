package rhub

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/gomodule/redigo/redis"
)

// hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients      map[IClient]bool
	psc          redis.PubSubConn
	redisConStr  string
	redisConSub  redis.Conn
	redisConPub  redis.Conn
	redisChannel string
	// Inbound messages from the clients.
	message      chan *ClientMessage
	redisMessage chan *MessageIn
	// Register requests from the clients.
	register chan IClient
	// Unregister requests from clients.
	unregister chan IClient
	closeChan  chan struct{}
	filters    []Filter
	handlers   map[string]Handler
	handlerWs  map[string]HandlerWs
	tick       *time.Ticker
	ticker     func(int)
	// hubs       *Hubs
	self IHub
	//identify hub
	id              interface{}
	beforeJoin      func(client IClient) error
	afterJoin       func(client IClient)
	beforeLeave     func(client IClient)
	afterLeave      func(client IClient)
	beforeWsMsg     func(msg *ClientMessage) bool
	seconds         int
	closed          bool
	redisResetCount int
}

func NewHub(id interface{}, redisConStr, redisChannel string) IHub {
	hub := &Hub{
		clients:      make(map[IClient]bool),
		redisConStr:  redisConStr,
		redisChannel: redisChannel,
		message:      make(chan *ClientMessage),
		redisMessage: make(chan *MessageIn),
		register:     make(chan IClient),
		unregister:   make(chan IClient),
		closeChan:    make(chan struct{}),
		handlers:     make(map[string]Handler),
		handlerWs:    make(map[string]HandlerWs),
		tick:         time.NewTicker(time.Second),
		id:           id,
		// psc:           redis.PubSubConn{Conn: redisConSub},
	}
	hub.ResetRedis()
	return hub
}

func (h *Hub) Run() {

	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
			debug.PrintStack()
			h.Run()
		}
	}()
	for {
		select {
		case client := <-h.register:
			// fmt.Println("Hub.register")
			if nil != h.beforeJoin {
				err := h.beforeJoin(client)
				if err != nil {
					return
				}
			}
			h.clients[client] = true
			if nil != h.afterJoin {
				h.afterJoin(client)
			}
		case client := <-h.unregister:
			// fmt.Println("Hub.unregister")
			if _, ok := h.clients[client]; ok {
				if nil != h.beforeLeave {
					h.beforeLeave(client)
				}
				delete(h.clients, client)
				close(client.SendChan())
				if nil != h.afterLeave {
					h.afterLeave(client)
				}
			}
		case msg := <-h.redisMessage:
			h.onRedisMessage(msg)
		case message := <-h.message:
			if nil != h.beforeWsMsg && !h.beforeWsMsg(message) {
				continue
			}
			h.onWsMessage(message)
		case <-h.closeChan:
			return
		case <-h.tick.C:
			h.seconds++
			if h.ticker != nil {
				h.ticker(h.seconds)
			}
		}
	}
}

func (h *Hub) ResetRedis() error {
	if nil != h.redisConPub {
		h.redisConPub.Close()
	}
	if nil != h.redisConSub {
		h.psc.Unsubscribe(h.redisChannel)
		h.redisConSub.Close()
	}
	var err error
	h.redisConSub, err = redis.DialURL(h.redisConStr)
	if err != nil {
		return err
	}
	h.redisConPub, err = redis.DialURL(h.redisConStr)
	if err != nil {
		return err
	}
	h.psc = redis.PubSubConn{Conn: h.redisConSub}
	go func() {
		h.psc.Subscribe(h.redisChannel)
		h.initRedisMessageChannel()
	}()
	return err
}
func (h *Hub) initRedisMessageChannel() {
	for {
		switch v := h.psc.Receive().(type) {
		case redis.Message:
			if h.redisResetCount > 0 {
				h.redisResetCount = 0
			}
			var msg MessageIn
			json.Unmarshal(v.Data, &msg)
			h.redisMessage <- &msg
		case redis.Subscription:
			// fmt.Printf("%s: %s %d\n", v.Channel, v.Kind, v.Count)
		case error:
			if h.closed {
				return
			}
			// if !h.closed {
			fmt.Println("Hub.onRedisMessage error, reset now.", v)
			sleepTime := time.Duration(h.redisResetCount) * time.Second
			if sleepTime > 3*time.Second {
				sleepTime = 3 * time.Second
			}
			if h.redisResetCount > 100 {
				return
			}
			time.Sleep(sleepTime)
			h.ResetRedis()
			h.redisResetCount++
			return
			// }
			// return
		}
	}
}

func (h *Hub) OnTick(cb func(int)) {
	h.ticker = cb
}
func (h *Hub) onRedisMessage(msg *MessageIn) {
	if handler, ok := h.handlers[msg.Subject]; ok {
		// h.Filter(handler, msg)
		handler(msg)
	}
}
func (h *Hub) onWsMessage(msg *ClientMessage) {
	if handler, ok := h.handlerWs[msg.Subject]; ok {
		handler(msg)
	} else {
		h.SendRedisRaw(msg.MessageIn)
	}

}
func (h *Hub) SendRedisRaw(msg *MessageIn) {
	// var rmsg MessageIn
	// rmsg.MessageIn = msg
	// rmsg.Props = props
	bs, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	_, err = h.redisConPub.Do("PUBLISH", h.redisChannel, string(bs))
	if err != nil {
		// panic(err)
		fmt.Println(err)
		h.ResetRedis()
	}
	// h.redisCon.Flush()
	// if nil != err {
	// 	fmt.Println("Send err", err)
	// }
	// return err
}
func (h *Hub) SendRedis(subject string, data interface{}) {
	dataBs, _ := json.Marshal(data)
	dataBsRaw := json.RawMessage(dataBs)
	h.SendRedisRaw(&MessageIn{Subject: subject, Data: &dataBsRaw})
}
func (h *Hub) GetSeconds() int {
	return h.seconds
}

// func (h *Hub) Filter(handler Handler, msg *MessageIn) {
// 	if len(h.filters) <= 0 {
// 		handler(msg)
// 		return
// 	}
// 	pos := 0
// 	filter := h.filters[pos]
// 	var next func()
// 	next = func() {
// 		pos++
// 		if pos >= len(h.filters) {
// 			handler(msg)
// 			return
// 		}
// 		filter = h.filters[pos]
// 		filter(msg, next)
// 	}
// 	filter(msg, next)

// }

func (h *Hub) BeforeJoin(callback func(client IClient) error) {
	h.beforeJoin = callback
}
func (h *Hub) AfterJoin(callback func(client IClient)) {
	h.afterJoin = callback
}
func (h *Hub) AfterLeave(callback func(client IClient)) {
	h.afterLeave = callback
}
func (h *Hub) BeforeLeave(callback func(client IClient)) {
	h.beforeLeave = callback
}
func (h *Hub) BeforeWsMsg(callback func(msg *ClientMessage) bool) {
	h.beforeWsMsg = callback
}

// func (h *Hub) Use(filter Filter) {
// 	h.filters = append(h.filters, filter)
// }

// func (h *Hub) Emit(msg *ClientMessage) {
// 	h.message <- msg
// }

func (h *Hub) On(subject string, handler Handler) {
	h.handlers[subject] = handler
}

func (h *Hub) Off(subject string, handler Handler) {
	delete(h.handlers, subject)
}
func (h *Hub) OnWs(subject string, handler HandlerWs) {
	h.handlerWs[subject] = handler
}

func (h *Hub) OffWs(subject string, handler HandlerWs) {
	delete(h.handlerWs, subject)
}
func (h *Hub) Close() {
	// h.hubs.RemoveHub(h.Id())
	h.closed = true
	go func() {
		defer func() {
			if err := recover(); err != nil {
				fmt.Println(err)
			}
		}()
		h.redisConPub.Close()
		h.redisConSub.Close()
		h.tick.Stop()
		for client, _ := range h.clients {
			// client.Close()
			select {
			case h.unregister <- client:
			default:
				close(client.SendChan())
				delete(h.clients, client)
			}
		}
		h.closeChan <- struct{}{}
		close(h.closeChan)
		close(h.register)
		close(h.unregister)
		close(h.message)
		close(h.redisMessage)

	}()
}
func (h *Hub) Id() interface{} {
	return h.id
}
func (h *Hub) ClientList() []IClient {
	r := make([]IClient, len(h.clients))
	i := 0
	for k := range h.clients {
		r[i] = k
		i++
	}
	return r
}
func (h *Hub) SendWsAll(subject string, message interface{}) {
	h.SendWs(subject, message, h.ClientList()...)
}
func (h *Hub) SendWsClient(client IClient, subject string, message interface{}) {
	bs, err := Encode(subject, message)
	if err != nil {
		fmt.Println(err)
	}
	h.SendWsBytes(client, bs)
}
func (h *Hub) SendWsBytes(client IClient, bs []byte) {
	select {
	case client.SendChan() <- bs:
	default:
		close(client.SendChan())
		delete(h.clients, client)
	}
}

func (h *Hub) SendWs(subject string, message interface{}, receivers ...IClient) {
	bs, err := Encode(subject, message)
	if err != nil {
		fmt.Println(err)
	}
	for _, client := range receivers {
		if client == nil {
			continue
		}
		h.SendWsBytes(client, bs)
	}
}
func (h *Hub) EchoWs(msg *ClientMessage) {
	h.SendWsAll(msg.Subject, msg.Data)
}

// func (h *Hub) SetSelf(self IHub) {
// 	h.self = self
// }
func (h *Hub) CloseChan() chan struct{} {
	return h.closeChan
}
func (h *Hub) RegisterChan() chan IClient {
	return h.register
}
func (h *Hub) UnregisterChan() chan IClient {
	return h.unregister
}

// RegisterChan() chan *Client
func (h *Hub) MessageChan() chan *ClientMessage {
	return h.message
}
func (h *Hub) Clients() map[IClient]bool {
	return h.clients
}
