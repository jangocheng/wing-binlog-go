package services

import (
	"net"
	log "github.com/sirupsen/logrus"
	"fmt"
	"library/app"
	"time"
)

type control struct {
	conn *net.TCPConn
}

func NewControl() *control {
	config, _ := GetTcpConfig()
	tcpAddr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:%d", config.ServiceIp, config.Port))
	if err != nil {
		log.Panicf("start control with error: %+v", err)
	}
	con := &control{}
	con.conn, err = net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		log.Errorf("start control with error: %+v", err)
	}
	con.auth()
	return con
}

func (con *control) auth() {
	token := app.GetKey(app.CachePath + "/token")
	data := pack(CMD_AUTH, token)
	con.conn.Write(data)
}

func (con *control) Close() {
	con.conn.Close()
}

// -stop
func (con *control) Stop() {
	data := pack(CMD_STOP, "")
	con.conn.Write(data)
}

//-service-reload http
//-service-reload tcp
//-service-reload all ##重新加载全部服务
//cmd: http、tcp、all
func (con *control) Reload(cmd string) {
	data := pack(CMD_RELOAD, cmd)
	con.conn.Write(data)
}

func (con *control) Restart() {

}

// -members
func (con *control) ShowMembers() {
	data := pack(CMD_SHOW_MEMBERS, "")
	con.conn.Write(data)
	var buf = make([]byte, 40960)
	con.conn.SetReadDeadline(time.Now().Add(time.Second*3))
	con.conn.Read(buf)
	fmt.Println(string(buf))
}