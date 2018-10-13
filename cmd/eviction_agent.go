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
	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "INFO")
	flag.Parse()

	eao := options.NewEvictionAgentOptions()
	eao.SetNodeNameOrDie()
	eao.SetPolicyConfigFileOrDie()

	glog.Infof("Start to run eviction agent on %v...\n", eao.NodeName)

	c := evictionclient.NewClientOrDie(eao)
	e := evictionmanager.NewEvictionManager(c, eao.PolicyConfigFile)

	if err := e.Run(); err != nil {
		glog.Fatalf("Eviction agent failed with error: %v", err)
	}
}
