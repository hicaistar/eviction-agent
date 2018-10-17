package condition

import (
	"time"
	"os"
	"fmt"
	"io/ioutil"
	"encoding/json"

	"github.com/fsnotify/fsnotify"

	"eviction-agent/pkg/types"
	"eviction-agent/pkg/evictionclient"
	"eviction-agent/pkg/log"
)

const (
	// updatePeriod is the period
	updatePeriod = 10 * time.Second
	statsBufferLen = 3
	taintThreshold = 0.9
	defaultDiskIOTotal = 10000
	defaultNetwortIOTotal = 100000000
	unTaintGracePeriod = 5 * time.Minute // Minutes
)

type NodeCondition struct {
	DiskIOAvailable    bool
	NetworkRxAvailabel bool
	NetworkTxAvailabel bool
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
	// GetUnTaintGracePeriod get value from policy file
	GetUnTaintGracePeriod() time.Duration
}

type conditionManager struct {
	client               evictionclient.Client
	policyConfigFile     string
	taintThreshold       map[string]float64
	untaintGracePeriod   time.Duration   // minutes
	nodeCondition        NodeCondition
	podToEvict           types.PodInfo
	nodeStats            []nodeStatsType
	autoEvict            bool
	networkInterfaces    []string
	diskDevName          string
	diskIoTotal          int32
	networkIoTotal       int32
	invalidEvictCount    int32
	cpuTotal             int64
	memTotal             int64
	lowPriorityThreshold int
}

type policyConfig struct {
	UntaintGracePeriod   int32               `json:"untaintGracePeriod"`
	TaintThreshold       map[string]float64  `json:"taintThreshold"`
	AutoEvictFlag        bool                `json:"autoEvictFlag"`
	//Resource total
	NetworkInterfaces    []string            `json:"networkInterfaces"`
	NetworkBPSTotal      int32               `json:"networkBPSTotal"`
	DiskDevName          string              `json:"diskDevName"`
	DiskIOPSTotal        int32               `json:"diskIOPSTotal"`
	LowPriorityThreshold int                 `json:"lowPriorityThreshold"`
}

// NewConditionManager creates a condition manager
func NewConditionManager(client evictionclient.Client, configFile string) ConditionManager {
	return &conditionManager{
		client:     client,
		policyConfigFile: configFile,
		nodeCondition: NodeCondition{
			CPUAvailable: true,
			MemoryAvailable: true,
			DiskIOAvailable:    true,
			NetworkRxAvailabel: true,
			NetworkTxAvailabel: true,
		},
		taintThreshold: make(map[string]float64),
		autoEvict: false,
		diskIoTotal: defaultDiskIOTotal,
		networkIoTotal: defaultNetwortIOTotal,
		untaintGracePeriod: unTaintGracePeriod,
	}
}

