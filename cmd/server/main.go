package main

import (
	"log"

	"github.com/accretional/collector/pkg/server"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := server.LoadConfig("")
	if err != nil {
		return err
	}

	srv, err := server.New(server.Config{
		DataDir:     cfg.DataDir,
		Port:        cfg.Port,
		Namespace:   cfg.Namespace,
		CollectorID: cfg.CollectorID,
		HealthPort:  cfg.HealthPort,
	})
	if err != nil {
		return err
	}
	defer srv.Close()

	if err := srv.Start(); err != nil {
		return err
	}

	srv.WaitForShutdown()

	return nil
}
