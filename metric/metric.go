package metric

import (
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	client "github.com/elastic/go-lumber/client/v2"
	"github.com/tengattack/minimetric/config"
	"github.com/tengattack/tgo/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	beatName    = "metricbeat"
	beatVersion = "6.5.4"
)

var (
	version string

	clientset *kubernetes.Clientset

	getOutputMutex *sync.RWMutex
	outputs        map[string]*client.Client
)

func init() {
	getOutputMutex = new(sync.RWMutex)
	outputs = make(map[string]*client.Client)
}

// SetVersion for metric
func SetVersion(v string) {
	version = v
}

func getOutput() (*client.Client, error) {
	hosts := config.Conf.Output.Logstash.Hosts
	host := hosts[rand.Intn(len(hosts))]

	var output *client.Client
	getOutputMutex.RLock()
	output = outputs[host]
	getOutputMutex.RUnlock()

	if output == nil {
		getOutputMutex.Lock()
		c, err := client.Dial(host)
		if err != nil {
			log.LogError.Errorf("output host %s dail error: %v", host, err)
			return nil, err
		}
		outputs[host] = c
		output = c
		defer getOutputMutex.Unlock()
	}

	return output, nil
}

func setOutputError(output *client.Client, err error) {
	getOutputMutex.Lock()
	defer getOutputMutex.Unlock()

	for host, c := range outputs {
		if c == output {
			outputs[host] = nil
			log.LogError.Errorf("output host %s error: %v", host, err)
			break
		}
	}

	output.Close()
}

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

func sendOutput(output **client.Client, eventData map[string]interface{}) error {
	var err error
	if *output == nil {
		*output, err = getOutput()
		if err != nil {
			return err
		}
	}
	err = (*output).Send([]interface{}{eventData})
	if err != nil {
		setOutputError(*output, err)
		*output = nil
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

	hostname, _ := os.Hostname()
	nodeName := config.Conf.Metric.NodeName
	if nodeName == "" {
		nodeName = hostname
	}
	var output *client.Client

	for _, n := range ns.Items {
		listHPAOpts := metav1.ListOptions{}
		hpas, err := clientset.AutoscalingV1().HorizontalPodAutoscalers(n.Name).List(listHPAOpts)
		if err != nil {
			log.LogError.Errorf("List HorizontalPodAutoscalers error: %v", err)
			continue
		}
		t := time.Now()
		tz := t.Format("Z07:00")
		ts := t.UTC().Format(time.RFC3339)
		for _, h := range hpas.Items {
			var (
				currentCPU  int32
				targetCPU   int32
				minReplicas int32
				ref         string
			)
			if h.Status.CurrentCPUUtilizationPercentage != nil {
				currentCPU = *h.Status.CurrentCPUUtilizationPercentage
			}
			if h.Spec.TargetCPUUtilizationPercentage != nil {
				targetCPU = *h.Spec.TargetCPUUtilizationPercentage
			}
			if h.Spec.MinReplicas != nil {
				minReplicas = *h.Spec.MinReplicas
			}
			ref = h.Spec.ScaleTargetRef.Kind + "/" + h.Spec.ScaleTargetRef.Name
			log.LogAccess.Debugf("[%s] %s %s %d%%/%d%% %d %d %d", h.Namespace, h.Name,
				ref, currentCPU, targetCPU,
				minReplicas, h.Spec.MaxReplicas, h.Status.CurrentReplicas)

			eventData := map[string]interface{}{
				"@metadata": map[string]interface{}{
					"beat":    beatName,
					"version": beatVersion,
				},
				"@timestamp": ts,
				"beat": map[string]interface{}{
					"hostname": hostname,
					"name":     nodeName,
					"timezone": tz,
					"version":  beatVersion,
				},
				"host": map[string]interface{}{
					"name": nodeName,
				},
				"kubernetes": map[string]interface{}{
					"namespace": h.Namespace,
					"hpa": map[string]interface{}{
						"name":      h.Name,
						"reference": ref,
						"targets": map[string]interface{}{
							"cpu": map[string]interface{}{
								"current": currentCPU,
								"target":  targetCPU,
							},
						},
						"minpods":  minReplicas,
						"maxpods":  h.Spec.MaxReplicas,
						"replicas": h.Status.CurrentReplicas,
					},
				},
				"metricset": map[string]interface{}{
					"module": "kubernetes",
					"name":   "hpa",
				},
			}
			if h.Spec.ScaleTargetRef.Kind == "Deployment" {
				eventData["kubernetes"].(map[string]interface{})["deployment"] = map[string]interface{}{
					"name": h.Spec.ScaleTargetRef.Name,
				}
			}

			// send output
			sendOutput(&output, eventData)
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
