package serverplugin

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	estore "github.com/bitini111/rpcx/plugin/etcdv3/store"
	"github.com/bitini111/rpcx/plugin/etcdv3/store/etcdv3"
	"github.com/rpcxio/libkv"
	"github.com/rpcxio/libkv/store"

	"github.com/bitini111/rpcx/log"
	metrics "github.com/rcrowley/go-metrics"
)

func init() {
	etcdv3.Register()
}

type etcdInfo struct {
	ID   int64      `json:"ID"`
	Vers []*version `json:"version"`
}

type version struct {
	Ver    string `json:"Ver"`
	IP     string `json:"IP"`
	Unique string `json:"Unique"`
}

// EtcdV3RegisterPlugin implements etcd registry.
type EtcdV3RegisterPlugin struct {
	// service address, for example, tcp@127.0.0.1:8972, quic@127.0.0.1:1234
	ServiceAddress string
	// etcd addresses
	EtcdServers []string
	// base path for rpcx server, for example com/example/rpcx
	BasePath string
	Metrics  metrics.Registry
	// Registered services
	Services []string

	//the version of SERVER
	Version string

	//服务的名字
	ServiceName string

	//the ID of SERVER
	ServerID int64

	metasLock      sync.RWMutex
	metas          map[string]string
	UpdateInterval time.Duration
	Expired        time.Duration

	Options *store.Config
	kv      store.Store

	dying chan struct{}
	done  chan struct{}
}

func NewEtcdV3Plugin(serviceAddress string, etcdServers []string, BasePath string, serverName, version string, serverID int32) *EtcdV3RegisterPlugin {
	item := &EtcdV3RegisterPlugin{
		ServiceAddress: serviceAddress, //服务监听的ip端口
		EtcdServers:    etcdServers,    //etcd地址
		BasePath:       BasePath,       //etcd的目录
		Metrics:        metrics.NewRegistry(),
		Version:        version, //rpc的版本
		ServiceName:    serverName,
		ServerID:       int64(serverID), //rpc的svrid
		//UpdateInterval: time.Minute,
	}
	return item
}

// Start starts to connect etcd cluster
func (p *EtcdV3RegisterPlugin) Start() error {
	if p.Expired == 0 {
		p.Expired = p.UpdateInterval
	}

	if p.done == nil {
		p.done = make(chan struct{})
	}
	if p.dying == nil {
		p.dying = make(chan struct{})
	}
	//The Version Format is NOT CORRECT, Use: x.x.x.x
	if p.Version == "" {
		p.Version = "0.0.0.1"
	}
	//The Version Format is NOT CORRECT, Use: 1 2 3
	if p.ServerID == 0 {
		p.ServerID = 1 //初始化磨人的id
	}

	if p.ServiceName == "" {
		p.ServiceName = "rpcx_path" //初始化磨人的id
	}

	if p.kv == nil {
		kv, err := libkv.NewStore(estore.ETCDV3, p.EtcdServers, p.Options)
		if err != nil {
			log.Errorf("cannot create etcd registry: %v", err)
			return err
		}
		p.kv = kv
	}

	//nodepath := fmt.Sprintf("%s/%s/%d", p.BasePath, p.ServiceName, p.ServerID)
	//err := p.kv.Put(nodepath, []byte("rpcx_path"), &store.WriteOptions{IsDir: true, TTL: p.UpdateInterval + p.Expired})
	//if err != nil && !strings.Contains(err.Error(), "Not a file") {
	//	log.Errorf("cannot create etcd path %s: %v", p.BasePath, err)
	//	return err
	//}

	//if p.UpdateInterval > 0 {
	//	ticker := time.NewTicker(p.UpdateInterval)
	//	go func() {
	//		defer p.kv.Close()
	//		// refresh service TTL
	//		for {
	//			select {
	//			case <-p.dying:
	//				close(p.done)
	//				return
	//			case <-ticker.C:
	//				extra := make(map[string]string)
	//				if p.Metrics != nil {
	//					extra["calls"] = fmt.Sprintf("%.2f", metrics.GetOrRegisterMeter("calls", p.Metrics).RateMean())
	//					extra["connections"] = fmt.Sprintf("%.2f", metrics.GetOrRegisterMeter("connections", p.Metrics).RateMean())
	//					extra["serverid"] = fmt.Sprintf("%d", p.ServerID)
	//					extra["addr"] = fmt.Sprintf("%s", p.ServiceAddress)
	//				}
	//				//set this same metrics for all services at this server
	//				for _, name := range p.Services {
	//					nodePath := fmt.Sprintf("%s/%s/%d", p.BasePath, p.ServiceName, p.ServerID)
	//					kvPair, err := p.kv.Get(nodePath)
	//					if err != nil {
	//						log.Warnf("can't get data of node: %s, because of %v", nodePath, err)
	//
	//						p.metasLock.RLock()
	//						meta := p.metas[name]
	//						p.metasLock.RUnlock()
	//
	//						err = p.kv.Put(nodePath, []byte(meta), &store.WriteOptions{TTL: p.UpdateInterval + p.Expired})
	//						if err != nil {
	//							log.Errorf("cannot re-create etcd path %s: %v", nodePath, err)
	//						}
	//
	//					} else {
	//						v, _ := url.ParseQuery(string(kvPair.Value))
	//						for key, value := range extra {
	//							v.Set(key, value)
	//						}
	//						p.kv.Put(nodePath, []byte(v.Encode()), &store.WriteOptions{TTL: p.UpdateInterval + p.Expired})
	//					}
	//				}
	//			}
	//		}
	//	}()
	//}

	return nil
}

