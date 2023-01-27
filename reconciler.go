package annotationscale

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
)

type DeploymentReconciler struct {
	client.Client
	log *logr.Logger
}

// This function will be called when there is a change to a Deployment or a ReplicaSet or a Pod with an OwnerReference
// to a Deployment.
func (r *DeploymentReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	r.log.V(4).Info("Reconcile", "request", req)
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, req.NamespacedName, deployment)
	if err != nil {
		if kerrors.IsNotFound(err) {
			r.log.Info("deployment resource not found. Ignoring since object must be deleted")
			return reconcile.Result{}, nil
		}
		r.log.Error(err, fmt.Sprintf("failed to get deployment %s", req.Name))
		return reconcile.Result{}, err
	}

	scaleAnnotation, err := ReadScaleAnnotation(deployment.Annotations)

	if err != nil {
		if errors.Is(err, ErrorScaleAnnotationParseSteps) ||
			errors.Is(err, ErrorScaleAnnotationParseCurrentStepIndex) ||
			errors.Is(err, ErrorScaleAnnotationParseCurrentStepState) {
			r.log.V(4).Info("failed to parse scale annotation", "error", err)
			return reconcile.Result{}, nil
		} else {
			r.log.V(5).Error(err, "failed to parse scale annotation")
			return reconcile.Result{}, err
		}
	}

	r.log.V(4).Info(fmt.Sprintf("deployment %s ", deployment.Name),
		"spec.paused", deployment.Spec.Paused,
		"spec.replicas", *deployment.Spec.Replicas,
		"status.replicas", deployment.Status.Replicas,
		"status.available-replicas", deployment.Status.AvailableReplicas,
		"status.unavailable-replicas", deployment.Status.UnavailableReplicas,
		"status.ready-replicas", deployment.Status.ReadyReplicas,
		"status.updated-replicas", deployment.Status.UpdatedReplicas,
	)

	r.log.V(4).Info(scaleAnnotation.String())

	switch scaleAnnotation.CurrentStepState {
	case StepStateUpgrade:
		if *deployment.Spec.Replicas != scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas {
			r.fixDeploymentReplicas(ctx, deployment, scaleAnnotation)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// Spec.Paused in StepUpgrade Status must be false
		if deployment.Spec.Paused {
			deployment.Spec.Paused = false
			r.log.V(4).Info(fmt.Sprintf("deployment %s is paused and set spec.paused false", deployment.Name))
			err = r.patchDeployment(ctx, deployment)
			if err != nil {
				r.log.Error(err, fmt.Sprintf("deployment %s failed to patch", deployment.Name))
				return reconcile.Result{}, err
			}
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if deployment.Status.Replicas != *deployment.Spec.Replicas {
			r.log.V(5).Info(fmt.Sprintf("deployment %s waiting for rollout to finish: %d out of %d new replicas have been updated",
				deployment.Name, deployment.Status.Replicas, *deployment.Spec.Replicas))
			return reconcile.Result{RequeueAfter: 5 * time.Second}, fmt.Errorf("waiting for rollout to finish: %d out of %d new replicas have been updated",
				deployment.Status.Replicas, *deployment.Spec.Replicas)
		}

		if deployment.Status.Replicas == deployment.Status.AvailableReplicas {
			if scaleAnnotation.CurrentStepIndex == len(scaleAnnotation.Steps) {
				// if deployment.Status.Replicas == scaleAnnotation.Steps[len(scaleAnnotation.Steps)-1].Replicas {
				r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStateCompleted))
				scaleAnnotation.CurrentStepState = StepStateCompleted
			} else {
				r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStateReady))
				scaleAnnotation.CurrentStepState = StepStateReady
			}
			scaleAnnotation.LastUpdateTime = time.Now()

		} else {
			now := time.Now()
			stepDeadline := scaleAnnotation.StepDeadline()
			if now.Before(stepDeadline) {
				r.log.V(4).Info(fmt.Sprintf("deployment %s upgrading now....status.Replicas(%d) status.AvailableReplicas(%d) ", deployment.Name, deployment.Status.Replicas, deployment.Status.AvailableReplicas))
				return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
			} else {
				r.log.V(4).Info(fmt.Sprintf("deployment %s deadline", deployment.Name), "from", stepDeadline.String(), "duration seconds", now.Sub(stepDeadline).Seconds())
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
			r.log.Error(err, "patchAnnotations failed")
			return reconcile.Result{}, err
		}

	case StepStatePaused:
		if *deployment.Spec.Replicas != scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas {
			r.fixDeploymentReplicas(ctx, deployment, scaleAnnotation)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if deployment.Status.Replicas != *deployment.Spec.Replicas {
			r.log.V(4).Info(fmt.Sprintf("deployment %s waiting for rollout to finish: %d out of %d new replicas have been updated",
				deployment.Name, deployment.Status.Replicas, *deployment.Spec.Replicas))
			return reconcile.Result{RequeueAfter: 5 * time.Second}, fmt.Errorf("waiting for rollout to finish: %d out of %d new replicas have been updated",
				deployment.Status.Replicas, *deployment.Spec.Replicas)
		}

		if deployment.Status.Replicas == deployment.Status.AvailableReplicas {
			if deployment.Spec.Paused {
				r.log.V(4).Info(fmt.Sprintf("deployment %s is paused, do not need set", deployment.Name))
				return reconcile.Result{}, nil
			}
			deployment.Spec.Paused = true
			scaleAnnotation.LastUpdateTime = time.Now()
			r.log.V(4).Info(fmt.Sprintf("deployment %s is paused and set spec.paused true", deployment.Name))
		} else {
			r.log.V(4).Info(fmt.Sprintf("deployment %s upgrading to pause point....", deployment.Name))
			return reconcile.Result{RequeueAfter: 5 * time.Second}, fmt.Errorf("deployment %s wait status.AvailableReplicas %d to status.Replicas %d",
				deployment.Name,
				deployment.Status.AvailableReplicas,
				deployment.Status.Replicas,
			)
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
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// Spec.Paused in StepReady Status must be false
		if deployment.Spec.Paused {
			r.log.V(4).Info(fmt.Sprintf("deployment %s is paused and set spec.paused false", deployment.Name))
			deployment.Spec.Paused = false
			err = r.patchDeployment(ctx, deployment)
			if err != nil {
				r.log.Error(err, fmt.Sprintf("deployment %s failed to patch", deployment.Name))
				return reconcile.Result{}, err
			}
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// handle out of index
		if scaleAnnotation.CurrentStepIndex == len(scaleAnnotation.Steps) {
			r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s when currentStepIndex %d equal len(scaleAnnotation.Steps)", deployment.Name, scaleAnnotation.CurrentStepState, StepStateCompleted, scaleAnnotation.CurrentStepIndex))
			scaleAnnotation.CurrentStepState = StepStateCompleted
			scaleAnnotation.LastUpdateTime = time.Now()
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
			return reconcile.Result{}, nil
		}

		nextStepIndex := scaleAnnotation.CurrentStepIndex + 1
		nextStep := scaleAnnotation.Steps[nextStepIndex-1]

		r.log.V(4).Info(fmt.Sprintf("deployment %s change:", deployment.Name),
			"replicas", fmt.Sprintf("%d --> %d", *deployment.Spec.Replicas, nextStep.Replicas),
			"step index", fmt.Sprintf("%d --> %d", scaleAnnotation.CurrentStepIndex, nextStepIndex),
			"step", fmt.Sprintf("%s --> %s", scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1], nextStep),
		)

		deployment.Spec.Replicas = &nextStep.Replicas
		scaleAnnotation.CurrentStepIndex = nextStepIndex

		if nextStep.Pause {
			r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStatePaused))
			scaleAnnotation.CurrentStepState = StepStatePaused
		} else {
			r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStateUpgrade))
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
			r.log.Error(err, fmt.Sprintf("deployment %s failed to patch", deployment.Name))
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil

	case StepStateCompleted:
		if *deployment.Spec.Replicas != scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas {
			r.fixDeploymentReplicas(ctx, deployment, scaleAnnotation)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		r.log.V(4).Info(fmt.Sprintf("deployment %s scale success", deployment.Name))
		return reconcile.Result{}, nil

	case StepStateError:
		r.log.V(2).Info(fmt.Sprintf("deployment %s scale error", deployment.Name))
		deployment.Spec.Paused = true
		err = r.patchDeployment(ctx, deployment)

		if err != nil {
			r.log.Error(err, fmt.Sprintf("deployment %s failed to patch", deployment.Name))
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func (r *DeploymentReconciler) InjectClient(c client.Client) error {
	r.Client = c
	return nil
}

func (r *DeploymentReconciler) patchDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
	r.log.V(5).Info(fmt.Sprintf("deployment %s patch deployment", deployment.Name), "deployment", deployment)
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
	r.log.V(2).Info(fmt.Sprintf("deployment %s replicas fix in state: %s , %d --> %d", deployment.Name, scaleAnnotation.CurrentStepState, &deployment.Spec.Replicas, scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas))

	*deployment.Spec.Replicas = scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas

	if scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Pause {
		r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStatePaused))
		scaleAnnotation.CurrentStepState = StepStatePaused
	} else {
		r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s", deployment.Name, scaleAnnotation.CurrentStepState, StepStateUpgrade))
		scaleAnnotation.CurrentStepState = StepStateUpgrade
	}

	scaleAnnotation.LastUpdateTime = time.Now()
	err := SetDeploymentScaleAnnotation(deployment, scaleAnnotation)
	if err != nil {
		r.log.Error(err, fmt.Sprintf("deployment %s failed set scale annotation", deployment.Name))
		return err
	}
	err = r.patchDeployment(ctx, deployment)
	if err != nil {
		r.log.V(1).Error(err, fmt.Sprintf("deployment %s patch failed", deployment.Name))
		return err
	}
	return nil
}
