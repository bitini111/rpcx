package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rpcxio/libkv"
	"github.com/rpcxio/libkv/store"
	estore "github.com/rpcxio/rpcx-etcd/store"
	etcd "github.com/rpcxio/rpcx-etcd/store/etcdv3"
	"github.com/smallnest/rpcx/client"
	"github.com/smallnest/rpcx/log"
)

func init() {
	etcd.Register()
}

type etcdDiscoverVersIP struct {
	Ver string `json:"Ver"`
	IP  string `json:"IP"`
}

type etcdDiscoverServIDVers struct {
	ID   int64                `json:"ID"`
	Vers []etcdDiscoverVersIP `json:"Vers"`
}

// EtcdV3Discovery is a etcd service discovery.
// It always returns the registered servers in etcd.
type EtcdV3Discovery struct {
	basePath string
	kv       store.Store
	pairsMu  sync.RWMutex
	pairs    []*client.KVPair
	chans    []chan []*client.KVPair
	mu       sync.Mutex

	// -1 means it always retry to watch until zookeeper is ok, 0 means no retry.
	RetriesAfterWatchFailed int

	filter           client.ServiceDiscoveryFilter
	AllowKeyNotFound bool

	stopCh chan struct{}
}

// NewEtcdV3Discovery returns a new EtcdV3Discovery.
func NewEtcdV3Discovery(basePath string, servicePath string, etcdAddr []string, allowKeyNotFound bool, options *store.Config) (client.ServiceDiscovery, error) {
	if basePath[0] == '/' {
		basePath = basePath[1:]
	}

	if len(basePath) > 1 && strings.HasSuffix(basePath, "/") {
		basePath = basePath[:len(basePath)-1]
	}

	kv, err := libkv.NewStore(estore.ETCDV3, etcdAddr, options)
	if err != nil {
		log.Infof("cannot create store: %v", err)
		panic(err)
	}

	if ev3, ok := kv.(*etcd.EtcdV3); ok {
		ev3.AllowKeyNotFound = allowKeyNotFound
	}

	//服务发现出现异常时关闭连接，避免socket连接泄露
	discovery, er := NewEtcdV3DiscoveryStore(basePath, servicePath, kv, allowKeyNotFound)
	if er != nil {
		kv.Close()
	}

	return discovery, er
}

// NewEtcdV3DiscoveryStore return a new EtcdV3Discovery with specified store.
func NewEtcdV3DiscoveryStore(basePath string, servicePath string, kv store.Store, allowKeyNotFound bool) (client.ServiceDiscovery, error) {
	if basePath[0] == '/' {
		basePath = basePath[1:]
	}

	nodePath:=basePath + "/" + servicePath
	d := &EtcdV3Discovery{basePath: nodePath, kv: kv}
	d.stopCh = make(chan struct{})

	ps, err := kv.List(nodePath)
	if err != nil {
		if !allowKeyNotFound || err != store.ErrKeyNotFound {
			log.Infof("cannot get services of from registry: %v, err: %v", nodePath, err)
			return nil, err
		}
	}

	var realIPPORT []*store.KVPair

	nodeLen:=len(nodePath)
	for _, p := range ps {
		if nodeLen>=len(p.Key){
			continue
		}
		key:=p.Key[nodeLen+1:len(p.Key)]
		var ID int
		ID, err = strconv.Atoi(key)
		if ID <= 0 {
			continue
		}

		//Get All the Versions of the ID
		//BASE/L/ServerName/ServerID
		strPerID := fmt.Sprintf("%s/%d",nodePath,ID)
		VersionPerID, err := kv.Get(strPerID)
		if err != nil {
			log.Infof("get kv.Get path:%s, err: %v", strPerID, err)
			return nil, fmt.Errorf("get kv.Get path:%s, err: %v", strPerID, err)
			//panic(err1)
		}

		var jsonValue bytes.Buffer
		jsonValue.Write(VersionPerID.Value)

		//Parse json to Struct
		var etcServID etcdDiscoverServIDVers
		err = json.Unmarshal(jsonValue.Bytes(), &etcServID)
		if err != nil {
			log.Infof("json.Unmarshal zk path %s: %v", string(jsonValue.Bytes()), err)
			return nil, fmt.Errorf("json.Unmarshal zk path %s: %v", string(jsonValue.Bytes()), err)
			//panic(err1)
		}

		topV, err := getTopVersion(etcServID.Vers)
		if err != nil {
			log.Infof("getTopVersion error path:%s, err:%v", strPerID, err)
			return nil, fmt.Errorf("getTopVersion error path:%s, err:%v", strPerID, err)
			//panic(err1)
		}

		//Get the latest Version
		for _, p := range etcServID.Vers {
			if p.Ver == topV {
				realIPPORT = append(realIPPORT, &store.KVPair{Key: strconv.Itoa(int(etcServID.ID)), Value: []byte(p.IP)})
			}
		}
	}

	var pairs = make([]*client.KVPair, 0, len(ps))
	for _, p := range realIPPORT {
		if string(p.Value) == "" {
			continue
		}
		pairs = append(pairs, &client.KVPair{Key: string(p.Value), Value: p.Key})
	}
	d.pairsMu.Lock()
	d.pairs = pairs
	d.pairsMu.Unlock()
	d.RetriesAfterWatchFailed = -1
	d.AllowKeyNotFound = allowKeyNotFound

	go d.watch()
	return d, nil
}