// Stop unregister all services.
func (p *EtcdV3RegisterPlugin) Stop() error {
	if p.kv == nil {
		kv, err := libkv.NewStore(estore.ETCDV3, p.EtcdServers, p.Options)
		if err != nil {
			log.Errorf("cannot create etcd registry: %v", err)
			return err
		}
		p.kv = kv
	}

	jsonByte, err := p.deleteNodeVerson()
	if err != nil {
		log.Errorf("cannot delete verson %s: %v", jsonByte, err)
		return err
	}
	nodepath := fmt.Sprintf("%s/%s/%d", p.BasePath, p.ServiceName, p.ServerID)
	if jsonByte == nil {
		p.kv.Delete(nodepath)
		log.Infof("delete path %s", nodepath)
	} else {
		p.kv.Put(nodepath, jsonByte, &store.WriteOptions{TTL: p.UpdateInterval + p.Expired})
	}

	close(p.dying)
	<-p.done
	return nil
}
func (p *EtcdV3RegisterPlugin) getNodeVerson() ([]byte, error) {
	var extInfo etcdInfo
	nodepath := fmt.Sprintf("%s/%s/%d", p.BasePath, p.ServiceName, p.ServerID)
	exist, err := p.kv.Exists(nodepath)
	if err != nil {
		log.Errorf("Exist err etcd path %s: %v", nodepath, err)
		return nil, err
	}

	extInfo.ID = p.ServerID
	text := fmt.Sprintf("%d[%s-%d-%s-%s]", time.Now().UnixNano(), p.Version, p.ServerID, p.ServiceName, p.ServiceAddress)
	md5str := fmt.Sprintf("%x", md5.Sum([]byte(text)))
	uniqueId := fmt.Sprintf("%s-%s", time.Now().Format("20060102 15:04:05"), md5str)
	newVersion := &version{Ver: p.Version, IP: p.ServiceAddress, Unique: uniqueId}
	if exist {
		ps, err := p.kv.Get(nodepath)
		if err != nil {
			log.Errorf("p.kv.List etcdv3 path %s: %v", nodepath, err)
			return nil, err
		}
		var jsonValue bytes.Buffer
		jsonValue.Write(ps.Value)
		err = json.Unmarshal(jsonValue.Bytes(), &extInfo)
		if err != nil {
			log.Errorf("meta Unmarshal err %s: %v", jsonValue.Bytes(), err)
			return nil, err
		}
		var arrs []*version
		for _, v := range extInfo.Vers {
			if v.Ver != p.Version {
				arrs = append(arrs, v)
			}
		}
		extInfo.Vers = arrs
	}
	extInfo.Vers = append(extInfo.Vers, newVersion)

	if num := len(extInfo.Vers); num > 5 {
		sort.Slice(extInfo.Vers, func(i, j int) bool {
			return p.ver2int(extInfo.Vers[i].Ver) < p.ver2int(extInfo.Vers[j].Ver)
		})
		extInfo.Vers = extInfo.Vers[num-5:]
	}

	return json.Marshal(extInfo)
}

func (p *EtcdV3RegisterPlugin) ver2int(ver string) int64 {
	ret, fields := int64(0), strings.SplitN(ver, ".", 4)
	if n, err := strconv.Atoi(fields[0]); err == nil && n > 0 && n < 65536 {
		ret |= int64(n) << 48
	}
	if n, err := strconv.Atoi(fields[1]); err == nil && n > 0 && n < 65536 {
		ret |= int64(n) << 32
	}
	if n, err := strconv.Atoi(fields[2]); err == nil && n > 0 && n < 65536 {
		ret |= int64(n) << 16
	}
	if n, err := strconv.Atoi(fields[3]); err == nil && n > 0 && n < 65536 {
		ret |= int64(n)
	}
	return ret
}

