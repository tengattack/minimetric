package metric

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tengattack/minimetric/config"
	"github.com/tengattack/tgo/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	clientset *kubernetes.Clientset
)

func initKubeClient() error {
	var kubeConfig *rest.Config
	var err error
	if config.Conf.Kubernetes.InCluster {
		kubeConfig, err = rest.InClusterConfig()
	} else {
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", config.Conf.Kubernetes.Config)
	}
	if err != nil {
		return err
	}
	clientset, err = kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	return nil
}

func metricLoop() {
	listNamespacesOpts := metav1.ListOptions{}

	ns, err := clientset.CoreV1().Namespaces().List(listNamespacesOpts)
	if err != nil {
		log.LogError.Errorf("List Namespaces error: %v", err)
		return
	}

	for _, n := range ns.Items {
		listHPAOpts := metav1.ListOptions{}
		hpas, err := clientset.AutoscalingV1().HorizontalPodAutoscalers(n.Name).List(listHPAOpts)
		if err != nil {
			log.LogError.Errorf("List HorizontalPodAutoscalers error: %v", err)
			continue
		}
		for _, h := range hpas.Items {
			log.LogAccess.Infof("[%s] %s %s %d%%/%d%% %d %d %d", n.Name, h.Name, h.Spec.ScaleTargetRef,
				*h.Status.CurrentCPUUtilizationPercentage, *h.Spec.TargetCPUUtilizationPercentage,
				*h.Spec.MinReplicas, h.Spec.MaxReplicas, h.Status.CurrentReplicas)
		}
	}
}

// Run metric
func Run() error {
	err := initKubeClient()
	if err != nil {
		return err
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	timer := time.NewTicker(config.Conf.Metric.PeriodDuration)
	defer timer.Stop()

mainLoop:
	for {
		metricLoop()
		select {
		case <-timer.C:
		case <-shutdown:
			log.LogAccess.Info("Got the signal. Shutting down...")
			break mainLoop
		}
	}

	return nil
}
