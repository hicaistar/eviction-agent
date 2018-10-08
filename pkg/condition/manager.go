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
	"eviction-agent/pkg/summary"
	"strings"
	"fmt"
)

const (
	// updatePeriod is the period
	updatePeriod = 1 * time.Second
)

type ConditionManager interface {
	// Start starts the condition manager
	Start() error
	GetNodeCondition() (*summary.NodeCondition)
	ChoseOnePodToEvict() (string, string, error)
}

type conditionManager struct {
	client             evictionclient.Client
	policyConfigFile   string
	clock              clock.Clock
	untaintGracePeriod string
	nodeCondition      summary.NodeCondition
	pods               []string // test store pod name
	podToEvict         string
	podNamespace       string
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
	go c.syncStats()

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
		time.Sleep(20 * time.Second)
		stats, err := c.client.GetSummaryStats()
		if err != nil {
			glog.Errorf("sync stats get summary stats error: %v", err)
			continue
		}

		// test for summary stats api
		/***
		netStats := stats.NodeNetStats
		glog.Infof("get node network stats: %v", netStats.Name)
		netIfaces := netStats.Interfaces
		for _, net := range netIfaces {
			glog.Infof("net %v, rx: %v, tx: %v", net.Name, net.RxBytes, net.TxBytes)
		}
		*/
		podStats := stats.PodStats
		for _, pod := range podStats {
			//glog.Infof("pod %v, ns: %v", pod.PodRef.Name, pod.PodRef.Namespace)
			if strings.Contains(pod.PodRef.Name, "contiv-deploy") {
				glog.Infof("get pod %v to evict\n", pod.PodRef.Name)
				c.podToEvict = pod.PodRef.Name
				c.podNamespace = pod.PodRef.Namespace
			}
		}
		// TODO: check Network IO stats, and change NodeCondition

		// TODO: check Disk IO stats, and change NodeCondition
	}
}

// GetNodeCondition
func (c *conditionManager) GetNodeCondition() (*summary.NodeCondition) {
	return &c.nodeCondition
}

// ChoseOnePodToEvict
func (c *conditionManager) ChoseOnePodToEvict() (string, string, error) {
	if c.podToEvict != "" {
		glog.Infof("chose pod: %v to evict\n", c.podToEvict)
		return c.podToEvict, c.podNamespace, nil
	}
	return "", "", fmt.Errorf("there is no pod to evict")
}
