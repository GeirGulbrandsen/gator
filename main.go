package main

import (
	"fmt"
	"log"

	"github.com/geirgulbrandsen/gator/internal/config"
)

func main() {
	cfg, err := config.Read()
	if err != nil {
		log.Fatalf("reading config: %v", err)
	}

	if err := cfg.SetUser("John Constantine"); err != nil {
		log.Fatalf("setting user: %v", err)
	}

	updatedCfg, err := config.Read()
	if err != nil {
		log.Fatalf("re-reading config: %v", err)
	}

	fmt.Printf("%+v\n", updatedCfg)
}
