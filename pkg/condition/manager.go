package condition

import (
	"time"
	"os"
	"io/ioutil"
	"encoding/json"

	"github.com/golang/glog"
	"github.com/fsnotify/fsnotify"

	"eviction-agent/pkg/types"
	"eviction-agent/pkg/evictionclient"
)

const (
	// updatePeriod is the period
	updatePeriod = 30 * time.Second
	statsBufferLen = 5
	taintThreshold = 0.9
)

type NodeCondition struct {
	DiskIOAvailable    bool
	NetworkIOAvailabel bool
}

type statType struct {
	time    time.Time
	rxBytes uint64
	txBytes uint64
}

type podStatType struct {
	time        time.Time
	name        string
	namespace   string
	netIOStats  statType
	diskIOStats statType
}

type nodeStatsType struct {
	netIOStats  statType
	diskIOStats statType
	podStats    map[string]podStatType
}

type ConditionManager interface {
	// Start starts the condition manager
	Start() error
	// Get node condition
	GetNodeCondition() (*NodeCondition)
	// Choose one pod to evict, according priority or some policies
	ChooseOnePodToEvict(string) (*types.PodInfo)
}

type conditionManager struct {
	client              evictionclient.Client
	policyConfigFile    string
	taintThreshold      float64
	untaintGracePeriod  string
	nodeCondition       NodeCondition
	podToEvict          types.PodInfo
	lastEvictPod        types.PodInfo
	nodeStats           []nodeStatsType
	autoEvict           bool
}

type policyConfig struct {
	UntaintGracePeriod string `json:"untaintGracePeriod"`
	TaintThreshold     string `json:"taintThreshold"`
	AutoEvictFlag      bool   `json:"autoEvictFalg"`
	//Resource total
	NetworkInterface   string `json:"networkInterface"`
	NetworkIOPSTotal   string `json:"networkIOPSTotal"`
	DiskDevName        string `json:"diskDevName"`
	DiskIOPSTotal      string `json:"diskIOPSTotal"`
}

// NewConditionManager creates a condition manager
func NewConditionManager(client evictionclient.Client, configFile string) ConditionManager {
	return &conditionManager{
		client:     client,
		policyConfigFile: configFile,
		nodeCondition: NodeCondition{
			DiskIOAvailable:    true,
			NetworkIOAvailabel: true,
		},
		taintThreshold: taintThreshold,
		autoEvict: false,
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

		// Get summary stats
		stats, err := c.client.GetSummaryStats()
		if err != nil {
			glog.Errorf("sync stats get summary stats error: %v", err)
			continue
		}


		newNodeStats := nodeStatsType{}
		newNodeStats.podStats = make(map[string]podStatType)

		// Get Network IO stats, add it to nodeStats
		netStats := stats.NodeNetStats
		newNodeStats.netIOStats.time = netStats.Time.Time
		newNodeStats.netIOStats.rxBytes = *netStats.RxBytes
		newNodeStats.netIOStats.txBytes = *netStats.TxBytes

		// TODO: Get Disk IO stats, add it to nodeStats

		// Get all pods stats, add them to nodeStats. podStats := stats.PodStats
		// TODO: add Disk IO stat to pod stats
		podStats := stats.PodStats
		for _, pod := range podStats {
			podStat := podStatType{
				name: pod.PodRef.Name,
				namespace: pod.PodRef.Namespace,
				time: pod.StartTime.Time,
				netIOStats: statType{
					time:    pod.Network.Time.Time,
					rxBytes: *pod.Network.RxBytes,
					txBytes: *pod.Network.TxBytes,
				},
				diskIOStats: statType{},
			}
			keyName := podStat.namespace + "." + podStat.name
			newNodeStats.podStats[keyName] = podStat
		}

		// add new node stats to list
		if len(c.nodeStats) == statsBufferLen {
			c.nodeStats = append(c.nodeStats[1:], newNodeStats)
		} else {
			c.nodeStats = append(c.nodeStats, newNodeStats)
		}


		time.Sleep(updatePeriod)
	}
}

// GetNodeCondition
func (c *conditionManager) GetNodeCondition() (*NodeCondition) {
	// Return directly, there are no enough stats
	if len(c.nodeStats) != statsBufferLen {
		return &c.nodeCondition
	}
	// Check network IO stats
	newNetworkStat := statType{
		time: c.nodeStats[statsBufferLen - 1].netIOStats.time,
		rxBytes: c.nodeStats[statsBufferLen - 1].netIOStats.rxBytes,
		txBytes: c.nodeStats[statsBufferLen - 1].netIOStats.txBytes,
	}
	lastNetworkStat := statType{
		time: c.nodeStats[statsBufferLen - 2].netIOStats.time,
		rxBytes: c.nodeStats[statsBufferLen - 2].netIOStats.rxBytes,
		txBytes: c.nodeStats[statsBufferLen - 2].netIOStats.txBytes,
	}
	// TODO: compare new/last time, if equal, skip; and also compare last condition time, if not change, skip
	networkIOPS := 1e9 * float64(newNetworkStat.rxBytes + newNetworkStat.txBytes -
		lastNetworkStat.rxBytes - lastNetworkStat.txBytes) /
		float64(newNetworkStat.time.UnixNano() - lastNetworkStat.time.UnixNano())
	glog.Infof("get networkIOPS: %v Bytes/s", networkIOPS)

	// TODO: add DiskIO check

	c.nodeCondition.NetworkIOAvailabel = true
	c.nodeCondition.DiskIOAvailable = true
	return &c.nodeCondition
}

// ChooseOnePodToEvict
func (c *conditionManager) ChooseOnePodToEvict(evictType string) (*types.PodInfo) {
	if len(c.nodeStats) != statsBufferLen {
		glog.Infof("wait for a minute\n")
		return &c.podToEvict
	}
	return &c.podToEvict
}
