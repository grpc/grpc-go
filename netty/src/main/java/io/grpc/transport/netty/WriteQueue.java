/*
 * Copyright 2015, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
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
 */

package io.grpc.transport.netty;

import com.google.common.base.Preconditions;

import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelPromise;

import java.util.ArrayDeque;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * A queue of pending writes to a {@link Channel} that is flushed as a single unit.
 */
class WriteQueue {

  private static final int DEQUE_CHUNK_SIZE = 128;

  /**
   * {@link Runnable} used to schedule work onto the tail of the event loop.
   */
  private final Runnable later = new Runnable() {
    @Override
    public void run() {
      flush();
    }
  };

  private final Channel channel;
  private final BlockingQueue<Runnable> queue;
  private final AtomicBoolean scheduled = new AtomicBoolean();

  /**
   * ArrayDeque to copy queue into when flushing in event loop.
   */
  private final ArrayDeque<Runnable> writeChunk =
      new ArrayDeque<Runnable>(DEQUE_CHUNK_SIZE);

  public WriteQueue(Channel channel) {
    this.channel = Preconditions.checkNotNull(channel, "channel");
    queue = new LinkedBlockingQueue<Runnable>();
  }

  /**
   * Schedule a flush on the channel.
   */
  void scheduleFlush() {
    if (scheduled.compareAndSet(false, true)) {
      // Add the queue to the tail of the event loop so writes will be executed immediately
      // inside the event loop. Note DO NOT do channel.write outside the event loop as
      // it will not wake up immediately without a flush.
      channel.eventLoop().execute(later);
    }
  }

  /**
   * Enqueue a write command on the channel.
   *
   * @param command a write to be executed on the channel.
   * @param flush true if a flush of the write should be schedule, false if a later call to
   *              enqueue will schedule the flush.
   */
  ChannelFuture enqueue(Object command, boolean flush) {
    return enqueue(command, channel.newPromise(), flush);
  }

  /**
   * Enqueue a write command with a completion listener.
   *
   * @param command a write to be executed on the channel.
   * @param promise to be marked on the completion of the write.
   * @param flush true if a flush of the write should be schedule, false if a later call to
   *              enqueue will schedule the flush.
   */
  ChannelFuture enqueue(Object command, ChannelPromise promise, boolean flush) {
    queue.add(new QueuedCommand(command, promise));
    if (flush) {
      scheduleFlush();
    }
    return promise;
  }

  /**
   * Process the queue of commands and dispatch them to the stream. This method is only
   * called in the event loop
   */
  private void flush() {
    try {
      boolean flushed = false;
      while (queue.drainTo(writeChunk, DEQUE_CHUNK_SIZE) > 0) {
        while (writeChunk.size() > 0) {
          writeChunk.poll().run();
        }
        // Flush each chunk so we are releasing buffers periodically. In theory this loop
        // might never end as new events are continuously added to the queue, if we never
        // flushed in that case we would be guaranteed to OOM.
        flushed = true;
        channel.flush();
      }
      if (!flushed) {
        // Must flush at least once
        channel.flush();
      }
    } finally {
      // Mark the write as done, if the queue is non-empty after marking trigger a new write.
      scheduled.set(false);
      if (!queue.isEmpty()) {
        scheduleFlush();
      }
    }
  }

  /**
   * Simple wrapper type around a command and its optional completion listener.
   */
  private final class QueuedCommand implements Runnable {
    private final Object command;
    private final ChannelPromise promise;

    private QueuedCommand(Object command, ChannelPromise promise) {
      this.command = command;
      this.promise = promise;
    }

    @Override
    public void run() {
      channel.write(command, promise);
    }
  }
}

