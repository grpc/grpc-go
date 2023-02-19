/*
 *
 * Copyright 2022 gRPC authors.
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

package clientutil

import (
	"google.golang.org/grpc/connectivity"
)

// ClientStateChangeSubscriberInterface defines the functions Management Server
// needs to subscribe connectivity state changes on ClientConn.
type ClientStateChangeSubscriberInterface interface {
	GetStateChannel() chan connectivity.State
	// ClientStateChangeListenOnChannel is invoked when connectivity state changes
	// on ClientConn is published.
	ClientStateChangeListenOnChannel(m connectivity.State)
}

type ClientStateChangePublisher struct {
	stateListeners []chan connectivity.State
}

// Register is going to be called by Management Server Sides
func (p *ClientStateChangePublisher) Register(s ClientStateChangeSubscriberInterface) {
	p.stateListeners = append(p.stateListeners, s.GetStateChannel())
	go func() {
		for {
			state := <-s.GetStateChannel()
			s.ClientStateChangeListenOnChannel(state)
			// TODO(miyoshi): goroutineメモリリークを避けるために終了処理が必要か？
		}
	}()
}

// ClientConn 側から呼ばれる想定
func (p *ClientStateChangePublisher) Publish(m connectivity.State) {
	for _, c := range p.stateListeners {
		c <- m
	}
}
