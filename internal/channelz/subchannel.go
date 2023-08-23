/*
 *
 * Copyright 2018 gRPC authors.
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

package channelz

import (
	"sync/atomic"
)

// SubChannelMetric defines the info channelz provides for a specific SubChannel,
// which includes ChannelInternalMetric and channelz-specific data, such as
// channelz id, child list, etc.
type SubChannelMetric struct {
	// ID is the channelz id of this subchannel.
	ID int64
	// RefName is the human readable reference string of this subchannel.
	RefName string
	// ChannelData contains subchannel internal metric reported by the subchannel
	// through ChannelzMetric().
	ChannelData *ChannelInternalMetric
	// NestedChans tracks the nested channel type children of this subchannel in the format of
	// a map from nested channel channelz id to corresponding reference string.
	// Note current grpc implementation doesn't allow subchannel to have nested channels
	// as children, therefore, this field is unused.
	NestedChans map[int64]string
	// SubChans tracks the subchannel type children of this subchannel in the format of a
	// map from subchannel channelz id to corresponding reference string.
	// Note current grpc implementation doesn't allow subchannel to have subchannels
	// as children, therefore, this field is unused.
	SubChans map[int64]string
	// Sockets tracks the socket type children of this subchannel in the format of a map
	// from socket channelz id to corresponding reference string.
	Sockets map[int64]string
	// Trace contains the most recent traced events.
	Trace *ChannelTrace
}

type subChannel struct {
	refName       string
	c             Channel
	closeCalled   bool
	sockets       map[int64]string
	id            int64
	pid           int64
	cm            *channelMap
	trace         *channelTrace
	traceRefCount int32
}

func (sc *subChannel) addChild(id int64, e entry) {
	if v, ok := e.(*normalSocket); ok {
		sc.sockets[id] = v.refName
	} else {
		logger.Errorf("cannot add a child (id = %d) of type %T to a subChannel", id, e)
	}
}

func (sc *subChannel) deleteChild(id int64) {
	delete(sc.sockets, id)
	sc.deleteSelfIfReady()
}

func (sc *subChannel) triggerDelete() {
	sc.closeCalled = true
	sc.deleteSelfIfReady()
}

func (sc *subChannel) getParentID() int64 {
	return sc.pid
}

// deleteSelfFromTree tries to delete the subchannel from the channelz entry relation tree, which
// means deleting the subchannel reference from its parent's child list.
//
// In order for a subchannel to be deleted from the tree, it must meet the criteria that, removal of
// the corresponding grpc object has been invoked, and the subchannel does not have any children left.
//
// The returned boolean value indicates whether the channel has been successfully deleted from tree.
func (sc *subChannel) deleteSelfFromTree() (deleted bool) {
	if !sc.closeCalled || len(sc.sockets) != 0 {
		return false
	}
	sc.cm.findEntry(sc.pid).deleteChild(sc.id)
	return true
}

// deleteSelfFromMap checks whether it is valid to delete the subchannel from the map, which means
// deleting the subchannel from channelz's tracking entirely. Users can no longer use id to query
// the subchannel, and its memory will be garbage collected.
//
// The trace reference count of the subchannel must be 0 in order to be deleted from the map. This is
// specified in the channel tracing gRFC that as long as some other trace has reference to an entity,
// the trace of the referenced entity must not be deleted. In order to release the resource allocated
// by grpc, the reference to the grpc object is reset to a dummy object.
//
// deleteSelfFromMap must be called after deleteSelfFromTree returns true.
//
// It returns a bool to indicate whether the channel can be safely deleted from map.
func (sc *subChannel) deleteSelfFromMap() (delete bool) {
	if sc.getTraceRefCount() != 0 {
		// free the grpc struct (i.e. addrConn)
		sc.c = &dummyChannel{}
		return false
	}
	return true
}

// deleteSelfIfReady tries to delete the subchannel itself from the channelz database.
// The delete process includes two steps:
//  1. delete the subchannel from the entry relation tree, i.e. delete the subchannel reference from
//     its parent's child list.
//  2. delete the subchannel from the map, i.e. delete the subchannel entirely from channelz. Lookup
//     by id will return entry not found error.
func (sc *subChannel) deleteSelfIfReady() {
	if !sc.deleteSelfFromTree() {
		return
	}
	if !sc.deleteSelfFromMap() {
		return
	}
	sc.cm.deleteEntry(sc.id)
	sc.trace.clear()
}

func (sc *subChannel) getChannelTrace() *channelTrace {
	return sc.trace
}

func (sc *subChannel) incrTraceRefCount() {
	atomic.AddInt32(&sc.traceRefCount, 1)
}

func (sc *subChannel) decrTraceRefCount() {
	atomic.AddInt32(&sc.traceRefCount, -1)
}

func (sc *subChannel) getTraceRefCount() int {
	i := atomic.LoadInt32(&sc.traceRefCount)
	return int(i)
}

func (sc *subChannel) getRefName() string {
	return sc.refName
}