//取最高版本
func getTopVersion(Versions []etcdDiscoverVersIP) (string, error) {
	if Versions == nil || len(Versions) == 0 {
		return "", nil
	}
	if len(Versions) == 1 {
		return Versions[0].Ver, nil
	}
	//取最高版本号
	topVer, topVerIdx := int64(0), 0
	for k, v := range Versions {
		if ver := ver2int(v.Ver); ver > topVer {
			topVer, topVerIdx = ver, k
		}
	}
	return Versions[topVerIdx].Ver, nil
}

//版本号转成数字
func ver2int(ver string) int64 {
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

// NewEtcdV3DiscoveryTemplate returns a new EtcdV3Discovery template.
func NewEtcdV3DiscoveryTemplate(basePath string, servicePath string, etcdAddr []string, allowKeyNotFound bool, options *store.Config) (client.ServiceDiscovery, error) {
	if len(basePath) > 1 && strings.HasSuffix(basePath, "/") {
		basePath = basePath[:len(basePath)-1]
	}

	kv, err := libkv.NewStore(estore.ETCDV3, etcdAddr, options)
	if err != nil {
		log.Infof("cannot create store: %v", err)
		return nil, err
	}

	if ev3, ok := kv.(*etcd.EtcdV3); ok {
		ev3.AllowKeyNotFound = allowKeyNotFound
	}

	return NewEtcdV3DiscoveryStore(basePath, servicePath, kv, allowKeyNotFound)
}

// Clone clones this ServiceDiscovery with new servicePath.
func (d *EtcdV3Discovery) Clone(servicePath string) (client.ServiceDiscovery, error) {
	return NewEtcdV3DiscoveryStore(d.basePath, servicePath, d.kv, d.AllowKeyNotFound)
}

//拷贝一份
func (d EtcdV3Discovery) Copy(basePath, servicePath string) client.ServiceDiscovery {
	if basePath[0] == '/' {
		basePath = basePath[1:]
	}

	if len(basePath) > 1 && strings.HasSuffix(basePath, "/") {
		basePath = basePath[:len(basePath)-1]
	}
	sd, _ := NewEtcdV3DiscoveryStore(basePath, servicePath, d.kv, d.AllowKeyNotFound)
	return sd
}

// SetFilter sets the filer.
func (d *EtcdV3Discovery) SetFilter(filter client.ServiceDiscoveryFilter) {
	d.filter = filter
}

// GetServices returns the servers
func (d *EtcdV3Discovery) GetServices() []*client.KVPair {
	d.pairsMu.RLock()
	defer d.pairsMu.RUnlock()
	return d.pairs
}

// WatchService returns a nil chan.
func (d *EtcdV3Discovery) WatchService() chan []*client.KVPair {
	d.mu.Lock()
	defer d.mu.Unlock()

	ch := make(chan []*client.KVPair, 10)
	d.chans = append(d.chans, ch)
	return ch
}

func (d *EtcdV3Discovery) RemoveWatcher(ch chan []*client.KVPair) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var chans []chan []*client.KVPair
	for _, c := range d.chans {
		if c == ch {
			continue
		}

		chans = append(chans, c)
	}

	d.chans = chans
}

