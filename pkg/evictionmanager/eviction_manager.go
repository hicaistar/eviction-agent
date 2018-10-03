package evictionmanager

import (
	"eviction-agent/pkg/evictionclient"
	"eviction-agent/pkg/condition"

	"k8s.io/apimachinery/pkg/util/clock"
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
	return nil
}