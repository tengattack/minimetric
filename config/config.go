package config

import (
	"errors"
	"io/ioutil"
	"time"

	"github.com/tengattack/tgo/log"
	yaml "gopkg.in/yaml.v2"
)

// Conf is the main config
var Conf Config

// errors
var (
	ErrNoOutput = errors.New("no output configured")
)

// Config is config structure.
type Config struct {
	Metric     SectionMetric     `yaml:"metric"`
	Log        log.Config        `yaml:"log"`
	Output     SectionOutput     `yaml:"output"`
	Kubernetes SectionKubernetes `yaml:"kubernetes"`
}

// SectionMetric is sub section of config.
type SectionMetric struct {
	NodeName       string `yaml:"node_name"`
	Period         string `yaml:"period"`
	PeriodDuration time.Duration
}

// SectionOutput is sub section of config.
type SectionOutput struct {
	Logstash struct {
		Hosts []string `yaml:"hosts"`
	} `yaml:"logstash"`
}

// SectionKubernetes is sub section of config.
type SectionKubernetes struct {
	InCluster bool   `yaml:"in_cluster"`
	Config    string `yaml:"config"`
}

// BuildDefaultConf is default config setting.
func BuildDefaultConf() Config {
	var conf Config

	// Metric
	conf.Metric.Period = "10s"
	conf.Metric.PeriodDuration, _ = time.ParseDuration(conf.Metric.Period)

	// Log
	conf.Log.Format = "string"
	conf.Log.AccessLog = "stdout"
	conf.Log.AccessLevel = "debug"
	conf.Log.ErrorLog = "stderr"
	conf.Log.ErrorLevel = "error"
	conf.Log.Agent.Enabled = false

	return conf
}

// LoadConfig load config from file
func LoadConfig(confPath string) (Config, error) {
	conf := BuildDefaultConf()

	configFile, err := ioutil.ReadFile(confPath)

	if err != nil {
		return conf, err
	}

	err = yaml.Unmarshal(configFile, &conf)
	if err != nil {
		return conf, err
	}

	conf.Metric.PeriodDuration, err = time.ParseDuration(conf.Metric.Period)
	if err != nil {
		return conf, err
	}

	if len(conf.Output.Logstash.Hosts) <= 0 {
		return conf, ErrNoOutput
	}

	return conf, nil
}
