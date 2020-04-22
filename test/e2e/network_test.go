package e2e

import (
	"fmt"
	"github.com/openshift/windows-machine-config-operator/pkg/controller/windowsmachineconfig/windows"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

// testNetwork runs all the cluster and node network tests
func testNetwork(t *testing.T) {
	t.Run("Hybrid overlay running", testHybridOverlayRunning)
	t.Run("OpenShift HNS networks", testHNSNetworksCreated)
	t.Run("CNI configuration", testCniConfiguration)
}

// testHNSNetworksCreated tests that the required HNS Networks have been created on the bootstrapped nodes
func testHNSNetworksCreated(t *testing.T) {
	testCtx, err := NewTestContext(t)
	require.NoError(t, err)
	defer testCtx.cleanup()

	for _, vm := range gc.windowsVMs {
		// We don't need to retry as we are waiting long enough for the secrets to be created which implies that the
		// network setup has succeeded.
		stdout, _, err := vm.Run("Get-HnsNetwork", true)
		require.NoError(t, err, "Could not run Get-HnsNetwork command")
		assert.Contains(t, stdout, windows.BaseOVNKubeOverlayNetwork,
			"Could not find %s in %s", windows.BaseOVNKubeOverlayNetwork, vm.GetCredentials().GetInstanceId())
		assert.Contains(t, stdout, windows.OVNKubeOverlayNetwork,
			"Could not find %s in %s", windows.OVNKubeOverlayNetwork, vm.GetCredentials().GetInstanceId())
	}
}

// testHybridOverlayRunning checks if the hybrid-overlay process is running on all the bootstrapped nodes
func testHybridOverlayRunning(t *testing.T) {
	testCtx, err := NewTestContext(t)
	require.NoError(t, err)
	defer testCtx.cleanup()

	for _, vm := range gc.windowsVMs {
		_, stderr, err := vm.Run("Get-Process -Name \""+windows.HybridOverlayProcess+"\"", true)
		require.NoError(t, err, "Could not run Get-Process command")
		// stderr being empty implies that hybrid-overlay was running.
		assert.Equal(t, "", stderr, "hybrid-overlay was not running in %s",
			vm.GetCredentials().GetInstanceId())
	}
}

func testCniConfiguration(t *testing.T) {
	testCtx, err := NewTestContext(t)
	require.NoError(t, err)
	defer testCtx.cleanup()

	for _, vm := range gc.windowsVMs {
		kubeletImagePath, _, err := vm.Run("$service=get-wmiobject -query \\\"select * from win32_service "+
			"where name='kubelet'\\\"; echo $service.pathname", true)
		require.NoError(t, err, "Could not fetch image path of the kubelet service")
		// Extracting directory path of the CNI Plugin binaries from kubelet service image path
		var cniBinaryDir string
		kubeletArgs := strings.Split(kubeletImagePath, " ")
		for _, kubeletArg := range kubeletArgs {
			kubeletArgOptionAndValue := strings.Split(strings.TrimSpace(kubeletArg), "=")
			// if kubelet arg is of the form --<option>=<value> and option='--network-plugin' does not have value='cni' then
			// we throw error since CNI Plugins are not enabled
			if len(kubeletArgOptionAndValue) == 2 && kubeletArgOptionAndValue[0] == "--network-plugin" &&
				kubeletArgOptionAndValue[1] != "cni" {
				err := fmt.Errorf("kubelet arg '--network-plugin' should have value 'cni', current value is '%s'",
					kubeletArgOptionAndValue[1])
				require.NoError(t, err, "CNI Plugins are not enabled")
			}
			// if kubelet arg is of the form --<option>=<value> and option='--cni-bin-dir' then its value will be the required
			// directory path of the CNI Plugin binaries
			if len(kubeletArgOptionAndValue) == 2 && kubeletArgOptionAndValue[0] == "--cnbin-dir" {
				cniBinaryDir = kubeletArgOptionAndValue[1]
			}
		}
		// Throwing error if directory of CNI Plugin binaries could not be fetched from kubelet service image path
		require.NotEmpty(t, cniBinaryDir, "Could not fetch directory path of CNI Plugin binaries")
	}
}
