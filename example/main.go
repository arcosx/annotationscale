package main

import (
	"context"
	"flag"
	"log"

	annotationscale "github.com/arcosx/annotationscale"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"
)

var mode string
var deploymentName string
var kubeconfig string
var server bool

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "kubeconfig path")
	flag.StringVar(&mode, "mode", "scaleup", "scaleup|scaledown|release|stop")
	flag.StringVar(&deploymentName, "deployment-name", "nginx-deployment", "deployment name")
	flag.BoolVar(&server, "server", false, "server mode")
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatal(err)
	}

	klogr := klog.NewKlogr().WithName("annotationscale-example")

	if server {
		klog.Info("server mode")
		m, err := annotationscale.NewAnnotationScaleManager(&klogr, &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/managed-by": "annotaionscale",
			},
		}, kubeconfig, 1)

		if err != nil {
			log.Fatal(err)
		}
		err = m.Start()
		if err != nil {
			log.Fatal(err)
		}
	} else {
		clientset, err := kubernetes.NewForConfig(kubeconfig)
		if err != nil {
			log.Fatal(err)
		}
		if err != nil {
			log.Fatal(err)
		}
		switch mode {
		case "scaleup":
			klog.Info("scaleup now...")
			scaleUp(context.TODO(), clientset)
		case "scaledown":
			klog.Info("scaledown now...")
			scaleDown(context.TODO(), clientset)
		case "release":
			klog.Info("release now...")
			release(context.TODO(), clientset)
		case "stop":
			klog.Info("stop now...")
			stop(context.TODO(), clientset)
		default:
			return
		}
	}
}

func scaleUp(ctx context.Context, clientset *kubernetes.Clientset) {

	deployment, err := clientset.AppsV1().Deployments("default").Get(ctx, deploymentName, metav1.GetOptions{})

	if err != nil {
		log.Fatal(err)
	}

	scaleAnnotation := annotationscale.NewScaleAnnotation()
	scaleAnnotation.CurrentStepIndex = 1
	scaleAnnotation.Steps = []annotationscale.Step{
		{
			Replicas: 1,
		},
		{
			Replicas: 2,
		},
		{
			Replicas: 5,
		},
		{
			Replicas: 8,
			Pause:    true,
		},
		{
			Replicas: 10,
		},
		{
			Replicas: 12,
		},
		{
			Replicas: 15,
		},
		{
			Replicas: 20,
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
	deployment, err := clientset.AppsV1().Deployments("default").Get(ctx, deploymentName, metav1.GetOptions{})

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
		},
		{
			Replicas: 15,
		},
		{
			Replicas: 10,
		},
		{
			Replicas: 5,
		},
		{
			Replicas: 1,
		},
		{
			Replicas: 0,
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

func release(ctx context.Context, clientset *kubernetes.Clientset) {
	deployment, err := clientset.AppsV1().Deployments("default").Get(ctx, deploymentName, metav1.GetOptions{})

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

func stop(ctx context.Context, clientset *kubernetes.Clientset) {
	deployment, err := clientset.AppsV1().Deployments("default").Get(ctx, deploymentName, metav1.GetOptions{})

	if err != nil {
		log.Fatal(err)
	}

	scaleAnnotation, err := annotationscale.ReadScaleAnnotation(deployment.Annotations)

	if err != nil {
		log.Fatal(err)
	}

	var pauseIndex int
	for index := scaleAnnotation.CurrentStepIndex; index <= len(scaleAnnotation.Steps); index++ {
		if scaleAnnotation.Steps[index-1].Replicas >= deployment.Status.AvailableReplicas {
			pauseIndex = index
			break
		}
	}

	scaleAnnotation.CurrentStepState = annotationscale.StepStatePaused
	scaleAnnotation.CurrentStepIndex = pauseIndex
	scaleAnnotation.Steps[pauseIndex-1].Pause = true

	err = annotationscale.SetDeploymentScaleAnnotation(deployment, scaleAnnotation)
	if err != nil {
		log.Fatal(err)
	}

	_, err = clientset.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		log.Fatal(err)
	}
}
