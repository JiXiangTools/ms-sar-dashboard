package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/app"
)

func main() {
	configPath := flag.String("config", "", "config file path")
	env := flag.String("env", "", "environment")
	flag.Parse()

	application, err := app.New(app.Options{
		ConfigPath:  *configPath,
		Environment: *env,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "init app: %v\n", err)
		os.Exit(1)
	}
	if err := application.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "run app: %v\n", err)
		os.Exit(1)
	}
}
