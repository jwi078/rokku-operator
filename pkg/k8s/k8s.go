package k8s

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/jwi078/rokku-operator/pkg/apis/rokku/v1alpha1"

	//"sort"
	"strings"

	_ "github.com/jwi078/rokku-operator/pkg/apis"
	tsuruConfig "github.com/tsuru/config"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/apimachinery/pkg/labels"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	defaultRokkuImage = "wbaa/rokku"

	defaultHTTPPort            = int32(8080)
	defaultHTTPHostNetworkPort = int32(80)
	defaultHTTPPortName        = "http"

	defaultHTTPSPort            = int32(8443)
	defaultHTTPSHostNetworkPort = int32(443)
	defaultHTTPSPortName        = "https"

	curlProbeCommand        = "curl -m%d -kfsS -o /dev/null %s"
	configMountPath         = "/etc/rokku"
	generatedFromAnnotation = "rokku.ing.com/generated-from"
	configFileName          = "ranger-s3-security.xml"
)

var rokkuEntrypoint = []string{
	"/bin/sh",
	"-c",
	// check what the command is
	"/opt/entrypoint.sh",
}

var defaultPostStartCommand = []string{
	"/bin/sh",
	"-c",
	// check what the command is
	"echo Hello from the postStart handler",
}

func NewDeployment(n *v1alpha1.Rokku) (*appv1.Deployment, error) {
	n.Spec.Image = valueOrDefault(n.Spec.Image, defaultRokkuImage)
	setDefaultPorts(&n.Spec.PodTemplate)

	if n.Spec.Replicas == nil {
		var one int32 = 1
		n.Spec.Replicas = &one
	}

	securityContext := n.Spec.PodTemplate.SecurityContext

	if hasLowPort(n.Spec.PodTemplate.Ports) {
		if securityContext == nil {
			securityContext = &corev1.SecurityContext{}
		}
		if securityContext.Capabilities == nil {
			securityContext.Capabilities = &corev1.Capabilities{}
		}
		securityContext.Capabilities.Add = append(securityContext.Capabilities.Add, "NET_BIND_SERVICE")

	}

	var maxSurge, maxUnavailable *intstr.IntOrString
	if n.Spec.PodTemplate.HostNetwork {
		// Round up instead of down as is the default behavior for maxUnvailable,
		// this is useful because we must allow at least one pod down for
		// hostNetwork deployments.
		adjustedValue := intstr.FromInt(int(math.Ceil(float64(*n.Spec.Replicas) * 0.25)))
		maxUnavailable = &adjustedValue
		maxSurge = &adjustedValue
	}

	deployment := appv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name,
			Namespace: n.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(n, schema.GroupVersionKind{
					Group:   v1alpha1.SchemeGroupVersion.Group,
					Version: v1alpha1.SchemeGroupVersion.Version,
					Kind:    "Rokku",
				}),
			},
		},
		Spec: appv1.DeploymentSpec{
			Strategy: appv1.DeploymentStrategy{
				Type: appv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appv1.RollingUpdateDeployment{
					MaxUnavailable: maxUnavailable,
					MaxSurge:       maxSurge,
				},
			},
			Replicas: n.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: LabelsForRokku(n.Name),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   n.Namespace,
					Annotations: assembleAnnotations(*n),
					Labels:      assembleLabels(*n),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "rokku",
							Image: n.Spec.Image,
							//Command:         rokkuEntrypoint,
							Resources:       n.Spec.Resources,
							SecurityContext: securityContext,
							Ports:           n.Spec.PodTemplate.Ports,
							VolumeMounts:    n.Spec.PodTemplate.VolumeMounts,
							Env: []corev1.EnvVar{
								{Name: "ROKKU_STORAGE_S3_HOST",
									Value: valueOrDefault("ceph-server", os.Getenv("ROKKU_STORAGE_S3_HOST")),
								},
								{Name: "ROKKU_STORAGE_S3_PORT",
									Value: valueOrDefault("1234", os.Getenv("ROKKU_STORAGE_S3_PORT"))},
								{Name: "ROKKU_HTTP_BIND",
									Value: valueOrDefault("8080", os.Getenv("ROKKU_HTTP_BIND"))},
								{Name: "ROKKU_STS_URI",
									Value: valueOrDefault("http://rokku-sts:8080", os.Getenv("ROKKU_STS_URI"))},
								{Name: "ALLOW_LIST_BUCKETS",
									Value: valueOrDefault("True", "False")},
								{Name: "ALLOW_CREATE_BUCKETS",
									Value: valueOrDefault("True", "False")},
								{Name: "ROKKU_ATLAS_ENABLED",
									Value: valueOrDefault("True", "False")},
								{Name: "ROKKU_BUCKET_NOTIFY_ENABLED",
									Value: valueOrDefault("True", "False")},
							},
						},
					},
					Affinity:                      n.Spec.PodTemplate.Affinity,
					HostNetwork:                   n.Spec.PodTemplate.HostNetwork,
					TerminationGracePeriodSeconds: n.Spec.PodTemplate.TerminationGracePeriodSeconds,
					Volumes:                       n.Spec.PodTemplate.Volumes,
				},
			},
		},
	}
	setupProbes(n.Spec, &deployment)
	setupConfig(n.Spec.Config, &deployment)
	//setupConfigVolume(n.Spec.Config, &deployment)
	setupLifecycle(n.Spec.Lifecycle, &deployment)

	// This is done on the last step because n.Spec may have mutated during these methods
	if err := SetRokkuSpec(&deployment.ObjectMeta, n.Spec); err != nil {
		return nil, err
	}

	return &deployment, nil
}

