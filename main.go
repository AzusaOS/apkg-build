package main

import (
	"log"
	"os"
)

func main() {
	// Check os.Args
	log.Printf("apkg-build running...")

	if len(os.Args) < 2 {
		log.Printf("Usage: %s action...", os.Args[0])
		os.Exit(1)
	}

	checkRepo()

	switch os.Args[1] {
	case "update":
		log.Printf("Updating repository...")
		updateRepo()
	default:
		log.Printf("args = %v", os.Args)
	}

}
