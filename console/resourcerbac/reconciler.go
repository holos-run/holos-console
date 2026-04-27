package resourcerbac

import (
	"context"
	"fmt"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reconciler keeps per-resource RBAC objects in sync for one
// templates.holos.run kind.
type Reconciler struct {
	Client client.Client
	Kube   kubernetes.Interface
	Config KindConfig
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj, ok := r.Config.NewObject().(client.Object)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("%s RBAC object does not implement client.Object", r.Config.Kind)
	}
	if err := r.Client.Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if err := EnsureResourceRBAC(ctx, r.Kube, obj, r.Config); err != nil {
		return ctrl.Result{}, err
	}
	if requeueAfter := NextGrantRequeueAfter(obj, time.Now()); requeueAfter > 0 {
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	obj, ok := r.Config.NewObject().(client.Object)
	if !ok {
		return fmt.Errorf("%s RBAC object does not implement client.Object", r.Config.Kind)
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(r.Config.ControllerName).
		For(obj).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Complete(r)
}

func setup(mgr ctrl.Manager, kube kubernetes.Interface, cfg KindConfig) error {
	return (&Reconciler{
		Client: mgr.GetClient(),
		Kube:   kube,
		Config: cfg,
	}).SetupWithManager(mgr)
}
