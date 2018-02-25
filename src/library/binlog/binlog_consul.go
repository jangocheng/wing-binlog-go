package binlog

import (
	log "github.com/sirupsen/logrus"
	"time"
	"os"
	"github.com/hashicorp/consul/api"
	"fmt"
	"strconv"
	"net"
	"strings"
	"library/app"
)

func (h *Binlog) consulInit() {
	var err error
	consulConfig, err := getConfig()

	h.LockKey     = consulConfig.Lock
	//consul config
	h.Address     = consulConfig.Consul.Address
	//h.isLock      = 0
	h.sessionId   = app.GetKey(app.CachePath + "/session")//GetSession()
	if consulConfig.Enable {
		h.status ^= disableConsul
		h.status |= enableConsul
	}
	h.kvChan      = make(chan []byte, kvChanLen)
	ConsulConfig := api.DefaultConfig()
	ConsulConfig.Address = h.Address
	h.Client, err = api.NewClient(ConsulConfig)
	if err != nil {
		log.Panicf("create consul session with error: %+v", err)
	}

	h.Session = &Session {
		Address : h.Address,
		ID      : "",
		handler : h.Client.Session(),
	}
	h.Session.create()
	h.Kv    = h.Client.KV()
	h.agent = h.Client.Agent()
	// check self is locked in start
	// if is locked, try unlock
	m := h.getService()
	if m != nil {
		if m.IsLeader && m.Status == statusOffline {
			log.Warnf("current node is lock in start, try to unlock")
			h.Unlock()
			h.Delete(h.LockKey)
		}
	}
	// 超时检测，即检测leader是否挂了，如果挂了，要重新选一个leader
	// 如果当前不是leader，重新选leader。leader不需要check
	// 如果被选为leader，则还需要执行一个onLeader回调
	// check other is alive, if not, try to select a new leader
	go h.checkAlive()
	// 还需要一个keepalive
	// keepalive
	go h.keepalive()
}

func (h *Binlog) getService() *ClusterMember{
	if h.status & disableConsul > 0 {
		return nil
	}
	members := h.GetMembers()
	if members == nil {
		return nil
	}
	for _, v := range members {
		if v != nil && v.SessionId == h.sessionId {
			return v
		}
	}
	return nil
}

// register service
func (h *Binlog) registerService() {
	if h.status & disableConsul > 0 {
		return
	}
	h.lock.Lock()
	defer h.lock.Unlock()
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	t := time.Now().Unix()
	isLeader := 0
	if h.status & consulIsLeader > 0 {
		isLeader = 1
	}
	service := &api.AgentServiceRegistration{
		ID:                h.sessionId,
		Name:              h.LockKey,
		Tags:              []string{fmt.Sprintf("%d", isLeader), h.sessionId, fmt.Sprintf("%d", t), hostname, h.LockKey},
		Port:              h.ServicePort,
		Address:           h.ServiceIp,
		EnableTagOverride: false,
		Check:             nil,
		Checks:            nil,
	}
	//log.Debugf("register service: %+v", *service)
	err = h.agent.ServiceRegister(service)
	if err != nil {
		log.Errorf("register service with error: %+v", err)
	}
}

func (h *Binlog) GetCurrent() (string, int) {
	return h.ServiceIp, h.ServicePort
}

// keepalive
func (h *Binlog) keepalive() {
	if h.status & disableConsul > 0 {
		return
	}
	for {
		h.Session.renew()
		h.registerService()
		time.Sleep(time.Second * keepaliveInterval)
	}
}

