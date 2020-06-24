package apis

import (
	"github.com/openshift/machine-api-operator/pkg/apis/machine"
	mapiv1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes, SchemeBuilder.AddToScheme)
}

var (
	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: machine.GroupName, Version: "v1beta1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
)

// Adds the list of known types to Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&mapiv1.Machine{},
		&mapiv1.MachineList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
