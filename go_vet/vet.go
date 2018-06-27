// vet.go is a script to check whether files that are supposed to be built on
// appengine import "syscall" package.
package main

import (
	"fmt"
	"go/build"
	"os"
)

func main() {
	b := build.Default
	b.BuildTags = []string{"appengine", "appenginevm"}
	good := true
	argsWithoutProg := os.Args[1:]
	for _, dir := range argsWithoutProg {
		p, _ := b.Import(".", dir, 0)
		for _, pkg := range p.Imports {
			if pkg == "syscall" {
				fmt.Println("Package", p.Dir+"/"+p.Name, "importing \"syscall\" package without appengine build tag is NOT ALLOWED!")
				good = false
			}
		}
	}
	if !good {
		os.Exit(1)
	}
}
