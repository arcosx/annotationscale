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
				return reconcile.Result{RequeueAfter: 5 * time.Second}, err
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
				newLastUpdateTime := time.Now()
				r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s,change last update time: %s --> %s",
					deployment.Name, scaleAnnotation.CurrentStepState, StepStateCompleted, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
				scaleAnnotation.CurrentStepState = StepStateCompleted
				scaleAnnotation.LastUpdateTime = newLastUpdateTime
			} else {
				newLastUpdateTime := time.Now()
				r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s,change last update time: %s --> %s",
					deployment.Name, scaleAnnotation.CurrentStepState, StepStateReady, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
				scaleAnnotation.CurrentStepState = StepStateReady
				scaleAnnotation.LastUpdateTime = newLastUpdateTime
			}

		} else {
			now := time.Now()
			stepDeadline := scaleAnnotation.StepDeadline()
			if now.Before(stepDeadline) {
				r.log.V(4).Info(fmt.Sprintf("deployment %s upgrading now....status.Replicas(%d) status.AvailableReplicas(%d) ", deployment.Name, deployment.Status.Replicas, deployment.Status.AvailableReplicas))
				return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
			} else {
				r.log.V(4).Info(fmt.Sprintf("deployment %s touch step deadline!", deployment.Name), "from", stepDeadline.String(), "duration seconds", now.Sub(stepDeadline).Seconds())
				if deployment.Status.UnavailableReplicas > int32(scaleAnnotation.MaxUnavailableReplicas) {
					r.log.V(4).Info(fmt.Sprintf("deployment %s touch step deadline!", deployment.Name),
						fmt.Sprintf("the unavailable replicas %d is [more than] maxUnavailableReplicas %d ",
							deployment.Status.UnavailableReplicas,
							scaleAnnotation.MaxUnavailableReplicas))
					newLastUpdateTime := time.Now()
					r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s,change last update time: %s --> %s",
						deployment.Name, scaleAnnotation.CurrentStepState, StepStateTimeout, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
					scaleAnnotation.CurrentStepState = StepStateTimeout
					scaleAnnotation.LastUpdateTime = newLastUpdateTime
				} else {
					// when timeout, but the unavailable replicas is less than maxUnavailableReplicas, we think it is completed
					r.log.V(4).Info(fmt.Sprintf("deployment %s touch step deadline!", deployment.Name),
						fmt.Sprintf("the unavailable replicas %d is [less than] maxUnavailableReplicas %d ",
							deployment.Status.UnavailableReplicas,
							scaleAnnotation.MaxUnavailableReplicas))

					if scaleAnnotation.CurrentStepIndex == len(scaleAnnotation.Steps) {
						newLastUpdateTime := time.Now()
						r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s,change last update time: %s --> %s",
							deployment.Name, scaleAnnotation.CurrentStepState, StepStateCompleted, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
						scaleAnnotation.CurrentStepState = StepStateCompleted
						scaleAnnotation.LastUpdateTime = newLastUpdateTime
					} else {
						newLastUpdateTime := time.Now()
						r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s,change last update time: %s --> %s",
							deployment.Name, scaleAnnotation.CurrentStepState, StepStateReady, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
						scaleAnnotation.CurrentStepState = StepStateReady
						scaleAnnotation.LastUpdateTime = newLastUpdateTime
					}
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
			newLastUpdateTime := time.Now()
			r.log.V(4).Info(fmt.Sprintf("deployment %s is paused and set spec.paused true,,change last update time: %s --> %s",
				deployment.Name, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
			deployment.Spec.Paused = true
			scaleAnnotation.LastUpdateTime = time.Now()
		} else {
			now := time.Now()
			stepDeadline := scaleAnnotation.StepDeadline()
			if now.Before(stepDeadline) {
				r.log.V(4).Info(fmt.Sprintf("deployment %s upgrading to pause point now....status.Replicas(%d) status.AvailableReplicas(%d) ",
					deployment.Name, deployment.Status.Replicas,
					deployment.Status.AvailableReplicas))
				return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
			} else {
				r.log.V(4).Info(fmt.Sprintf("deployment %s touch step deadline!", deployment.Name), "from", stepDeadline.String(), "duration seconds", now.Sub(stepDeadline).Seconds())
				if deployment.Status.UnavailableReplicas > int32(scaleAnnotation.MaxUnavailableReplicas) {
					r.log.V(4).Info(fmt.Sprintf("deployment %s touch step deadline!", deployment.Name),
						fmt.Sprintf("the unavailable replicas %d is [more than] maxUnavailableReplicas %d ",
							deployment.Status.UnavailableReplicas,
							scaleAnnotation.MaxUnavailableReplicas))
					newLastUpdateTime := time.Now()
					r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s,change last update time: %s --> %s",
						deployment.Name, scaleAnnotation.CurrentStepState, StepStateTimeout, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
					scaleAnnotation.CurrentStepState = StepStateTimeout
					scaleAnnotation.LastUpdateTime = newLastUpdateTime
				} else {
					// when timeout, but the unavailable replicas is less than maxUnavailableReplicas, we think it is completed
					r.log.V(4).Info(fmt.Sprintf("deployment %s touch step deadline!", deployment.Name),
						fmt.Sprintf("the unavailable replicas %d is [less than] maxUnavailableReplicas %d ",
							deployment.Status.UnavailableReplicas,
							scaleAnnotation.MaxUnavailableReplicas))
					if deployment.Spec.Paused {
						r.log.V(4).Info(fmt.Sprintf("deployment %s is paused, do not need set", deployment.Name))
						return reconcile.Result{}, nil
					}
					newLastUpdateTime := time.Now()
					r.log.V(4).Info(fmt.Sprintf("deployment %s is paused and set spec.paused true,,change last update time: %s --> %s",
						deployment.Name, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
					deployment.Spec.Paused = true
					scaleAnnotation.LastUpdateTime = newLastUpdateTime
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
				return reconcile.Result{RequeueAfter: 5 * time.Second}, err
			}
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// handle out of index
		if scaleAnnotation.CurrentStepIndex == len(scaleAnnotation.Steps) {
			newLastUpdateTime := time.Now()
			r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s,change last update time: %s --> %s",
				deployment.Name, scaleAnnotation.CurrentStepState, StepStateCompleted, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
			scaleAnnotation.CurrentStepState = StepStateCompleted
			scaleAnnotation.LastUpdateTime = newLastUpdateTime
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

		newLastUpdateTime := time.Now()
		if nextStep.Pause {
			r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s,change last update time: %s --> %s",
				deployment.Name, scaleAnnotation.CurrentStepState, StepStatePaused, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
			scaleAnnotation.CurrentStepState = StepStatePaused
		} else {
			r.log.V(4).Info(fmt.Sprintf("deployment %s change step state: %s --> %s,change last update time: %s --> %s",
				deployment.Name, scaleAnnotation.CurrentStepState, StepStateUpgrade, scaleAnnotation.LastUpdateTime, newLastUpdateTime))
			scaleAnnotation.CurrentStepState = StepStateUpgrade
		}
		scaleAnnotation.LastUpdateTime = newLastUpdateTime

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

	case StepStateTimeout:
		if *deployment.Spec.Replicas != scaleAnnotation.Steps[scaleAnnotation.CurrentStepIndex-1].Replicas {
			r.fixDeploymentReplicas(ctx, deployment, scaleAnnotation)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}
		r.log.V(4).Info(fmt.Sprintf("deployment %s scale timeout", deployment.Name))
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
