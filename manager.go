package annotationscale

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type AnnotationScaleManager struct {
	log     logr.Logger
	manager manager.Manager
	config  *rest.Config
	match   *metav1.LabelSelector

	stopCh  chan struct{}
	mutex   sync.Mutex
	stopped bool
}

func NewAnnotationScaleManager(logger logr.Logger, managerName string, match *metav1.LabelSelector, config *rest.Config) (*AnnotationScaleManager, error) {
	log := logger.WithName(managerName)

	labelMap, err := metav1.LabelSelectorAsMap(match)
	if err != nil {
		log.Error(err, "could not create label map from match")
		return nil, err
	}

	var mgr manager.Manager
	var mgrCreateErr error

	if err != nil {
		log.Error(err, "could not create manager")
		return nil, err
	}

	if len(labelMap) != 0 {
		mgr, mgrCreateErr = manager.New(config, manager.Options{
			MetricsBindAddress: "0",
			NewCache: cache.BuilderWithOptions(cache.Options{
				SelectorsByObject: cache.SelectorsByObject{
					&appsv1.Deployment{}: {
						Label: labels.SelectorFromSet(labelMap),
					},
					&appsv1.ReplicaSet{}: {
						Label: labels.SelectorFromSet(labelMap),
					},
					&corev1.Pod{}: {
						Label: labels.SelectorFromSet(labelMap),
					},
				},
			})})
	} else {
		mgr, mgrCreateErr = manager.New(config, manager.Options{
			MetricsBindAddress: "0",
		})
	}

	if mgrCreateErr != nil {
		log.Error(mgrCreateErr, "could not create manager with ")
		return nil, mgrCreateErr
	}

	return &AnnotationScaleManager{
		manager: mgr,
		config:  config,
		log:     log,
		stopCh:  make(chan struct{}),
		stopped: false,
		match:   match,
	}, nil
}

func (m *AnnotationScaleManager) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-ctx.Done():
		case <-m.stopCh:
			cancel()
		}
	}()
	err := builder.
		ControllerManagedBy(m.manager).
		For(&appsv1.Deployment{}).
		Owns(&appsv1.ReplicaSet{}).
		Owns(&corev1.Pod{}).
		Complete(&DeploymentReconciler{log: m.log})
	if err != nil {
		m.log.Error(err, "could not create controller")
		return err
	}
	if err := m.manager.Start(ctx); err != nil {
		m.log.Error(err, "could not start manager")
		return err
	}
	return nil
}

func (m *AnnotationScaleManager) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if !m.stopped {
		m.stopped = true
		close(m.stopCh)
	}
}

func (m *AnnotationScaleManager) Stopping() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.stopped
}
