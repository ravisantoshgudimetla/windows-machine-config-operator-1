package aws

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	awssession "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/iam"
	mapiv1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/openshift/windows-machine-config-operator/test/e2e/clusterinfo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	awsprovider "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsprovider/v1beta1"
)

const (
	infraIDTagKeyPrefix = "kubernetes.io/cluster/"
	infraIDTagValue     = "owned"
	windowsLabel        = "node.openshift.io/os_id"
)

// number of replicas of windows machine to be created
var replicas = int32(1)

type AwsProvider struct {
	// imageID is the AMI image-id to be used for creating Virtual Machine
	ImageID string
	// instanceType is the flavor of VM to be used
	InstanceType string
	// A client for IAM.
	IAM *iam.IAM
	// A client for EC2. to query Windows AMI images
	EC2 ec2iface.EC2API
	// openShiftClient is the client of the existing OpenShift cluster.
	openShiftClient *clusterinfo.OpenShift
	// region in which the Machine needs to be created
	region string
}

// newSession uses AWS credentials to create and returns a session for interacting with EC2.
func newSession(credentialPath, credentialAccountID, region string) (*awssession.Session, error) {
	if _, err := os.Stat(credentialPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to find AWS credentials from path '%v'", credentialPath)
	}
	return awssession.NewSession(&aws.Config{
		Credentials: credentials.NewSharedCredentials(credentialPath, credentialAccountID),
		Region:      aws.String(region),
	})
}

// New returns the AWS implementations of the Cloud interface with AWS session in the same region as OpenShift Cluster.
// credentialPath is the file path the AWS credentials file.
// credentialAccountID is the account name the user uses to create VM instance.
// The credentialAccountID should exist in the AWS credentials file pointing at one specific credential.
func NewAWSProvider(openShiftClient *clusterinfo.OpenShift, credentialPath, credentialAccountID, instanceType, region string) (*AwsProvider, error) {
	session, err := newSession(credentialPath, credentialAccountID, region)
	if err != nil {
		return nil, fmt.Errorf("could not create new AWS session: %v", err)
	}

	ec2Client := ec2.New(session, aws.NewConfig())

	iamClient := iam.New(session, aws.NewConfig())

	imageID, err := getLatestWindowsAMI(ec2Client)
	if err != nil {
		return nil, fmt.Errorf("unable to get latest Windows AMI: %v", err)
	}

	return &AwsProvider{imageID, instanceType,
		iamClient,
		ec2Client,
		openShiftClient,
		region,
	}, nil
}

// GetInfraID returns the infrastructure ID associated with the OpenShift cluster. This is public for
// testing purposes as of now.
func (a *AwsProvider) GetInfraID() (string, error) {
	infraID, err := a.openShiftClient.GetInfrastructureID()
	if err != nil {
		return "", fmt.Errorf("erroring getting OpenShift infrastructure ID associated with the cluster")
	}
	return infraID, nil
}

// getLatestWindowsAMI returns the imageid of the latest released "Windows Server with Containers" image
func getLatestWindowsAMI(ec2Client *ec2.EC2) (string, error) {
	// Have to create these variables, as the below functions require pointers to them
	windowsAMIOwner := "amazon"
	windowsAMIFilterName := "name"
	// This filter will grab all ami's that match the exact name. The '?' indicate any character will match.
	// The ami's will have the name format: Windows_Server-2019-English-Full-ContainersLatest-2020.01.15
	// so the question marks will match the date of creation
	windowsAMIFilterValue := "Windows_Server-2019-English-Full-ContainersLatest-????.??.??"
	searchFilter := ec2.Filter{Name: &windowsAMIFilterName, Values: []*string{&windowsAMIFilterValue}}

	describedImages, err := ec2Client.DescribeImages(&ec2.DescribeImagesInput{
		Filters: []*ec2.Filter{&searchFilter},
		Owners:  []*string{&windowsAMIOwner},
	})
	if err != nil {
		return "", err
	}
	if len(describedImages.Images) < 1 {
		return "", fmt.Errorf("found zero images matching given filter: %v", searchFilter)
	}

	// Find the last created image
	latestImage := describedImages.Images[0]
	latestTime, err := time.Parse(time.RFC3339, *latestImage.CreationDate)
	if err != nil {
		return "", err
	}
	for _, image := range describedImages.Images[1:] {
		newTime, err := time.Parse(time.RFC3339, *image.CreationDate)
		if err != nil {
			return "", err
		}
		if newTime.After(latestTime) {
			latestImage = image
			latestTime = newTime
		}
	}
	return *latestImage.ImageId, nil
}

