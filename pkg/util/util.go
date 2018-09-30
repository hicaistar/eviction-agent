package util

import (
	"os"

	"github.com/golang/glog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

)

// SetupClient created client
func SetupClient() *kubernetes.Clientset {
	var config *rest.Config
	var err error

	kubeconfigFile := os.Getenv("KUBECONFIG")
	if kubeconfigFile != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigFile)
		if err != nil {
			glog.Fatalf("Error creating config from %s error %v\n", kubeconfigFile, err)
		}
		glog.Infof("Create client using kubeconfig file %s", kubeconfigFile)
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			glog.Fatalf("Error creating InCluster config: %v\n", err)
		}
		glog.Infof("Create client using in-cluster config")
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Error creating clientSet: %v\n", err)
	}
	return clientSet
}
