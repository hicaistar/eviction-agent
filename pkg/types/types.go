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
	DiskIO = "DiskIOBusy"
	NetworkIO = "NetworkIOBusy"
)