func GetRokkuNameFromObject(o metav1.Object) string {
	return o.GetLabels()["rokku.ing.com/resource-name"]
}

func valueOrDefault(value, def string) string {
	if value != "" {
		return value
	}
	return def
}

// LabelsForRokku returns the labels for a Rokku CR with the given name
func LabelsForRokku(name string) map[string]string {
	return map[string]string{
		"rokku.ing.com/resource-name": name,
		"rokku.ing.com/app":           "rokku",
	}
}

func mergeMap(a, b map[string]string) map[string]string {
	if a == nil {
		return b
	}
	for k, v := range b {
		a[k] = v
	}
	return a
}

func SetRokkuSpec(o *metav1.ObjectMeta, spec v1alpha1.RokkuSpec) error {
	if o.Annotations == nil {
		o.Annotations = make(map[string]string)
	}
	origSpec, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	o.Annotations[generatedFromAnnotation] = string(origSpec)
	return nil
}

func portByName(ports []corev1.ContainerPort, name string) *corev1.ContainerPort {
	for i, port := range ports {
		if port.Name == name {
			return &ports[i]
		}
	}
	return nil
}

func setupLifecycle(lifecycle *v1alpha1.RokkuLifecycle, dep *appv1.Deployment) {
	defaultLifecycle := corev1.Lifecycle{
		PostStart: &corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: defaultPostStartCommand,
			},
		},
	}
	dep.Spec.Template.Spec.Containers[0].Lifecycle = &defaultLifecycle
	if lifecycle == nil {
		return
	}
	if lifecycle.PreStop != nil && lifecycle.PreStop.Exec != nil {
		dep.Spec.Template.Spec.Containers[0].Lifecycle.PreStop = &corev1.Handler{Exec: lifecycle.PreStop.Exec}
	}
	if lifecycle.PostStart != nil && lifecycle.PostStart.Exec != nil {
		var postStartCommand []string
		if len(lifecycle.PostStart.Exec.Command) > 0 {
			lastElemIndex := len(defaultPostStartCommand) - 1
			for i, item := range defaultPostStartCommand {
				if i < lastElemIndex {
					postStartCommand = append(postStartCommand, item)
				}
			}
			postStartCommandString := defaultPostStartCommand[lastElemIndex]
			lifecyclePoststartCommandString := strings.Join(lifecycle.PostStart.Exec.Command, " ")
			postStartCommand = append(postStartCommand, fmt.Sprintf("%s && %s", postStartCommandString, lifecyclePoststartCommandString))
		} else {
			postStartCommand = defaultPostStartCommand
		}
		dep.Spec.Template.Spec.Containers[0].Lifecycle.PostStart.Exec.Command = postStartCommand
	}
}

