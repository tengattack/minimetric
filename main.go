package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/tengattack/minimetric/config"
	"github.com/tengattack/minimetric/metric"
	"github.com/tengattack/tgo/log"
)

var (
	// Version control for dandelion
	Version = "0.0.1-dev"
)

func main() {
	var defaultConfigPath string
	if runtime.GOOS == "windows" {
		defaultConfigPath = "config.yml"
	} else {
		defaultConfigPath = "/etc/dandelion/config.yml"
	}
	configPath := flag.String("config", defaultConfigPath, "config file")
	showVerbose := flag.Bool("verbose", false, "show verbose debug log")
	showHelp := flag.Bool("help", false, "show help message")
	flag.Parse()

	if *showHelp {
		flag.Usage()
		return
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "Please specify a config file")
		flag.Usage()
		os.Exit(1)
	}

	conf, err := config.LoadConfig(*configPath)
	if err != nil {
		panic(err)
	}
	if *showVerbose {
		conf.Log.AccessLevel = "debug"
		conf.Log.ErrorLevel = "debug"
	}
	config.Conf = conf

	err = log.InitLog(&config.Conf.Log)
	if err != nil {
		panic(err)
	}

	err = metric.Run()
	if err != nil {
		log.LogError.Fatalf("metric start error: %v", err)
	}
}
