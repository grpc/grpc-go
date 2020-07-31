/*
 *
 * Copyright 2020 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package main

import (
	"fmt"
	"log"
	"os"

	compiler "google.golang.org/grpc/security/authorization/compiler/api"
)

func main() {
	var inputFile string
	var outputFile string
	if len(os.Args) == 1 {
		fmt.Println("Please Enter your security policy file path:")
		_, err := fmt.Scanln(&inputFile)
		if err != nil {
			log.Panicf("Incorrect File Path:  %v", err)
		}
		fmt.Println("Please Enter your serialized RBAC proto output file path:")
		_, err = fmt.Scanln(&outputFile)
		if err != nil {
			log.Panicf("Incorrect File Path:  %v", err)
		}
	} else if len(os.Args) == 3 {
		inputFile = os.Args[1]
		outputFile = os.Args[2]
	} else {
		log.Fatalf("Incorrect Number of files. Please Enter the input file path and the output file path")
	}

	err := compiler.Compile(inputFile, outputFile)
	if err != nil {
		log.Panicf("Failed to serialize RBAC proto %v", err)
	}
	fmt.Printf("Compiled %s into %s \n", inputFile, outputFile)
}
