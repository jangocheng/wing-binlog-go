package services

import (
	log "github.com/sirupsen/logrus"
	"library/http"
	"runtime"
	"sync"
	"library/app"
)

func NewHttpService(ctx *app.Context) *HttpService {
	config, _ := getHttpConfig()
	log.Debugf("start http service with config: %+v", config)
	if !config.Enable {
		return &HttpService{
			status: serviceDisable,
		}
	}
	gc := len(config.Groups)
	client := &HttpService{
		lock:             new(sync.Mutex),
		groups:           make(map[string]*httpGroup, gc),
		status:           serviceEnable,
		timeTick:         config.TimeTick,
		wg:               new(sync.WaitGroup),
		ctx:              ctx,
	}
	for _, cgroup := range config.Groups {
		group := &httpGroup{
			name: cgroup.Name,
			filter: cgroup.Filter,
		}
		gl := len(cgroup.Nodes)
		group.nodes = make([]*httpNode, gl)
		for i := 0; i < gl; i++ {
			group.nodes[i] = &httpNode{
				url:              cgroup.Nodes[i],
				sendQueue:        make(chan string, httpMaxSendQueue),
				lock:             new(sync.Mutex),
			}
		}
		client.groups[cgroup.Name] = group
	}
	return client
}

// 开始服务
func (client *HttpService) Start() {
	if client.status & serviceDisable > 0 {
		return
	}
	cpu := runtime.NumCPU()
	for _, cgroup := range client.groups {
		for _, cnode := range cgroup.nodes {
			// 启用cpu数量的服务协程
			for i := 0; i < cpu; i++ {
				client.wg.Add(1)
				go client.clientSendService(cnode)
			}
		}
	}
}

// 节点服务协程
func (client *HttpService) clientSendService(node *httpNode) {
	defer client.wg.Done()
	for {
		select {
		case msg, ok := <-node.sendQueue:
			if !ok {
				log.Warnf("http service, sendQueue channel was closed")
				return
			}
			data, err := http.Post(node.url, []byte(msg))
			if err != nil {
				log.Errorf("http service node %s error: %v", node.url, err)
			}
			log.Debugf("post %v to %s return %s", msg, node.url, string(data))
		case <-client.ctx.Ctx.Done():
			if len(node.sendQueue) <= 0 {
				log.Debugf("%s clientSendService exit", node.url)
				return
			}
		}
	}
}

func (client *HttpService) SendAll(table string, data []byte) bool {
	if client.status & serviceDisable > 0 {
		return false
	}
	for _, cgroup := range client.groups {
		if cgroup.nodes == nil || len(cgroup.nodes) <= 0 ||
			!matchFilters(cgroup.filter, table) {
			continue
		}
		for _, cnode := range cgroup.nodes {
			log.Debugf("http send broadcast: %s=>%s", cnode.url, string(data))
			if len(cnode.sendQueue) >= cap(cnode.sendQueue) {
				log.Debugf("http send buffer full:%s, %s", cnode.url, string(data))
				log.Debugf("send sync %s", cnode.url)
				client.syncSend(cnode, data)
				continue
			}
			cnode.sendQueue <- string(data)
		}
	}
	return true
}

func (client *HttpService) syncSend(node *httpNode, data []byte) {
	data, err := http.Post(node.url, data)
	if err != nil {
		log.Warnf("http service node %s error: %v", node.url, err)
	}
	log.Debugf("http service post to %s return %s", node.url, string(data))
}

func (client *HttpService) Close() {
	log.Debug("http service closing, waiting for buffer send complete.")
	for _, cgroup := range client.groups {
		if len(cgroup.nodes) > 0 {
			client.wg.Wait()
			break
		}
	}
	log.Debug("http service closed.")
}

func (client *HttpService) Reload() {
	config, _ := getHttpConfig()
	log.Debug("http service reloading...")

	client.status = serviceDisable
	if config.Enable {
		client.status = serviceEnable
	}

	for name := range client.groups {
		delete(client.groups, name)
	}

	for _, cgroup := range config.Groups {
		group := &httpGroup{
			name: cgroup.Name,
		}
		group.filter = make([]string, len(cgroup.Filter))
		group.filter = append(group.filter[:0], cgroup.Filter...)

		nc := len(cgroup.Nodes)
		group.nodes = make([]*httpNode, nc)
		for i := 0; i < nc; i++ {
			group.nodes[i] = &httpNode{
				url:              cgroup.Nodes[i],
				sendQueue:        make(chan string, httpMaxSendQueue),
				lock:             new(sync.Mutex),
			}
		}
		client.groups[cgroup.Name] = group
	}
	log.Debug("http service reloaded.")
}

func (client *HttpService) AgentStart(serviceIp string, port int) {}

func (client *HttpService) AgentStop() {}

func (client *HttpService) SendPos(data []byte) {

}