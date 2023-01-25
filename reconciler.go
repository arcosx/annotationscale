package annotationscale

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
)

type DeploymentReconciler struct {
	client.Client
	log logr.Logger
}

// This function will be called when there is a change to a Deployment or a ReplicaSet or a Pod with an OwnerReference
// to a Deployment.
func (r *DeploymentReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, req.NamespacedName, deployment)
	if err != nil {
		if kerrors.IsNotFound(err) {
			r.log.Info("deployment resource not found. Ignoring since object must be deleted")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	scaleAnnotation, err := ReadScaleAnnotation(deployment.Annotations)

	if err != nil {
		if errors.Is(err, ErrorScaleAnnotationParseSteps) ||
			errors.Is(err, ErrorScaleAnnotationParseCurrentStepIndex) ||
			errors.Is(err, ErrorScaleAnnotationParseCurrentStepState) {
			return reconcile.Result{}, nil
		} else {
			return reconcile.Result{}, err
		}
	}

	// In a nutshell the difference is actual state vs desired state.
	if deployment.Status.Replicas != *deployment.Spec.Replicas {
		return reconcile.Result{}, fmt.Errorf("waiting for rollout to finish: %d out of %d new replicas have been updated",
			deployment.Status.Replicas, *deployment.Spec.Replicas)
	}

	r.log.Info(fmt.Sprintf("deployment %s ", deployment.Name),
		"spec.replicas", deployment.Spec.Replicas,
		"status.replicas", deployment.Status.Replicas,
		"status.available-replicas", deployment.Status.AvailableReplicas,
		"status.unavailable-replicas", deployment.Status.UnavailableReplicas,
		"status.ready-replicas", deployment.Status.ReadyReplicas,
		"status.updated-replicas", deployment.Status.UpdatedReplicas,
		"current_step_index", scaleAnnotation.CurrentStepIndex,
		"current_step_state", scaleAnnotation.CurrentStepState,
		"step", scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1],
		"max_wait_available_second", scaleAnnotation.MaxWaitAvailableSecond,
		"max_unavailable_replicas", scaleAnnotation.MaxUnavailableReplicas,
		"last_update_time", scaleAnnotation.LastUpdateTime,
	)

	switch scaleAnnotation.CurrentStepState {
	case StepStateUpgrade:
		if *deployment.Spec.Replicas != scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas {
			r.fixDeploymentReplicas(ctx, deployment, scaleAnnotation)
			return reconcile.Result{}, nil
		}

		if deployment.Status.Replicas == deployment.Status.AvailableReplicas {
			if scaleAnnotation.CurrentStepIndex == len(scaleAnnotation.Steps) {
				// if deployment.Status.Replicas == scaleAnnotation.Steps[len(scaleAnnotation.Steps)-1].Replicas {
				r.log.Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStateCompleted))
				scaleAnnotation.CurrentStepState = StepStateCompleted
			} else {
				r.log.Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStateReady))
				scaleAnnotation.CurrentStepState = StepStateReady
			}
			scaleAnnotation.LastUpdateTime = time.Now()

		} else {
			now := time.Now()
			stepDeadline := scaleAnnotation.StepDeadline()
			if now.Before(stepDeadline) {
				r.log.Info(fmt.Sprintf("deployment %s upgrading now....", deployment.Name))
			} else {
				r.log.Info(fmt.Sprintf("deployment %s deadline", deployment.Name), "from", stepDeadline.String(), "duration seconds", now.Sub(stepDeadline).Seconds())
				if deployment.Status.UnavailableReplicas > int32(scaleAnnotation.MaxUnavailableReplicas) {
					scaleAnnotation.CurrentStepState = StepStateError
				}
			}
		}

		err = SetDeploymentScaleAnnotation(deployment, scaleAnnotation)
		if err != nil {
			r.log.Error(err, fmt.Sprintf("deployment %s failed set scale annotation", deployment.Name))
			return reconcile.Result{}, err
		}

		err = r.patchDeployment(ctx, deployment)

		if err != nil {
			r.log.V(1).Error(err, "patchAnnotations failed")
			return reconcile.Result{}, err
		}

	case StepStatePaused:
		if deployment.Status.Replicas != scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas {
			r.fixDeploymentReplicas(ctx, deployment, scaleAnnotation)
			return reconcile.Result{}, nil
		}

		if deployment.Status.Replicas == deployment.Status.AvailableReplicas {
			deployment.Spec.Paused = true
			scaleAnnotation.LastUpdateTime = time.Now()
			r.log.Info(fmt.Sprintf("deployment %s is paused set spec.paused true", deployment.Name))
		} else {
			r.log.Info(fmt.Sprintf("deployment %s upgrading to pause point....", deployment.Name))
		}

		err = SetDeploymentScaleAnnotation(deployment, scaleAnnotation)
		if err != nil {
			r.log.Error(err, fmt.Sprintf("deployment %s failed set scale annotation", deployment.Name))
			return reconcile.Result{}, err
		}

		err = r.patchDeployment(ctx, deployment)

		if err != nil {
			r.log.Error(err, fmt.Sprintf("deployment %s failed to patch", deployment.Name))
			return reconcile.Result{}, err
		}

	case StepStateReady:
		if *deployment.Spec.Replicas != scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas {
			r.fixDeploymentReplicas(ctx, deployment, scaleAnnotation)
			return reconcile.Result{}, nil
		}

		nextStepIndex := scaleAnnotation.CurrentStepIndex + 1
		nextStep := scaleAnnotation.Steps[nextStepIndex-1]

		r.log.Info(fmt.Sprintf("deployment %s change:", deployment.Name),
			"replicas", fmt.Sprintf("%d --> %d", *deployment.Spec.Replicas, nextStep.Replicas),
			"step index", fmt.Sprintf("%d --> %d", scaleAnnotation.CurrentStepIndex, nextStepIndex),
			"step", fmt.Sprintf("%s --> %s", scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1], nextStep),
		)

		deployment.Spec.Replicas = &nextStep.Replicas
		scaleAnnotation.CurrentStepIndex = nextStepIndex

		if nextStep.Pause {
			r.log.Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStatePaused))
			scaleAnnotation.CurrentStepState = StepStatePaused
		} else {
			r.log.Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStateUpgrade))
			scaleAnnotation.CurrentStepState = StepStateUpgrade
		}
		scaleAnnotation.LastUpdateTime = time.Now()

		err = SetDeploymentScaleAnnotation(deployment, scaleAnnotation)
		if err != nil {
			r.log.Error(err, fmt.Sprintf("deployment %s failed set scale annotation", deployment.Name))
			return reconcile.Result{}, err
		}

		err = r.patchDeployment(ctx, deployment)
		if err != nil {
			r.log.V(1).Error(err, "patchAnnotations failed")
			return reconcile.Result{}, err
		}

	case StepStateCompleted:
		if *deployment.Spec.Replicas != scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas {
			r.fixDeploymentReplicas(ctx, deployment, scaleAnnotation)
			return reconcile.Result{}, nil
		}

		r.log.Info(fmt.Sprintf("deployment %s scale success", deployment.Name))

	case StepStateError:
		deployment.Spec.Paused = true
		err = r.patchDeployment(ctx, deployment)

		if err != nil {
			r.log.V(1).Error(err, "patchAnnotations failed")
			return reconcile.Result{}, err
		}
		r.log.Info(fmt.Sprintf("deployment %s is paused set spec.paused true ", deployment.Name))
	}
	return reconcile.Result{}, nil
}

