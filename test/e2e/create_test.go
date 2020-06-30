package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"testing"
	"time"

	ocv1 "github.com/openshift/api/config/v1"
	mapiv1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	operator "github.com/openshift/windows-machine-config-operator/pkg/apis/wmc/v1alpha1"
	"github.com/openshift/windows-machine-config-operator/pkg/controller/windowsmachineconfig/nodeconfig"
	"github.com/openshift/windows-machine-config-operator/test/e2e/clusterinfo"
	awsprovider "github.com/openshift/windows-machine-config-operator/test/e2e/providers/aws"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	aws "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsprovider/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	//instanceType        = "m5a.large"
	credentialAccountID = "default"
	wmcCRName           = "instance"
	windowsLabel        = "node.openshift.io/os_id"
)

var (
	// awsProvider is setup as a variable for interacting with AWS SDK
	awsProvider = &awsprovider.AwsProvider{}
	// number of replicas of windows machine to be created
	replicas = int32(1)
	// machineSetList keeps a track of all machine sets created
	machineSetList = []*mapiv1.MachineSet{}
)

func creationTestSuite(t *testing.T) {
	// The order of tests here are important. testValidateSecrets is what populates the windowsVMs slice in the gc.
	// testNetwork needs that to check if the HNS networks have been installed. Ideally we would like to run testNetwork
	// before testValidateSecrets and testConfigMapValidation but we cannot as the source of truth for the credentials
	// are the secrets but they are created only after the VMs have been fully configured.
	// Any node object related tests should be run only after testNodeCreation as that initializes the node objects in
	// the global context.
	//t.Run("WMC CR validation", testWMCValidation)
	//// the failure behavior test will be skipped if gc.nodes = 0
	//t.Run("failure behavior", testFailureSuite)
	//t.Run("Creation", func(t *testing.T) { testWindowsNodeCreation(t) })
	//t.Run("Status", func(t *testing.T) { testStatusWhenSuccessful(t) })
	//t.Run("ConfigMap validation", func(t *testing.T) { testConfigMapValidation(t) })
	//t.Run("Secrets validation", func(t *testing.T) { testValidateSecrets(t) })
	//t.Run("Network validation", testNetwork)
	//t.Run("Label validation", func(t *testing.T) { testWorkerLabel(t) })
	//t.Run("NodeTaint validation", func(t *testing.T) { testNodeTaint(t) })
	t.Run("vmcreate", func(t *testing.T) { testWindowsVMCreation(t) })

}

// createMachineSet creates a machine set with required Windows os_id label
func createAWSMachineSet(isWindows bool) (*mapiv1.MachineSet, error) {
	// Create a Machine set and get the condition for it, if it successful or not.
	// Query the machines associated with the machineset if len(machines) > 1 return machineSet name if not return error

	clusterName, err := awsProvider.GetInfraID()
	if err != nil {
		return nil, fmt.Errorf("unable to get infrastructure id %v", err)
	}

	instanceProfile, err := awsProvider.GetIAMWorkerRole(clusterName)
	if err != nil {
		return nil, fmt.Errorf("unable to get instance profile %v", err)
	}

	sgID, err := awsProvider.GetClusterWorkerSGID(clusterName)
	if err != nil {
		return nil, fmt.Errorf("unable to get securoty group id: %v", err)
	}

	subnet, err := awsProvider.GetSubnet(clusterName)
	if err != nil {
		return nil, fmt.Errorf("unable to get subnet: %v", err)
	}

	region, err := awsProvider.GetOpenshiftRegion()
	if err != nil {
		return nil, fmt.Errorf("unable to get region: %v", err)
	}

	machineSetName := "windows-machineset-"
	matchLabels := map[string]string{
		"machine.openshift.io/cluster-api-cluster":    clusterName,
		"machine.openshift.io/cluster-api-machineset": *subnet.AvailabilityZone,
	}

	if isWindows {
		matchLabels[windowsLabel] = "Windows"
		machineSetName = machineSetName + "with-windows-label-"
	}

	providerSpec := &aws.AWSMachineProviderConfig{
		AMI: aws.AWSResourceReference{
			ID: &awsProvider.ImageID,
		},
		InstanceType: awsProvider.InstanceType,
		IAMInstanceProfile: &aws.AWSResourceReference{
			ARN: instanceProfile.Arn,
		},
		CredentialsSecret: &v1.LocalObjectReference{
			Name: "aws-cloud-credentials",
		},
		SecurityGroups: []aws.AWSResourceReference{
			{
				ID: &sgID,
			},
		},
		Subnet: aws.AWSResourceReference{
			ID: subnet.SubnetId,
		},
		// query placement
		Placement: aws.Placement{
			region,
			*subnet.AvailabilityZone,
		},
	}

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		return nil, err
	}

	// Set up the test machineSet
	machineSet := &mapiv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: machineSetName,
			Namespace:    "openshift-machine-api",
			Labels: map[string]string{
				mapiv1.MachineClusterIDLabel: clusterName,
			},
		},
		Spec: mapiv1.MachineSetSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Replicas: &replicas,
			Template: mapiv1.MachineTemplateSpec{
				ObjectMeta: mapiv1.ObjectMeta{Labels: matchLabels},
				Spec: mapiv1.MachineSpec{
					ProviderSpec: mapiv1.ProviderSpec{
						Value: &runtime.RawExtension{
							Raw: rawBytes,
						},
					},
				},
			},
		},
	}
	return machineSet, nil
}

