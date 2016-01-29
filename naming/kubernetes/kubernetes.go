package kubernetes

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"google.golang.org/grpc/naming"
)

type watchResult struct {
	ep  *Object
	err error
}

// A Watcher provides name resolution updates from Kubernetes endpoints
// identified by name. Updates consists of pod IPs and the first port
// defined on the endpoint.
type Watcher struct {
	Client          *http.Client
	EndpointName    string
	MasterURL       string
	Namespace       string
	endpoints       map[string]interface{}
	done            chan struct{}
	result          chan watchResult
	resourceVersion string
}

// Close closes the watcher, cleaning up any open connections.
func (w *Watcher) Close() {
	w.done <- struct{}{}
}

// Next updates the endpoints for the name being watched.
func (w *Watcher) Next() ([]*naming.Update, error) {
	updates := make([]*naming.Update, 0)

	u, err := url.Parse(fmt.Sprintf("%s/api/v1/watch/namespaces/%s/endpoints/%s",
		w.MasterURL, w.Namespace, w.EndpointName))
	if err != nil {
		return nil, err
	}

	// Calls to the Kubernetes endpoints watch API must include the resource
	// version to ensure watches only return updates since the last watch.
	q := u.Query()
	q.Set("resourceVersion", w.resourceVersion)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	go func() {
		var e Object
		resp, err := w.Client.Do(req)
		if err != nil {
			w.result <- watchResult{nil, err}
			return
		}
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&e); err != nil {
			w.result <- watchResult{nil, err}
			return
		}

		w.result <- watchResult{&e, nil}
	}()

	var ep *Object
	updatedEndpoints := make(map[string]interface{})

	select {
	case <-w.done:
		return updates, nil
	case r := <-w.result:
		if r.err != nil {
			return nil, err
		}
		ep = r.ep
	}

	for _, subset := range ep.Object.Subsets {
		for _, address := range subset.Addresses {
			endpoint := net.JoinHostPort(address.IP, strconv.Itoa(subset.Ports[0].Port))
			updatedEndpoints[endpoint] = nil
		}
	}

	// Create updates to add new endpoints.
	for addr, md := range updatedEndpoints {
		if _, ok := w.endpoints[addr]; !ok {
			updates = append(updates, &naming.Update{naming.Add, addr, md})
		}
	}

	// Create updates to delete old endpoints.
	for addr, _ := range w.endpoints {
		if _, ok := updatedEndpoints[addr]; !ok {
			updates = append(updates, &naming.Update{naming.Delete, addr, nil})
		}
	}

	// Increment the resource version so the next watch on the Kubernetes
	// endpoints API blocks until there is an update.
	currentResourceVersion, err := strconv.Atoi(ep.Object.Metadata.ResourceVersion)
	if err != nil {
		return nil, err
	}
	w.resourceVersion = strconv.Itoa(currentResourceVersion + 1)

	w.endpoints = updatedEndpoints
	return updates, nil
}

// Resolver resolves service names using Kubernetes endpoints.
type Resolver struct {
	MasterURL string
	Namespace string
}

// NewResolver returns a new Kubernetes resolver.
func NewResolver(masterURL, namespace string) Resolver {
	if masterURL == "" {
		masterURL = "http://127.0.0.1:8080"
	}
	if namespace == "" {
		namespace = "default"
	}
	return Resolver{masterURL, namespace}
}

// Resolve creates a Kubernetes watcher for the named target.
func (r *Resolver) Resolve(target string) (naming.Watcher, error) {
	w := &Watcher{
		Client:          http.DefaultClient,
		EndpointName:    target,
		endpoints:       make(map[string]interface{}),
		MasterURL:       r.MasterURL,
		Namespace:       r.Namespace,
		done:            make(chan struct{}),
		result:          make(chan watchResult),
		resourceVersion: "0",
	}
	return w, nil
}
