package condition

import (
	"time"
	"os"
	"fmt"
	"io/ioutil"
	"encoding/json"

	"github.com/golang/glog"
	"github.com/fsnotify/fsnotify"

	"eviction-agent/pkg/types"
	"eviction-agent/pkg/evictionclient"
)

const (
	// updatePeriod is the period
	updatePeriod = 10 * time.Second
	statsBufferLen = 3
	taintThreshold = 0.9
	defaultInterface = "eth0"
	defaultDiskIOTotal = 10000
	defaultNetwortIOTotal = 10000
)

type NodeCondition struct {
	DiskIOAvailable    bool
	NetworkIOAvailabel bool
}

type statType struct {
	time time.Time
	name string
	rx   uint64
	tx   uint64
}

type podStatType struct {
	time        time.Time
	name        string
	namespace   string
	netIOStats  statType
	diskIOStats statType
}

type nodeStatsType struct {
	time        time.Time
	netIOStats  statType
	diskIOStats statType
	podStats    map[string]podStatType  // key=PodNamespace.Name
}

type ConditionManager interface {
	// Start starts the condition manager
	Start() error
	// Get node condition
	GetNodeCondition() (*NodeCondition)
	// Choose one pod to evict, according priority or some policies
	ChooseOnePodToEvict(string) (*types.PodInfo, bool, string, error)
}

type conditionManager struct {
	client              evictionclient.Client
	policyConfigFile    string
	taintThreshold      float64
	untaintGracePeriod  int32   // minutes
	nodeCondition       NodeCondition
	podToEvict          types.PodInfo
	nodeStats           []nodeStatsType
	autoEvict           bool
	networkInterface    string
	diskIoTotal         int32
	networkIoTotal      int32
}

type policyConfig struct {
	UntaintGracePeriod int32   `json:"untaintGracePeriod"`
	TaintThreshold     float64 `json:"taintThreshold"`
	AutoEvictFlag      bool    `json:"autoEvictFlag"`
	//Resource total
	NetworkInterface   string  `json:"networkInterface"`
	NetworkIOPSTotal   int32   `json:"networkIOPSTotal"`
	DiskDevName        string  `json:"diskDevName"`
	DiskIOPSTotal        int32   `json:"diskIOPSTotal"`
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
		networkInterface: defaultInterface,
		diskIoTotal: defaultDiskIOTotal,
		networkIoTotal: defaultNetwortIOTotal,
	}
}

