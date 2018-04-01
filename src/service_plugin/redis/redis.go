package redis

import (
	"library/services"
    "github.com/go-redis/redis"
	"github.com/BurntSushi/toml"
	"library/app"
	"library/file"
	log "github.com/sirupsen/logrus"
)

type Redis struct {
	services.Service
	client *redis.Client
	config *redisConfig
}

var _ services.Service = &Redis{}

type redisConfig struct{
	Enable bool `toml:"enable"`
	Address string `toml:"address"`
	Password string `toml:"password"`
	Filter []string `toml:"filter"`
	Db int `toml:"db"`
	Queue string `toml:"queue"`
}

func getRedisConfig() (*redisConfig, error) {
	var config redisConfig
	configFile := app.ConfigPath + "/redis.toml"
	if !file.Exists(configFile) {
		log.Errorf("config file not found: %s", configFile)
		return nil, app.ErrorFileNotFound
	}
	if _, err := toml.DecodeFile(configFile, &config); err != nil {
		log.Println(err)
		return nil, app.ErrorFileParse
	}
	return &config, nil
}

func NewRedis() services.Service {
	config, err := getRedisConfig()
	if err != nil {
		log.Errorf("get redis config error")
		return &Redis{client:nil}
	}
	log.Debugf("redis service with: %+v", config)
	client := redis.NewClient(&redis.Options{
		Addr:     config.Address,  //"localhost:6379",
		Password: config.Password,
		DB:       config.Db,
	})
	return &Redis{
		client:client,
		config:config,
	}
}
func (r *Redis) SendAll(table string, data []byte) bool {
	if !r.config.Enable || r.client == nil {
		return true
	}
	//if match
	if services.MatchFilters(r.config.Filter, table) {
		err := r.client.RPush(r.config.Queue, string(data)).Err()
		if err != nil {
			log.Errorf("redis RPush error: %+v", err)
		}
	}
	return true
}
func (r *Redis) SendPos(data []byte) {}
func (r *Redis) Start() {}
func (r *Redis) Close() {
	if r.client == nil {
		return
	}
	r.client.Close()
}
func (r *Redis) Reload() {
	r.Close()
	var err error
	r.config, err = getRedisConfig()
	if err != nil {
		r.client = nil
		log.Errorf("redis reload with error: %v", err)
		return
	}
	r.client = redis.NewClient(&redis.Options{
		Addr:     r.config.Address,
		Password: r.config.Password,
		DB:       r.config.Db,
	})
}
func (r *Redis) AgentStart(serviceIp string, port int) {}
func (r *Redis) AgentStop() {}
func (r *Redis) Name() string {
	return "redis"
}