func assembleLabels(n v1alpha1.Rokku) map[string]string {
	labels := LabelsForRokku(n.Name)
	if value, err := tsuruConfig.Get("rokku-controller:pod-template:labels"); err == nil {
		if controllerLabels, ok := value.(map[interface{}]interface{}); ok {
			labels = mergeMap(labels, convertToStringMap(controllerLabels))
		}
	}
	return mergeMap(labels, n.Spec.PodTemplate.Labels)
}

func assembleAnnotations(n v1alpha1.Rokku) map[string]string {
	var annotations map[string]string
	if value, err := tsuruConfig.Get("rokku-controller:pod-template:annotations"); err == nil {
		if controllerAnnotations, ok := value.(map[interface{}]interface{}); ok {
			annotations = convertToStringMap(controllerAnnotations)
		}
	}
	return mergeMap(annotations, n.Spec.PodTemplate.Annotations)
}

func setDefaultPorts(podSpec *v1alpha1.RokkuPodTemplateSpec) {
	if portByName(podSpec.Ports, defaultHTTPPortName) == nil {
		httpPort := defaultHTTPPort
		if podSpec.HostNetwork {
			httpPort = defaultHTTPHostNetworkPort
		}
		podSpec.Ports = append(podSpec.Ports, corev1.ContainerPort{
			Name:          defaultHTTPPortName,
			ContainerPort: httpPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	if portByName(podSpec.Ports, defaultHTTPSPortName) == nil {
		httpsPort := defaultHTTPSPort
		if podSpec.HostNetwork {
			httpsPort = defaultHTTPSHostNetworkPort
		}
		podSpec.Ports = append(podSpec.Ports, corev1.ContainerPort{
			Name:          defaultHTTPSPortName,
			ContainerPort: httpsPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}
}

func convertToStringMap(m map[interface{}]interface{}) map[string]string {
	var result map[string]string
	for k, v := range m {
		if result == nil {
			result = make(map[string]string)
		}
		key, ok := k.(string)
		if !ok {
			continue
		}
		value, ok := v.(string)
		if !ok {
			continue
		}
		result[key] = value
	}
	return result
}

func hasLowPort(ports []corev1.ContainerPort) bool {
	for _, port := range ports {
		if port.ContainerPort < 1024 {
			return true
		}
	}
	return false
}

func setupConfig(conf *v1alpha1.ConfigRef, dep *appv1.Deployment) {
	if conf == nil {
		return
	}
	dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      "rokku-config",
		MountPath: fmt.Sprintf("%s/%s", configMountPath, configFileName),
		SubPath:   configFileName,
	})
	switch conf.Kind {
	case v1alpha1.ConfigKindConfigMap:
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "rokku-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: conf.Name,
					},
				},
			},
		})
	case v1alpha1.ConfigKindInline:
		// FIXME: inline content is being written out of order
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}
		dep.Spec.Template.Annotations[conf.Name] = conf.Value
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "rokku-config",
			VolumeSource: corev1.VolumeSource{
				DownwardAPI: &corev1.DownwardAPIVolumeSource{
					Items: []corev1.DownwardAPIVolumeFile{
						{
							Path: "ranger-s3-securirty.xml",
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: fmt.Sprintf("metadata.annotations['%s']", conf.Name),
							},
						},
					},
				},
			},
		})
	}
}

