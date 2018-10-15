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
	CPUAvailable       bool
	MemoryAvailable    bool
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
	cpuUsage    float64
	memoryUsage uint64
	netIOStats  statType
	diskIOStats statType
}

type nodeStatsType struct {
	time        time.Time
	netIOStats  statType
	diskIOStats statType
	cpuUsage    float64
	memoryUsage uint64
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
	diskDevName         string
	diskIoTotal         int32
	networkIoTotal      int32
	invalidEvictCount   int32
	cpuTotal            int64
	memTotal            int64
}

type policyConfig struct {
	UntaintGracePeriod int32   `json:"untaintGracePeriod"`
	TaintThreshold     float64 `json:"taintThreshold"`
	AutoEvictFlag      bool    `json:"autoEvictFlag"`
	//Resource total
	NetworkInterface   string  `json:"networkInterface"`
	NetworkIOPSTotal   int32   `json:"networkIOPSTotal"`
	DiskDevName        string  `json:"diskDevName"`
	DiskIOPSTotal      int32   `json:"diskIOPSTotal"`
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
	nodeIOPSTotal, err := c.client.GetResourcesTotalFromAnnotations()
	if err != nil {
		glog.Errorf("Get resource from node error: %v", err)
		return err
	}
	c.networkIoTotal = nodeIOPSTotal.NetworkIOPSTotal
	c.diskIoTotal = nodeIOPSTotal.DiskIOPSTotal
	c.cpuTotal = nodeIOPSTotal.CPUTotal
	c.memTotal = nodeIOPSTotal.MemoryTotal
	glog.Infof("Get total value, networkIOPS: %v, diskIOPS: %v, cpu: %v, memory: %v",
		c.networkIoTotal, c.diskIoTotal, c.cpuTotal, c.memTotal)

	// load policy configuration
	err = c.loadPolicyConfig()
	if err != nil {
		return err
	}

	if c.networkIoTotal == 0 || c.diskIoTotal == 0 {
		return fmt.Errorf("IOPS config is not in pod annotations or configuration file.")
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
		c.diskDevName = config.DiskDevName
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
	glog.Infof("Get diskIoTotal: %v, taintThreshold: %v, network name: %v, " +
		"networkIOTotal: %v, autoEvictFlag: %v, diskDevName: %v",
		c.diskIoTotal, c.taintThreshold, c.networkInterface, c.networkIoTotal, c.autoEvict, c.diskDevName)

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

		// Get CPU and Memory stats
		if stats.NodeCPUStats != nil {
			if stats.NodeCPUStats.UsageNanoCores != nil {
				newNodeStats.cpuUsage = float64(*stats.NodeCPUStats.UsageNanoCores) / 1e9
			}
		}
		if stats.NodeMemoryStats != nil {
			if stats.NodeMemoryStats.UsageBytes != nil {
				newNodeStats.memoryUsage = *stats.NodeMemoryStats.UsageBytes
			}
		}
		glog.V(10).Infof("Get cpu: %v, memory: %v Bytes.", newNodeStats.cpuUsage, newNodeStats.memoryUsage)

		// Get Network IO stats, add it to nodeStats
		netStats := stats.NodeNetStats
		for _, iface := range netStats.Interfaces {
			net := statType{}
			if iface.RxBytes != nil && iface.TxBytes != nil {
				net.rx = *iface.RxBytes
				net.tx = *iface.TxBytes
			}
			if iface.Name == c.networkInterface {
				newNodeStats.netIOStats.time = netStats.Time.Time
				newNodeStats.netIOStats.rx = net.rx
				newNodeStats.netIOStats.tx = net.tx
				newNodeStats.netIOStats.name = iface.Name
			}
		}

		// Get all pods stats, add them to nodeStats. podStats := stats.PodStats
		podStats := stats.PodStats
		for _, pod := range podStats {
			diskStats := statType{}
			// Sum all containers' stats together
			// Maybe some pod doesn't has DiskIoStats, set them to ZERO
			for _, container := range pod.Containers {
				if container.Diskio != nil {
					if container.Diskio.DiskIoStats != nil {
						ioServiced := container.Diskio.DiskIoStats.IoServiced
						if c.diskDevName != "" {
							// care about the specified name
							diskStats.name = c.diskDevName
							diskStats.time = pod.Diskio.Time.Time
							for _, io := range ioServiced {
								if io.Device == c.diskDevName {
									diskStats.rx += io.Stats["Read"]
									diskStats.tx += io.Stats["Write"]
								}
							}
						} else if len(ioServiced) != 0 {
							// choose the first one
							diskStats.time = pod.Diskio.Time.Time
							diskStats.name = ioServiced[0].Device
							diskStats.rx += ioServiced[0].Stats["Read"]
							diskStats.tx += ioServiced[0].Stats["Write"]
						}
					}
				}
			}
			netIoStats := statType{}
			if pod.Network != nil {
				if pod.Network.RxBytes != nil && pod.Network.TxBytes != nil &&
					pod.Network.Name == c.networkInterface {
					netIoStats.rx = *pod.Network.RxBytes
					netIoStats.tx = *pod.Network.TxBytes
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

		// Get disk stats together, include system containers and user pods
		newNodeStats.diskIOStats.time = stats.NodeDiskIoStats.Time.Time
		// add system container disk-io stats to node stats
		sysContainers := stats.SysContainers
		for _, container := range sysContainers {
			if container.Diskio != nil {
				if container.Diskio.DiskIoStats != nil {
					ioServiced := container.Diskio.DiskIoStats.IoServiced
					if c.diskDevName != "" {
						// care about the specified name
						newNodeStats.diskIOStats.name = c.diskDevName
						for _, io := range ioServiced {
							if io.Device == c.diskDevName {
								newNodeStats.diskIOStats.rx += io.Stats["Read"]
								newNodeStats.diskIOStats.tx += io.Stats["Write"]
							}
						}
					} else if len(ioServiced) != 0 {
						if name := ioServiced[0].Device; name != "" {
							newNodeStats.diskIOStats.name = name
						}
						newNodeStats.diskIOStats.rx += ioServiced[0].Stats["Read"]
						newNodeStats.diskIOStats.tx += ioServiced[0].Stats["Write"]
					}
				}
			}
		}
		// add user pod disk-io stats to node stats
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
	newStats := c.nodeStats[statsBufferLen - 1]
	lastStats := c.nodeStats[statsBufferLen - 2]
	// CPU check
	if newStats.cpuUsage < float64(c.cpuTotal) * c.taintThreshold {
		c.nodeCondition.CPUAvailable = true
	} else {
		c.nodeCondition.CPUAvailable = false
	}
	// Memory check
	if float64(newStats.memoryUsage) < float64(c.memTotal) * c.taintThreshold {
		c.nodeCondition.MemoryAvailable = true
	} else {
		c.nodeCondition.MemoryAvailable = false
	}
	glog.Info("Get CPU: %v, Memory: %v", )
	// Compute Network IOPS. IOPS = (newIO - lastIO) / duration_time
	newNetworkStat := statType{
		time: newStats.netIOStats.time,
		rx:   newStats.netIOStats.rx,
		tx:   newStats.netIOStats.tx,
		name: newStats.netIOStats.name,
	}
	lastNetworkStat := statType{
		time: lastStats.netIOStats.time,
		rx:   lastStats.netIOStats.rx,
		tx:   lastStats.netIOStats.tx,
		name: lastStats.netIOStats.name,
	}
	networkIOPS := 1e9 * float64(newNetworkStat.rx + newNetworkStat.tx -
		lastNetworkStat.rx - lastNetworkStat.tx) /
		float64(newNetworkStat.time.UnixNano() - lastNetworkStat.time.UnixNano())
	if networkIOPS < 0 {
		glog.Errorf("get network iops error, a negative value, ignore it")
		networkIOPS = 0
	}
	glog.Infof("get network %s bps: %v Bytes/s\n",
		newNetworkStat.name, int(networkIOPS))

	// Compute Disk IOPS
	newDiskIoStat := statType{
		time: newStats.diskIOStats.time,
		rx:   newStats.diskIOStats.rx,
		tx:   newStats.diskIOStats.tx,
		name: newStats.diskIOStats.name,
	}
	lastDiskIoStat := statType{
		time: lastStats.diskIOStats.time,
		rx:   lastStats.diskIOStats.rx,
		tx:   lastStats.diskIOStats.tx,
		name: lastStats.diskIOStats.name,
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
func (c *conditionManager) getEvilPod(evictType string, pods []types.PodInfo) (bool, string) {
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
		if len(pods) != 0 {
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
		}
		// find no pod consume these resources
		if evilPod.Name == "" {
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
		if len(pods) != 0 {
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
		}
		if evilPod.Name == "" {
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
	} else if evictType == types.CPUBusy {
		evilValue := 0.0
		evilPod := types.PodInfo{}
		if len(pods) != 0 {
			for _, pod := range pods {
				keyName := pod.Namespace + "." + pod.Name
				cpuStats := c.nodeStats[statsBufferLen - 1].podStats[keyName].cpuUsage
				if cpuStats > evilValue {
					evilValue = cpuStats
					evilPod.Name = pod.Name
					evilPod.Namespace = pod.Namespace
				}
			}
			priority = types.NeedEvict
		}
		if evilPod.Name == "" {
			for _, pod := range c.nodeStats[statsBufferLen - 1].podStats {
				cpuStats := pod.cpuUsage
				if cpuStats > evilValue {
					evilValue = cpuStats
					evilPod.Name = pod.name
					evilPod.Namespace = pod.namespace
				}
			}
			priority = types.EvictCandidate
		}
	} else if evictType == types.MemBusy{
		evilValue := 0.0
		evilPod := types.PodInfo{}
		if len(pods) != 0 {
			for _, pod := range pods {
				keyName := pod.Namespace + "." + pod.Name
				memStats := c.nodeStats[statsBufferLen - 1].podStats[keyName].memoryUsage
				if float64(memStats) > evilValue {
					evilValue = float64(memStats)
					evilPod.Name = pod.Name
					evilPod.Namespace = pod.Namespace
				}
			}
			priority = types.NeedEvict
		}
		if evilPod.Name == "" {
			for _, pod := range c.nodeStats[statsBufferLen - 1].podStats {
				memStats := pod.memoryUsage
				if float64(memStats) > evilValue {
					evilValue = float64(memStats)
					evilPod.Name = pod.name
					evilPod.Namespace = pod.namespace
				}
			}
			priority = types.EvictCandidate
		}
	}

	return false, priority
}
