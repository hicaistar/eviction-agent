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
)

type NodeInfo struct {
	Name           string
	ConnectAddress string
	Port           int
}

type SummaryStatsApi interface {
	GetSummaryStats()
}

type kubeletClient struct {
	port int
	host string  // Connect address
	client *http.Client
}

type ErrNotFound struct {
	endpoint string
}

func (err *ErrNotFound) Error() string {
	return fmt.Sprintf("%q not found", err.endpoint)
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

func (kc *kubeletClient) GetSummaryStats() {
	kc.collect()
}

func (kc *kubeletClient) collect() error {
	summary, err := kc.GetSummary()
	if err != nil {
		glog.Errorf("get summary error: %v\n", err)
		return err
	}
	glog.Infof("Get summary node name: %v", summary.Node.NodeName)
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
		return &ErrNotFound{req.URL.String()}
	} else if response.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed - %q, response: %q", response.Status, string(body))
	}

	kubeletAddr := "[unknown]"
	if req.URL != nil {
		kubeletAddr = req.URL.Host
	}
	glog.V(10).Infof("Raw response from Kubelet at %s: %s", kubeletAddr, string(body))

	err = json.Unmarshal(body, value)
	if err != nil {
		return fmt.Errorf("failed to parse output. Response: %q. Error: %v", string(body), err)
	}
	return nil
}

func (kc *kubeletClient) GetSummary() (*stats.Summary, error) {
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

