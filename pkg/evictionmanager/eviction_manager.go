package evictionmanager

import (
	"time"

	"github.com/golang/glog"

	"eviction-agent/pkg/types"
	"eviction-agent/pkg/evictionclient"
	"eviction-agent/pkg/condition"
)

const (
	// updatePeriod is the period
	taintUpdatePeriod = 30 * time.Second
	taintGracePeriod = 5 * time.Minute
)

type EvictionManager interface {
	Run() error
}

type evictionManager struct {
	client              evictionclient.Client
	conditionManager    condition.ConditionManager
	evictChan           chan string
	nodeTaint           types.NodeTaintInfo
	lastTaintDiskIOTime time.Time
	lastTaintNetIOTime  time.Time
	lastTaintCPUTime    time.Time
	lastTaintMemTime    time.Time
}

// NewEvictionManager creates the eviction manager.
func NewEvictionManager(client evictionclient.Client, configFile string) EvictionManager {
	return &evictionManager{
		client:           client,
		conditionManager: condition.NewConditionManager(client, configFile),
		evictChan:        make(chan string, 1),
		nodeTaint:        types.NodeTaintInfo{
			DiskIO:    false,
			NetworkIO: false,
			CPU:       false,
			Memory:    false,
		},
	}
}

// Run starts the eviction manager
func (e *evictionManager) Run() error {
	// Start condition manager
	// get and update node condition and pod condition
	err := e.conditionManager.Start()
	if err != nil {
		return err
	}

	// Taint process
	go e.taintProcess()

	// Main run loop waiting on evicting request
	for {
		// wait for evict event
		select {
		case evictType := <-e.evictChan:
			glog.Infof("evict pod because %s is not available", evictType)
		    e.evictOnePod(evictType)
		}
	}
	return nil
}

// evictOnePod call client to evict pod
func (e *evictionManager) evictOnePod(evictType string) {
	podToEvict, isEvict, priority, err:= e.conditionManager.ChooseOnePodToEvict(evictType)
	if err != nil {
		glog.Errorf("evictOnePod choose one pod to evict error: %v", err)
		return
	}
	glog.Infof("Get pod: %v to evict.\n", podToEvict.Name)

	if isEvict {
		err = e.client.EvictOnePod(podToEvict)
	} else {
		err = e.client.AnnotatePod(podToEvict, priority, "Add")
	}
	glog.Errorf("Evict pod error: %v", err)
	return
}

