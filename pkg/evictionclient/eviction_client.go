package evictionclient

import (
	"fmt"
	"encoding/json"
	"github.com/golang/glog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"eviction-agent/cmd/options"
)

// Client is the interface of eviction client
type Client interface {
	// GetTaintConditions get all specific taint conditions of current node
	GetTaintConditions(string) (bool, error)
	// SetTaintConditions set or update taint conditions of current node
	SetTaintConditions(string, string) (error)
	// GetSummaryStats get node/pod stats from summary API
	GetSummaryStats()
}

type evictionClient struct {
	nodeName string
	client   *kubernetes.Clientset
	clock    clock.Clock
}

// NewClientOrDie creates a new eviction client, panics if error occurs.
func NewClientOrDie(eao *options.EvictionAgentOptions) Client {
	c := &evictionClient{clock: clock.RealClock{}}
	var config *rest.Config
	var err error

	kubeconfigFile := eao.KubeconfigFile
	if kubeconfigFile != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigFile)
		if err != nil {
			panic(err)
		}
		glog.Infof("Create client using kubeconfig file %s", kubeconfigFile)
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			panic(err)
		}
		glog.Infof("Create client using in-cluster config")
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	c.client = clientSet
	c.nodeName = eao.NodeName

	return c
}

func (c *evictionClient) GetTaintConditions(taintKey string) (bool, error) {
	node, err := c.client.CoreV1().Nodes().Get(c.nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("get node taint condition error %v", err)
		return false, err
	}
	taints := node.Spec.Taints
	for _, t := range taints {
		if taintKey == t.Key {
			return true, nil
		}
	}
	return false, nil
}

func (c* evictionClient) SetTaintConditions(taintKey string, action string) error {
	oldNode, err := c.client.CoreV1().Nodes().Get(c.nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("get node taint condition error %v", err)
		return err
	}

	oldData, err := json.Marshal(oldNode)
	if err != nil {
		return fmt.Errorf("failed to marshal old node for node %v : %v", c.nodeName, err)
	}

	newTaints := oldNode.Spec.Taints
	newNodeClone := oldNode.DeepCopy()

	if action == "Taint" {
		currentTaint := v1.Taint{
			Key: taintKey,
			Value: "Unavailable",
			Effect: "NoSchedule",
		}
		newTaints = append(newTaints, currentTaint)
	} else if action == "UnTaint" {
		for i, t := range newTaints {
			if taintKey == t.Key {
				newTaints = append(newTaints[:i], newTaints[i+1:]...)
				break
			}
		}
	}
	newNodeClone.Spec.Taints = newTaints
	newData, err := json.Marshal(newNodeClone)
	if err != nil {
		return fmt.Errorf("failed to marshal new node for node %v : %v", c.nodeName, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1.Node{})
	if err != nil {
		return fmt.Errorf("failed to create pathc for node %v", c.nodeName)
	}

	_, err = c.client.CoreV1().Nodes().Patch(c.nodeName, types.StrategicMergePatchType, patchBytes)
	return err
}

func (c *evictionClient) GetSummaryStats() {

}