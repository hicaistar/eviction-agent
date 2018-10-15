package evictionclient

import (
	"encoding/json"
	"fmt"
	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"eviction-agent/cmd/options"
	"eviction-agent/pkg/summary"
	"eviction-agent/pkg/types"
	"strconv"
)

// Client is the interface of eviction client
type Client interface {
	// GetTaintConditions get all specific taint conditions of current node
	GetTaintConditions() (types.NodeTaintInfo, error)
	// SetTaintConditions set or update taint conditions of current node
	SetTaintConditions(string, string) error
	// GetSummaryStats get node/pod stats from summary API
	GetSummaryStats() (*summary.ConditionStats, error)
	// EvictOnePod evict one pod
	EvictOnePod(*types.PodInfo) error
	// GetLowerPriorityPods
	GetLowerPriorityPods(int) ([]types.PodInfo, error)
	// AddEvictAnnotationToPod
	AnnotatePod(podInfo *types.PodInfo, priority string, action string) error
	// GetIOPSTotalFromAnnotations
	GetResourcesTotalFromAnnotations() (*types.NodeIOPSTotal, error)
	//ClearAllEvictAnnotations
	ClearAllEvictAnnotations() error
}

type evictionClient struct {
	nodeName   string
	client     *kubernetes.Clientset
	nodeInfo   summary.NodeInfo
	summaryApi summary.SummaryStatsApi
}

// NewClientOrDie creates a new eviction client, panics if error occurs.
func NewClientOrDie(eao *options.EvictionAgentOptions) Client {
	c := &evictionClient{}
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
		Name:           c.nodeName,
		Port:           10255, // get port from node?
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

func (c evictionClient) GetResourcesTotalFromAnnotations() (*types.NodeIOPSTotal, error) {
	node, err := c.client.CoreV1().Nodes().Get(c.nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("get node taint condition error %v", err)
		return nil, err
	}
	nodeIOPSTotal := types.NodeIOPSTotal{}
	// Get disk IOPS and network IOPS from annotations
	annotations := node.Annotations
	for key, value := range annotations {
		if key == types.NodeDiskIOPSTotal {
			v, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				return nil, err
			}
			nodeIOPSTotal.DiskIOPSTotal = int32(v)
		}
		if key == types.NodeNetworkBPSTotal {
			v, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				return nil, err
			}
			nodeIOPSTotal.NetworkBPSTotal = int32(v)
		}
	}
	// Get CPU and Memory form node status
	capacity := node.Status.Capacity
	cpu, ok := capacity.Cpu().AsInt64()
	if ok {
		nodeIOPSTotal.CPUTotal = cpu
	} else {
		return nil, fmt.Errorf("Get cpu from node status error")
	}
	memory, ok := capacity.Memory().AsInt64()
	if ok {
		nodeIOPSTotal.MemoryTotal = memory
	} else {
		return nil, fmt.Errorf("Get memory from node status error")
	}

	return &nodeIOPSTotal, nil
}

func (c *evictionClient) GetTaintConditions() (types.NodeTaintInfo, error) {
	nodeTaintInfo := types.NodeTaintInfo{
		DiskIO:    false,
		NetworkIO: false,
		CPU:       false,
		Memory:    false,
	}

	node, err := c.client.CoreV1().Nodes().Get(c.nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("get node taint condition error %v", err)
		return nodeTaintInfo, err
	}
	taints := node.Spec.Taints

	for _, t := range taints {
		if t.Key == types.NetworkIO {
			nodeTaintInfo.NetworkIO = true
		}
		if t.Key == types.DiskIO {
			nodeTaintInfo.DiskIO = true
		}
		if t.Key == types.CPUBusy {
			nodeTaintInfo.CPU = true
		}
		if t.Key == types.MemBusy {
			nodeTaintInfo.Memory = true
		}
	}
	return nodeTaintInfo, nil
}

func (c *evictionClient) SetTaintConditions(taintKey string, action string) error {
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
			Key:    taintKey,
			Value:  "True",
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
		return fmt.Errorf("failed to create patch for node %v", c.nodeName)
	}

	_, err = c.client.CoreV1().Nodes().Patch(c.nodeName, k8stypes.StrategicMergePatchType, patchBytes)
	return err
}

