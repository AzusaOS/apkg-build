package main

import (
	"flag"
	"log"
	"os"
)

func main() {
	// Check os.Args
	log.Printf("apkg-build running...")

	flag.Parse()

	args := flag.Args()

	if len(args) < 1 {
		log.Printf("Usage: %s action...", os.Args[0])
		os.Exit(1)
	}

	checkRepo()

	switch args[0] {
	case "update":
		log.Printf("Updating repository...")
		updateRepo()
	case "build":
		if len(args) != 2 {
			log.Printf("Usage: %s build package", os.Args[0])
			os.Exit(1)
		}
		pkg := loadPackage(args[1])
		if pkg == nil {
			os.Exit(1)
		}
		pkg.build()
	default:
		log.Printf("args = %v", os.Args)
	}

}
