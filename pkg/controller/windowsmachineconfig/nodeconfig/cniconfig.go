package nodeconfig

import (
	"encoding/json"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	wkl "github.com/openshift/windows-machine-config-operator/pkg/controller/wellknownlocations"
	"github.com/pkg/errors"
	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
)

var (
	cni = new(cniConfigVars)
)

// cniConfigVars holds the information required for populating cni config
type cniConfigVars struct {
	ServiceNetworkCIDR string
	hostSubnet         string
}

// cniConfig holds the structure of the cni config template
type cniConfig struct {
	CniVersion   string `json: "cniVersion"`
	Name         string `json: "name"`
	Type         string `json: "type"`
	Capabilities struct {
		Dns bool `json: "dns"`
	} `json: "capabilities"`
	Ipam struct {
		Type   string `json: "type"`
		Subnet string `json: "subnet"`
	} `json: "ipam"`
	Policies []struct {
		Name  string `json: "name"`
		Value struct {
			Type              string   `json: "Type"`
			ExceptionList     []string `json: "ExceptionList"`
			DestinationPrefix string   `json: "DestinationPrefix"`
			NeedEncap         bool     `json: "NeedEncap"`
		} `json: "value"`
	} `json: "policies"`
}

// GetServiceNetworkCIDR gets the serviceCIDR using cluster config
// this is required for cni configuration
func GetServiceNetworkCIDR(oclient configclient.Interface) error {
	// Get the cluster network object so that we can find the service network
	networkCR, err := oclient.ConfigV1().Networks().Get("cluster", metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "error getting cluster network object")
	}
	cni.ServiceNetworkCIDR = networkCR.Spec.ServiceNetwork[0]
	return nil
}

// populateCniConfig populates the cni config template with necessary information and
// copies the modified json to the payload directory
func populateCniConfig() error {
	cniConfigFile, err := os.Open(wkl.CNIConfigPath)
	if err != nil {
		return errors.Wrapf(err, "error opening CNI config file from %s", wkl.CNIConfigPath)
	}
	defer cniConfigFile.Close()
	decoder := json.NewDecoder(cniConfigFile)
	Config := cniConfig{}
	err = decoder.Decode(&Config)
	if err != nil {
		return errors.Wrap(err, "can't decode config JSON")
	}

	Config.Ipam.Subnet = cni.hostSubnet
	Config.Policies[0].Value.ExceptionList[0] = cni.ServiceNetworkCIDR
	Config.Policies[1].Value.DestinationPrefix = cni.ServiceNetworkCIDR

	response, err := json.Marshal(&Config)
	if err != nil {
		return errors.Wrap(err, "can't retrieve config JSON using modified config struct")
	}

	err = ioutil.WriteFile(wkl.CNIConfigPath, response, os.ModePerm)
	if err != nil {
		return errors.Wrapf(err, "can't write JSON config file to %s", wkl.CNIConfigPath)
	}
	return nil
}