func (r *DeploymentReconciler) InjectClient(c client.Client) error {
	r.Client = c
	return nil
}

func (r *DeploymentReconciler) patchDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
	latest := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKeyFromObject(deployment), latest)
	if err != nil {
		return err
	}
	patch := client.MergeFrom(latest.DeepCopy())

	latest.SetAnnotations(deployment.Annotations)
	latest.Spec.Replicas = deployment.Spec.Replicas
	latest.Spec.Paused = deployment.Spec.Paused

	return r.Client.Patch(ctx, latest, patch, &client.PatchOptions{})
}

func (r *DeploymentReconciler) fixDeploymentReplicas(ctx context.Context, deployment *appsv1.Deployment, scaleAnnotation *ScaleAnnotation) error {
	r.log.Info(fmt.Sprintf("deployment %s replicas fix from: %s , %d --> %d", deployment.Name, scaleAnnotation.CurrentStepState, &deployment.Spec.Replicas, scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas))

	*deployment.Spec.Replicas = scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas

	if scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Pause {
		r.log.Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStatePaused))
		scaleAnnotation.CurrentStepState = StepStatePaused
	} else {
		r.log.Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStateUpgrade))
		scaleAnnotation.CurrentStepState = StepStateUpgrade
	}

	scaleAnnotation.LastUpdateTime = time.Now()
	fixedAnnotation, err := SetScaleAnnotation(deployment.Annotations, scaleAnnotation)
	if err != nil {
		log.Fatal(err)
	}

	deployment.SetAnnotations(fixedAnnotation)
	err = r.patchDeployment(ctx, deployment)
	if err != nil {
		r.log.V(1).Error(err, "patchAnnotations failed")
		return err
	}
	return nil
}