func (c *conditionManager) Start() error {
	log.Infof("Start condition manager\n")

	// get node iops total value
	nodeIOPSTotal, err := c.client.GetResourcesTotalFromAnnotations()
	if err != nil {
		log.Errorf("Get resource from node error: %v", err)
		return err
	}
	c.networkIoTotal = nodeIOPSTotal.NetworkBPSTotal
	c.diskIoTotal = nodeIOPSTotal.DiskIOPSTotal
	c.cpuTotal = nodeIOPSTotal.CPUTotal
	c.memTotal = nodeIOPSTotal.MemoryTotal
	c.taintThreshold["CPU"] = 1
	c.taintThreshold["DiskIo"] = 1
	c.taintThreshold["NetworkIo"] = 1
	c.taintThreshold["Memory"] = 1
	log.Infof("Get total value, networkBPS: %v, diskIOPS: %v, cpu: %v, memory: %v",
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
	log.Infof("Start policy file watcher\n")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Errorf("create a new file wather error %v\n", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(c.policyConfigFile); err != nil {
		log.Errorf("add policy config file watcher error %v\n", err)
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
		log.Errorf("open policy config file error: %v", err)
		return err
	}
	defer configFile.Close()

	byteValue, _ := ioutil.ReadAll(configFile)

	var config policyConfig
	err = json.Unmarshal(byteValue, &config)
	if err != nil {
		log.Errorf("json unmarshal failed for file: %v, error: %v", c.policyConfigFile, err)
		return err
	}

	// TODO: add other configure here
	if config.UntaintGracePeriod != 0 {
		c.untaintGracePeriod = time.Minute * time.Duration(config.UntaintGracePeriod)
	}

	if config.DiskDevName != "" {
		c.diskDevName = config.DiskDevName
	}

	if config.DiskIOPSTotal != 0 {
		c.diskIoTotal = config.DiskIOPSTotal
	}
	if config.TaintThreshold != nil {
		if v, ok := config.TaintThreshold["CPU"]; ok && v > 0 {
			c.taintThreshold["CPU"] = v
		}
		if v, ok := config.TaintThreshold["DiskIo"]; ok && v > 0 {
			c.taintThreshold["DiskIo"] = v
		}
		if v, ok := config.TaintThreshold["NetworkIo"]; ok && v > 0 {
			c.taintThreshold["NetworkIo"] = v
		}
		if v, ok := config.TaintThreshold["Memory"]; ok && v > 0 {
			c.taintThreshold["Memory"] = v
		}
	}
	if config.NetworkBPSTotal != 0 {
		c.networkIoTotal = config.NetworkBPSTotal
	}
	if config.NetworkInterfaces != nil {
		c.networkInterfaces = config.NetworkInterfaces
	}
	if config.LowPriorityThreshold != 0 {
		c.lowPriorityThreshold = config.LowPriorityThreshold
	}
	c.autoEvict = config.AutoEvictFlag
	log.Infof("Get configuration --diskIoTotal=%v, --taintThreshold=%v, --network interfaces=%v, " +
		"--networkIOTotal=%v, --autoEvictFlag=%v, --diskDevName=%v, --untaintGracePeriod=%v, " +
		"--lowPriorityThreshold=%v",
		c.diskIoTotal, c.taintThreshold, c.networkInterfaces,
		c.networkIoTotal, c.autoEvict, c.diskDevName, c.untaintGracePeriod,
		c.lowPriorityThreshold)

	return nil
}

// GetUnTaintGracePeriod return un-Taint grace period to taint process
func (c conditionManager) GetUnTaintGracePeriod() time.Duration {
	return c.untaintGracePeriod
}

// syncStats
func (c *conditionManager) syncStats() {
	log.Infof("Start sync stats\n")
	for {
		// Get summary stats
		stats, err := c.client.GetSummaryStats()
		if err != nil {
			log.Errorf("sync stats get summary stats error: %v", err)
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
		log.Debugf("Get cpu: %v, memory: %v Bytes.", newNodeStats.cpuUsage, newNodeStats.memoryUsage)

		// Get Network IO stats, add it to nodeStats
		netStats := stats.NodeNetStats
		newNodeStats.netIOStats.time = netStats.Time.Time
		for _, netName := range c.networkInterfaces {
			newNodeStats.netIOStats.name += netName + "."
		}
		for _, iface := range netStats.Interfaces {
			net := statType{}
			if iface.RxBytes != nil && iface.TxBytes != nil {
				net.rx = *iface.RxBytes
				net.tx = *iface.TxBytes
			}
			// traverse all net interfaces
			for _, netName := range c.networkInterfaces {
				if iface.Name == netName {
					newNodeStats.netIOStats.rx += net.rx
					newNodeStats.netIOStats.tx += net.tx
				}
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
									if v, ok := io.Stats["Read"]; ok {
										diskStats.rx += v
									}
									if v, ok := io.Stats["Write"]; ok {
										diskStats.tx += v
									}
								}
							}
						} else if len(ioServiced) != 0 {
							// choose the first one
							diskStats.time = pod.Diskio.Time.Time
							diskStats.name = ioServiced[0].Device
							if v, ok := ioServiced[0].Stats["Read"]; ok {
								diskStats.rx += v
							}
							if v, ok := ioServiced[0].Stats["Write"]; ok {
								diskStats.tx += v
							}
						}
					}
				}
			}
			netIoStats := statType{}
			if pod.Network != nil {
				if pod.Network.RxBytes != nil && pod.Network.TxBytes != nil {
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
			if pod.CPU != nil {
				if pod.CPU.UsageNanoCores != nil {
					podStat.cpuUsage = float64(*pod.CPU.UsageNanoCores) / 1e9
				}
			}
			if pod.Memory != nil {
				if pod.Memory.UsageBytes != nil {
					podStat.memoryUsage = *pod.Memory.UsageBytes
				}
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
								if v, ok := io.Stats["Read"]; ok {
									newNodeStats.diskIOStats.rx += v
								}
								if v, ok := io.Stats["Write"]; ok {
									newNodeStats.diskIOStats.tx += v
								}
							}
						}
					} else if len(ioServiced) != 0 {
						if name := ioServiced[0].Device; name != "" {
							newNodeStats.diskIOStats.name = name
						}
						if v, ok := ioServiced[0].Stats["Read"]; ok {
							newNodeStats.diskIOStats.rx += v
						}
						if v, ok := ioServiced[0].Stats["Write"]; ok {
							newNodeStats.diskIOStats.tx += v
						}
					}
				}
			}
		}
		// add user pod disk-io stats to node stats
		for _, pod := range newNodeStats.podStats {
			newNodeStats.diskIOStats.rx += pod.diskIOStats.rx
			newNodeStats.diskIOStats.tx += pod.diskIOStats.tx
		}

		log.Debugf("get summary stats from node: %v", stats.NodeName)
		log.Debugf("netName: %v, netRX: %v, netTX: %v at %v, diskName: %v, diskRead: %v, diskWrite: %v at %v",
			netStats.Name, *netStats.RxPackets, *netStats.TxPackets, netStats.Time.Time,
			newNodeStats.diskIOStats.name, newNodeStats.diskIOStats.rx,
			newNodeStats.diskIOStats.tx, newNodeStats.diskIOStats.time)
		log.Debugf("diskname: %v, Read: %v, Write: %v",
			newNodeStats.diskIOStats.name, newNodeStats.diskIOStats.rx, newNodeStats.diskIOStats.tx)

		// add new node stats to list
		if len(c.nodeStats) == statsBufferLen {
			// If get the same time, ignore it.
			if newNodeStats.time != c.nodeStats[statsBufferLen - 1].time {
				c.nodeStats = append(c.nodeStats[1:], newNodeStats)
			} else {
				log.Debugf("Abandon this stats at: %v", newNodeStats.time)
			}
		} else {
			c.nodeStats = append(c.nodeStats, newNodeStats)
		}
		time.Sleep(updatePeriod)
	}
	log.Errorf("Sync stats stop")
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
	if newStats.cpuUsage < float64(c.cpuTotal) * c.taintThreshold["CPU"] {
		c.nodeCondition.CPUAvailable = true
	} else {
		c.nodeCondition.CPUAvailable = false
	}
	// Memory check
	if float64(newStats.memoryUsage) < float64(c.memTotal) * c.taintThreshold["Memory"] {
		c.nodeCondition.MemoryAvailable = true
	} else {
		c.nodeCondition.MemoryAvailable = false
	}
	log.Debugf("Get CPU: %v, Memory: %v", newStats.cpuUsage, newStats.memoryUsage)
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
	networkRxBps := 1e9 * float64(newNetworkStat.rx - lastNetworkStat.rx) /
		float64(newNetworkStat.time.UnixNano() - lastNetworkStat.time.UnixNano())
	networkTxBps := 1e9 * float64(newNetworkStat.tx - lastNetworkStat.tx) /
		float64(newNetworkStat.time.UnixNano() - lastNetworkStat.time.UnixNano())
	if networkRxBps < 0 {
		log.Errorf("get network iops error, a negative value, ignore it")
		networkRxBps = 0
	}
	if networkTxBps < 0 {
		log.Errorf("get network iops error, a negative value, ignore it")
		networkTxBps = 0
	}
	log.Debugf("get network %s Rx bps: %v Bytes/s, Tx bps: %v Bytes/s ",
		newNetworkStat.name, int(networkRxBps), int(networkTxBps))

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
		log.Errorf("get disk iops error, a negative value, ignore it")
		diskIOPS = 0
	}
	log.Debugf("get disk %s, iops: %v", newDiskIoStat.name, int(diskIOPS))

	if diskIOPS > float64(c.diskIoTotal) * c.taintThreshold["DiskIo"] {
			log.Infof("disk %s out of limits, iops: %v", newDiskIoStat.name, int(diskIOPS))
			c.nodeCondition.DiskIOAvailable = false
	} else {
		c.nodeCondition.DiskIOAvailable = true
	}

	// sum all network interfaces together
	if networkRxBps > float64(len(c.networkInterfaces)) * float64(c.networkIoTotal) * c.taintThreshold["NetworkIo"]  {
		log.Infof("network %s out of limis, Rx bps: %v", newNetworkStat.name, int(networkRxBps))
		c.nodeCondition.NetworkRxAvailabel = false
	} else {
		c.nodeCondition.NetworkRxAvailabel = true
	}
	if networkTxBps > float64(len(c.networkInterfaces)) * float64(c.networkIoTotal) * c.taintThreshold["NetworkIo"]  {
		log.Infof("network %s out of limis, Tx bps: %v", newNetworkStat.name, int(networkTxBps))
		c.nodeCondition.NetworkTxAvailabel = false
	} else {
		c.nodeCondition.NetworkTxAvailabel = true
	}


	return &c.nodeCondition
}