func (e *evictionManager) taintProcess() {
	// taint process cycle
	var err error
	for {
		// wait for some second
		time.Sleep(taintUpdatePeriod)
		// get taint condition
		e.nodeTaint, err = e.client.GetTaintConditions()
		if err != nil {
			glog.Errorf("get taint condition error: %v\n", err)
			continue
		}

		// get node condition
		condition := e.conditionManager.GetNodeCondition()

		// node is in good condition currently
		if condition.NetworkIOAvailabel && condition.DiskIOAvailable &&
			condition.CPUAvailable && condition.MemoryAvailable &&
			!e.nodeTaint.DiskIO && !e.nodeTaint.NetworkIO && !e.nodeTaint.CPU && !e.nodeTaint.Memory {
			// node is in good condition, there is no need to taint or un-taint
			// there is no need to evict any pod either
			// only need to clear all annotations on pods
			e.client.ClearAllEvictAnnotations()
			continue
		}

		isEvicted := false
		// CPU condition process
		if condition.CPUAvailable {
			if e.nodeTaint.CPU {
				// node is tainted CPU busy
				// TODO: wait taintGraceTime
				duration := time.Now().Sub(e.lastTaintCPUTime)
				glog.Infof("last taint duration: %v\n", duration)
				if duration.Minutes() > taintGracePeriod.Minutes() {
					err = e.client.SetTaintConditions(types.CPUBusy, "UnTaint")
					glog.Infof("Untaint node %s", types.CPUBusy)
					if err != nil {
						glog.Errorf("untaint node %s error: %v\n", types.CPUBusy, err)
					}
					// TODO: clear annotations
				}
			}
		} else {
			// node is in CPU busy
			// update taint time
			e.lastTaintCPUTime = time.Now()
			if !e.nodeTaint.CPU {
				// taint node, evict pod
				glog.Infof("taint node %s ", types.CPUBusy)
				err = e.client.SetTaintConditions(types.CPUBusy, "Taint")
				if err != nil {
					glog.Errorf("add taint %s error: %v", types.CPUBusy, err)
				}
			}
			// evict one pod to reclaim resources
			if !isEvicted {
				isEvicted = true
				e.evictChan <- types.CPUBusy
			}
		}

		// Memory condition process
		if condition.MemoryAvailable {
			if e.nodeTaint.Memory {
				// node is tainted Memory busy
				// TODO: wait taintGraceTime
				duration := time.Now().Sub(e.lastTaintMemTime)
				glog.Infof("last taint duration: %v\n", duration)
				if duration.Minutes() > taintGracePeriod.Minutes() {
					err = e.client.SetTaintConditions(types.MemBusy, "UnTaint")
					glog.Infof("Untaint node %s", types.MemBusy)
					if err != nil {
						glog.Errorf("untaint node %s error: %v\n", types.MemBusy, err)
					}
					// TODO: clear annotations
				}
			}
		} else {
			// node is in Memory busy
			// update taint time
			e.lastTaintMemTime = time.Now()
			if !e.nodeTaint.Memory {
				// taint node, evict pod
				glog.Infof("taint node %s ", types.MemBusy)
				err = e.client.SetTaintConditions(types.MemBusy, "Taint")
				if err != nil {
					glog.Errorf("add taint %s error: %v", types.MemBusy, err)
				}
			}
			// evict one pod to reclaim resources
			if !isEvicted {
				isEvicted = true
				e.evictChan <- types.MemBusy
			}
		}

		// DiskIO condition process
		if condition.DiskIOAvailable {
			if e.nodeTaint.DiskIO {
				// node is tainted DiskIO busy
				// TODO: wait taintGraceTime
				duration := time.Now().Sub(e.lastTaintDiskIOTime)
				glog.Infof("last taint duration: %v\n", duration)
				if duration.Minutes() > taintGracePeriod.Minutes() {
					err = e.client.SetTaintConditions(types.DiskIO, "UnTaint")
					glog.Infof("Untaint node %s", types.DiskIO)
					if err != nil {
						glog.Errorf("untaint node %s error: %v\n", types.DiskIO, err)
					}
					// TODO: clear annotations
				}
			}
		} else {
			// node is in DiskIO busy
			// update taint time
			e.lastTaintDiskIOTime = time.Now()
			if !e.nodeTaint.DiskIO {
				// taint node, evict pod
				glog.Infof("taint node %s ", types.DiskIO)
				err = e.client.SetTaintConditions(types.DiskIO, "Taint")
				if err != nil {
					glog.Errorf("add taint %s error: %v", types.DiskIO, err)
				}
			}
			// evict one pod to reclaim resources
			if !isEvicted {
				isEvicted = true
				e.evictChan <- types.DiskIO
			}
		}

		// NetworkIO condition process
		if condition.NetworkIOAvailabel {
			if e.nodeTaint.NetworkIO {
				duration := time.Now().Sub(e.lastTaintNetIOTime)
				glog.Infof("last taint duration: %v\n", duration)
				if duration.Minutes() > taintGracePeriod.Minutes() {
					err = e.client.SetTaintConditions(types.NetworkIO, "UnTaint")
					if err != nil {
						glog.Errorf("untaint node %s error: %v\n", types.NetworkIO, err)
					}
					// TODO: clear annotations
					glog.Infof("untaint node %s\n", types.NetworkIO)
				}
			}
		} else {
			// node is in NetworkIO busy
			e.lastTaintNetIOTime = time.Now()
			if !e.nodeTaint.NetworkIO {
				glog.Infof("taint node %s unavailable", types.NetworkIO)
				// taint node, evict pod
				err = e.client.SetTaintConditions(types.NetworkIO, "Taint")
				if err != nil {
					glog.Errorf("add taint %s error: %v", types.NetworkIO, err)
				}
			}
			// evict one pod to reclaim resources
			if !isEvicted {
				isEvicted = true
				e.evictChan <- types.NetworkIO
			}
		}
	}
}