//
func testWindowsVMCreation(t *testing.T) {

	cfg, err := config.GetConfig()
	if err != nil {
		t.Errorf("%v", err)
	}
	k8sClient, err := client.New(cfg, client.Options{})

	oc, err := clusterinfo.GetOpenShift(os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Errorf("%v", err)
	}
	mapiv1.AddToScheme(scheme.Scheme)

	cloudProvider, err := oc.GetCloudProvider()
	if cloudProvider.Type == ocv1.AWSPlatformType {
		awsProvider, err = setupAWSCloudProvider()
		machineSetNoLabel, err := createAWSMachineSet(false)
		if err != nil {
			t.Fatalf("failed to create machine sets %v", err)
		}
		err = k8sClient.Create(context.TODO(), machineSetNoLabel)
		if err != nil {
			t.Fatalf("failed to create machine sets %v", err)
		}
		machineSetList = append(machineSetList, machineSetNoLabel)
		//err = k8sClient.Delete(context.TODO(), machineSetNoLabel)
		//if err != nil {
		//	t.Fatalf("failed to create machine sets %v", err)
		//}
		machineSetWithLabel, err := createAWSMachineSet(true)
		if err != nil {
			t.Fatal("failed to create  machine sets ")
		}
		err = k8sClient.Create(context.TODO(), machineSetWithLabel)
		if err != nil {
			t.Fatalf("failed to create machine sets %v", err)
		}
		machineSetList = append(machineSetList, machineSetNoLabel)
		t.Fatal("created machine sets ")
	}

	//test := []struct {
	//{ case1: Create a Windows VM with label expectEvent: true},
	//	{case2: Create a Windows VM without label},
	//	{case3: Create a Linux VM without label},
	//}
	//
	//
	//for _, test := range tests {
	//	machineName, err := createMachineSet(expectEvent)
	//	require.NoError(t, err)
	//	// Get the events from the windows-machine-config-operator namespace
	//	eventList := []v1.EventList
	//	if expectEvent {
	//		for _, event := range eventList {
	//			require.Contains(t,event.message, machineName)
	//		}
	//	}
	//}
}

// testWindowsNodeCreation tests the Windows node creation in the cluster
func testWindowsNodeCreation(t *testing.T) {
	testCtx, err := NewTestContext(t)
	require.NoError(t, err)

	// create WMCO custom resource
	if _, err := testCtx.createWMC(gc.numberOfNodes, gc.sshKeyPair); err != nil {
		t.Fatalf("error creating wcmo custom resource  %v", err)
	}
	if err := testCtx.waitForWindowsNodes(gc.numberOfNodes, true); err != nil {
		t.Fatalf("windows node creation failed  with %v", err)
	}
	log.Printf("Created %d Windows worker nodes", len(gc.nodes))
}

// createWMC creates a WMC object with the given replicas and keypair
func (tc *testContext) createWMC(replicas int, keyPair string) (*operator.WindowsMachineConfig, error) {
	wmco := &operator.WindowsMachineConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WindowsMachineConfig",
			APIVersion: "wmc.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      wmcCRName,
			Namespace: tc.namespace,
		},
		Spec: operator.WindowsMachineConfigSpec{
			InstanceType: instanceType,
			AWS:          &operator.AWS{CredentialAccountID: credentialAccountID, SSHKeyPair: keyPair},
			Replicas:     replicas,
		},
	}
	return wmco, framework.Global.Client.Create(context.TODO(), wmco,
		&framework.CleanupOptions{TestContext: tc.osdkTestCtx,
			Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
}

// waitForWindowsNode waits until there exists nodeCount Windows nodes with the correct set of annotations.
// if waitForAnnotations = false, the function will return when the node object is first seen and not wait until
// the expected annotations are present.
func (tc *testContext) waitForWindowsNodes(nodeCount int, waitForAnnotations bool) error {
	var nodes *v1.NodeList
	annotations := []string{nodeconfig.HybridOverlaySubnet, nodeconfig.HybridOverlayMac}

	// As per testing, each windows VM is taking roughly 12 minutes to be shown up in the cluster, so to be on safe
	// side, let's make it as 20 minutes per node. The value comes from nodeCreationTime variable.  If we are testing a
	// scale down from n nodes to 0, then we should not take the number of nodes into account.
	err := wait.Poll(nodeRetryInterval, time.Duration(math.Max(float64(nodeCount), 1))*nodeCreationTime, func() (done bool, err error) {
		nodes, err = tc.kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: nodeconfig.WindowsOSLabel})
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Printf("waiting for %d Windows nodes", gc.numberOfNodes)
				return false, nil
			}
			return false, err
		}
		if len(nodes.Items) != nodeCount {
			log.Printf("waiting for %d/%d Windows nodes", len(nodes.Items), gc.numberOfNodes)
			return false, nil
		}
		if !waitForAnnotations {
			return true, nil
		}
		// Wait for annotations to be present on the node objects in the scale up caseoc
		if nodeCount != 0 {
			log.Printf("waiting for annotations to be present on %d Windows nodes", nodeCount)
		}
		for _, node := range nodes.Items {
			for _, annotation := range annotations {
				_, found := node.Annotations[annotation]
				if !found {
					return false, nil
				}
			}
		}

		return true, nil
	})

	// Initialize/update nodes to avoid staleness
	gc.nodes = nodes.Items

	return err
}
