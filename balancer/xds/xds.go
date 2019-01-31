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
	"context"
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
	ctx, cancel := context.WithCancel(context.Background())
	return &xdsBalancer{
		cc:           cc,
		buildOpts:    opts,
		timeout:      defaultTimeout,
		connStateMgr: &connStateMgr{},
		disconnected: make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
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
	cc           balancer.ClientConn   // won't change
	buildOpts    balancer.BuildOptions // won't change
	timeout      time.Duration         // won't change
	connStateMgr *connStateMgr         // won't change
	ctx          context.Context       // won't change
	cancel       context.CancelFunc    // won't change

	mu       sync.Mutex
	client   *client // may change when passed a different service config
	LBPolicy string  // may change upon new CDS response
	// disconnected is nil when client maintains contact with the remote balancer. It is not nil, when
	// contact is lost.
	disconnected chan struct{}
	config       *xdsConfig // may change when passed a different service config
	lb           balancer.Balancer
	fallbackLB   balancer.Balancer
	fallBackData *fallBackData // may change when HandleResolved address is called
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

type xdsClientConn struct {
	updateState func(s connectivity.State)
	balancer.ClientConn
}

func (w *xdsClientConn) UpdateBalancerState(s connectivity.State, p balancer.Picker) {
	w.updateState(s)
	w.ClientConn.UpdateBalancerState(s, p)
}

func (x *xdsBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.lb != nil {
		x.lb.HandleSubConnStateChange(sc, state)
	}
}

func (x *xdsBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.lb != nil {
		x.lb.HandleResolvedAddrs(addrs, err)
	}
	// remember last returned fallback balancer initialization info.
	x.fallBackData = &fallBackData{addrs, err}
}

func (x *xdsBalancer) HandleBalancerConfig(config json.RawMessage) error {
	var cfg xdsConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return errors.New("unable to unmarshal balancer config into xds config")
	}

	x.mu.Lock()
	//x.config = &cfg
	//balancerName := x.config.balancerName
	//doCDS := x.config.childPolicy != ""
	if x.config.balancerName != cfg.balancerName {
		if x.client != nil {
			x.client.Close()
		}
		x.client = newClient(cfg.balancerName, x.cc.Target(), x.config.childPolicy != "", x.buildOpts, x.newCDSResponse, x.newEDSResponse, x.loseContact)
	}

	if x.config.childPolicy != cfg.childPolicy {
		if x.lb != nil {
		}
	}

	if x.config.fallBackPolicy != cfg.fallBackPolicy {

	}
	x.mu.Unlock()
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
	x.mu.Lock()
	defer x.mu.Unlock()
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
			case <-x.ctx.Done():
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
		case <-x.ctx.Done():
			return
		}
	}()
}

func (x *xdsBalancer) switchFallBack() {
	x.mu.Lock()
	defer x.mu.Unlock()
	select {
	case <-x.disconnected:
		return
	default:
	}
	x.mu.Lock()
	builder := balancer.Get(x.config.fallBackPolicy)
	x.mu.Unlock()
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
	x.cancel()
	if x.lb != nil {
		x.lb.Close()
	}
	if x.client != nil {
		x.client.Close()
	}
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
