package evictionmanager

import (
	"k8s.io/apimachinery/pkg/util/clock"

	"eviction-agent/pkg/evictionclient"
	"eviction-agent/pkg/condition"
)

type EvictionManager interface {
	Run() error
}

type evictionManager struct {
	client           evictionclient.Client
	conditionManager condition.ConditionManager
}

// NewEvictionManager creates the eviction manager.
func NewEvictionManager(client evictionclient.Client) EvictionManager {
	return &evictionManager{
		client: client,
		conditionManager: condition.NewConditionManager(client, clock.RealClock{}),
	}
}

// Run starts the eviction manager
func (e *evictionManager) Run() error {
	e.conditionManager.Start()
	return nil
}