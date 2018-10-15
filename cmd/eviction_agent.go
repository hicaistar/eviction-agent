package main

import (
	"flag"
	"math/rand"
	"time"

	"github.com/golang/glog"

	"eviction-agent/cmd/options"
	"eviction-agent/pkg/evictionclient"
	"eviction-agent/pkg/evictionmanager"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	// Init from environment
	eao := options.NewEvictionAgentOptions()
	eao.SetNodeNameOrDie()
	eao.SetPolicyConfigFileOrDie()
	eao.SetLogDirOrDie()

	flag.Set("log_dir", eao.LogDir)
	flag.Parse()

	glog.Infof("Start to run eviction agent on %v...\n", eao.NodeName)

	c := evictionclient.NewClientOrDie(eao)
	e := evictionmanager.NewEvictionManager(c, eao.PolicyConfigFile)

	if err := e.Run(); err != nil {
		glog.Fatalf("Eviction agent failed with error: %v", err)
	}
}
