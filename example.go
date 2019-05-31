package rhub

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
)

type Conf struct {
	Redis string
}

var conf Conf

func init() {
	bs, err := ioutil.ReadFile("test.conf")
	if err != nil {
		fmt.Println("no test.conf in cwd")
		return
	}
	err = json.Unmarshal(bs, &conf)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(conf)
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path != "/" {
		http.Error(w, "Not found", 404)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	http.ServeFile(w, r, "home.html")
}

type User struct {
	Name string
}

func startServer() {
	hub := NewHub(1, conf.Redis, "test-room-1")
	hub.AfterJoin(func(c IClient) {
		hub.SendRedis("join", nil)
	})
	hub.AfterLeave(func(c IClient) {
		hub.SendRedis("leave", nil)
	})
	hub.On("join", func(m *MessageIn) {
		fmt.Println("join", *m)
	})
	hub.On("leave", func(m *MessageIn) {
		fmt.Println("leave", *m)
	})
	hub.OnWs("im", func(m *ClientMessage) {
		fmt.Println("ws receive im:", string(*m.Data))
		hub.SendRedisRaw(m.MessageIn)
	})
	hub.On("im", func(m *MessageIn) {
		var str string
		fmt.Println("redis receive im ", string(*m.Data))
		err := m.Decode(&str)
		fmt.Println(str, err)
		// hub.SendRedis("im", "your say:"+str, nil)
		hub.SendWsAll("im", "your say:"+str)
		if "close" == str {
			fmt.Println("close client")
			hub.Close()
			// go func() { hub.UnregisterChan() <- m.Client }()
		}
	})

	go hub.Run()
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		clientProp := map[string]interface{}{"user": &User{Name: "jim"}}
		ServeWs(hub, w, r, clientProp, DefaultUpgrader(), DefaultWsConfig())
	})
	fmt.Println("start server al 8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
