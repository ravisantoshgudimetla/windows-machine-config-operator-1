package e2e

import (
	"context"
	"testing"

	operator "github.com/openshift/windows-machine-config-operator/pkg/apis/wmc/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func deletionTestSuite(t *testing.T) {
	t.Run("Deletion", func(t *testing.T) { testWindowsNodeDeletion(t) })
	t.Run("Status", func(t *testing.T) { testStatusWhenSuccessful(t) })
	t.Run("ConfigMap validation", func(t *testing.T) { testConfigMapValidation(t) })
	t.Run("Secrets validation", func(t *testing.T) { testValidateSecrets(t) })
//	t.Run("Cleanup MachineSets", func(t *testing.T) { testMachineSetDeletion(t) })
}

// testWindowsNodeDeletion tests the Windows node deletion from the cluster.
func testWindowsNodeDeletion(t *testing.T) {
	testCtx, err := NewTestContext(t)
	require.NoError(t, err)

	// get WMCO custom resource
	wmco := &operator.WindowsMachineConfig{}
	// Get the WMCO resource called instance
	err = framework.Global.Client.Get(context.TODO(), types.NamespacedName{Name: wmcCRName,
		Namespace: testCtx.namespace}, wmco)
	if err != nil && k8serrors.IsNotFound(err) {
		// We did not find WMCO CR, let's recreate it. This is a possibility when the creation and deletion tests are
		// run independently.
		wmco, err = testCtx.createWMC(gc.numberOfNodes, gc.sshKeyPair)
		require.NoError(t, err)
	}
	// Reset the number of nodes to be deleted to 0
	gc.numberOfNodes = 0
	// Delete the Windows VM that got created.
	wmco.Spec.Replicas = gc.numberOfNodes
	if err := framework.Global.Client.Update(context.TODO(), wmco); err != nil {
		t.Fatalf("error updating wcmo custom resource  %v", err)
	}
	// As per testing, each windows VM is taking roughly 12 minutes to be shown up in the cluster, so to be on safe
	// side, let's make it as 60 minutes.
	err = testCtx.waitForWindowsNodes(gc.numberOfNodes, true)
	if err != nil {
		t.Fatalf("windows node deletion failed  with %v", err)
	}
}

//func testMachineSetDeletion(t *testing.T) {
//	cfg, err := config.GetConfig()
//	require.NoError(t, err)
//	k8sClient, err := client.New(cfg, client.Options{})
//	require.NoError(t, err)
//
//	for _, machineSet := range machineSetList {
//		err = k8sClient.Delete(context.TODO(), machineSet)
//		require.NoError(t, err)
//	}
//}
