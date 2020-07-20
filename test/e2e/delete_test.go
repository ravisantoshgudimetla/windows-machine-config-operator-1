package e2e

import (
	"context"
	"testing"

	mapi "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func deletionTestSuite(t *testing.T) {
	t.Run("Deletion", func(t *testing.T) { testWindowsNodeDeletion(t) })
}

// testWindowsNodeDeletion tests the Windows node deletion from the cluster.
func testWindowsNodeDeletion(t *testing.T) {
	testCtx, err := NewTestContext(t)
	require.NoError(t, err)

	// get windowsMachineSet custom resource
	windowsMachineSet := &mapi.MachineSet{}
	err = framework.Global.Client.Get(context.TODO(), types.NamespacedName{Name: machineSetName,
		Namespace: "openshift-machine-api"}, windowsMachineSet)
	if err != nil && k8serrors.IsNotFound(err) {
		// We did not find windowsMachineSet CR, let's recreate it. This is a possibility when the creation and deletion tests are
		// run independently.
		err = testCtx.createWindowsMachineSet(1)
		require.NoError(t, err)

	}
	// Reset the number of nodes to be deleted to 0
	gc.numberOfNodes = 0
	// Delete the Windows VM that got created.
	windowsMachineSet.Spec.Replicas = &gc.numberOfNodes
	if err := framework.Global.Client.Update(context.TODO(), windowsMachineSet); err != nil {
		t.Fatalf("error updating windowsMachineSet custom resource  %v", err)
	}
	// As per testing, each windows VM is taking roughly 12 minutes to be shown up in the cluster, so to be on safe
	// side, let's make it as 60 minutes.
	err = testCtx.waitForWindowsNodes(gc.numberOfNodes, true)
	if err != nil {
		t.Fatalf("windows node deletion failed  with %v", err)
	}
}
