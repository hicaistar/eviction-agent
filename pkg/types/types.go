package types

type PodInfo struct {
	Name      string
	Namespace string
	Priority  int
}

type NodeTaintInfo struct {
	DiskIO    bool
	NetworkIO bool
	CPU       bool
	Memory    bool
}

type NodeIOPSTotal struct {
	DiskIOPSTotal    int32
	NetworkBPSTotal  int32
	CPUTotal         int64
	MemoryTotal      int64
}

const (
	DiskIO = "DiskIOBusy"
	CPUBusy = "CPUBusy"
	MemBusy = "MemBusy"
	NodeDiskIOPSTotal = "sncloud.com/diskIOPSCapacity"
	NodeNetworkBPSTotal = "sncloud.com/networkBandwidthCapacity"
	NetworkIO = "NetworkIOBusy"
	NetworkTxBusy = "NetworkTxBusy"
	NetworkRxBusy = "NetworkRxBusy"
	NeedEvict = "NeedsEviction"
	EvictCandidate = "EvictionCandidate"
	LowestPriority = 0
)
