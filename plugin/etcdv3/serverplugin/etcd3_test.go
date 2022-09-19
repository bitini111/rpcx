package serverplugin

import (
	"fmt"
	"github.com/bitini111/rpcx/plugin/common/serverplugin"
	"testing"
	"time"

	"github.com/bitini111/rpcx/server"
	metrics "github.com/rcrowley/go-metrics"
)

func TestEtcdV3Registry(t *testing.T) {
	s := server.NewServer()

	r := &EtcdV3RegisterPlugin{
		ServiceAddress: "ws@192.168.8.136:8972",
		EtcdServers:    []string{"192.168.8.100:12379"},
		BasePath:       "/rpcx_test",
		ServerID:       2,
		ServiceName:    "TssService",
		Version:        "0.0.0.1",
		Metrics:        metrics.NewRegistry(),
		UpdateInterval: time.Second * 3,
	}
	err := r.Start()
	if err != nil {
		return
	}
	s.Plugins.Add(r)

	name := fmt.Sprintf("%s/%s/%d#%s", r.BasePath, r.ServiceName, r.ServerID, r.Version)

	s.RegisterName(name, new(serverplugin.Arith), "")
	go s.Serve("ws", "192.168.8.136:8972")
	defer s.Close()

	if len(r.Services) != 1 {
		t.Fatal("failed to register services in etcd")
	}
	if err := r.Stop(); err != nil {
		fmt.Println(err)
		t.Fatal(err)
	}

	for true {

	}
}
