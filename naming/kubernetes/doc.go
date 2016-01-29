// Package kubernetes implements gRPC name resolution for Kubernetes.
//
// It aims to support communication with the Kubernetes API using
// kubectl as a proxy.
//
// References:
//   https://github.com/grpc/grpc/blob/master/doc/naming.md
//   http://kubernetes.io/v1.0/docs/user-guide/accessing-the-cluster.html#using-kubectl-proxy
//
// Names are resolved using the Kubernetes endpoints API and returns pod IPs and the first
// port number defined on the endpoint.
//
package kubernetes
