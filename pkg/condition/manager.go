package condition

import (
	"time"
	"eviction-agent/pkg/evictionclient"
	"k8s.io/apimachinery/pkg/util/clock"
	"github.com/golang/glog"
)

const (
	// updatePeriod is the period
	updatePeriod = 1 * time.Second
)

type ConditionManager interface {
	// Start starts the condition manager
	Start()
}

type conditionManager struct {
	client evictionclient.Client
	clock  clock.Clock
}

// NewConditionManager creates a condition manager
func NewConditionManager(client evictionclient.Client, clock clock.Clock) ConditionManager {
	return &conditionManager{
		client:     client,
		clock:      clock,
	}
}

func (c *conditionManager) Start() {
	glog.Infof("Start condition manager\n")
	err := c.client.SetTaintConditions("DiskIO", "Taint")
	if err != nil {
		glog.Errorf("taint node error %v", err)
	}
	for {
		time.Sleep(30 * time.Second)
		taint, err := c.client.GetTaintConditions("DiskIO")
		if err != nil {
			glog.Errorf("get taint error %v", err)
			continue
		}
		if taint {
			break
		}
	}
	time.Sleep(100 * time.Second)
	err = c.client.SetTaintConditions("DiskIO", "UnTaint")
	if err != nil {
		glog.Errorf("untaint node error %v", err)
	}
	for {
		time.Sleep(30 * time.Second)
		taint, err := c.client.GetTaintConditions("DiskIO")
		if err != nil {
			glog.Errorf("get taint error %v", err)
			continue
		}
		glog.Infof("get DiskIO taint %v", taint)
	}
}