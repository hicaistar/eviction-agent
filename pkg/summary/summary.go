package summary

import (
	"fmt"
	"net/http"
	"net/url"
	"net"
	"strconv"
	"io/ioutil"
	"encoding/json"

	"github.com/golang/glog"

	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	statsapi "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
)

type NodeInfo struct {
	Name           string
	Port           int
	ConnectAddress string
}

type ConditionStats struct {
	NodeNetStats *statsapi.NetworkStats
	PodStats     []statsapi.PodStats
}

type SummaryStatsApi interface {
	GetSummaryStats() (*ConditionStats, error)
}

type kubeletClient struct {
	port   int
	host   string  // Connect address
	client *http.Client
	stats  ConditionStats
}

func NewSummaryStatsApi(transport http.RoundTripper, nodeInfo NodeInfo) (SummaryStatsApi, error) {
	c := &http.Client{
		Transport: transport,
	}
	return &kubeletClient{
		port:   nodeInfo.Port,
		host:   nodeInfo.ConnectAddress,
		client: c,
	}, nil
}

func (kc *kubeletClient) GetSummaryStats() (*ConditionStats, error) {
	err := kc.collect()
	return &kc.stats, err
}

func (kc *kubeletClient) collect() error {
	summary, err := kc.getSummary()
	if err != nil {
		glog.Errorf("get summary error: %v\n", err)
		return err
	}
	glog.Infof("Get summary node name: %v", summary.Node.NodeName)
	kc.stats.NodeNetStats = summary.Node.Network
	kc.stats.PodStats = summary.Pods
	return nil
}

func (kc *kubeletClient) makeRequestAndGetValue(client *http.Client, req *http.Request, value interface{}) error {
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("do http request error: %v", err)
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body - %v", err)
	}
	if response.StatusCode == http.StatusNotFound {
		return fmt.Errorf("request not found: %v", req.URL.String())
	} else if response.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed - %q, response: %q", response.Status, string(body))
	}

	kubeletAddr := "[unknown]"
	if req.URL != nil {
		kubeletAddr = req.URL.Host
	}
	glog.V(10).Infof("Raw response from Kubelet at %s: %s", kubeletAddr, string(body))
	//glog.Infof("Raw response from kubelet at %s: %s", kubeletAddr, string(body))

	err = json.Unmarshal(body, value)
	if err != nil {
		return fmt.Errorf("failed to parse output. Response: %q. Error: %v", string(body), err)
	}
	return nil
}

func (kc *kubeletClient) getSummary() (*stats.Summary, error) {
	scheme := "http"

	url := url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(kc.host, strconv.Itoa(kc.port)),
		Path:   "/stats/summary/",
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}
	summary := &stats.Summary{}
	client := kc.client
	if client == nil {
		client = http.DefaultClient
	}
	err = kc.makeRequestAndGetValue(client, req, summary)
	return summary, err
}

func (kc *kubeletClient) getCAdvisor() (error) {
	glog.Infof("Inside get cadvisor info\n")
	scheme := "http"

	url := url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(kc.host, strconv.Itoa(kc.port)),
		Path:   "/metrics/cadvisor",
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return err
	}
	summary := &stats.Summary{}
	client := kc.client
	if client == nil {
		client = http.DefaultClient
	}
	err = kc.makeRequestAndGetValue(client, req, summary)
	return err
}