func (h *Binlog) ShowMembers() string {
	if h.status & disableConsul > 0 {
		return ""
	}
	members := h.GetMembers()
	currentIp, currentPort := h.GetCurrent()
	if members != nil {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = ""
		}
		l := len(members)
		res := fmt.Sprintf("current node: %s(%s:%d)\r\n", hostname, currentIp, currentPort)
		res += fmt.Sprintf("cluster size: %d node(s)\r\n", l)
		res += fmt.Sprintf("======+=============================================+==========+===============\r\n")
		res += fmt.Sprintf("%-6s| %-43s | %-8s | %s\r\n", "index", "node", "role", "status")
		res += fmt.Sprintf("------+---------------------------------------------+----------+---------------\r\n")
		for i, member := range members {
			role := "follower"
			if member.IsLeader {
				role = "leader"
			}
			res += fmt.Sprintf("%-6d| %-43s | %-8s | %s\r\n", i, fmt.Sprintf("%s(%s:%d)", member.Hostname, member.ServiceIp, member.Port), role, member.Status)
		}
		res += fmt.Sprintf("------+---------------------------------------------+----------+---------------\r\n")
		//c.Write([]byte(res))
		return res
	} else {
		return ""
		//c.Write([]byte("no members found"))
	}
}

// get all members nodes
func (h *Binlog) GetMembers() []*ClusterMember {
	if h.status & disableConsul > 0 {
		return nil
	}
	members, err := h.agent.Services()
	if err != nil {
		log.Errorf("get service list error: %+v", err)
		return nil
	}
	if members == nil {
		return nil
	}
	data := make([]*ClusterMember, 0)
	//fmt.Println("")
	for _, v := range members {
		// 这里的两个过滤，为了避免与其他服务冲突，只获取相同lockkey的服务，即 当前集群
		if len(v.Tags) < 5 {
			continue
		}
		if v.Tags[4] != h.LockKey {
			continue
		}
		m := &ClusterMember{}
		t, _:= strconv.ParseInt(v.Tags[2], 10, 64)
		m.Status = statusOnline
		if time.Now().Unix() - t > serviceKeepaliveTimeout {
			m.Status = statusOffline
			log.Debugf("now: %d, t:%d, diff: %d", time.Now().Unix(), t, time.Now().Unix() - t)
		}
		m.IsLeader  = v.Tags[0] == "1"
		m.Hostname  = v.Tags[3]
		m.SessionId   = v.Tags[1]
		m.ServiceIp = v.Address
		m.Port      = v.Port
		data = append(data, m)
		//log.Debugf("member: %+v, %+v", *v, *m)
	}

	return data
}

// check service is alive
// if leader is not alive, try to select a new one
func (h *Binlog) checkAlive() {
	if h.status & disableConsul > 0 {
		return
	}
	// 延迟执行
	//t := srand(30000, 60000)
	time.Sleep(6)
	for {
		//获取所有的服务
		//判断服务的心跳时间是否超时
		//如果超时，更新状态为
		members := h.GetMembers()
		if members == nil {
			time.Sleep(time.Second * checkAliveInterval)
			continue
		}
		leaderCount := 0
		for _, v := range members {
			if v.IsLeader && (v.Status != statusOffline || h.alive(v.ServiceIp, v.Port)) {
				leaderCount++
			}
			if v.SessionId == h.sessionId {
				continue
			}
			if v.Status == statusOffline {
				log.Warnf("%s is timeout", v.SessionId)
				isAlive := h.alive(v.ServiceIp, v.Port)
				if !isAlive {
					h.agent.ServiceDeregister(v.SessionId)
				}
				// if is leader, try delete lock and reselect a new leader
				// if is leader, ping, check leader is alive again
				if v.IsLeader && !isAlive {
					h.Delete(h.LockKey)
				}
			}
		}
		if leaderCount == 0 && len(members) > 0 {
			log.Warnf("no leader is running")
			for _, v := range members {
				log.Debugf("member: %+v", *v)
			}
			//h.newleader()
			// current not leader
			if h.status & consulIsFollower > 0 {
				log.Warnf("current is not leader, will unlock")
				h.Delete(h.LockKey)
			}
		}
		if leaderCount > 1 {
			log.Warnf("%d leaders is running", leaderCount)
			h.Delete(h.LockKey)
		}
		time.Sleep(time.Second * checkAliveInterval)
	}
}