func (d *EtcdV3Discovery) watch() {
	defer func() {
		d.kv.Close()
	}()


	for {
		var err error
		var c <-chan []*store.KVPair
		var tempDelay time.Duration

		retry := d.RetriesAfterWatchFailed
		for d.RetriesAfterWatchFailed < 0 || retry >= 0 {
			c, err = d.kv.WatchTree(d.basePath, nil)
			if err != nil {
				if d.RetriesAfterWatchFailed > 0 {
					retry--
				}
				if tempDelay == 0 {
					tempDelay = 1 * time.Second
				} else {
					tempDelay *= 2
				}
				if max := 30 * time.Second; tempDelay > max {
					tempDelay = max
				}
				log.Warnf("can not watchtree (with retry %d, sleep %v): %s: %v", retry, tempDelay, d.basePath, err)
				time.Sleep(tempDelay)
				continue
			}
			break
		}

		if err != nil {
			log.Errorf("can't watch %s: %v", d.basePath, err)
			return
		}
	rewatch:
		for {
			select {
			case <-d.stopCh:
				log.Info("discovery has been closed")
				return
			case ps, ok := <-c:
				if !ok {
					break rewatch
				}
				var pairs []*client.KVPair // latest servers
				if ps == nil && !d.AllowKeyNotFound {
					d.pairsMu.Lock()
					d.pairs = pairs
					d.pairsMu.Unlock()
					continue
				}

				for _, p := range ps {
					arr:=strings.Split(p.Key,"/")
					var ID int
					ID, err = strconv.Atoi(arr[len(arr)-1])
					if ID <= 0 {
						continue
					}
					//Decode json

					var jsonValue bytes.Buffer
					jsonValue.Write(p.Value)

					var etcServID etcdDiscoverServIDVers
					err = json.Unmarshal(jsonValue.Bytes(), &etcServID)
					if err != nil {
						log.Errorf("json.Unmarshal etcd path %s: %v", string(jsonValue.Bytes()), err)
					}

					topV, err1 := getTopVersion(etcServID.Vers)
					if err1 != nil {
						log.Infof("getTopVersion error path:%s, err:%v", string(p.Value), err1)
					}

					for _, pv := range etcServID.Vers {
						if pv.Ver == topV {
							pairs = append(pairs, &client.KVPair{Key: pv.IP, Value: p.Key})
						}
					}

					//pairs = append(pairs, &KVPair{Key: p.Key, Value: string(p.Value)})
				}
				d.pairsMu.Lock()
				d.pairs = pairs
				d.pairsMu.Unlock()

				d.mu.Lock()
				for _, ch := range d.chans {
					ch := ch
					go func() {
						defer func() {
							recover()
						}()

						select {
						case ch <- pairs:
						case <-time.After(time.Minute):
							log.Warn("chan is full and new change has been dropped")
						}
					}()
				}
				d.mu.Unlock()
			}
		}
	}
}

func (d *EtcdV3Discovery) Close() {
	close(d.stopCh)
}

//检测zk链路是否ok 此方法不是特别友好，应该是在kv那层暴露连接是否可用
func (d *EtcdV3Discovery) IsClosed(basePath, servicePath string) bool {
	if basePath[0] == '/' {
		basePath = basePath[1:]
	}

	if len(basePath) > 1 && strings.HasSuffix(basePath, "/") {
		basePath = basePath[:len(basePath)-1]
	}
	_, err := d.kv.List(basePath + "/" + servicePath)
	if err != nil && err != store.ErrKeyNotFound { //未找到则证明这条链路是没有问题的
		log.Errorf("get list error err=%s", err)
		return true
	}
	return false
}
