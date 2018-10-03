package condition

import (
	"time"
	"os"
	"io/ioutil"
	"encoding/json"

	"github.com/golang/glog"
	"github.com/fsnotify/fsnotify"

	"eviction-agent/pkg/evictionclient"

	"k8s.io/apimachinery/pkg/util/clock"
)

const (
	// updatePeriod is the period
	updatePeriod = 1 * time.Second
)

type ConditionManager interface {
	// Start starts the condition manager
	Start() error
}

type conditionManager struct {
	client           evictionclient.Client
	policyConfigFile string
	clock            clock.Clock
	untaintGracePeriod string
}

type policyConfig struct {
	UntaintGracePeriod string `json:"untaintGracePeriod"`
}

// NewConditionManager creates a condition manager
func NewConditionManager(client evictionclient.Client, clock clock.Clock,
	configFile string) ConditionManager {
	return &conditionManager{
		client:     client,
		clock:      clock,
		policyConfigFile: configFile,
	}
}

func (c *conditionManager) Start() error {
	glog.Infof("Start condition manager\n")

	err := c.loadPolicyConfig()
	if err != nil {
		return err
	}
	go c.policyFileWatcher()
	c.syncStats()

	return nil
}

// policyFileWatcher watch policy file for updating
func (c *conditionManager) policyFileWatcher() {
	glog.Infof("start policy file watcher\n")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		glog.Errorf("create a new file wather error %v\n", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(c.policyConfigFile); err != nil {
		glog.Errorf("add policy config file watcher error %v\n", err)
		return
	}

	for {
		select {
		// watch for events
		case event := <- watcher.Events:
			glog.Infof("%#v\n", event)
			if event.Op == fsnotify.Write || event.Op == fsnotify.Create {
				c.loadPolicyConfig()
			}
		}
	}
}

// loadPolicyConfig read configuration from policyConfigFile
func (c *conditionManager) loadPolicyConfig() error {
	configFile, err := os.Open(c.policyConfigFile)
	if err != nil {
		return err
	}
	defer configFile.Close()

	byteValue, _ := ioutil.ReadAll(configFile)

	var config policyConfig
	err = json.Unmarshal(byteValue, &config)
	if err != nil {
		glog.Errorf("json unmarshal failed for file: %v", c.policyConfigFile)
	}

	// TODO: add other configure here
	c.untaintGracePeriod = config.UntaintGracePeriod

	return nil
}

// syncStats
func (c *conditionManager) syncStats() {
	glog.Infof("start sync stats\n")
	for {
		time.Sleep(30 * time.Second)
		glog.Infof("get grace period: %v", c.untaintGracePeriod)
	}
}