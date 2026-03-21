// main.go starts the etcdisc server process for local development and testing.
package main

import (
	"log"

	"etcdisc/internal/app/bootstrap"
)

func main() {
	if err := bootstrap.Run(); err != nil {
		log.Fatal(err)
	}
}
