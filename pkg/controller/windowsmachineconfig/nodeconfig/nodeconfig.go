package nodeconfig

import (
	"fmt"
	"k8s.io/api/core/v1"
	"strings"
	"time"

	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/types"
	"github.com/openshift/windows-machine-config-operator/pkg/controller/windowsmachineconfig/windows"
	"github.com/pkg/errors"
	certificates "k8s.io/api/certificates/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// RetryCount is the amount of times we will retry an api operation
	RetryCount = 20
	// RetryInterval is the interval of time until we retry after a failure
	RetryInterval = 5 * time.Second
	// bootstrapCSR is the CSR name associated with a worker node that just got bootstrapped.
	bootstrapCSR = "system:serviceaccount:openshift-machine-config-operator:node-bootstrapper"
	// workerLabel is the label that needs to be applied to the Windows node to make it worker node
	WorkerLabel = "node-role.kubernetes.io/worker"
)

// nodeConfig holds the information to make the given VM a kubernetes node. As of now, it holds the information
// related to kubeclient and the windowsVM.
type nodeConfig struct {
	// k8sclientset holds the information related to kubernetes clientset
	k8sclientset *kubernetes.Clientset
	// Windows holds the information related to the windows VM
	*windows.Windows
}

// NewNodeConfig creates a new instance of nodeConfig to be used by the caller.
func NewNodeConfig(clientset *kubernetes.Clientset, windowsVM types.WindowsVM) *nodeConfig {
	return &nodeConfig{k8sclientset: clientset, Windows: windows.New(windowsVM)}
}

var log = logf.Log.WithName("nodeconfig")

// Configure configures the Windows VM to make it a Windows worker node
func (nc *nodeConfig) Configure() error {
	if err := nc.Windows.Configure(); err != nil {
		return errors.Wrap(err, "configuring the Windows VM failed")
	}
	if err := nc.handleCSRs(); err != nil {
		return errors.Wrap(err, "handling CSR for the given node failed")
	}

	var instanceID string
	// As the CSR has been approved get the Kubernetes node object associated
	// the Windows VM created
	if nc.Windows.GetCredentials() != nil && len(nc.Windows.GetCredentials().GetInstanceId()) > 0 {
		instanceID = nc.Windows.GetCredentials().GetInstanceId()
	}

	// Get the node object associated with node
	node, err := nc.getNodeObjectAssociated(instanceID)
	if err != nil {
		return errors.Wrap(err, "node object associated with the node")
	}

	// Apply worker labels
	if err := nc.applyWorkerLabel(node); err != nil {
		return errors.Wrap(err, "failed applying worker label")
	}
	return nil
}

func (nc *nodeConfig) getNodeObjectAssociated(instanceID string) (*v1.Node, error) {
	nodes, err := nc.k8sclientset.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: "node.openshift.io/os_id=Windows"})
	if err != nil {
		return nil, errors.Wrap(err, "error listing nodes")
	}
	for _, node := range nodes.Items {
		if instanceID == GetInstanceID(node.Spec.ProviderID) {
			return &node, nil
		}
	}
	return nil, errors.Errorf("unable to find node for instanceID %s", instanceID)
}

// GetInstanceID gets the instanceID of VM for a given cloud provider ID
// Ex: aws:///us-east-1e/i-078285fdadccb2eaa. We always want the last entry which is the instanceID
func GetInstanceID(providerID string) string {
	providerTokens := strings.Split(providerID, "/")
	return providerTokens[len(providerTokens)-1]
}

// applyWorkerLabel applies the worker label to the Windows Node we created.
func (nc *nodeConfig) applyWorkerLabel(node *v1.Node) error {
	node.Labels[WorkerLabel] = ""
	if _, err := nc.k8sclientset.CoreV1().Nodes().Update(node); err != nil {
		return errors.Wrap(err, "error updating node object")
	}
	return nil
}

// HandleCSRs handles the approval of bootstrap and node CSRs
func (nc *nodeConfig) handleCSRs() error {
	// Handle the bootstrap CSR
	err := nc.handleCSR(bootstrapCSR)
	if err != nil {
		return errors.Wrap(err, "unable to handle bootstrap CSR")
	}

	// TODO: Handle the node CSR
	// 		Note: for the product we want to get the node name from the instance information
	//		jira story: https://issues.redhat.com/browse/WINC-271
	err = nc.handleCSR("system:node:")
	if err != nil {
		return errors.Wrap(err, "unable to handle node CSR")
	}
	return nil
}

// findCSR finds the CSR that contains the requestor filter
func (nc *nodeConfig) findCSR(requestor string) (*certificates.CertificateSigningRequest, error) {
	var foundCSR *certificates.CertificateSigningRequest
	// Find the CSR
	for retries := 0; retries < RetryCount; retries++ {
		csrs, err := nc.k8sclientset.CertificatesV1beta1().CertificateSigningRequests().List(metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("unable to get CSR list: %v", err)
		}
		if csrs == nil {
			time.Sleep(RetryInterval)
			continue
		}

		for _, csr := range csrs.Items {
			if !strings.Contains(csr.Spec.Username, requestor) {
				continue
			}
			var handledCSR bool
			for _, c := range csr.Status.Conditions {
				if c.Type == certificates.CertificateApproved || c.Type == certificates.CertificateDenied {
					handledCSR = true
					break
				}
			}
			if handledCSR {
				continue
			}
			foundCSR = &csr
			break
		}

		if foundCSR != nil {
			break
		}
		time.Sleep(RetryInterval)
	}

	if foundCSR == nil {
		return nil, errors.Errorf("unable to find CSR with requestor %s", requestor)
	}
	return foundCSR, nil
}

// approve approves the given CSR if it has not already been approved
// Based on https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/certificates/certificates.go#L237
func (nc *nodeConfig) approve(csr *certificates.CertificateSigningRequest) error {
	// Check if the certificate has already been approved
	for _, c := range csr.Status.Conditions {
		if c.Type == certificates.CertificateApproved {
			return nil
		}
	}

	// Approve the CSR
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Ensure we get the current version
		csr, err := nc.k8sclientset.CertificatesV1beta1().CertificateSigningRequests().Get(
			csr.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Add the approval status condition
		csr.Status.Conditions = append(csr.Status.Conditions, certificates.CertificateSigningRequestCondition{
			Type:           certificates.CertificateApproved,
			Reason:         "WMCBe2eTestRunnerApprove",
			Message:        "This CSR was approved by WMCO runner",
			LastUpdateTime: metav1.Now(),
		})

		_, err = nc.k8sclientset.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(csr)
		return err
	})
}

// handleCSR finds the CSR based on the requestor filter and approves it
func (nc *nodeConfig) handleCSR(requestorFilter string) error {
	csr, err := nc.findCSR(requestorFilter)
	if err != nil {
		return errors.Wrapf(err, "error finding CSR for %s", requestorFilter)
	}

	if err = nc.approve(csr); err != nil {
		return errors.Wrapf(err, "error approving CSR for %s", requestorFilter)
	}

	return nil
}
