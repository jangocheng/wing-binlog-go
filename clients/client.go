package main

import (
	"sync"
	"net"
	"fmt"
	log "github.com/sirupsen/logrus"
	"time"
	"sync/atomic"
	"encoding/json"
)

const (
	tcpDefaultReadBufferSize = 4096
 	CMD_SET_PRO = 1
 	CMD_AUTH    = 2
 	CMD_OK      = 3
 	CMD_ERROR   = 4
 	CMD_TICK    = 5
 	CMD_EVENT   = 6
)

type Client struct {
	node *Node
	isClose bool
	lock *sync.Mutex
	buffer []byte
	total int64
	starttime int64
	Services []*service
}

type Node struct {
	conn *net.TCPConn
	isConnect bool
}

func NewClient(s []*service) *Client{
	agent := &Client{
		isClose   : true,
		node      : nil,
		lock      : new(sync.Mutex),
		buffer    : make([]byte, 0),
		total     : 0,
		starttime : time.Now().Unix(),
		Services  : s,
	}
	return agent
}

func (client *Client) init(ip string, port int) {
	log.Debugf("init tcp connect, connect to %s:%d", ip, port)
	client.lock.Lock()
	defer client.lock.Unlock()
	if client.node != nil && client.node.isConnect {
		client.disconnect()
	}
	tcpAddr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		log.Errorf("connect to %s:%d with error: %+v", ip, port, err)
		return
	}
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	client.node = &Node{
		conn:conn,
		isConnect:true,
	}
	if err != nil {
		log.Errorf("start agent with error: %+v", err)
		client.node.isConnect = false
		client.node.conn = nil
	} else {
		client.isClose = false
	}
}

func (client *Client) pack(cmd int, content string) []byte {
	// 数据打包
	c := []byte(content)
	l := len(c) + 2
	r := make([]byte, l + 4)
	// 4字节数据包长度
	r[0] = byte(l)
	r[1] = byte(l >> 8)
	r[2] = byte(l >> 16)
	r[3] = byte(l >> 32)
	// 2字节cmd
	r[4] = byte(cmd)
	r[5] = byte(cmd >> 8)
	// 实际数据内容
	r = append(r[:6], c...)
	return r
}

func (client *Client) Start() {
	keepalive := client.pack(CMD_TICK, "")
	go func() {
		for {
			if client.node == nil {
				time.Sleep(time.Second * 5)
				continue
			}
			client.lock.Lock()
			if client.node.conn != nil {
				client.node.conn.Write(keepalive)
			}
			client.lock.Unlock()
			time.Sleep(time.Second * 5)
		}
	}()
	for {
		for _, server := range client.Services {
			client.init(server.ip, server.port)
			if  client.node == nil {
				time.Sleep(time.Second)
				log.Warnf("node is nil")
				continue
			}
			if client.node.conn == nil {
				time.Sleep(time.Second)
				log.Warnf("conn is nil")
				continue
			}
			if !client.node.isConnect {
				time.Sleep(time.Second)
				log.Warnf("isConnect is false")
				continue
			}
			log.Debugf("====================client start====================")
			//握手包
			clientH := client.pack(CMD_SET_PRO, server.groupName)
			var readBuffer [tcpDefaultReadBufferSize]byte
			if client.isClose {
				return
			}
			//握手
			client.node.conn.Write(clientH)
			for {
				if client.isClose {
					return
				}
				buf := readBuffer[:tcpDefaultReadBufferSize]
				//清空旧数据 memset
				for i := range buf {
					buf[i] = byte(0)
				}
				size, err := client.node.conn.Read(buf[0:])
				if err != nil || size <= 0 {
					log.Warnf("agent read with error: %+v", err)
					client.disconnect()
					break
				}
				client.onMessage(buf[:size])
			}
		}
	}
}

func (client *Client) disconnect() {
	client.lock.Lock()
	defer client.lock.Unlock()
	if client.node == nil || !client.node.isConnect {
		return
	}
	log.Warnf("---------------agent disconnect---------------")
	client.node.conn.Close()
	client.node.isConnect = false
}

func (client *Client) Close() {
	if client.isClose {
		log.Debugf("client close was called, but not running")
		return
	}
	log.Warnf("---------------client close---------------")
	client.disconnect()
	client.lock.Lock()
	client.isClose = true
	client.lock.Unlock()
}

func (client *Client) onMessage(msg []byte) {
	client.buffer = append(client.buffer, msg...)
	//todo send broadcast
	//这里还需要解包数据
	for {
		bufferLen := len(client.buffer)
		if bufferLen < 6 {
			return
		}
		//4字节长度，包含2自己的cmd
		contentLen := int(client.buffer[0]) | int(client.buffer[1]) << 8 | int(client.buffer[2]) << 16 | int(client.buffer[3]) << 24
		//2字节 command
		cmd := int(client.buffer[4]) | int(client.buffer[5]) << 8
		//数据未接收完整，等待下一次处理
		if bufferLen < 4 + contentLen {
			return
		}
		dataB := client.buffer[6:4 + contentLen]
		atomic.AddInt64(&client.total, 1)
		total := atomic.LoadInt64(&client.total)
		p := int64(0)
		sp := time.Now().Unix() - client.starttime
		if sp > 0 {
			p = int64(total/sp)
		}
		switch(cmd) {
		case CMD_EVENT:
			log.Debugf("收到数据库事件")
			var data interface{}
			json.Unmarshal(dataB, &data)
			log.Debugf("%+v", data)
		default:
			log.Debugf("收到其他消息")
		}
		log.Debugf("每秒接收数据 %d 条， clen=%d, cmd=%d, %+v", p, contentLen, cmd, string(dataB))
		//数据移动，清除已读数据
		client.buffer = append(client.buffer[:0], client.buffer[contentLen + 4:]...)
	}
}


type service struct {
	groupName string
	ip string
	port int
}

func main() {
	log.SetFormatter(&log.TextFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		ForceColors:      true,
		QuoteEmptyFields: true,
		FullTimestamp:    true,
	})
	log.SetLevel(log.Level(5))
	ser1 := &service{
		groupName : "group1",
		ip : "127.0.0.1",
		port :10008,
	}
	ser2 := &service{
		groupName : "group1",
		ip : "127.0.0.1",
		port :9998,
	}
	ser3 := &service{
		groupName : "group1",
		ip : "127.0.0.1",
		port :10010,
	}
	ser4 := &service{
		groupName : "group1",
		ip : "127.0.0.1",
		port :10009,
	}

	s := make([]*service, 0)
	s = append(s, ser1)
	s = append(s, ser2)
	s = append(s, ser3)
	s = append(s, ser4)

	client := NewClient(s)
	client.Start()
}