package main

import (
	"flag"
	"math/rand"
	"time"

	"eviction-agent/cmd/options"
	"eviction-agent/pkg/evictionclient"
	"eviction-agent/pkg/evictionmanager"
	"eviction-agent/pkg/log"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	// Init from environment
	eao := options.NewEvictionAgentOptions()
	eao.SetNodeNameOrDie()
	eao.SetPolicyConfigFileOrDie()
	eao.SetLogDirOrDie()
	log.Config("info", eao.LogDir,false,1 * 1024 * 1024,5)

	flag.Set("log_dir", eao.LogDir)
	flag.Parse()

	log.Infof("Start to run eviction agent on %v...", eao.NodeName)

	c := evictionclient.NewClientOrDie(eao)
	e := evictionmanager.NewEvictionManager(c, eao.PolicyConfigFile)

	if err := e.Run(); err != nil {
		log.Fatalf("Eviction agent failed with error: %v", err)
	}
}
