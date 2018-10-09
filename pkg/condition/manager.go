package condition

import (
	"time"
	"os"
	"io/ioutil"
	"encoding/json"

	"github.com/golang/glog"
	"github.com/fsnotify/fsnotify"

	"k8s.io/apimachinery/pkg/util/clock"

	"eviction-agent/pkg/types"
	"eviction-agent/pkg/evictionclient"
)

const (
	// updatePeriod is the period
	updatePeriod = 3 * time.Minute
)

type NodeCondition struct {
	DiskIOAvailable    bool
	NetworkIOAvailabel bool
}

type ConditionManager interface {
	// Start starts the condition manager
	Start() error
	GetNodeCondition() (*NodeCondition)
	ChooseOnePodToEvict(string) (*types.PodInfo)
}

type conditionManager struct {
	client              evictionclient.Client
	policyConfigFile    string
	clock               clock.Clock
	untaintGracePeriod  string
	nodeCondition       NodeCondition  //
	//pods               []string // test store pod name
	podToEvict          types.PodInfo
	lastEvictPod        types.PodInfo
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
		nodeCondition: NodeCondition{
			DiskIOAvailable:    true,
			NetworkIOAvailabel: true,
		},
	}
}

func (c *conditionManager) Start() error {
	glog.Infof("Start condition manager\n")

	// load policy configuration
	err := c.loadPolicyConfig()
	if err != nil {
		return err
	}
	// watch policy configuration
	go c.policyConfigFileWatcher()

	// get node stats periodically
	go c.syncStats()

	return nil
}

// policyFileWatcher watch policy file for updating
func (c *conditionManager) policyConfigFileWatcher() {
	glog.Infof("Start policy file watcher\n")
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
	glog.Infof("Start sync stats\n")
	for {
		c.nodeCondition.NetworkIOAvailabel = !c.nodeCondition.NetworkIOAvailabel
		c.nodeCondition.DiskIOAvailable = !c.nodeCondition.DiskIOAvailable

		/***
		stats, err := c.client.GetSummaryStats()
		if err != nil {
			glog.Errorf("sync stats get summary stats error: %v", err)
			continue
		}

		// test for summary stats api

		netStats := stats.NodeNetStats
		glog.Infof("get node network stats: %v", netStats.Name)
		netIfaces := netStats.Interfaces
		for _, net := range netIfaces {
			glog.Infof("net %v, rx: %v, tx: %v", net.Name, net.RxBytes, net.TxBytes)
		}

		podStats := stats.PodStats
		for _, pod := range podStats {
			//glog.Infof("pod %v, ns: %v", pod.PodRef.Name, pod.PodRef.Namespace)
			if strings.Contains(pod.PodRef.Name, "contiv-deploy") {
				glog.Infof("get pod %v to evict\n", pod.PodRef.Name)
				c.podToEvict.Name = pod.PodRef.Name
				c.podToEvict.Namespace = pod.PodRef.Namespace
			}
		}
		*/
		// TODO: check Network IO stats, and change NodeCondition

		// TODO: check Disk IO stats, and change NodeCondition
		time.Sleep(updatePeriod)
	}
}

// GetNodeCondition
func (c *conditionManager) GetNodeCondition() (*NodeCondition) {
	return &c.nodeCondition
}

// ChoseOnePodToEvict
func (c *conditionManager) ChooseOnePodToEvict(evictType string) (*types.PodInfo) {
	return &c.podToEvict
}
