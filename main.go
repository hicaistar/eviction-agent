package main

import (
	"flag"
	"math/rand"
	"os"
	"time"

	"eviction-agent/pkg/util"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	flag.Set("logtostderr", "ture")
	flag.Parse()

	glog.Infof("Start to run eviction agent...\n")

	nodeName := os.Getenv("MY_NODE_NAME")
	if nodeName == "" {
		glog.Fatalf("MY_NODE_NAME environment variable not set\n")
	}

	client := util.SetupClient()
	node := getNode(client, nodeName)
	if node != nil {
		glog.Infof("get node %v\n", node.Name)
	}

	for {
		glog.Infof("get node: %v\n", (node != nil))
		time.Sleep(time.Second * 30)
	}
}

func getNode(client *kubernetes.Clientset, name string) *v1.Node {
	node, err := client.CoreV1().Nodes().Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Fatalf("Could not get node information: %v\n", err)
	}
	return node
}