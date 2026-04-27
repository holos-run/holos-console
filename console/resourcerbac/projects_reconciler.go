package resourcerbac

import (
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

func SetupProjectReconciler(mgr ctrl.Manager, kube kubernetes.Interface) error {
	return setup(mgr, kube, Projects)
}
