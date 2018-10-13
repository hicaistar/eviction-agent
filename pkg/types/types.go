package types

type PodInfo struct {
	Name      string
	Namespace string
}

type NodeTaintInfo struct {
	DiskIO    bool
	NetworkIO bool
}

type NodeIOPSTotal struct {
	DiskIOPSTotal    int32
	NetworkIOPSTotal int32
}

const (
	DiskIO = "DiskIOBusy"
	NodeDiskIOPSTotal = "DiskIOPSTotal"
	NodeNetworkIOPSTotal = "NetworkIOPSTotal"
	NetworkIO = "NetworkIOBusy"
	NeedEvict = "needEvict"
	EvictCandidate = "evictionCandidate"
	LowPriority = 0
	HighPriority = 1

)