func (c *conditionManager) Start() error {
	glog.Infof("Start condition manager\n")

	// get node iops total value
	nodeIOPSTotal, err := c.client.GetIOPSTotalFromAnnotations()
	if err != nil {
		return err
	}
	c.networkIoTotal = nodeIOPSTotal.NetworkIOPSTotal
	c.diskIoTotal = nodeIOPSTotal.DiskIOPSTotal
	glog.Infof("Get total value, networkIOPS: %v, diskIOPS: %v",c.networkIoTotal, c.diskIoTotal)

	// load policy configuration
	err = c.loadPolicyConfig()
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
		glog.Errorf("open policy config file error: %v", err)
		return err
	}
	defer configFile.Close()

	byteValue, _ := ioutil.ReadAll(configFile)

	var config policyConfig
	err = json.Unmarshal(byteValue, &config)
	if err != nil {
		glog.Errorf("json unmarshal failed for file: %v, error: %v", c.policyConfigFile, err)
		return err
	}

	// TODO: add other configure here
	if config.UntaintGracePeriod != 0 {
		c.untaintGracePeriod = config.UntaintGracePeriod
	}

	if config.DiskDevName != "" {
	}

	if config.DiskIOPSTotal != 0 {
		c.diskIoTotal = config.DiskIOPSTotal
	}
	if int(config.TaintThreshold) != 0 {
		c.taintThreshold = config.TaintThreshold
	}
	if config.NetworkIOPSTotal != 0 {
		c.networkIoTotal = config.NetworkIOPSTotal
	}
	if config.NetworkInterface != "" {
		c.networkInterface = config.NetworkInterface
	}
	c.autoEvict = config.AutoEvictFlag
	glog.Infof("Get diskIoTotal: %v, taintThreshold: %v, network name: %v, networkIOTotal: %v, autoEvictFlag: %v",
		c.diskIoTotal, c.taintThreshold, c.networkInterface, c.networkIoTotal, c.autoEvict)

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
		newNodeStats.time = stats.NodeNetStats.Time.Time

		// Get Network IO stats, add it to nodeStats
		// TODO: if specify network interface, get it from interface list, otherwise, we get the default: eth0
		netStats := stats.NodeNetStats
		for _, iface := range netStats.Interfaces {
			net := statType{}
			if iface.TxPackets != nil && iface.RxPackets != nil {
				net.rx = *iface.TxPackets
				net.tx = *iface.RxPackets
			}
			if iface.Name == c.networkInterface {
				newNodeStats.netIOStats.time = netStats.Time.Time
				newNodeStats.netIOStats.rx = net.rx
				newNodeStats.netIOStats.tx = net.tx
				newNodeStats.netIOStats.name = iface.Name
			}
		}

		// Get all pods stats, add them to nodeStats. podStats := stats.PodStats
		// TODO: add Disk IO stat to pod stats
		podStats := stats.PodStats
		for _, pod := range podStats {
			diskStats := statType{}
			// Maybe some pod doesn't has DiskIoStats, set them to ZERO
			for _, container := range pod.Containers {
				if container.Diskio != nil {
					if container.Diskio.DiskIoStats != nil {
						ioServiced := container.Diskio.DiskIoStats.IoServiced
						if len(ioServiced) != 0 {
							diskStats.time = pod.Diskio.Time.Time
							diskStats.name = pod.Diskio.DiskIoStats.IoServiced[0].Device
							diskStats.rx += ioServiced[0].Stats["Read"]
							diskStats.tx += ioServiced[0].Stats["Write"]
						}
					}
				}
			}
			netIoStats := statType{}
			if pod.Network != nil {
				if pod.Network.RxPackets != nil && pod.Network.TxPackets != nil &&
					pod.Network.Name == c.networkInterface {
					netIoStats.rx = *pod.Network.RxPackets
					netIoStats.tx = *pod.Network.TxPackets
				}
			}
			podStat := podStatType{
				name: pod.PodRef.Name,
				namespace: pod.PodRef.Namespace,
				time: pod.StartTime.Time,
				netIOStats: statType{
					name: pod.Network.Name,
					time: pod.Network.Time.Time,
					rx:   netIoStats.rx,
					tx:   netIoStats.tx,
				},
				diskIOStats: diskStats,
			}
			keyName := podStat.namespace + "." + podStat.name
			newNodeStats.podStats[keyName] = podStat
		}

		// TODO: if specify disk name, get it from device list, otherwise, we get the first one
		newNodeStats.diskIOStats.time = stats.NodeDiskIoStats.Time.Time
		// Add system container disk-io stats to node stats
		sysContainers := stats.SysContainers
		for _, container := range sysContainers {
			if container.Diskio != nil {
				if container.Diskio.DiskIoStats != nil {
					ioServiced := container.Diskio.DiskIoStats.IoServiced
					if len(ioServiced) != 0 {
						if name := ioServiced[0].Device; name != "" {
							newNodeStats.diskIOStats.name = name
						}
						newNodeStats.diskIOStats.rx += ioServiced[0].Stats["Read"]
						newNodeStats.diskIOStats.tx += ioServiced[0].Stats["Write"]
					}
				}
			}
		}
		// Add user pod disk-io stats to node stats
		for _, pod := range newNodeStats.podStats {
			newNodeStats.diskIOStats.rx += pod.diskIOStats.rx
			newNodeStats.diskIOStats.tx += pod.diskIOStats.tx
		}

		glog.V(10).Infof("get summary stats from node: %v\n", stats.NodeName)
		glog.V(10).Infof("netName: %v, netRX: %v, netTX: %v at %v, diskName: %v, diskRead: %v, diskWrite: %v at %v\n",
			netStats.Name, *netStats.RxPackets, *netStats.TxPackets, netStats.Time.Time,
			newNodeStats.diskIOStats.name, newNodeStats.diskIOStats.rx,
			newNodeStats.diskIOStats.tx, newNodeStats.diskIOStats.time)
		glog.V(10).Infof("diskname: %v, Read: %v, Write: %v\n",
			newNodeStats.diskIOStats.name, newNodeStats.diskIOStats.rx, newNodeStats.diskIOStats.tx)

		// add new node stats to list
		if len(c.nodeStats) == statsBufferLen {
			// If get the same time, ignore it.
			if newNodeStats.time != c.nodeStats[statsBufferLen - 1].time {
				c.nodeStats = append(c.nodeStats[1:], newNodeStats)
			} else {
				glog.V(10).Infof("Abandon this stats at: %v", newNodeStats.time)
			}
		} else {
			c.nodeStats = append(c.nodeStats, newNodeStats)
		}
		time.Sleep(updatePeriod)
	}
	glog.Errorf("Sync stats stop")
}

