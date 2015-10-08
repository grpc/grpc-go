/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package etcd

import (
	"sync"

	etcdcl "github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"google.golang.org/grpc/naming"
)

// getNode builds the key-value map starting from node recursively. It returns the
// max etcdcl.Node.ModifiedIndex starting from that node.
func getNode(node *etcdcl.Node, kv map[string]string) uint64 {
	if !node.Dir {
		kv[node.Key] = node.Value
		return node.ModifiedIndex
	}
	var max uint64
	for _, v := range node.Nodes {
		i := getNode(v, kv)
		if max < i {
			max = i
		}
	}
	return max
}

type resolver struct {
	kapi etcdcl.KeysAPI
}

func (r *resolver) Resolve(target string) (naming.Watcher, error) {
	resp, err := r.kapi.Get(context.Background(), target, &etcdcl.GetOptions{Recursive: true})
	if err != nil {
		return nil, err
	}
	kv := make(map[string]string)
	// Record the index in order to avoid missing updates between Get returning and
	// watch starting.
	index := getNode(resp.Node, kv)
	return &watcher{
		wr: r.kapi.Watcher(target, &etcdcl.WatcherOptions{
			AfterIndex: index,
			Recursive:  true}),
		kv: kv,
	}, nil
}

// NewResolver creates an etcd-based naming.Resolver.
func NewResolver(cfg etcdcl.Config) (naming.Resolver, error) {
	c, err := etcdcl.New(cfg)
	if err != nil {
		return nil, err
	}
	return &resolver{
		kapi: etcdcl.NewKeysAPI(c),
	}, nil
}

type watcher struct {
	wr etcdcl.Watcher
	mu sync.Mutex
	kv map[string]string
}

var once sync.Once

func (w *watcher) Next(ctx context.Context) (nu []*naming.Update, err error) {
	once.Do(func() {
		select {
		case <-ctx.Done():
			err = ctx.Err()
		default:
			for _, v := range w.kv {
				nu = append(nu, &naming.Update{
					Op:   naming.Add,
					Addr: v,
				})
			}
		}
	})
	if len(nu) > 0 || err != nil {
		// once.Do ran. Return directly.
		return
	}
	for {
		resp, err := w.wr.Next(ctx)
		if err != nil {
			return nil, err
		}
		if resp.Node.Dir {
			continue
		}
		w.mu.Lock()
		switch resp.Action {
		case "set":
			if resp.PrevNode == nil {
				nu = append(nu, &naming.Update{
					Op:   naming.Add,
					Addr: resp.Node.Value,
				})
				w.kv[resp.Node.Key] = resp.Node.Value
			} else {
				nu = append(nu, &naming.Update{
					Op:   naming.Delete,
					Addr: w.kv[resp.Node.Key],
				})
				nu = append(nu, &naming.Update{
					Op:   naming.Add,
					Addr: resp.Node.Value,
				})
				w.kv[resp.Node.Key] = resp.Node.Value
			}
		case "delete":
			nu = append(nu, &naming.Update{
				Op:   naming.Delete,
				Addr: resp.Node.Value,
			})
			delete(w.kv, resp.Node.Key)
		}
		w.mu.Unlock()
		return nu, nil
	}
}
