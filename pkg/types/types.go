package types

type PodInfo struct {
	Name      string
	Namespace string
}

type NodeTaintInfo struct {
	DiskIO    bool
	NetworkIO bool
}

const (
	DiskIO = "DiskIoBusy"
	NetworkIO = "NetworkIoBusy"
	LowPriority = 0
	HighPriority = 1
)