// ChooseOnePodToEvict
func (c *conditionManager) ChooseOnePodToEvict(evictType string) (*types.PodInfo, bool, string, error) {
	isEvict := false
	if len(c.nodeStats) != statsBufferLen {
		log.Infof("wait for a minute")
		return nil, isEvict, "", fmt.Errorf("wait for a minute")
	}

	// Get lower priority pod, if autoEvict
	pods, err := c.client.GetLowerPriorityPods(c.lowPriorityThreshold)
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
	if len(pods) != 0 && c.autoEvict {
		for _, pod := range pods {
			if pod.Name == c.podToEvict.Name && pod.Namespace == c.podToEvict.Namespace {
				return true, priority
			}
		}
	}
	// compute and get the evil pod
	if evictType == types.DiskIO {
		evilValue := 0.0
		weight := 0.0
		evilPod := types.PodInfo{}
		if len(pods) != 0 {
			for _, pod := range pods {
				keyName := pod.Namespace + "." + pod.Name
				newPodDiskStats := c.nodeStats[statsBufferLen - 1].podStats[keyName].diskIOStats
				lastPodDiskStats := c.nodeStats[statsBufferLen - 2].podStats[keyName].diskIOStats
				iops := 1e9 * float64(newPodDiskStats.rx + newPodDiskStats.tx -
					lastPodDiskStats.rx - lastPodDiskStats.tx) /
					float64(newPodDiskStats.time.UnixNano() - lastPodDiskStats.time.UnixNano())
				// choose the bigger weight
				if pod.Priority == 0 {
					weight = iops
				} else {
					weight = iops / float64(pod.Priority)
				}

				if weight > evilValue {
					evilValue = weight
					evilPod.Name = pod.Name
					evilPod.Namespace = pod.Namespace
					evilPod.Priority = pod.Priority
				}
			}
			priority = types.NeedEvict
			log.Infof("get evil pod: %v, iops: %v, priority: %v, diskio busy", evilPod.Name, evilValue, evilPod.Priority)
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
			log.Infof("get evil pod: %v, iops: %v from other pods, diskio busy", evilPod.Name, evilValue)
		}
		c.podToEvict = evilPod
	} else if evictType == types.NetworkRxBusy || evictType == types.NetworkTxBusy {
		evilValue := 0.0
		weight := 0.0
		evilPod := types.PodInfo{}
		if len(pods) != 0 {
			for _, pod := range pods {
				bps := 0.0
				keyName := pod.Namespace + "." + pod.Name
				newPodNetStats := c.nodeStats[statsBufferLen - 1].podStats[keyName].netIOStats
				lastPodNetStats := c.nodeStats[statsBufferLen - 2].podStats[keyName].netIOStats
				if evictType == types.NetworkTxBusy {
					bps = 1e9 * float64(newPodNetStats.tx - lastPodNetStats.tx) /
						float64(newPodNetStats.time.UnixNano() - lastPodNetStats.time.UnixNano())
				} else {
					bps = 1e9 * float64(newPodNetStats.rx - lastPodNetStats.rx) /
						float64(newPodNetStats.time.UnixNano() - lastPodNetStats.time.UnixNano())
				}

				// choose the bigger weight
				if pod.Priority == 0 {
					weight = bps
				} else {
					weight = bps / float64(pod.Priority)
				}

				if weight > evilValue {
					evilValue = weight
					evilPod.Name = pod.Name
					evilPod.Namespace = pod.Namespace
					evilPod.Priority = pod.Priority
				}
			}
			priority = types.NeedEvict
			log.Infof("get evil pod: %v, bps: %v, priority: %v, network busy", evilPod.Name, evilValue, evilPod.Priority)
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
			log.Infof("get evil pod: %v, iops: %v from other pods, network busy", evilPod.Name, evilValue)
		}
		c.podToEvict = evilPod
	} else if evictType == types.CPUBusy {
		evilValue := 0.0
		weight := 0.0
		evilPod := types.PodInfo{}
		if len(pods) != 0 {
			for _, pod := range pods {
				keyName := pod.Namespace + "." + pod.Name
				cpuStats := c.nodeStats[statsBufferLen - 1].podStats[keyName].cpuUsage
				// choose the bigger weight
				if pod.Priority == 0 {
					weight = cpuStats
				} else {
					weight = cpuStats / float64(pod.Priority)
				}
				if weight > evilValue {
					evilValue = weight
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
		log.Infof("get evil pod: %v, cpu: %v", evilPod.Name, evilValue)
		c.podToEvict = evilPod
	} else if evictType == types.MemBusy{
		evilValue := 0.0
		weight := 0.0
		evilPod := types.PodInfo{}
		if len(pods) != 0 {
			for _, pod := range pods {
				keyName := pod.Namespace + "." + pod.Name
				memStats := c.nodeStats[statsBufferLen - 1].podStats[keyName].memoryUsage
				// choose the bigger weight
				if pod.Priority == 0 {
					weight = float64(memStats)
				} else {
					weight = float64(memStats) / float64(pod.Priority)
				}
				if weight > evilValue {
					evilValue = weight
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
		log.Infof("get evil pod: %v, memory: %v", evilPod.Name, evilValue)
		c.podToEvict = evilPod
	}
	return false, priority
}
