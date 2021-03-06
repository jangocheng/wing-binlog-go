package subscribe

import (
	"sync"
	"library/app"
	"net"
	"library/service"
	"github.com/BurntSushi/toml"
	"library/file"
	log "github.com/sirupsen/logrus"
)

const (
	CMD_SET_PRO = iota // 注册客户端操作，加入到指定分组
	CMD_AUTH           // 认证（暂未使用）
	CMD_ERROR          // 错误响应
	CMD_TICK           // 心跳包
	CMD_EVENT          // 事件
	CMD_AGENT
	CMD_STOP
	CMD_RELOAD
	CMD_SHOW_MEMBERS
	CMD_POS
)

const (
	tcpMaxSendQueue               = 10000
	tcpDefaultReadBufferSize      = 1024
)

const (
	FlagSetPro = iota
	FlagPing
)

const (
	serviceEnable = 1 << iota
	serviceClosed
)

const (
	tcpNodeOnline = 1 << iota
)

const ServiceName = "wing-binlog-go-subscribe"

type tcpClientNode struct {
	conn             *net.Conn   // 客户端连接进来的资源句柄
	sendQueue        chan []byte // 发送channel
	sendFailureTimes int64       // 发送失败次数
	topics           []string      // 订阅的主题
	recvBuf          []byte      // 读缓冲区
	connectTime      int64       // 连接成功的时间戳
	status           int
	wg               *sync.WaitGroup
	ctx              *app.Context
	lock             *sync.Mutex          // 互斥锁，修改资源时锁定
	onclose []NodeFunc
	//onpro SetProFunc
}

type NodeFunc func(n *tcpClientNode)
type SetProFunc func(n *tcpClientNode, groupName string) bool
type NodeOption func(n *tcpClientNode)

type tcpClients []*tcpClientNode
//type tcpGroups map[string]*tcpGroup

type tcpGroup struct {
	name   string
	filter []string
	nodes  tcpClients
	lock *sync.Mutex
}

type TcpService struct {
	service.Service
	Listen           string               // 监听ip
	lock             *sync.Mutex
	statusLock       *sync.Mutex
	ctx              *app.Context
	listener         *net.Listener
	wg               *sync.WaitGroup
	status           int
	conn             *net.TCPConn
	buffer           []byte
	Enable bool

	sendAll []SendAllFunc
	sendRaw []SendRawFunc
	onConnect []OnConnectFunc
	onClose []CloseFunc
	onKeepalive []KeepaliveFunc
	reload []ReloadFunc
}

var (
	_              service.Service = &TcpService{}
	packDataTickOk                 = service.Pack(CMD_TICK, []byte("ok"))
	packDataSetPro                 = service.Pack(CMD_SET_PRO, []byte("ok"))
)

type TcpServiceOption func(service *TcpService)
type SendAllFunc func(table string, data []byte) bool
type SendRawFunc func(msg []byte)
type OnConnectFunc func(conn *net.Conn)
type CloseFunc func()
type KeepaliveFunc func(data []byte)
type ReloadFunc func()



type TcpConfig struct {
	Listen string `toml:"listen"` //like 0.0.0.0:9996
	Enable bool   `toml:"enable"` //service enable
	ConsulAddress string `toml:"consul_address"`
	ConsulEnable bool `toml:"consul_enable"`
}

func getConfig() (*TcpConfig, error) {
	configFile := app.ConfigPath + "/subscribe.toml"
	var err error
	if !file.Exists(configFile) {
		log.Warnf("config %s does not exists", configFile)
		return nil, app.ErrorFileNotFound
	}
	var tcpConfig TcpConfig
	if _, err = toml.DecodeFile(configFile, &tcpConfig); err != nil {
		log.Println(err)
		return nil, app.ErrorFileParse
	}
	return &tcpConfig, nil
}




