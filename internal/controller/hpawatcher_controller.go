package controller

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
)

type HPAWatcherReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *HPAWatcherReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the HPA instance
	var hpa autoscalingv1.HorizontalPodAutoscaler
	if err := r.Get(ctx, req.NamespacedName, &hpa); err != nil {
		if errors.IsNotFound(err) {
			log.Info("HPA resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get HPA.")
		return ctrl.Result{}, err
	}
	name := hpa.ObjectMeta.Name
	advancedConfig := &kedav1alpha1.AdvancedConfig{
		//RestoreToOriginalReplicaCount: true,
		HorizontalPodAutoscalerConfig: &kedav1alpha1.HorizontalPodAutoscalerConfig{
			Name: name,
		},
	}

	// Check for the specific annotation
	if hpa.Annotations["transfer-hpa"] == "true" && hpa.Annotations["app.kubernetes.io/managed-by"] != "true" {
		// Define a new ScaledObject
		scaledObject := &kedav1alpha1.ScaledObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hpa.Name + "-scaledobject",
				Namespace: hpa.Namespace,
				Annotations: map[string]string{
					"scaledobject.keda.sh/transfer-hpa-ownership": "true",
					"validations.keda.sh/hpa-ownership":           "true",
				},
			},
			Spec: kedav1alpha1.ScaledObjectSpec{
				ScaleTargetRef: &kedav1alpha1.ScaleTarget{
					Name:       hpa.Spec.ScaleTargetRef.Name,
					APIVersion: hpa.Spec.ScaleTargetRef.APIVersion,
					Kind:       hpa.Spec.ScaleTargetRef.Kind,
				},
				MaxReplicaCount: toInt32Ptr(int(hpa.Spec.MaxReplicas)),
				MinReplicaCount: hpa.Spec.MinReplicas,
				Advanced:        advancedConfig,
				Triggers:        []kedav1alpha1.ScaleTriggers{},
				// Add additional ScaledObject configuration
			},
		}

		// Set HPA instance as the owner and controller
		if err := controllerutil.SetControllerReference(&hpa, scaledObject, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		// Check if the ScaledObject already exists
		found := &kedav1alpha1.ScaledObject{}
		err := r.Get(ctx, types.NamespacedName{Name: scaledObject.Name, Namespace: scaledObject.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			log.Info("Creating a new ScaledObject", "Namespace", scaledObject.Namespace, "Name", scaledObject.Name)
			err = r.Create(ctx, scaledObject)
			if err != nil {
				fmt.Println("!!!!!!!!!!!!!!!!")
				log.Error(err, "Failed to create new ScaledObject", "Namespace", scaledObject.Namespace, "Name", scaledObject.Name)
				return ctrl.Result{}, err
			}
			// ScaledObject created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			log.Error(err, "Failed to get ScaledObject.")
			return ctrl.Result{}, err
		}

		// ScaledObject already exists - don't requeue
		log.Info("ScaledObject already exists", "Namespace", found.Namespace, "Name", found.Name)
	}

	return ctrl.Result{}, nil
}

func (r *HPAWatcherReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1.HorizontalPodAutoscaler{}).
		Complete(r)
}

func toInt32Ptr(i int) *int32 {
	i32 := int32(i)
	return &i32
}
