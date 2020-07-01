package aws

import (
	"fmt"
	"os"

	oc "github.com/openshift/windows-machine-config-operator/test/e2e/clusterinfo"
)

const (
	// instanceType is the AWS specific instance type to create the VM with
	instanceType = "m4.large"
)

// SetupAWSCloudProvider creates AWS provider using the give OpenShift client
// This is the first step of the e2e test and fails the test upon error.
func SetupAWSCloudProvider(region string) (*AwsProvider, error) {
	oc, err := oc.GetOpenShift(os.Getenv("KUBECONFIG"))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OpenShift client with error: %v", err)
	}
	// awsCredentials is set by OpenShift CI
	awsCredentials := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")

	awsProvider, err := NewAWSProvider(oc, awsCredentials, "default", instanceType, region)
	if err != nil {
		return nil, fmt.Errorf("error obtaining aws interface object: %v", err)
	}
	return awsProvider, nil
}
