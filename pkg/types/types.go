package types

type PodInfo struct {
	Name      string
	Namespace string
}

type NodeTaintInfo struct {
	DiskIO    bool
	NetworkIO bool
	CPU       bool
	Memory    bool
}

type NodeIOPSTotal struct {
	DiskIOPSTotal    int32
	NetworkIOPSTotal int32
	CPUTotal         int64
	MemoryTotal      int64
}

const (
	DiskIO = "DiskIOBusy"
	CPUBusy = "CPUBusy"
	MemBusy = "MemBusy"
	NodeDiskIOPSTotal = "DiskIOPSTotal"
	NodeNetworkIOPSTotal = "NetworkIOPSTotal"
	NetworkIO = "NetworkIOBusy"
	NeedEvict = "needEvict"
	EvictCandidate = "evictionCandidate"
	LowPriority = 0
	HighPriority = 1

)