func (h *Binlog) alive(ip string, port int) bool {
	log.Debugf("ping %s:%d", ip, port)
	//tcpAddr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:%d", ip, port))
	//if err != nil {
	//	log.Debugf("is not alive")
	//	return false
	//}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip, port), time.Second * 3)
	//conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil || conn == nil {
		log.Debugf("is not alive")
		return false
	}
	conn.Close()
	log.Debugf("is alive")
	return true
}

// get leader service ip and port
// if not found or some error happened
// return empty string and 0
func (h *Binlog) GetLeader() (string, int) {
	if h.status & disableConsul > 0 {
		log.Debugf("not enable")
		return "", 0
	}
	members := h.GetMembers()
	if members == nil || len(members) == 0 {
		return "", 0
	}

	ip := ""
	port := 0
	for _, v := range members {
		//log.Debugf("GetLeader--%+v", v)
		if v.IsLeader && v.Status == statusOnline {
			ip, port = v.ServiceIp, v.Port
			break
		}
	}
	// reselect again
	if ip == "" || port == 0 {
		for _, v := range members {
			// check alive
			if v.IsLeader && h.alive(v.ServiceIp, v.Port) {
				ip, port = v.ServiceIp, v.Port
				break
			}
		}
	}
	return ip, port
}

// if app is close, it will be call for clear some source
func (h *Binlog) closeConsul() {
	if h.status & disableConsul > 0  {
		return
	}
	h.Delete(prefixKeepalive + h.sessionId)
	//log.Debugf("current is leader", h.isLock)
	//h.lock.Lock()
	//l := h.isLock
	//h.lock.Unlock()
	if h.status & consulIsLeader > 0 {
		log.Debugf("delete lock %s", h.LockKey)
		h.Unlock()
		h.Delete(h.LockKey)
	}
	h.Session.delete()
}

// lock if success, the current will be a leader
func (h *Binlog) Lock() (bool, error) {
	if h.status & disableConsul > 0 {
		return true, nil
	}
	if h.Session.ID == "" {
		h.Session.create()
	}
	if h.Session.ID == "" {
		log.Errorf("error: %v", ErrorSessionEmpty)
		return false, sessionEmpty
	}
	//key string, value []byte, sessionID string
	p := &api.KVPair{Key: h.LockKey, Value: nil, Session: h.Session.ID}
	success, _, err := h.Kv.Acquire(p, nil)
	if err != nil {
		// try to create a new session
		log.Errorf("lock error: %+v", err)
		if strings.Contains(strings.ToLower(err.Error()), "session") {
			log.Errorf("try to create a new session")
			h.Session.create()
		}
		return false, err
	}
	if success && h.status & consulIsFollower > 0 {
		h.status ^= consulIsFollower
		h.status |= consulIsLeader
	}
	return success, nil
}

// unlock
func (h *Binlog) Unlock() (bool, error) {
	if h.status & disableConsul > 0 {
		return true, nil
	}
	if h.Session.ID == "" {
		h.Session.create()
	}
	if h.Session.ID == "" {
		log.Errorf("error: %v", ErrorSessionEmpty)
		return false, sessionEmpty
	}
	p := &api.KVPair{Key: h.LockKey, Value: nil, Session: h.Session.ID}
	success, _, err := h.Kv.Release(p, nil)
	if err != nil {
		log.Errorf("unlock error: %+v", err)
		if strings.Contains(strings.ToLower(err.Error()), "session") {
			log.Errorf("try to create a new session")
			h.Session.create()
		}
		return false, err
	}
	if success && h.status & consulIsLeader > 0 {
		//h.lock.Lock()
		//h.isLock = 0
		//h.lock.Unlock()
		h.status ^= consulIsLeader
		h.status |= consulIsFollower
	}
	return success, nil
}

// delete a lock
func (h *Binlog) Delete(key string) error {
	if h.status & disableConsul > 0 {
		return nil
	}
	if h.Session.ID == "" {
		h.Session.create()
	}
	if h.Session.ID == "" {
		return nil
	}
	_, err := h.Kv.Delete(key, nil)
	if err == nil && key == h.LockKey && h.status & consulIsLeader > 0 {
		h.status ^= consulIsLeader
		h.status |= consulIsFollower
	}
	return err
}

