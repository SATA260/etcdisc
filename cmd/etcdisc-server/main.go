// main.go starts the etcdisc server process for local development and testing.
package main

import (
	"log"

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