// GetNodeCondition
func (c *conditionManager) GetNodeCondition() (*NodeCondition) {
	// Return directly, there are no enough stats
	if len(c.nodeStats) != statsBufferLen {
		return &c.nodeCondition
	}
	// Compute Network IOPS. IOPS = (newIO - lastIO) / duration_time
	newNetworkStat := statType{
		time: c.nodeStats[statsBufferLen - 1].netIOStats.time,
		rx:   c.nodeStats[statsBufferLen - 1].netIOStats.rx,
		tx:   c.nodeStats[statsBufferLen - 1].netIOStats.tx,
		name: c.nodeStats[statsBufferLen - 1].netIOStats.name,
	}
	lastNetworkStat := statType{
		time: c.nodeStats[statsBufferLen - 2].netIOStats.time,
		rx:   c.nodeStats[statsBufferLen - 2].netIOStats.rx,
		tx:   c.nodeStats[statsBufferLen - 2].netIOStats.tx,
		name: c.nodeStats[statsBufferLen - 2].netIOStats.name,
	}
	networkIOPS := 1e9 * float64(newNetworkStat.rx + newNetworkStat.tx -
		lastNetworkStat.rx - lastNetworkStat.tx) /
		float64(newNetworkStat.time.UnixNano() - lastNetworkStat.time.UnixNano())
	if networkIOPS < 0 {
		glog.Errorf("get network iops error, a negative value, ignore it")
		networkIOPS = 0
	}
	glog.Infof("get network %s iops: %v Packets/s\n",
		newNetworkStat.name, int(networkIOPS))

	// Compute Disk IOPS
	newDiskIoStat := statType{
		time: c.nodeStats[statsBufferLen - 1].diskIOStats.time,
		rx:   c.nodeStats[statsBufferLen - 1].diskIOStats.rx,
		tx:   c.nodeStats[statsBufferLen - 1].diskIOStats.tx,
		name: c.nodeStats[statsBufferLen - 1].diskIOStats.name,
	}
	lastDiskIoStat := statType{
		time: c.nodeStats[statsBufferLen - 2].diskIOStats.time,
		rx:   c.nodeStats[statsBufferLen - 2].diskIOStats.rx,
		tx:   c.nodeStats[statsBufferLen - 2].diskIOStats.tx,
		name: c.nodeStats[statsBufferLen - 2].diskIOStats.name,
	}
	diskIOPS := 1e9 * float64(newDiskIoStat.rx + newDiskIoStat.tx - lastDiskIoStat.rx - lastDiskIoStat.tx) /
		float64(newDiskIoStat.time.UnixNano() - lastDiskIoStat.time.UnixNano())
	if diskIOPS < 0 {
		glog.Errorf("get disk iops error, a negative value, ignore it")
		diskIOPS = 0
	}
	glog.Infof("get disk %s, iops: %v\n",
		newDiskIoStat.name, int(diskIOPS))

	if diskIOPS > float64(c.diskIoTotal) * c.taintThreshold {
			glog.Infof("disk %s out of limits, iops: %v", newDiskIoStat.name, int(diskIOPS))
			c.nodeCondition.DiskIOAvailable = false
	} else {
		c.nodeCondition.DiskIOAvailable = true
	}

	if networkIOPS > float64(c.networkIoTotal) * c.taintThreshold {
		glog.Infof("network %s out of limis, iops: %v", newNetworkStat.name, int(networkIOPS))
		c.nodeCondition.NetworkIOAvailabel = false
	} else {
		c.nodeCondition.NetworkIOAvailabel = true
	}

	return &c.nodeCondition
}

// ChooseOnePodToEvict
func (c *conditionManager) ChooseOnePodToEvict(evictType string) (*types.PodInfo, bool, string, error) {
	isEvict := false
	if len(c.nodeStats) != statsBufferLen {
		glog.Infof("wait for a minute\n")
		return nil, isEvict, "", fmt.Errorf("wait for a minute")
	}

	// Get lower priority pod, if autoEvict
	pods, err := c.client.GetLowerPriorityPods()
	if err != nil {
		return nil, isEvict, "", err
	}

	// if auto-evict and there are some lower priority pods, evict pod in agent.
	if c.autoEvict {
		glog.Infof("Config eviction-agent AUTO-EVICT")
		if len(pods) != 0 {
			isEvict = true
		}
	}

	// Get pod which consume resource seriously
	isEvicting, priority := c.getEvilPod(evictType, pods)
	if isEvicting {
		return nil, isEvict, "", fmt.Errorf("Pod: %v is evicting...", c.podToEvict.Name)
	}

	return &c.podToEvict, isEvict, priority, nil
}

