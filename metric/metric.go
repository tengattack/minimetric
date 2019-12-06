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
	autoscalingv2beta2 "k8s.io/api/autoscaling/v2beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	appName     = "minimetric"
	beatName    = "metricbeat"
	beatVersion = "6.5.4"
)

// MetricValue shows current value & target spec
type MetricValue struct {
	Current int64 `json:"current"`
	Target  int64 `json:"target"`
}

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

func getNodeName(hostname string) string {
	nodeName := config.Conf.Metric.NodeName
	if nodeName == "" {
		nodeName = os.Getenv("NODE_NAME")
	}
	if nodeName == "" {
		nodeName = hostname
	}
	return nodeName
}

func getMetricValue(target autoscalingv2beta2.MetricTarget, v autoscalingv2beta2.MetricValueStatus) MetricValue {
	switch target.Type {
	case autoscalingv2beta2.UtilizationMetricType:
		return MetricValue{
			Current: int64(*v.AverageUtilization),
			Target:  int64(*target.AverageUtilization),
		}
	case autoscalingv2beta2.ValueMetricType:
		return MetricValue{
			Current: v.Value.Value(),
			Target:  target.Value.Value(),
		}
	case autoscalingv2beta2.AverageValueMetricType:
		return MetricValue{
			Current: v.AverageValue.Value(),
			Target:  target.AverageValue.Value(),
		}
	}
	return MetricValue{}
}

func metricLoop() {
	listNamespacesOpts := metav1.ListOptions{}

	ns, err := clientset.CoreV1().Namespaces().List(listNamespacesOpts)
	if err != nil {
		log.LogError.Errorf("List Namespaces error: %v", err)
		return
	}

	hostname, _ := os.Hostname()
	nodeName := getNodeName(hostname)

	var output *client.Client

	for _, n := range ns.Items {
		listHPAOpts := metav1.ListOptions{}
		hpas, err := clientset.AutoscalingV2beta2().HorizontalPodAutoscalers(n.Name).List(listHPAOpts)
		if err != nil {
			log.LogError.Errorf("List HorizontalPodAutoscalers error: %v", err)
			continue
		}
		t := time.Now()
		tz := t.Format("Z07:00")
		ts := t.UTC().Format(time.RFC3339)
		for _, h := range hpas.Items {
			var (
				ref         string
				minReplicas int32
			)
			metrics := make(map[string]interface{})
			for j, m := range h.Status.CurrentMetrics {
				var sourceType string
				source := make(map[string]interface{})
				spec := h.Spec.Metrics[j]
				switch m.Type {
				case autoscalingv2beta2.ResourceMetricSourceType:
					sourceType = "resource"
					if _, ok := metrics[sourceType]; ok {
						source = metrics[sourceType].(map[string]interface{})
					} else {
						metrics[sourceType] = source
					}
					source[string(m.Resource.Name)] = getMetricValue(spec.Resource.Target, m.Resource.Current)
				case autoscalingv2beta2.PodsMetricSourceType:
					sourceType = "pods"
					if _, ok := metrics[sourceType]; ok {
						source = metrics[sourceType].(map[string]interface{})
					} else {
						metrics[sourceType] = source
					}
					source[string(m.Pods.Metric.Name)] = getMetricValue(spec.Pods.Target, m.Pods.Current)
				case autoscalingv2beta2.ExternalMetricSourceType:
					sourceType = "external"
					if _, ok := metrics[sourceType]; ok {
						source = metrics[sourceType].(map[string]interface{})
					} else {
						metrics[sourceType] = source
					}
					source[string(m.External.Metric.Name)] = getMetricValue(spec.External.Target, m.External.Current)
				default:
					log.LogAccess.Warnf("Unknown metric source type %v for hpa %s", m.Type, h.Name)
					continue
				}
			}

			ref = h.Spec.ScaleTargetRef.Kind + "/" + h.Spec.ScaleTargetRef.Name
			if h.Spec.MinReplicas != nil {
				minReplicas = *h.Spec.MinReplicas
			}
			log.LogAccess.Debugf("[%s] %s %s %v %d %d %d", h.Namespace, h.Name,
				ref, metrics,
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
					"version":  version,
				},
				"host": map[string]interface{}{
					"name": nodeName,
				},
				"kubernetes": map[string]interface{}{
					"namespace": h.Namespace,
					"hpa": map[string]interface{}{
						"name":      h.Name,
						"reference": ref,
						"metrics":   metrics,
						"minpods":   minReplicas,
						"maxpods":   h.Spec.MaxReplicas,
						"desired":   h.Status.DesiredReplicas,
						"replicas":  h.Status.CurrentReplicas,
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
