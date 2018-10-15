package options

import (
	"os"
	"fmt"
	"github.com/golang/glog"
)

type EvictionAgentOptions struct {
	// command line options

	// PolicyConfigPath specifies the path to taint policy configuration file.
	PolicyConfigFile string
	// KubeconfigFile specifies the path to kubeconfig file.
	KubeconfigFile string
	// LogDir specifies the path to log file
	LogDir string
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
		glog.Errorf("Failed to get node node from environment")
		panic(fmt.Errorf("failed to get node name from environment"))
	}
}

func (eao *EvictionAgentOptions) SetPolicyConfigFileOrDie() {
	eao.PolicyConfigFile = os.Getenv("POLICY_CONFIG_FILE")
	if eao.PolicyConfigFile == "" {
		glog.Errorf("Failed to get policy configure file")
		panic(fmt.Errorf("failed to get policy configuration file"))
	}
}

func(eao *EvictionAgentOptions) SetLogDirOrDie() {
	eao.LogDir = os.Getenv("LOG_DIR")
	if eao.LogDir == "" {
		glog.Errorf("Failed to get log dir configure")
		panic(fmt.Errorf("failed to get log dir configure"))
	}
	if _, err := os.Stat(eao.LogDir); err != nil {
		if os.IsNotExist(err) {
			err := os.Mkdir(eao.LogDir, os.ModePerm)
			if err != nil {
				glog.Errorf("Failed to create log dir: %v, error: %v", eao.LogDir, err)
				panic(err)
			}
		}
	}
}