func setupConfigVolume(config v1alpha1.RokkuConfigSpec, dep *appv1.Deployment) {
	if config.Path == "" {
		return
	}
	const cacheVolName = "cache-vol"
	medium := corev1.StorageMediumDefault
	if config.InMemory {
		medium = corev1.StorageMediumMemory
	}
	dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: cacheVolName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    medium,
				SizeLimit: config.Size,
			},
		},
	})
	dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      cacheVolName,
		MountPath: config.Path,
	})
}

func ExtractRokkuSpec(o metav1.ObjectMeta) (v1alpha1.RokkuSpec, error) {
	ann, ok := o.Annotations[generatedFromAnnotation]
	if !ok {
		return v1alpha1.RokkuSpec{}, fmt.Errorf("missing %q annotation in deployment", generatedFromAnnotation)
	}
	var spec v1alpha1.RokkuSpec
	if err := json.Unmarshal([]byte(ann), &spec); err != nil {
		return v1alpha1.RokkuSpec{}, fmt.Errorf("failed to unmarshal rokku from annotation: %v", err)
	}
	return spec, nil
}

func setupProbes(rokkuSpec v1alpha1.RokkuSpec, dep *appv1.Deployment) {
	httpPort := portByName(rokkuSpec.PodTemplate.Ports, defaultHTTPPortName)
	cmdTimeoutSec := int32(1)

	var commands []string
	if httpPort != nil {
		httpURL := fmt.Sprintf("http://localhost:%d%s", httpPort.ContainerPort, rokkuSpec.HealthcheckPath)
		commands = append(commands, fmt.Sprintf(curlProbeCommand, cmdTimeoutSec, httpURL))
	}

	if len(commands) == 0 {
		return
	}

	dep.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
		TimeoutSeconds: cmdTimeoutSec * int32(len(commands)),
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"sh", "-c",
					strings.Join(commands, " && "),
				},
			},
		},
	}
}

// LabelsForRokkuString returns the labels in string format.
func LabelsForRokkuString(name string) string {
	return k8slabels.FormatLabels(LabelsForRokku(name))
}

func rokkuService(n *v1alpha1.Rokku) corev1.ServiceType {
	if n == nil || n.Spec.Service == nil {
		return corev1.ServiceTypeClusterIP
	}
	return corev1.ServiceType(n.Spec.Service.Type)
}

func NewService(n *v1alpha1.Rokku) *corev1.Service {
	var labels, annotations map[string]string
	var lbIP string
	var externalTrafficPolicy corev1.ServiceExternalTrafficPolicyType
	labelSelector := LabelsForRokku(n.Name)
	if n.Spec.Service != nil {
		labels = n.Spec.Service.Labels
		annotations = n.Spec.Service.Annotations
		lbIP = n.Spec.Service.LoadBalancerIP
		externalTrafficPolicy = n.Spec.Service.ExternalTrafficPolicy
		if n.Spec.Service.UsePodSelector != nil && !*n.Spec.Service.UsePodSelector {
			labelSelector = nil
		}
	}
	service := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.Name + "-service",
			Namespace: n.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(n, schema.GroupVersionKind{
					Group:   v1alpha1.SchemeGroupVersion.Group,
					Version: v1alpha1.SchemeGroupVersion.Version,
					Kind:    "Rokku",
				}),
			},
			Labels:      mergeMap(labels, LabelsForRokku(n.Name)),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       defaultHTTPPortName,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(defaultHTTPPortName),
					Port:       int32(80),
				},
				{
					Name:       defaultHTTPSPortName,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(defaultHTTPSPortName),
					Port:       int32(443),
				},
			},
			Selector:              labelSelector,
			LoadBalancerIP:        lbIP,
			Type:                  rokkuService(n),
			ExternalTrafficPolicy: externalTrafficPolicy,
		},
	}
	return &service
}
