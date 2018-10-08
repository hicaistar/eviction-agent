package evictionmanager

import (
	"eviction-agent/pkg/evictionclient"
	"eviction-agent/pkg/condition"

	"k8s.io/apimachinery/pkg/util/clock"
	"time"
	"github.com/golang/glog"
)

type EvictionManager interface {
	Run() error
}

type evictionManager struct {
	client           evictionclient.Client
	conditionManager condition.ConditionManager
}

// NewEvictionManager creates the eviction manager.
func NewEvictionManager(client evictionclient.Client, configFile string) EvictionManager {
	return &evictionManager{
		client: client,
		conditionManager: condition.NewConditionManager(client, clock.RealClock{}, configFile),
	}
}

// Run starts the eviction manager
func (e *evictionManager) Run() error {
	err := e.conditionManager.Start()
	if err != nil {
		return err
	}
	// TODO: add taint checking
	// evict pod
	for {
		time.Sleep(30 * time.Second)
		glog.Infof("now chose one pod to evict\n")
		podName, podNamespace, err := e.conditionManager.ChoseOnePodToEvict()
		if err != nil {
			glog.Errorf("error: %v", err)
			continue
		}
		err = e.evictOnePod(podName, podNamespace)
		if err != nil {
			glog.Errorf("evict pod %v error: %v", podName, err)
		}
	}

	return nil
}

// evictOnePod
func (e *evictionManager) evictOnePod(podName string, namespace string) error {
	err := e.client.EvictOnePodByName(podName, namespace)
	return err
}