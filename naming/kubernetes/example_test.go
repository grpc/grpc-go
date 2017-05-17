package kubernetes_test

import (
	"fmt"
	"log"

	"google.golang.org/grpc/naming/kubernetes"
)

func Example() {
	r := kubernetes.NewResolver("", "")
	watcher, err := r.Resolve("hello")
	if err != nil {
		log.Fatal(err)
	}

	for {
		updates, err := watcher.Next()
		if err != nil {
			log.Fatal(err)
		}

		for _, u := range updates {
			fmt.Printf("Operation: %d, Addr: %s\n", u.Op, u.Addr)
		}
	}
}
