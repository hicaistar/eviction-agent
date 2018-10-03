package options

import (
	"os"
	"fmt"
)

type EvictionAgentOptions struct {
	// command line options

	// PolicyConfigPath specifies the path to taint policy configuration file.
	PolicyConfigFile string
	// KubeconfigFile specifies the path to kubeconfig file.
	KubeconfigFile string
	// NodeName is the node name used to communicate with Kubernetes ApiServer.
	NodeName string
}

func NewEvictionAgentOptions() *EvictionAgentOptions {
	return &EvictionAgentOptions{}
}

// SetNodeNameOrDie sets `NodeName` field with valid value
func (eao *EvictionAgentOptions) SetNodeNameOrDie() {
	// Get node name from environment variable NODE_NAME
	// By default, assume that the NODE_NAME env should have been set with
	// downward api or user defined exported environment variable.
	eao.NodeName = os.Getenv("NODE_NAME")
	if eao.NodeName == "" {
		panic(fmt.Errorf("Failed to get node name from environment"))
	}
}

func (eao *EvictionAgentOptions) SetPolicyConfigFileOrDie() {
	eao.PolicyConfigFile = os.Getenv("POLICY_CONFIG_FILE")
	if eao.PolicyConfigFile == "" {
		panic(fmt.Errorf("Failed to get policy configuration file"))
	}
}