func (c *evictionClient) GetSummaryStats() (*summary.ConditionStats, error) {
	stats, err := c.summaryApi.GetSummaryStats()
	return stats, err
}

// EvictOnePodByName call evict-api to evict one pod
func (c *evictionClient) EvictOnePod(podToEvict *types.PodInfo) error {
	if podToEvict.Name == "" {
		return fmt.Errorf("pod name should not be empty")
	}
	eviction := policyv1.Eviction{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      podToEvict.Name,
			Namespace: podToEvict.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{},
	}
	err := c.client.CoreV1().Pods(eviction.Namespace).Evict(&eviction)
	return err
}

// GetLowerPriorityPods return pods which are set low priority
func (c *evictionClient) GetLowerPriorityPods(lowPriorityThreshold int) ([]types.PodInfo, error) {
	var pods []types.PodInfo
	options := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", c.nodeName),
	}
	podLists, err := c.client.CoreV1().Pods(metav1.NamespaceAll).List(options)
	if err != nil {
		glog.Errorf("List pods on %s error\n", c.nodeName)
		return nil, err
	}
	for _, pod := range podLists.Items {
		// default is High priority
		priority := types.LowestPriority
		if pod.Spec.Priority != nil {
			priority = int(*pod.Spec.Priority)
		} else {
			// TODO: should we take them as lowest?
			continue
		}

		if priority <= lowPriorityThreshold && priority > 0 {
			newPod := types.PodInfo{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				Priority:  priority,
			}
			pods = append(pods, newPod)
			glog.Infof("Get low priority pod: %v priority: %v", pod.Name, priority)
		}
	}
	return pods, nil
}

// AddEvictAnnotationToPod add an evict annotation on pod
func (c *evictionClient) AnnotatePod(podInfo *types.PodInfo, priority string, action string) error {
	if podInfo.Name == "" {
		return fmt.Errorf("pod name should not be empty")
	}
	oldPod, err := c.client.CoreV1().Pods(podInfo.Namespace).Get(podInfo.Name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("get pod %s error", podInfo.Name)
		return err
	}
	oldData, err := json.Marshal(oldPod)
	if err != nil {
		return fmt.Errorf("failed to marshal old node for node %v : %v", c.nodeName, err)
	}
	newPod := oldPod.DeepCopy()

	if newPod.Annotations == nil {
		glog.Infof("there is no annotation on this pod: %v, create it", podInfo.Name)
		newPod.Annotations = make(map[string]string)
	}
	if action == "Add" {
		newPod.Annotations[priority] = "true"
	} else if action == "Delete" {
		delete(newPod.Annotations, priority)
	}

	newData, err := json.Marshal(newPod)
	if err != nil {
		return fmt.Errorf("failed to marshal new pod %v : %v", podInfo.Name, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1.Pod{})
	if err != nil {
		return fmt.Errorf("failed to create patch for pod %v", podInfo.Name)
	}
	_, err = c.client.CoreV1().Pods(oldPod.Namespace).Patch(oldPod.Name, k8stypes.StrategicMergePatchType, patchBytes)

	glog.Infof("Annotate pod: %v, action:%v", podInfo.Name, action)
	return err
}

// ClearAllEvictAnnotations clear evict annotations from pod if node is not in bad condition
func (c *evictionClient) ClearAllEvictAnnotations() error {
	options := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", c.nodeName),
	}
	podLists, err := c.client.CoreV1().Pods(metav1.NamespaceAll).List(options)
	if err != nil {
		glog.Errorf("List pods on %s error\n", c.nodeName)
		return err
	}

	for _, pod := range podLists.Items {
		for k := range pod.Annotations {
			podInfo := types.PodInfo{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			}
			if k == types.EvictCandidate {
				c.AnnotatePod(&podInfo, k, "Delete")
			}
			if k == types.NeedEvict {
				c.AnnotatePod(&podInfo, k, "Delete")
			}
		}
	}

	return nil
}
