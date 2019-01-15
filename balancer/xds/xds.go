/*
 *
 * Copyright 2019 gRPC authors.
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

package xds

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
)

const defaultTimeout = 10 * time.Second

func init() {
	balancer.Register(newXDSBalancerBuilder())
}

type xdsBalancerBuilder struct{}

func (b *xdsBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &xdsBalancer{
		cc:           cc,
		buildOpts:    opts,
		timeout:      defaultTimeout,
		connStateMgr: &connStateMgr{},
		disconnected: make(chan struct{}),
		closed:       make(chan struct{}),
	}
}

func (b *xdsBalancerBuilder) Name() string {
	return "xds"
}

func newXDSBalancerBuilder() balancer.Builder {
	return &xdsBalancerBuilder{}
}

type fallBackData struct {
	addrs []resolver.Address
	err   error
}

type xdsBalancer struct {
	cc        balancer.ClientConn
	buildOpts balancer.BuildOptions
	// mutex to protect????
	client   *client
	LBPolicy string
	timeout  time.Duration
	// disconnected is nil when client maintains contact with the remote balancer. It is not nil, when
	// contact is lost.
	disconnected chan struct{}
	connStateMgr *connStateMgr
	closed       chan struct{}

	configMu sync.RWMutex
	config   *xdsConfig

	lbMu         sync.Mutex
	lb           balancer.Balancer
	fallbackLB   balancer.Balancer
	fallBackData *fallBackData
}

type connStateMgr struct {
	mu       sync.Mutex
	curState connectivity.State
	notify   chan struct{}
}

func (c *connStateMgr) updateState(s connectivity.State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.curState = s
	if s != connectivity.Ready && c.notify != nil {
		close(c.notify)
		c.notify = nil
	}
}

func (c *connStateMgr) notifyWhenNotReady() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.curState != connectivity.Ready {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	c.notify = make(chan struct{})
	return c.notify
}

type wrappedClientConn struct {
	updateState func(s connectivity.State)
	balancer.ClientConn
}

func (w *wrappedClientConn) UpdateBalancerState(s connectivity.State, p balancer.Picker) {
	w.updateState(s)
	w.ClientConn.UpdateBalancerState(s, p)
}

func (x *xdsBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	x.lbMu.Lock()
	defer x.lbMu.Unlock()
	if x.lb != nil {
		x.lb.HandleSubConnStateChange(sc, state)
	}
	if x.fallbackLB != nil {
		x.fallbackLB.HandleSubConnStateChange(sc, state)
	}
}

func (x *xdsBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	x.lbMu.Lock()
	defer x.lbMu.Unlock()
	if x.fallbackLB != nil {
		x.fallbackLB.HandleResolvedAddrs(addrs, err)
	}
	x.fallBackData = &fallBackData{addrs, err}
}

func (x *xdsBalancer) HandleBalancerConfig(config json.RawMessage) error {
	var cfg xdsConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return errors.New("unable to unmarshal balancer config into xds config")
	}

	x.configMu.Lock()
	x.config = &cfg
	balancerName := x.config.balancerName
	doCDS := x.config.childPolicy != ""
	x.configMu.Unlock()

	if x.client != nil {
		x.client.Close()
	}
	x.client = newClient(balancerName, x.cc.Target(), doCDS, x.buildOpts, x.newCDSResponse, x.newEDSResponse, x.loseContact)
	x.client.Run()
	return nil
}

func (x *xdsBalancer) newCDSResponse(resp *api.Cluster) error {
	if resp.GetName() != x.cc.Target() {
		return fmt.Errorf("unmatched service name, got %s, want %s", resp.GetName(), x.cc.Target())
	}
	if resp.GetType() != api.Cluster_EDS {
		return fmt.Errorf("unexpected service discovery type, got %v, want %v", resp.GetType(), api.Cluster_EDS)
	}
	if x.LBPolicy != "" {
		// let LB knows
	}
	x.LBPolicy = resp.GetLbPolicy().String()
	return nil
}

func (x *xdsBalancer) newEDSResponse(resp *api.ClusterLoadAssignment) error {
	if x.disconnected != nil {
		close(x.disconnected)
	}
	// let LB knows
	x.lbMu.Lock()
	defer x.lbMu.Unlock()
	if x.lb == nil {
		if x.fallbackLB != nil {
			x.fallbackLB.Close()
		}
		x.lb = newLB()
	}
	x.lb.UpdateEDS(resp)
	return nil
}

func (x *xdsBalancer) loseContact() {
	if x.disconnected != nil {
		return
	}
	x.disconnected = make(chan struct{})
	go func() {
		if x.lb == nil {
			// this is the startup
			select {
			case <-time.After(x.timeout):
				//TODO: start fallback
				x.switchFallBack()
				return
			case <-x.disconnected:
				return
			case <-x.closed:
				return
			}
		}

		select {
		case <-time.After(x.timeout):
			x.switchFallBack()
		case <-x.connStateMgr.notifyWhenNotReady():
			x.switchFallBack()
		case <-x.disconnected:
			return
		case <-x.closed:
			return
		}
	}()
}

func (x *xdsBalancer) switchFallBack() {
	x.lbMu.Lock()
	defer x.lbMu.Unlock()
	select {
	case <-x.disconnected:
		return
	default:
	}
	x.configMu.RLock()
	builder := balancer.Get(x.config.fallBackPolicy)
	x.configMu.RUnlock()
	x.fallbackLB = builder.Build(x.cc, x.buildOpts)
	if x.fallBackData != nil {
		x.fallbackLB.HandleResolvedAddrs(x.fallBackData.addrs, x.fallBackData.err)
	}
	if x.lb != nil {
		x.lb.Close()
		x.lb = nil
	}
}

func (x *xdsBalancer) Close() {
	if x.fallbackLB != nil {
		x.fallbackLB.Close()
	}
	if x.lb != nil {
		x.lb.Close()
	}
	if x.client != nil {
		x.client.Close()
	}
	close(x.closed)
	panic("implement me")
}

func (p *xdsConfig) UnmarshalJSON(data []byte) error {
	var val map[string]interface{}
	if err := json.Unmarshal(data, &val); err != nil {
		return err
	}
	for k, v := range val {
		switch k {
		case "balancerName":
			p.balancerName = v.(string)
		case "childPolicy":
			for sk, _ := range v.(map[string]interface{}) {
				p.childPolicy = sk
				break
			}
		case "fallBackPolicy":
			for sk, _ := range v.(map[string]interface{}) {
				p.fallBackPolicy = sk
				break
			}
		}
	}

	return nil
}

func (p *xdsConfig) MarshalJSON() ([]byte, error) {
	return nil, nil
}

type xdsConfig struct {
	balancerName   string
	childPolicy    string
	fallBackPolicy string
}
