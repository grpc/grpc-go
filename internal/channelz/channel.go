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
	"time"

	"google.golang.org/grpc/connectivity"
)

// ChannelMetric defines the info channelz provides for a specific Channel, which
// includes ChannelInternalMetric and channelz-specific data, such as channelz id,
// child list, etc.
type ChannelMetric struct {
	// ID is the channelz id of this channel.
	ID int64
	// RefName is the human readable reference string of this channel.
	RefName string
	// ChannelData contains channel internal metric reported by the channel through
	// ChannelzMetric().
	ChannelData *ChannelInternalMetric
	// NestedChans tracks the nested channel type children of this channel in the format of
	// a map from nested channel channelz id to corresponding reference string.
	NestedChans map[int64]string
	// SubChans tracks the subchannel type children of this channel in the format of a
	// map from subchannel channelz id to corresponding reference string.
	SubChans map[int64]string
	// Trace contains the most recent traced events.
	Trace *ChannelTrace
}

// ChannelInternalMetric defines a collection of metrics related to a channel.
type ChannelInternalMetric struct {
	// current connectivity state of the channel.
	State connectivity.State
	// The target this channel originally tried to connect to.  May be absent
	Target string
	// The number of calls started on the channel.
	CallsStarted int64
	// The number of calls that have completed with an OK status.
	CallsSucceeded int64
	// The number of calls that have a completed with a non-OK status.
	CallsFailed int64
	// The last time a call was started on the channel.
	LastCallStartedTimestamp time.Time
}

// Channel is the interface that should be satisfied in order to be tracked by
// channelz as Channel or SubChannel.
type Channel interface {
	ChannelzMetric() *ChannelInternalMetric
}

type dummyChannel struct{}

func (d *dummyChannel) ChannelzMetric() *ChannelInternalMetric {
	return &ChannelInternalMetric{}
}

type channel struct {
	refName     string
	c           Channel
	closeCalled bool
	nestedChans map[int64]string
	subChans    map[int64]string
	id          int64
	pid         int64
	cm          *channelMap
	trace       *channelTrace
	// traceRefCount is the number of trace events that reference this channel.
	// Non-zero traceRefCount means the trace of this channel cannot be deleted.
	traceRefCount int32
}

func (c *channel) addChild(id int64, e entry) {
	switch v := e.(type) {
	case *subChannel:
		c.subChans[id] = v.refName
	case *channel:
		c.nestedChans[id] = v.refName
	default:
		logger.Errorf("cannot add a child (id = %d) of type %T to a channel", id, e)
	}
}

func (c *channel) deleteChild(id int64) {
	delete(c.subChans, id)
	delete(c.nestedChans, id)
	c.deleteSelfIfReady()
}

func (c *channel) triggerDelete() {
	c.closeCalled = true
	c.deleteSelfIfReady()
}

func (c *channel) getParentID() int64 {
	return c.pid
}

// deleteSelfFromTree tries to delete the channel from the channelz entry relation tree, which means
// deleting the channel reference from its parent's child list.
//
// In order for a channel to be deleted from the tree, it must meet the criteria that, removal of the
// corresponding grpc object has been invoked, and the channel does not have any children left.
//
// The returned boolean value indicates whether the channel has been successfully deleted from tree.
func (c *channel) deleteSelfFromTree() (deleted bool) {
	if !c.closeCalled || len(c.subChans)+len(c.nestedChans) != 0 {
		return false
	}
	// not top channel
	if c.pid != 0 {
		c.cm.findEntry(c.pid).deleteChild(c.id)
	}
	return true
}

// deleteSelfFromMap checks whether it is valid to delete the channel from the map, which means
// deleting the channel from channelz's tracking entirely. Users can no longer use id to query the
// channel, and its memory will be garbage collected.
//
// The trace reference count of the channel must be 0 in order to be deleted from the map. This is
// specified in the channel tracing gRFC that as long as some other trace has reference to an entity,
// the trace of the referenced entity must not be deleted. In order to release the resource allocated
// by grpc, the reference to the grpc object is reset to a dummy object.
//
// deleteSelfFromMap must be called after deleteSelfFromTree returns true.
//
// It returns a bool to indicate whether the channel can be safely deleted from map.
func (c *channel) deleteSelfFromMap() (delete bool) {
	if c.getTraceRefCount() != 0 {
		c.c = &dummyChannel{}
		return false
	}
	return true
}

// deleteSelfIfReady tries to delete the channel itself from the channelz database.
// The delete process includes two steps:
//  1. delete the channel from the entry relation tree, i.e. delete the channel reference from its
//     parent's child list.
//  2. delete the channel from the map, i.e. delete the channel entirely from channelz. Lookup by id
//     will return entry not found error.
func (c *channel) deleteSelfIfReady() {
	if !c.deleteSelfFromTree() {
		return
	}
	if !c.deleteSelfFromMap() {
		return
	}
	c.cm.deleteEntry(c.id)
	c.trace.clear()
}

func (c *channel) getChannelTrace() *channelTrace {
	return c.trace
}

func (c *channel) incrTraceRefCount() {
	atomic.AddInt32(&c.traceRefCount, 1)
}

func (c *channel) decrTraceRefCount() {
	atomic.AddInt32(&c.traceRefCount, -1)
}

func (c *channel) getTraceRefCount() int {
	i := atomic.LoadInt32(&c.traceRefCount)
	return int(i)
}

func (c *channel) getRefName() string {
	return c.refName
}
