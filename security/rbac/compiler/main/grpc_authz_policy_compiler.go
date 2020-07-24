package main

import (
	"fmt"
	"os"

	"google.golang.org/grpc/security/compiler/api"
)

func main() {
	inputFile := os.Args[1]
	outputFile := os.Args[2]
	compiler.Compile(inputFile, outputFile)
	fmt.Printf("Compiled %s into %s \n", inputFile, outputFile)
}
