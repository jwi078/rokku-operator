package rokku

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	rokkuv1alpha1 "github.com/jwi078/rokku-operator/pkg/apis/rokku/v1alpha1"
	"github.com/jwi078/rokku-operator/pkg/k8s"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_rokku")

// Add creates a new Rokku Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileRokku{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("rokku-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Rokku
	err = c.Watch(&source.Kind{Type: &rokkuv1alpha1.Rokku{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// HACK(nettoclaudio): Since the Rokku needs store all its pods' info into
	// the status field, we need watching every pod changes and enqueue a new
	// reconcile request to its Rokku owner, if any.
	return c.Watch(&source.Kind{Type: &corev1.Pod{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(o handler.MapObject) []reconcile.Request {
				rokkuResourceName := k8s.GetRokkuNameFromObject(o.Meta)
				if rokkuResourceName == "" {
					return nil
				}

				return []reconcile.Request{
					{NamespacedName: types.NamespacedName{
						Name:      rokkuResourceName,
						Namespace: o.Meta.GetNamespace(),
					}},
				}
			}),
		},
	)
}

var _ reconcile.Reconciler = &ReconcileRokku{}

// ReconcileRokku reconciles a Rokku object
type ReconcileRokku struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Rokku object and makes changes based on the state read
// and what is in the Rokku.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileRokku) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Rokku", request)
	reqLogger.Info("Starting Rokku reconciling")
	defer reqLogger.Info("Finishing Rokku reconciling")

	ctx := context.Background()

	instance := &rokkuv1alpha1.Rokku{}
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Rokku resource not found, skipping reconcile")
			return reconcile.Result{}, nil
		}

		reqLogger.Error(err, "Unable to get Rokku resource")
		return reconcile.Result{}, err
	}

	if err := r.reconcileRokku(ctx, instance); err != nil {
		reqLogger.Error(err, "Fail to reconcile")
		return reconcile.Result{}, err
	}

	if err := r.refreshStatus(ctx, instance); err != nil {
		reqLogger.Error(err, "Fail to refresh status subresource")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileRokku) reconcileRokku(ctx context.Context, rokku *rokkuv1alpha1.Rokku) error {
	if err := r.reconcileDeployment(ctx, rokku); err != nil {
		return err
	}

	if err := r.reconcileService(ctx, rokku); err != nil {
		return err
	}

	return nil
}

func (r *ReconcileRokku) reconcileDeployment(ctx context.Context, rokku *rokkuv1alpha1.Rokku) error {
	newDeploy, err := k8s.NewDeployment(rokku)
	if err != nil {
		return fmt.Errorf("failed to assemble deployment from Rokku: %v", err)
	}

	err = r.client.Create(ctx, newDeploy)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create deployment: %v", err)
	}

	if err == nil {
		return nil
	}

	currDeploy := &appv1.Deployment{}

	err = r.client.Get(ctx, types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, currDeploy)
	if err != nil {
		return fmt.Errorf("failed to retrieve deployment: %v", err)
	}

	currSpec, err := k8s.ExtractRokkuSpec(currDeploy.ObjectMeta)
	if err != nil {
		return fmt.Errorf("failed to extract rokku from deployment: %v", err)
	}

	if reflect.DeepEqual(rokku.Spec, currSpec) {
		return nil
	}

	currDeploy.Spec = newDeploy.Spec
	if err := k8s.SetRokkuSpec(&currDeploy.ObjectMeta, rokku.Spec); err != nil {
		return fmt.Errorf("failed to set rokku spec into object meta: %v", err)
	}

	if err := r.client.Update(ctx, currDeploy); err != nil {
		return fmt.Errorf("failed to update deployment: %v", err)
	}

	return nil
}