func (p *EtcdV3RegisterPlugin) deleteNodeVerson() ([]byte, error) {
	nodepath := fmt.Sprintf("%s/%s/%d", p.BasePath, p.ServiceName, p.ServerID)
	ps, err := p.kv.Get(nodepath)
	if err != nil {
		log.Errorf("Exist err etcd path %s: %v", nodepath, err)
		return nil, err
	}

	var jsonValue bytes.Buffer
	jsonValue.Write(ps.Value)
	var extInfo etcdInfo
	err = json.Unmarshal(jsonValue.Bytes(), &extInfo)
	if err != nil {
		log.Errorf("json.Unmarshal zk path %s: %v", string(jsonValue.Bytes()), err)
		return nil, err
	}

	var arrs []*version
	for _, v := range extInfo.Vers {
		if v.Ver != p.Version {
			arrs = append(arrs, v)
		}
	}
	extInfo.Vers = arrs
	if len(arrs) == 0 {
		return nil, nil
	}

	return json.Marshal(extInfo)
}

// HandleConnAccept handles connections from clients
func (p *EtcdV3RegisterPlugin) HandleConnAccept(conn net.Conn) (net.Conn, bool) {
	if p.Metrics != nil {
		metrics.GetOrRegisterMeter("connections", p.Metrics).Mark(1)
	}
	return conn, true
}

// PreCall handles rpc call from clients
func (p *EtcdV3RegisterPlugin) PreCall(_ context.Context, _, _ string, args interface{}) (interface{}, error) {
	if p.Metrics != nil {
		metrics.GetOrRegisterMeter("calls", p.Metrics).Mark(1)
	}
	return args, nil
}

// Register handles registering event.
// this service is registered at BASE/serviceName/thisIpAddress node
func (p *EtcdV3RegisterPlugin) Register(name string, rcvr interface{}, metadata string) (err error) {
	if strings.TrimSpace(name) == "" {
		err = errors.New("Register service `name` can't be empty")
		return
	}

	if p.kv == nil {
		etcdv3.Register()
		kv, err := libkv.NewStore(estore.ETCDV3, p.EtcdServers, nil)
		if err != nil {
			log.Errorf("cannot create etcd registry: %v", err)
			return err
		}
		p.kv = kv
	}

	nodePath := fmt.Sprintf("%s/%s/%d", p.BasePath, p.ServiceName, p.ServerID)
	jsondata, err := p.getNodeVerson()
	if err != nil {
		log.Errorf("cannot getnode data %s: %v", jsondata, err)
		return err
	}
	err = p.kv.Put(nodePath, jsondata, &store.WriteOptions{TTL: p.UpdateInterval + p.Expired})
	if err != nil {
		log.Errorf("cannot create etcd path %s: %v", nodePath, err)
		return err
	}

	p.Services = append(p.Services, name)

	p.metasLock.Lock()
	if p.metas == nil {
		p.metas = make(map[string]string)
	}
	p.metas[name] = metadata
	p.metasLock.Unlock()
	return
}

func (p *EtcdV3RegisterPlugin) UpdateMetadata(value []byte) {

}

func (p *EtcdV3RegisterPlugin) RegisterFunction(serviceName, fname string, fn interface{}, metadata string) error {
	return p.Register(serviceName, fn, metadata)
}

func (p *EtcdV3RegisterPlugin) Unregister(name string) (err error) {
	if len(p.Services) == 0 {
		return nil
	}

	if strings.TrimSpace(name) == "" {
		err = errors.New("Register service `name` can't be empty")
		return
	}

	if p.kv == nil {
		etcdv3.Register()
		kv, err := libkv.NewStore(estore.ETCDV3, p.EtcdServers, nil)
		if err != nil {
			log.Errorf("cannot create etcd registry: %v", err)
			return err
		}
		p.kv = kv
	}

	//err = p.kv.Put(p.BasePath, []byte("rpcx_path"), &store.WriteOptions{IsDir: true})
	//if err != nil && !strings.Contains(err.Error(), "Not a file") {
	//	log.Errorf("cannot create etcd path %s: %v", p.BasePath, err)
	//	return err
	//}
	//
	//nodePath := fmt.Sprintf("%s/%s", p.BasePath, name)
	//err = p.kv.Put(nodePath, []byte(name), &store.WriteOptions{IsDir: true})
	//if err != nil && !strings.Contains(err.Error(), "Not a file") {
	//	log.Errorf("cannot create etcd path %s: %v", nodePath, err)
	//	return err
	//}

	nodePath := fmt.Sprintf("%s/%s/%d", p.BasePath, p.ServiceName, p.ServerID)
	err = p.kv.Delete(nodePath)
	if err != nil {
		log.Errorf("cannot create consul path %s: %v", nodePath, err)
		return err
	}

	if len(p.Services) > 0 {
		var services = make([]string, 0, len(p.Services)-1)
		for _, s := range p.Services {
			if s != name {
				services = append(services, s)
			}
		}
		p.Services = services
	}

	p.metasLock.Lock()
	if p.metas == nil {
		p.metas = make(map[string]string)
	}
	delete(p.metas, name)
	p.metasLock.Unlock()
	return
}
