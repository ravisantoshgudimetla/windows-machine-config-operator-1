package e2e

import (
	"fmt"
	"github.com/openshift/windows-machine-config-operator/pkg/client"
	"github.com/openshift/windows-machine-config-operator/pkg/providers"
	"os"
)

const (
	// instanceType is the AWS specific instance type to create the VM with
	instanceType = "m4.large"
)

// setupAWSCloudProvider creates AWS provider using the give OpenShift client
// This is the first step of the e2e test and fails the test upon error.
func setupAWSCloudProvider() (*providers.AwsProvider, error) {
	oc, err := client.GetOpenShift(os.Getenv("KUBECONFIG"))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OpenShift client with error: %v", err)
	}
	// awsCredentials is set by OpenShift CI
	awsCredentials := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")

	awsProvider, err := providers.NewAWSProvider(oc, awsCredentials, "default", instanceType)
	if err != nil {
		return nil, fmt.Errorf("error obtaining aws interface object: %v", err)
	}
	return awsProvider, nil
}
