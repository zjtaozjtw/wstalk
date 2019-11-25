/****************************************************
1.建立ws
2.监听同时开启start
3.对于每个监听到的ws链接，分别创建一个write和read协程
  read协程用来从链接中读数据放到broadcast中
  write协程用来把send中的内容发送给客户端
  在start函数中，会把broadcast中的数据放到send中
****************************************************/
package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"
	"net/http"
)

type ClientManager struct {
	clients    map[*Client]bool //用来保存客户端的websocket链接，其中bool表示客户端是否在线
	broadcast  chan []byte      //广播，客户端发送的消息会转发给所有在线的客户端
	register   chan *Client     //保存客户端信息的，新建立一个websocket链接的时候，客户端信息先保存到register中
	unregister chan *Client     //也是用来保存客户端信息的，当websocket链接断开的时候，把客户端信息放到unregister中
}

type Client struct {
	id   string          //客户端id，随机生成
	conn *websocket.Conn //websocket链接
	send chan []byte     //发送给客户端的信息
}

//会把Message格式化成json
type Message struct {
	//消息struct
	Sender    string `json:"sender,omitempty"`    //发送者
	Recipient string `json:"recipient,omitempty"` //接收者
	Content   string `json:"content,omitempty"`   //内容
}

var manager = ClientManager{
	clients:    make(map[*Client]bool),
	broadcast:  make(chan []byte),
	register:   make(chan *Client),
	unregister: make(chan *Client),
}

func (manager *ClientManager) start() {
	for {
		select {
		//当有新的websocket链接时
		case conn := <-manager.register: //监听，当有新链接的时候，保存链接，并告诉所有客户端我连上来了
			manager.clients[conn] = true //表示在线
			jsonMessage, _ := json.Marshal(&Message{Content: "A new socket has connect."})
			manager.send(jsonMessage, conn) //把jsonMessage发送给客户端

			//有链接要断开了
		case conn := <-manager.unregister:
			if _, ok := manager.clients[conn]; ok {
				close(conn.send)
				delete(manager.clients, conn)
				jsonMessage, _ := json.Marshal(&Message{Content: "A socket has disconnect."})
				manager.send(jsonMessage, conn) //告诉其他客户端，断开链接

			}

			//广播，向所有客户端发消息
		case message := <-manager.broadcast:
			for conn := range manager.clients {
				select {
				case conn.send <- message: //遍历已经连接的客户端，把消息发送给他们
				default:
					close(conn.send)
					delete(manager.clients, conn)

				}
			}
		}

	}
}

//向所有客户端发送消息
func (manager *ClientManager) send(message []byte, ignore *Client) {
	for conn := range manager.clients {
		if conn != ignore {
			conn.send <- message
		}
	}
}

//从websocket链接中读message放到broadca中
func (c *Client) read() {
	defer func() {
		manager.unregister <- c
		c.conn.Close()
	}()

	for {
		//读取错误，则注销该链接并关闭
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			manager.unregister <- c
			c.conn.Close()
			break
		}

		//把读取到的消息放到broadcast中
		jsonMessage, _ := json.Marshal(&Message{Sender: c.id, Content: string(message)})
		manager.broadcast <- jsonMessage
	}
}

//
func (c *Client) write() {
	defer func() {
		c.conn.Close()
	}()

	for {
		select {
		//从send里面读消息
		case message, ok := <-c.send:
			if !ok { //没有消息
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			//有消息就写入，发给web端
			c.conn.WriteMessage(websocket.TextMessage, message)
		}
	}
}

func main() {
	fmt.Println("Websocket Start ...")

	//开启一个协程，处理所有的websocket链接
	go manager.start()

	//监听websocket链接
	http.HandleFunc("/", wsHandler)
	http.ListenAndServe(":8080", nil)
}

func wsHandler(res http.ResponseWriter, req *http.Request) {
	//升级HTTP协议成websocket协议
	conn, err := (&websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}).Upgrade(res, req, nil)
	if err != nil {
		return
	}

	//每次websocket链接都会新生成一个Client
	client := &Client{
		id:   uuid.Must(uuid.NewV4()).String(),
		conn: conn,
		send: make(chan []byte),
	}

	//注册一个新连接
	manager.register <- client

	//接受从客户端发送的消息
	go client.read()

	//把消息返回给客户端
	go client.write()

}