func (r *ReconcileRokku) reconcileService(ctx context.Context, rokku *rokkuv1alpha1.Rokku) error {
	svcName := types.NamespacedName{
		Name:      fmt.Sprintf("%s-service", rokku.Name),
		Namespace: rokku.Namespace,
	}

	logger := log.WithName("reconcileService").WithValues("Service", svcName)
	logger.V(4).Info("Getting Service resource")

	newService := k8s.NewService(rokku)

	var currentService corev1.Service
	err := r.client.Get(ctx, svcName, &currentService)
	if err != nil && errors.IsNotFound(err) {
		logger.
			WithValues("ServiceResource", newService).V(4).Info("Creating a Service resource")

		return r.client.Create(ctx, newService)
	}

	if err != nil {
		return fmt.Errorf("failed to retrieve Service resource: %v", err)
	}

	newService.ResourceVersion = currentService.ResourceVersion
	newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	newService.Spec.HealthCheckNodePort = currentService.Spec.HealthCheckNodePort

	// avoid nodeport reallocation preserving the current ones
	for _, currentPort := range currentService.Spec.Ports {
		for index, newPort := range newService.Spec.Ports {
			if currentPort.Port == newPort.Port {
				newService.Spec.Ports[index].NodePort = currentPort.NodePort
			}
		}
	}

	logger.WithValues("ServiceResource", newService).V(4).Info("Updating Service resource")

	return r.client.Update(ctx, newService)
}

func (r *ReconcileRokku) refreshStatus(ctx context.Context, rokku *rokkuv1alpha1.Rokku) error {
	pods, err := listPods(ctx, r.client, rokku)
	if err != nil {
		return fmt.Errorf("failed to list pods for Rokku: %v", err)
	}
	services, err := listServices(ctx, r.client, rokku)
	if err != nil {
		return fmt.Errorf("failed to list services for rokku: %v", err)

	}

	sort.Slice(rokku.Status.Pods, func(i, j int) bool {
		return rokku.Status.Pods[i].Name < rokku.Status.Pods[j].Name
	})

	sort.Slice(rokku.Status.Services, func(i, j int) bool {
		return rokku.Status.Services[i].Name < rokku.Status.Services[j].Name
	})

	if !reflect.DeepEqual(pods, rokku.Status.Pods) || !reflect.DeepEqual(services, rokku.Status.Services) {
		rokku.Status.Pods = pods
		rokku.Status.Services = services
		rokku.Status.CurrentReplicas = int32(len(pods))
		rokku.Status.PodSelector = k8s.LabelsForRokkuString(rokku.Name)
		err := r.client.Status().Update(ctx, rokku)
		if err != nil {
			return fmt.Errorf("failed to update rokku status: %v", err)
		}
	}

	return nil
}

// listPods return all the pods for the given rokku sorted by name
func listPods(ctx context.Context, c client.Client, rokku *rokkuv1alpha1.Rokku) ([]rokkuv1alpha1.PodStatus, error) {
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(k8s.LabelsForRokku(rokku.Name))
	listOps := &client.ListOptions{Namespace: rokku.Namespace, LabelSelector: labelSelector}
	err := c.List(ctx, podList, listOps)
	if err != nil {
		return nil, err
	}

	var pods []rokkuv1alpha1.PodStatus

	for _, p := range podList.Items {
		if p.Status.PodIP == "" {
			p.Status.PodIP = "<pending>"
		}

		if p.Status.HostIP == "" {
			p.Status.HostIP = "<pending>"
		}

		pods = append(pods, rokkuv1alpha1.PodStatus{
			Name:   p.Name,
			PodIP:  p.Status.PodIP,
			HostIP: p.Status.HostIP,
		})
	}
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})

	return pods, nil
}

// listServices return all the services for the given rokku sorted by name
func listServices(ctx context.Context, c client.Client, rokku *rokkuv1alpha1.Rokku) ([]rokkuv1alpha1.ServiceStatus, error) {
	serviceList := &corev1.ServiceList{}
	labelSelector := labels.SelectorFromSet(k8s.LabelsForRokku(rokku.Name))
	listOps := &client.ListOptions{Namespace: rokku.Namespace, LabelSelector: labelSelector}
	err := c.List(ctx, serviceList, listOps)
	if err != nil {
		return nil, err
	}

	var services []rokkuv1alpha1.ServiceStatus
	for _, s := range serviceList.Items {
		services = append(services, rokkuv1alpha1.ServiceStatus{
			Name: s.Name,
		})
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return services, nil
}
