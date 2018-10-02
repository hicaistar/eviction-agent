package evictionclient

import (
	"github.com/golang/glog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/apimachinery/pkg/util/clock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"eviction-agent/cmd/options"
)

// Client is the interface of eviction client
type Client interface {
	// GetTaintConditions get all specific taint conditions of current node
	GetTaintConditions()
	// SetTaintConditions set or update taint conditions of current node
	SetTaintConditions()
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

func (c *evictionClient) GetTaintConditions() {
	glog.Infof("GetTaintConditions\n")
	node, err := c.client.CoreV1().Nodes().Get(c.nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("get node taint condition error %v", err)
		return
	}
	taints := node.Spec.Taints
	for _, t := range taints {
		glog.Infof("get taint key: %v, effect: %v", t.Key, t.Effect)
	}
}

func (c* evictionClient) SetTaintConditions() {

}

func (c *evictionClient) GetSummaryStats() {

}