// getEvilPod pick the pod which consume the resource most
func (c *conditionManager) getEvilPod(evictType string, pods []types.PodInfo) (bool,string) {
	// check if it is evicting
	priority := types.NeedEvict
	if len(pods) != 0 {
		for _, pod := range pods {
			if pod.Name == c.podToEvict.Name && pod.Namespace == c.podToEvict.Namespace {
				return true, priority
			}
		}
	}
	// compute and get the evil pod
	if evictType == types.DiskIO {
		evilValue := 0.0
		evilPod := types.PodInfo{}
		if len(pods) != 0  {
			for _, pod := range pods {
				keyName := pod.Namespace + "." + pod.Name
				newPodDiskStats := c.nodeStats[statsBufferLen - 1].podStats[keyName].diskIOStats
				lastPodDiskStats := c.nodeStats[statsBufferLen - 2].podStats[keyName].diskIOStats
				iops := 1e9 * float64(newPodDiskStats.rx + newPodDiskStats.tx -
					lastPodDiskStats.rx - lastPodDiskStats.tx) /
					float64(newPodDiskStats.time.UnixNano() - lastPodDiskStats.time.UnixNano())
				if iops > evilValue {
					evilValue = iops
					evilPod.Name = pod.Name
					evilPod.Namespace = pod.Namespace
				}
			}
			priority = types.NeedEvict
			glog.Infof("get evil pod: %v, iops: %v from low priority pods, diskio busy", evilPod.Name, evilValue)
		} else {
			for keyName, pod := range c.nodeStats[statsBufferLen - 1].podStats {
				newPodDiskStats := pod.diskIOStats
				lastStats, ok := c.nodeStats[statsBufferLen - 2 ].podStats[keyName]
				if ok {
					lastPodDiskStats := lastStats.diskIOStats
					iops := 1e9 * float64(newPodDiskStats.rx + newPodDiskStats.tx -
						lastPodDiskStats.rx - lastPodDiskStats.tx) /
						float64(newPodDiskStats.time.UnixNano() - lastPodDiskStats.time.UnixNano())
					if iops > evilValue {
						evilValue = iops
						evilPod.Name = pod.name
						evilPod.Namespace = pod.namespace
					}
				}
			}
			priority = types.EvictCandidate
			glog.Infof("get evil pod: %v, iops: %v from other pods, diskio busy", evilPod.Name, evilValue)
		}
		c.podToEvict = evilPod
	} else if evictType == types.NetworkIO {
		evilValue := 0.0
		evilPod := types.PodInfo{}
		if len(pods) != 0  {
			for _, pod := range pods {
				keyName := pod.Namespace + "." + pod.Name
				newPodNetStats := c.nodeStats[statsBufferLen - 1].podStats[keyName].netIOStats
				lastPodNetStats := c.nodeStats[statsBufferLen - 2].podStats[keyName].netIOStats
				iops := 1e9 * float64(newPodNetStats.rx + newPodNetStats.tx - lastPodNetStats.rx - lastPodNetStats.tx) /
					float64(newPodNetStats.time.UnixNano() - lastPodNetStats.time.UnixNano())
				if iops > evilValue {
					evilValue = iops
					evilPod.Name = pod.Name
					evilPod.Namespace = pod.Namespace
				}
			}
			priority = types.NeedEvict
			glog.Infof("get evil pod: %v, iops: %v from low priority pods, network busy", evilPod.Name, evilValue)
		} else {
			for keyName, pod := range c.nodeStats[statsBufferLen - 1].podStats {
				newPodNetStats := pod.netIOStats
				lastStats, ok := c.nodeStats[statsBufferLen - 2 ].podStats[keyName]
				if ok {
					lastPodNetStats := lastStats.netIOStats
					iops := 1e9 * float64(newPodNetStats.rx + newPodNetStats.tx -
						lastPodNetStats.rx - lastPodNetStats.tx) /
						float64(newPodNetStats.time.UnixNano() - lastPodNetStats.time.UnixNano())
					if iops > evilValue {
						evilValue = iops
						evilPod.Name = pod.name
						evilPod.Namespace = pod.namespace
					}
				}
			}
			priority = types.EvictCandidate
			glog.Infof("get evil pod: %v, iops: %v from other pods, network busy", evilPod.Name, evilValue)
		}
		c.podToEvict = evilPod
	}
	return false, priority
}
