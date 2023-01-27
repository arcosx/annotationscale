package annotationscale

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
)

const DefaultMaxWaitAvailableSecond = 100
const DefaultMaxUnavailableReplicas = 1

var (
	ErrorScaleAnnotationParseSteps            error = errors.New("not include steps")
	ErrorScaleAnnotationParseCurrentStepIndex error = errors.New("not include current_step_index")
	ErrorScaleAnnotationParseCurrentStepState error = errors.New("not include current_step_state")
)

type ScaleAnnotation struct {
	Steps                  []Step    `json:"steps,omitempty"`
	CurrentStepIndex       int       `json:"current_step_index,omitempty"`
	CurrentStepState       StepState `json:"current_step_state,omitempty"`
	Message                string    `json:"message,omitempty"`
	MaxWaitAvailableSecond int       `json:"max_wait_available_second,omitempty"`
	MaxUnavailableReplicas int       `json:"max_unavailable_replicas,omitempty"`
	LastUpdateTime         time.Time `json:"last_update_time,omitempty"`
}

func (sa *ScaleAnnotation) String() string {
	return fmt.Sprintf("steps: %v, current_step_index: %d, current_step_state: %s, message: %s, max_wait_available_second: %d, max_unavailable_replicas: %d, last_update_time: %s",
		sa.Steps, sa.CurrentStepIndex, sa.CurrentStepState, sa.Message, sa.MaxWaitAvailableSecond, sa.MaxUnavailableReplicas, sa.LastUpdateTime)
}

func (sa *ScaleAnnotation) StepDeadline() time.Time {
	deadline := sa.LastUpdateTime.Add(time.Duration(sa.MaxWaitAvailableSecond) * time.Second)
	return deadline
}

func NewScaleAnnotation() ScaleAnnotation {
	var scaleAnnotation ScaleAnnotation
	scaleAnnotation.Message = ""
	// 10min
	scaleAnnotation.MaxWaitAvailableSecond = 600
	scaleAnnotation.MaxUnavailableReplicas = 1
	scaleAnnotation.LastUpdateTime = time.Now()
	return scaleAnnotation
}

func SetDeploymentScaleAnnotation(deployment *appsv1.Deployment, scaleAnnotation *ScaleAnnotation) error {
	fixedAnnotation, err := SetScaleAnnotation(deployment.Annotations, scaleAnnotation)
	if err != nil {
		return err
	}
	deployment.SetAnnotations(fixedAnnotation)
	return nil
}

func SetScaleAnnotation(annotations map[string]string, scaleAnnotation *ScaleAnnotation) (map[string]string, error) {
	stepsJSONBytes, err := json.Marshal(scaleAnnotation.Steps)
	if err != nil {
		return annotations, err
	}
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["steps"] = string(stepsJSONBytes)
	annotations["current_step_index"] = strconv.Itoa(int(scaleAnnotation.CurrentStepIndex))
	annotations["current_step_state"] = string(scaleAnnotation.CurrentStepState)
	annotations["message"] = scaleAnnotation.Message
	annotations["max_wait_available_time"] = strconv.Itoa(int(scaleAnnotation.MaxWaitAvailableSecond))
	annotations["max_unavailable_replicas"] = strconv.Itoa(scaleAnnotation.MaxUnavailableReplicas)
	annotations["last_update_time"] = strconv.FormatInt(scaleAnnotation.LastUpdateTime.Unix(), 10)

	return annotations, nil
}
func ReadScaleAnnotation(annotations map[string]string) (*ScaleAnnotation, error) {
	scaleAnnotation := NewScaleAnnotation()
	if stepsJSON, ok := annotations["steps"]; ok {
		var steps []Step
		err := json.Unmarshal([]byte(stepsJSON), &steps)
		if err != nil {
			return &scaleAnnotation, err
		}
		scaleAnnotation.Steps = steps
	} else {
		return nil, ErrorScaleAnnotationParseSteps
	}

	if currentStepIndex, ok := annotations["current_step_index"]; ok {
		currentStepIndexInt, err := strconv.ParseInt(currentStepIndex, 10, 0)
		if err != nil {
			return &scaleAnnotation, err
		}
		scaleAnnotation.CurrentStepIndex = int(currentStepIndexInt)
	} else {
		return nil, ErrorScaleAnnotationParseCurrentStepIndex
	}

	if currentStepState, ok := annotations["current_step_state"]; ok {
		scaleAnnotation.CurrentStepState = StepState(currentStepState)
	} else {
		return nil, ErrorScaleAnnotationParseCurrentStepState
	}

	if maxWaitAvailableTime, ok := annotations["max_wait_available_time"]; ok {
		maxWaitAvailableTimeInt, err := strconv.ParseInt(maxWaitAvailableTime, 10, 0)
		if err != nil {
			return &scaleAnnotation, err
		}
		scaleAnnotation.MaxWaitAvailableSecond = int(maxWaitAvailableTimeInt)
	}

	if maxUnavailableReplicas, ok := annotations["max_unavailable_replicas"]; ok {
		maxUnavailableReplicasInt, err := strconv.ParseInt(maxUnavailableReplicas, 10, 0)
		if err != nil {
			return &scaleAnnotation, err
		}
		scaleAnnotation.MaxUnavailableReplicas = int(maxUnavailableReplicasInt)
	}

	if lastUpdateTime, ok := annotations["last_update_time"]; ok {
		lastUpdateTimeInt64, err := strconv.ParseInt(lastUpdateTime, 10, 64)
		if err != nil {
			return &scaleAnnotation, err
		}
		scaleAnnotation.LastUpdateTime = time.Unix(lastUpdateTimeInt64, 0)
	}

	if message, ok := annotations["message"]; ok {
		scaleAnnotation.Message = message
	}

	return &scaleAnnotation, nil
}

type StepState string

const (
	StepStateUpgrade StepState = "StepUpgrade"
	StepStatePaused  StepState = "StepPaused"
	StepStateReady   StepState = "StepReady"

	StepStateCompleted StepState = "Completed"
	StepStateError     StepState = "Error"
)

type Step struct {
	Replicas int32 `json:"replicas,omitempty"`
	Pause    bool  `json:"pause,omitempty"`
}

func (s Step) String() string {
	return fmt.Sprintf("replicas: %d,pause: %v", s.Replicas, s.Pause)
}
