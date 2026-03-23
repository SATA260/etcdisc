// main.go starts the etcdisc server process for local development and testing.
//
// @title etcdisc HTTP API
// @version 1.0
// @description etcdisc phase-1 control plane API for registry, discovery, config, A2A, and admin operations.
// @BasePath /
// @schemes http
// @contact.name etcdisc
package main

import (
	"log"

	_ "etcdisc/docs"
	"etcdisc/internal/app/bootstrap"
)

var bootstrapRun = bootstrap.Run

func run() error {
	return bootstrapRun()
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
