package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RokkuProxySpec defines the desired state of RokkuProxy
//TODO init container for vault stuff
type RokkuSpec struct {
	Size            int32                       `json:"size"`
	Image           string                      `json:"image,omitempty"`
	Replicas        *int32                      `json:"replicas,omitempty"`
	PodTemplate     RokkuPodTemplateSpec        `json:"podTemplate,omitempty"`
	SecurityContext *corev1.SecurityContext     `json:"securityContext,omitempty"`
	Service         *RokkuService               `json:"service,omitempty"`
	Config          *ConfigRef                  `json:"config,omitempty"`
	Lifecycle       *RokkuLifecycle             `json:"lifecycle,omitempty"`
	HealthcheckPath string                      `json:"healthcheckPath,omitempty"`
	Resources       corev1.ResourceRequirements `json:"resources,omitempty"`
	Environment     *RokkuEnvironment           `json:"env,omitempty"`
}

type RokkuEnvironment struct {
	EnvName  string `json:"name,omitempty"`
	EnvValue string `json:"value,omitempty"`
}

// RokkuProxyStatus defines the observed state of RokkuProxy
type RokkuStatus struct {
	Pods            []PodStatus     `json:"pods,omitempty"`
	Services        []ServiceStatus `json:"services,omitempty"`
	CurrentReplicas int32           `json:"currentReplicas,omitempty"`
	PodSelector     string          `json:"podSelector,omitempty"`
}

type RokkuLifecycle struct {
	PostStart *RokkuLifecycleHandler `json:"postStart,omitempty"`
	PreStop   *RokkuLifecycleHandler `json:"preStop,omitempty"`
}

type RokkuLifecycleHandler struct {
	Exec *corev1.ExecAction `json:"exec,omitempty"`
}

type RokkuService struct {
	// Type is the type of the service. Defaults to the default service type value.
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`
	// LoadBalancerIP is an optional load balancer IP for the service.
	// +optional
	LoadBalancerIP string `json:"loadBalancerIP,omitempty"`
	// Labels are extra labels for the service.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations are extra annotations for the service.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// ExternalTrafficPolicy defines whether external traffic will be routed to
	// node-local or cluster-wide endpoints. Defaults to the default Service
	// externalTrafficPolicy value.
	// +optional
	ExternalTrafficPolicy corev1.ServiceExternalTrafficPolicyType `json:"externalTrafficPolicy,omitempty"`
	// UsePodSelector defines whether Service should automatically map the
	// endpoints using the pod's label selector. Defaults to true.
	// +optional
	UsePodSelector *bool `json:"usePodSelector,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RokkuProxy is the Schema for the rokkuproxies API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=rokkuproxies,scope=Namespaced
type Rokku struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RokkuSpec   `json:"spec,omitempty"`
	Status RokkuStatus `json:"status,omitempty"`
}

type RokkuConfigSpec struct {
	// InMemory if set to true creates a memory backed volume.
	InMemory bool `json:"inMemory,omitempty"`
	// Path is the mount path for the config volume.
	Path string `json:"path"`
	// Size is the maximum size allowed for the config volume.
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RokkuProxyList contains a list of RokkuProxy
type RokkuList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Rokku `json:"items"`
}

type RokkuPodTemplateSpec struct {
	// Affinity to be set on the rokku pod.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// Annotations are custom annotations to be set into Pod.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// Labels are custom labels to be added into Pod.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// HostNetwork enabled causes the pod to use the host's network namespace.
	// +optional
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// Ports is the list of ports used by Rokku.
	// +optional
	Ports []corev1.ContainerPort `json:"ports,omitempty"`
	// TerminationGracePeriodSeconds defines the max duration seconds which the
	// pod needs to terminate gracefully. Defaults to pod's
	// terminationGracePeriodSeconds default value.
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// SecurityContext configures security attributes for the rokku pod.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// Volumes that will attach to Rokku instances
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// VolumeMounts will mount volume declared above in directories
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
}

// ConfigRef is a reference to a config object.
type ConfigRef struct {
	// Name of the config object. Required when Kind is ConfigKindConfigMap.
	// +optional
	Name string `json:"name,omitempty"`
	// Kind of the config object. Defaults to ConfigKindConfigMap.
	// +optional
	Kind ConfigKind `json:"kind,omitempty"`
	// Value is a inline configuration content. Required when Kind is ConfigKindInline.
	// +optional
	Value string `json:"value,omitempty"`
}

type ConfigKind string

const (
	// ConfigKindConfigMap is a Kind of configuration that points to a configmap
	ConfigKindConfigMap = ConfigKind("ConfigMap")
	// ConfigKindInline is a kinda of configuration that is setup as a annotation on the Pod
	// and is inject as a file on the container using the Downward API.
	ConfigKindInline = ConfigKind("Inline")
)

type PodStatus struct {
	// Name is the name of the POD running rokku
	Name string `json:"name"`
	// PodIP is the IP if the POD
	PodIP string `json:"podIP"`
	// HostIP is the IP where POD is running
	HostIP string `json:"hostIP"`
}

type ServiceStatus struct {
	// Name is the name of the Service created by rokku
	Name string `json:"name"`
}

// FilesRef is a reference to arbitrary files stored into a ConfigMap in the
// cluster.
type FilesRef struct {
	// Name points to a ConfigMap resource (in the same namespace) which holds
	// the files.
	Name string `json:"name"`
	// Files maps each key entry from the ConfigMap to its relative location on
	// the rokku filesystem.
	// +optional
	Files map[string]string `json:"files,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Rokku{}, &RokkuList{})
}
