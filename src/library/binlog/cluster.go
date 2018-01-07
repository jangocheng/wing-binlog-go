package binlog

import (
	"context"
	"net"
	"os"
	"sync"
	"library/buffer"
	"library/file"
	"github.com/BurntSushi/toml"
	log "github.com/sirupsen/logrus"
)

type tcpClient struct {
	dns          string
	conn         *net.Conn
	isClosed     bool
	recvTimes    int64
	recvBuf      *buffer.WBuffer // []byte
	lock         *sync.Mutex     // 互斥锁，修改资源时锁定
	binlog       *Binlog
	ServiceIp    string
	ServicePort  int
	confirmCount int32
	startConfirm bool
	waitTimeout  bool
}

type tcpClientNode struct {
	conn             *net.Conn       // 客户端连接进来的资源句柄
	isConnected      bool            // 是否还连接着 true 表示正常 false表示已断开
	sendQueue        chan []byte     // 发送channel
	sendFailureTimes int64           // 发送失败次数
	recvBuf          *buffer.WBuffer // []byte          // 读缓冲区
	connectTime      int64           // 连接成功的时间戳
	sendTimes        int64           // 发送次数，用来计算负载均衡，如果 mode == 2
	ServiceDns       string          // 节点服务器的服务 ip:port
}

type TcpServer struct {
	listen           string
	port             int
	Client           *tcpClient
	clients          []*tcpClientNode // 所有的集群几点服务器
	lock             *sync.Mutex      // 互斥锁，修改资源时锁定
	listener         *net.Listener
	wg               *sync.WaitGroup
	sendFailureTimes int64
	ctx              *context.Context
	binlog           *Binlog
	ServiceIp        string
	cacheHandler     *os.File
}

type clusterConfig struct {
	Listen    string `toml:"listen"`
	Port      int    `toml:"port"`
	ServiceIp string `toml:"service_ip"`
}

func getServiceConfig() (*clusterConfig, error) {
	var config clusterConfig
	configFile := file.CurrentPath + "/config/cluster.toml"
	wfile := file.WFile{configFile}
	if !wfile.Exists() {
		log.Errorf("config file %s does not exists", configFile)
		return nil, ErrorFileNotFound
	}
	if _, err := toml.DecodeFile(configFile, &config); err != nil {
		log.Println(err)
		return nil, ErrorFileParse
	}
	return &config, nil
}

func NewCluster(ctx *context.Context, binlog *Binlog) *TcpServer {
	config, _ := getServiceConfig()
	log.Debugf("cluster server init with config: %+v", config)
	server := &TcpServer{
		listen:           config.Listen,
		port:             config.Port,
		lock:             new(sync.Mutex),
		listener:         nil,
		clients:          nil,
		wg:               new(sync.WaitGroup),
		sendFailureTimes: int64(0),
		ctx:              ctx,
		binlog:           binlog,
		ServiceIp:        config.ServiceIp,
	}
	server.Client = &tcpClient{
		isClosed:     true,
		recvTimes:    int64(0),
		recvBuf:      buffer.NewBuffer(TCP_RECV_DEFAULT_SIZE),
		lock:         new(sync.Mutex),
		binlog:       binlog,
		ServiceIp:    config.ServiceIp,
		ServicePort:  config.Port,
		confirmCount: 0,
		startConfirm: false,
		waitTimeout : false,
	}
	// 初始化缓存文件句柄
	cache := file.CurrentPath + "/cache/nodes.list"
	dir := file.WPath{cache}
	dir = file.WPath{dir.GetParent()}
	dir.Mkdir()
	flag := os.O_WRONLY | os.O_CREATE | os.O_SYNC // | os.O_TRUNC
	var err error
	server.cacheHandler, err = os.OpenFile(cache, flag, 0755)
	if err != nil {
		log.Panicf("binlog service, open cache file %s with error: %+v", cache, err)
	}
	return server
}
