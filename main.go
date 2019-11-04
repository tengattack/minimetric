package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"time"

	"github.com/tengattack/minimetric/config"
	"github.com/tengattack/minimetric/metric"
	"github.com/tengattack/tgo/log"
)

var (
	// Version control for minimetric
	Version = "0.0.1-dev"
)

func main() {
	var defaultConfigPath string
	if runtime.GOOS == "windows" {
		defaultConfigPath = "minimetric.yml"
	} else {
		defaultConfigPath = "/etc/minimetric/minimetric.yml"
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

	// set global rand seed
	rand.Seed(time.Now().UnixNano())

	metric.SetVersion(Version)
	err = metric.Run()
	if err != nil {
		log.LogError.Fatalf("metric start error: %v", err)
	}
}
