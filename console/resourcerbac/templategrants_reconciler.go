package resourcerbac

import (
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

func SetupTemplateGrantReconciler(mgr ctrl.Manager, kube kubernetes.Interface) error {
	return setup(mgr, kube, TemplateGrants)
}