// getSubnet tries to find a subnet under the VPC and returns subnet or an error.
// These subnets belongs to the OpenShift cluster.
func (a *AwsProvider) GetSubnet(infraID string) (*ec2.Subnet, error) {
	vpc, err := a.getVPCByInfrastructure(infraID)
	if err != nil {
		return nil, fmt.Errorf("unable to get the VPC %v", err)
	}
	// search subnet by the vpcid owned by the vpcID
	subnets, err := a.EC2.DescribeSubnets(&ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{vpc.VpcId},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Get the instance offerings that support Windows instances
	scope := "Availability Zone"
	productDescription := "Windows"
	f := false
	offerings, err := a.EC2.DescribeReservedInstancesOfferings(&ec2.DescribeReservedInstancesOfferingsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("scope"),
				Values: []*string{&scope},
			},
		},
		IncludeMarketplace: &f,
		InstanceType:       &a.InstanceType,
		ProductDescription: &productDescription,
	})
	if err != nil {
		return nil, fmt.Errorf("error checking instance offerings of %s: %v", a.InstanceType, err)
	}
	if offerings.ReservedInstancesOfferings == nil {
		return nil, fmt.Errorf("no instance offerings returned for %s", a.InstanceType)
	}

	// Finding required subnet within the vpc.
	foundSubnet := false
	requiredSubnet := "-private-"

	for _, subnet := range subnets.Subnets {
		for _, tag := range subnet.Tags {
			// TODO: find public subnet by checking igw gateway in routing.
			if *tag.Key == "Name" && strings.Contains(*tag.Value, infraID+requiredSubnet) {
				foundSubnet = true
				// Ensure that the instance type we want is supported in the zone that the subnet is in
				for _, instanceOffering := range offerings.ReservedInstancesOfferings {
					if instanceOffering.AvailabilityZone == nil {
						continue
					}
					if *instanceOffering.AvailabilityZone == *subnet.AvailabilityZone {
						return subnet, nil
					}
				}
			}
		}
	}

	err = fmt.Errorf("could not find the required subnet in VPC: %v", *vpc.VpcId)
	if !foundSubnet {
		err = fmt.Errorf("could not find the required subnet in a zone that supports %s instance type",
			a.InstanceType)
	}
	return nil, err
}

// GetClusterWorkerSGID gets worker security group id from the existing cluster or returns an error.
// This function is exposed for testing purpose.
func (a *AwsProvider) GetClusterWorkerSGID(infraID string) (string, error) {
	sg, err := a.EC2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: aws.StringSlice([]string{fmt.Sprintf("%s-worker-sg", infraID)}),
			},
			{
				Name:   aws.String("tag:" + infraIDTagKeyPrefix + infraID),
				Values: aws.StringSlice([]string{infraIDTagValue}),
			},
		},
	})
	if err != nil {
		return "", err
	}
	if sg == nil || len(sg.SecurityGroups) < 1 {
		return "", fmt.Errorf("no security group is found for the cluster worker nodes")
	}
	return *sg.SecurityGroups[0].GroupId, nil
}

// GetVPCByInfrastructure finds the VPC of an infrastructure and returns the VPC struct or an error.
func (a *AwsProvider) getVPCByInfrastructure(infraID string) (*ec2.Vpc, error) {
	res, err := a.EC2.DescribeVpcs(&ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:" + infraIDTagKeyPrefix + infraID),
				Values: aws.StringSlice([]string{infraIDTagValue}),
			},
			{
				Name:   aws.String("state"),
				Values: aws.StringSlice([]string{"available"}),
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(res.Vpcs) < 1 {
		return nil, fmt.Errorf("failed to find the VPC of the infrastructure")
	} else if len(res.Vpcs) > 1 {
		log.Printf("more than one VPC is found, using %s", *res.Vpcs[0].VpcId)
	}
	return res.Vpcs[0], nil
}

func (a *AwsProvider) GetOpenshiftRegion() string {
	return a.region
}

// GetIAMWorkerRole gets worker IAM information from the existing cluster including IAM arn or an error.
// This function is exposed for testing purpose.
func (a *AwsProvider) GetIAMWorkerRole(infraID string) (*ec2.IamInstanceProfileSpecification, error) {
	iamspc, err := a.IAM.GetInstanceProfile(&iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(fmt.Sprintf("%s-worker-profile", infraID)),
	})
	if err != nil {
		return nil, err
	}
	return &ec2.IamInstanceProfileSpecification{
		Arn: iamspc.InstanceProfile.Arn,
	}, nil
}

// GenerateMachineSet generates the machineset object which is aws provider specific
func (a *AwsProvider) GenerateMachineSet(withWindowsLabel bool) (*mapiv1.MachineSet, error) {
	clusterName, err := a.GetInfraID()
	if err != nil {
		return nil, fmt.Errorf("unable to get infrastructure id %v", err)
	}

	instanceProfile, err := a.GetIAMWorkerRole(clusterName)
	if err != nil {
		return nil, fmt.Errorf("unable to get instance profile %v", err)
	}

	sgID, err := a.GetClusterWorkerSGID(clusterName)
	if err != nil {
		return nil, fmt.Errorf("unable to get securoty group id: %v", err)
	}

	subnet, err := a.GetSubnet(clusterName)
	if err != nil {
		return nil, fmt.Errorf("unable to get subnet: %v", err)
	}

	machineSetName := "windows-machineset"
	matchLabels := map[string]string{
		"machine.openshift.io/cluster-api-cluster":    clusterName,
		"machine.openshift.io/cluster-api-machineset": *subnet.AvailabilityZone,
	}

	if withWindowsLabel {
		matchLabels[windowsLabel] = "Windows"
		machineSetName = machineSetName + "-with-windows-label"
	}

	providerSpec := &awsprovider.AWSMachineProviderConfig{
		AMI: awsprovider.AWSResourceReference{
			ID: &a.ImageID,
		},
		InstanceType: a.InstanceType,
		IAMInstanceProfile: &awsprovider.AWSResourceReference{
			ARN: instanceProfile.Arn,
		},
		CredentialsSecret: &v1.LocalObjectReference{
			Name: "aws-cloud-credentials",
		},
		SecurityGroups: []awsprovider.AWSResourceReference{
			{
				ID: &sgID,
			},
		},
		Subnet: awsprovider.AWSResourceReference{
			ID: subnet.SubnetId,
		},
		// query placement
		Placement: awsprovider.Placement{
			a.region,
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
			Name: machineSetName,
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
