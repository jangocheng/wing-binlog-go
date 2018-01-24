package cluster

import (
	"github.com/hashicorp/consul/api"
)

type Session struct {
	Address string
	ID string
	Client *api.Client
}

// timeout 单位为秒
func (ses *Session) create() {
	se := &api.SessionEntry{
		Behavior : "delete",
	}
	ID, _, err := ses.Client.Session().Create(se, nil)
	if err != nil {
		return
	}
	ses.ID = ID
}

func (ses *Session) Renew() (err error) {
	_, _, err = ses.Client.Session().Renew(ses.ID, nil)
	return err
}