package rhub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// const (
// 	// Time allowed to write a message to the peer.
// 	writeWait = 10 * time.Second

// 	// Time allowed to read the next pong message from the peer.
// 	PongWait = 20 * time.Second

// 	// Send pings to peer with this period. Must be less than pongWait.
// 	pingPeriod = (PongWait * 9) / 10

// 	// Maximum message size allowed from peer.
// 	maxMessageSize = 512
// )

type WsConfig struct {
	WriteWait, PongWait, pingPeriod time.Duration
	MaxMessageSize                  int64
}

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub    IHub
	config WsConfig

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte
	// props sync.Map
	Props map[string]interface{}
	// user       interface{}
}

func NewClient(conn *websocket.Conn, hub IHub, config WsConfig) IClient {
	client := &Client{
		hub:    hub,
		config: config,
		conn:   conn,
		send:   make(chan []byte, 256),
		Props:  make(map[string]interface{}),
	}

	return client
}
func (c *Client) GetClient() *Client {
	return c
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.UnregisterChan() <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(c.config.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		// c.hub.broadcast <- message
		msg, err := c.NewClientMessage(message)
		if err != nil {
			// c.hub.message <- msg
			fmt.Println("Client.ReadPump", err)
			continue
		}
		c.hub.MessageChan() <- msg
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) WritePump() {
	ticker := time.NewTicker(c.config.pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			// n := len(c.send)
			// for i := 0; i < n; i++ {
			// 	w.Write(newline)
			// 	w.Write(<-c.send)
			// }

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}
func (c *Client) NewClientMessage(data []byte) (*ClientMessage, error) {
	msg, err := c.DecodeMessage(data)
	if err != nil {
		return nil, err
	}
	return &ClientMessage{
		Client:    c,
		MessageIn: msg,
	}, nil
}
func (c *Client) DecodeMessage(data []byte) (*MessageIn, error) {
	msg := new(MessageIn)
	err := json.Unmarshal(data, msg)
	if nil != err {
		fmt.Println("DecodeMessage failed.", err)
	}
	return msg, err
}
func (c *Client) EncodeMessage(msg interface{}) []byte {
	bs, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return bs
}

func (c *Client) Encode(subject string, msg interface{}) []byte {
	return c.EncodeMessage(&MessageOut{Subject: subject, Data: msg})
}
func Encode(subject string, msg interface{}) ([]byte, error) {
	msg = MessageOut{Subject: subject, Data: msg}
	bs, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return bs, err
}

// func (c *Client) Send(subject string, msg interface{}) {
// 	c.send <- c.Encode(subject, msg)
// }
func (c *Client) SendChan() chan []byte {
	return c.send
}

//Close client ,in message loop should in go client.Close()
func (c *Client) Close() {
	c.hub.UnregisterChan() <- c
}
func (c *Client) GetProps() map[string]interface{} {
	return c.Props
}
func (c *Client) Hub() IHub {
	return c.hub
}

func DefaultUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
}

func DefaultWsConfig() WsConfig {
	return WsConfig{
		WriteWait:      10 * time.Second,
		PongWait:       20 * time.Second,
		MaxMessageSize: 4 * 1024,
	}
}

// serveWs handles websocket requests from the peer.
func ServeWs(hub IHub, w http.ResponseWriter, r *http.Request, clientKeyValues map[string]interface{}, upgrader websocket.Upgrader, config WsConfig) {
	config.pingPeriod = (config.PongWait * 9) / 10
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := NewClient(conn, hub, config)
	props := client.GetProps()
	if nil != clientKeyValues {
		for k, v := range clientKeyValues {
			props[k] = v
		}
	}
	client.Hub().RegisterChan() <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.WritePump()
	go client.ReadPump()
}
