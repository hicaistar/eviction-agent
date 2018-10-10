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
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	policyv1 "k8s.io/api/policy/v1beta1"

	"eviction-agent/cmd/options"
	"eviction-agent/pkg/summary"
	"eviction-agent/pkg/types"
)

// Client is the interface of eviction client
type Client interface {
	// GetTaintConditions get all specific taint conditions of current node
	GetTaintConditions() (bool, bool, error)
	// SetTaintConditions set or update taint conditions of current node
	SetTaintConditions(string, string) (error)
	// GetSummaryStats get node/pod stats from summary API
	GetSummaryStats() (*summary.ConditionStats, error)
	// EvictOnePod evict one pod
	EvictOnePod(*types.PodInfo) (error)
}

type evictionClient struct {
	nodeName string
	client   *kubernetes.Clientset
	clock    clock.Clock
	nodeInfo summary.NodeInfo
	summaryApi summary.SummaryStatsApi
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

	ipAddr, err := c.getNodeAddress()
	if err != nil {
		glog.Errorf("%v", err)
		panic(err)
	}

	transport, err := rest.TransportFor(config)
	if err != nil {
		glog.Errorf("get transport error: %v", err)
		panic(err)
	}

	c.nodeInfo = summary.NodeInfo{
		Name: c.nodeName,
		Port: 10255,  // get port from node?
		ConnectAddress: ipAddr,
	}

	// NewSummaryStatsApi
	c.summaryApi, err = summary.NewSummaryStatsApi(transport, c.nodeInfo)

	return c
}

func (c *evictionClient) getNodeAddress() (string, error) {
	node, err := c.client.CoreV1().Nodes().Get(c.nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("get node taint condition error %v", err)
		return "", err
	}
	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			glog.Infof("Get node ip address: %v", addr.Address)
			return addr.Address, nil
		}
	}
	return "", fmt.Errorf("node had no addresses that matched\n")
}

func (c *evictionClient) GetTaintConditions() (bool, bool, error) {
	node, err := c.client.CoreV1().Nodes().Get(c.nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("get node taint condition error %v", err)
		return false, false, err
	}
	taints := node.Spec.Taints
	nodeTaintInfo := types.NodeTaintInfo{
		DiskIO:    false,
		NetworkIO: false,
	}
	for _, t := range taints {
		if t.Key == types.NetworkIO {
			nodeTaintInfo.NetworkIO = true
		}
		if t.Key == types.DiskIO {
			nodeTaintInfo.DiskIO = true
		}
	}
	return nodeTaintInfo.NetworkIO, nodeTaintInfo.DiskIO, nil
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
			Value: "True",
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

	_, err = c.client.CoreV1().Nodes().Patch(c.nodeName, k8stypes.StrategicMergePatchType, patchBytes)
	return err
}

func (c *evictionClient) GetSummaryStats() (*summary.ConditionStats, error){
	stats, err := c.summaryApi.GetSummaryStats()
	return stats, err
}

// EvictOnePodByName
func (c *evictionClient) EvictOnePod(podToEvict *types.PodInfo) error {
	eviction := policyv1.Eviction{
		TypeMeta: metav1.TypeMeta{
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: podToEvict.Name,
			Namespace: podToEvict.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{},
	}
	err := c.client.CoreV1().Pods(eviction.Namespace).Evict(&eviction)
	return err
}
