package main

import (
	"context"
	"fmt"
	"log"
	"os"

	annotationscale "github.com/arcosx/annotationscale"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	mode := os.Args[1]

	kubeconfigPath := "/root/.kube/config"
	kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		log.Fatal(err)
	}

	m, err := annotationscale.NewAnnotationScaleManager("cluster1", &metav1.LabelSelector{}, kubeconfig)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		clientset, err := kubernetes.NewForConfig(kubeconfig)
		if err != nil {
			log.Fatal(err)
		}
		if err != nil {
			log.Fatal(err)
		}
		switch mode {
		case "scaleup":
			fmt.Println("scaleup now")
			scaleUp(context.TODO(), clientset)
		case "scaledown":
			fmt.Println("scaledown now")
			scaleDown(context.TODO(), clientset)
		case "releasing":
			fmt.Println("releasing now")
			releasing(context.TODO(), clientset)
		default:
			return
		}
	}()
	err = m.Start()
	if err != nil {
		log.Fatal(err)
	}
}

func scaleUp(ctx context.Context, clientset *kubernetes.Clientset) {

	deployment, err := clientset.AppsV1().Deployments("default").Get(ctx, "nginx-deployment", metav1.GetOptions{})

	if err != nil {
		log.Fatal(err)
	}

	scaleAnnotation := annotationscale.NewScaleAnnotation()
	scaleAnnotation.CurrentStepIndex = 1
	scaleAnnotation.Steps = []annotationscale.Step{
		{
			Replicas: 1,
			Pause:    false,
		},
		{
			Replicas: 2,
			Pause:    false,
		},
		{
			Replicas: 5,
			Pause:    false,
		},
		{
			Replicas: 8,
			Pause:    false,
		},
		{
			Replicas: 10,
			Pause:    true,
		},
		{
			Replicas: 12,
			Pause:    false,
		},
	}
	scaleAnnotation.CurrentStepState = annotationscale.StepStateReady

	fixedAnnotation, err := annotationscale.SetScaleAnnotation(deployment.Annotations, &scaleAnnotation)

	if err != nil {
		log.Fatal(err)
	}

	deployment.SetAnnotations(fixedAnnotation)

	_, err = clientset.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		log.Fatal(err)
	}
}

func scaleDown(ctx context.Context, clientset *kubernetes.Clientset) {
	deployment, err := clientset.AppsV1().Deployments("default").Get(ctx, "nginx-deployment", metav1.GetOptions{})

	if err != nil {
		log.Fatal(err)
	}

	scaleAnnotation, err := annotationscale.ReadScaleAnnotation(deployment.Annotations)

	if err != nil {
		log.Fatal(err)
	}

	scaleAnnotation.CurrentStepIndex = 1
	scaleAnnotation.Steps = []annotationscale.Step{
		{
			Replicas: 12,
			Pause:    false,
		},
		{
			Replicas: 5,
			Pause:    false,
		},
		{
			Replicas: 1,
			Pause:    false,
		},
	}

	scaleAnnotation.CurrentStepState = annotationscale.StepStateReady

	err = annotationscale.SetDeploymentScaleAnnotation(deployment, scaleAnnotation)
	if err != nil {
		log.Fatal(err)
	}

	_, err = clientset.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		log.Fatal(err)
	}
}

func releasing(ctx context.Context, clientset *kubernetes.Clientset) {
	deployment, err := clientset.AppsV1().Deployments("default").Get(ctx, "nginx-deployment", metav1.GetOptions{})

	if err != nil {
		log.Fatal(err)
	}

	scaleAnnotation, err := annotationscale.ReadScaleAnnotation(deployment.Annotations)

	if err != nil {
		log.Fatal(err)
	}

	scaleAnnotation.CurrentStepState = annotationscale.StepStateReady

	err = annotationscale.SetDeploymentScaleAnnotation(deployment, scaleAnnotation)
	if err != nil {
		log.Fatal(err)
	}

	_, err = clientset.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		log.Fatal(err)
	}
}
