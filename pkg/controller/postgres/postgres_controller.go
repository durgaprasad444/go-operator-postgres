package postgres

import (
    "context"
    "reflect"

    appv1alpha1 "github.com/postgres/postgres-operator/pkg/apis/app/v1alpha1"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/labels"
    "k8s.io/apimachinery/pkg/runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/controller"
    "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
    "sigs.k8s.io/controller-runtime/pkg/handler"
    logf "sigs.k8s.io/controller-runtime/pkg/log"
    "sigs.k8s.io/controller-runtime/pkg/manager"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"
    "sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_postgres")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Postgres Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
    return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
    return &ReconcilePostgres{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
    // Create a new controller
    c, err := controller.New("postgres-controller", mgr, controller.Options{Reconciler: r})
    if err != nil {
        return err
    }

    // Watch for changes to primary resource Postgres
    err = c.Watch(&source.Kind{Type: &appv1alpha1.Postgres{}}, &handler.EnqueueRequestForObject{})
    if err != nil {
        return err
    }

    // TODO(user): Modify this to be the types you create that are owned by the primary resource
    // Watch for changes to secondary resource Pods and requeue the owner Postgres
    err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
        IsController: true,
        OwnerType:    &appv1alpha1.Postgres{},
    })
    if err != nil {
        return err
    }

    return nil
}

// blank assignment to verify that ReconcilePostgres implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcilePostgres{}

// ReconcilePostgres reconciles a Postgres object
type ReconcilePostgres struct {
    // This client, initialized using mgr.Client() above, is a split client
    // that reads objects from the cache and writes to the apiserver
    client client.Client
    scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Postgres object and makes changes based on the state read
// and what is in the Postgres.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcilePostgres) Reconcile(request reconcile.Request) (reconcile.Result, error) {
    reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
    reqLogger.Info("Reconciling Postgres")

    // Fetch the Postgres instance
    instance := &appv1alpha1.Postgres{}
    err := r.client.Get(context.TODO(), request.NamespacedName, instance)
    if err != nil {
        if errors.IsNotFound(err) {
            // Request object not found, could have been deleted after reconcile request.
            // Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
            // Return and don't requeue
            return reconcile.Result{}, nil
        }
        // Error reading the object - requeue the request.
        return reconcile.Result{}, err
    }
        // List all pods owned by this Postgres instance
    podSet := instance
        podList := &corev1.PodList{}
        lbs := map[string]string{
        "app":     podSet.Name,
        "version": "v0.1",
}
        labelSelector := labels.SelectorFromSet(lbs)
        listOps := &client.ListOptions{Namespace: podSet.Namespace, LabelSelector: labelSelector}
        if err = r.client.List(context.TODO(), podList, listOps); err != nil {
                return reconcile.Result{}, err
}



    // Count the pods that are pending or running as available
    var available []corev1.Pod
    for _, pod := range podList.Items {
        if pod.ObjectMeta.DeletionTimestamp != nil {
            continue
        }
        if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
            available = append(available, pod)
        }
    }
    numAvailable := int32(len(available))
    availableNames := []string{}
    for _, pod := range available {
        availableNames = append(availableNames, pod.ObjectMeta.Name)
    }



    // Update the status if necessary
    status := appv1alpha1.PostgresStatus{
        PodNames: availableNames,
    }
    if !reflect.DeepEqual(podSet.Status, status) {
        podSet.Status = status
        err = r.client.Status().Update(context.TODO(), podSet)
        if err != nil {
            reqLogger.Error(err, "Failed to update Postgres status")
            return reconcile.Result{}, err
        }
    }




    if numAvailable > podSet.Spec.Replicas {
        reqLogger.Info("Scaling down pods", "Currently available", numAvailable, "Required replicas", podSet.Spec.Replicas)
        diff := numAvailable - podSet.Spec.Replicas
        dpods := available[:diff]
        for _, dpod := range dpods {
            err = r.client.Delete(context.TODO(), &dpod)
            if err != nil {
                reqLogger.Error(err, "Failed to delete pod", "pod.name", dpod.Name)
                return reconcile.Result{}, err
            }
        }
        return reconcile.Result{Requeue: true}, nil
    }

    if numAvailable < podSet.Spec.Replicas {
        reqLogger.Info("Scaling up pods", "Currently available", numAvailable, "Required replicas", podSet.Spec.Replicas)
        // Define a new Pod object
        pod := newPodForCR(podSet)
        // Set Postgres instance as the owner and controller
        if err := controllerutil.SetControllerReference(podSet, pod, r.scheme); err != nil {
            return reconcile.Result{}, err
        }
        err = r.client.Create(context.TODO(), pod)
        if err != nil {
            reqLogger.Error(err, "Failed to create pod", "pod.name", pod.Name)
            return reconcile.Result{}, err
        }
        return reconcile.Result{Requeue: true}, nil
    }

    return reconcile.Result{}, nil
}




// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newPodForCR(cr *appv1alpha1.Postgres) *corev1.Pod {
        labels := map[string]string{
            "app":     cr.Name,
            "version": "v0.1",
        }
        return &corev1.Pod{
            ObjectMeta: metav1.ObjectMeta{
                GenerateName: cr.Name + "-pod",
                Namespace:    cr.Namespace,
                Labels:       labels,
            },
            Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "postgres",
					Image:   "bitnami/postgresql:11.7.0-debian-10-r9",
					Ports: []corev1.ContainerPort{{
						ContainerPort: 5432,
						Name: "postgres",
					}},
					Env: []corev1.EnvVar{
						{
							Name: "POSTGRES_DB",
							Value: "wiki",
						},
						{
							Name: "POSTGRES_USER",
							Value: "postgres",
						},
						{
							Name: "POSTGRES_PASSWORD",
							Value: "password",
						},
					},
				},
			},
		},
	}
}
