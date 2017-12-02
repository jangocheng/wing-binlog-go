package services

import (
    "github.com/BurntSushi/toml"
    "github.com/gorilla/websocket"
    "library/file"
    log "github.com/sirupsen/logrus"
    "errors"
    "sync"
)

type tcpGroupConfig struct {
    Mode int     // "1 broadcast" ##(广播)broadcast or  2 (权重)weight
    Name string  // = "group1"
    Filter []string
}
type tcpConfig struct {
    Listen string
    Port int
}
type TcpConfig struct {
    Enable bool
    Groups map[string]tcpGroupConfig
    Tcp tcpConfig
}

type HttpConfig struct {
    Enable bool
    Groups map[string]httpNodeConfig
}

type httpNodeConfig struct {
    Mode int
    Nodes [][]string
    Filter []string
}

type websocketClientNode struct {
    conn *websocket.Conn     // 客户端连接进来的资源句柄
    is_connected bool        // 是否还连接着 true 表示正常 false表示已断开
    send_queue chan []byte   // 发送channel
    send_failure_times int64 // 发送失败次数
    mode int                 // broadcast = 1 weight = 2 支持两种方式，广播和权重
    weight int               // 权重 0 - 100
    group string             // 所属分组
    recv_bytes int           // 收到的待处理字节数量
    connect_time int64       // 连接成功的时间戳
    send_times int64         // 发送次数，用来计算负载均衡，如果 mode == 2
}

type WebSocketService struct {
    Ip string                             // 监听ip
    Port int                              // 监听端口
    recv_times int64                      // 收到消息的次数
    send_times int64                      // 发送消息的次数
    send_failure_times int64              // 发送失败的次数
    send_queue chan []byte                // 发送队列-广播
    lock *sync.Mutex                      // 互斥锁，修改资源时锁定
    groups map[string][]*websocketClientNode // 客户端分组，现在支持两种分组，广播组合负载均衡组
    groups_mode map[string] int           // 分组的模式 1，2 广播还是复载均衡
    groups_filter map[string] []string    // 分组的过滤器
    clients_count int32                   // 成功连接（已经进入分组）的客户端数量
}

var (
    ErrorFileNotFound = errors.New("配置文件不存在")
    ErrorFileParse = errors.New("配置解析错误")
)

const (
    MODEL_BROADCAST = 1  // 广播
    MODEL_WEIGHT    = 2  // 权重

    CMD_SET_PRO = 1 // 注册客户端操作，加入到指定分组
    CMD_AUTH    = 2 // 认证（暂未使用）
    CMD_OK      = 3 // 正常响应
    CMD_ERROR   = 4 // 错误响应
    CMD_TICK    = 5 // 心跳包
    CMD_EVENT   = 6 // 事件

    TCP_MAX_SEND_QUEUE            = 1000000 //100万缓冲区
    TCP_DEFAULT_CLIENT_SIZE       = 64
    TCP_DEFAULT_READ_BUFFER_SIZE  = 1024
    TCP_RECV_DEFAULT_SIZE         = 4096
    TCP_DEFAULT_WRITE_BUFFER_SIZE = 4096
)


func getTcpConfig() (*TcpConfig, error) {
    var tcp_config TcpConfig
    tcp_config_file := file.GetCurrentPath() + "/config/tcp.toml"
    wfile := file.WFile{tcp_config_file}
    if !wfile.Exists() {
        log.Printf("配置文件%s不存在", tcp_config_file)
        return nil, ErrorFileNotFound
    }
    if _, err := toml.DecodeFile(tcp_config_file, &tcp_config); err != nil {
        log.Println(err)
        return nil, ErrorFileParse
    }
    return &tcp_config, nil
}

func getHttpConfig() (*HttpConfig, error) {
    var config HttpConfig
    http_config_file := file.GetCurrentPath() + "/config/http.toml"
    wfile := file.WFile{http_config_file}
    if !wfile.Exists() {
        log.Printf("配置文件%s不存在", http_config_file)
        return nil, ErrorFileNotFound
    }
    if _, err := toml.DecodeFile(http_config_file, &config); err != nil {
        log.Println(err)
        return nil, ErrorFileParse
    }
    return &config, nil
}


func getWebsocketConfig() (*TcpConfig, error) {
    var config TcpConfig
    config_file := file.GetCurrentPath() + "/config/websocket.toml"
    wfile := file.WFile{config_file}
    if !wfile.Exists() {
        log.Printf("配置文件%s不存在", config_file)
        return nil, ErrorFileNotFound
    }
    if _, err := toml.DecodeFile(config_file, &config); err != nil {
        log.Println(err)
        return nil, ErrorFileParse
    }
    return &config, nil
}
