/*
 * Copyright 2018 The gRPC Authors
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
 */

package io.grpc.internal;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.ManagedChannel;
import java.lang.ref.Reference;
import java.lang.ref.ReferenceQueue;
import java.lang.ref.SoftReference;
import java.lang.ref.WeakReference;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.logging.Level;
import java.util.logging.LogRecord;
import java.util.logging.Logger;

final class ManagedChannelOrphanWrapper extends ForwardingManagedChannel {
  private static final ReferenceQueue<ManagedChannelOrphanWrapper> refqueue =
      new ReferenceQueue<>();
  // Retain the References so they don't get GC'd
  private static final ConcurrentMap<ManagedChannelReference, ManagedChannelReference> refs =
      new ConcurrentHashMap<>();
  private static final Logger logger =
      Logger.getLogger(ManagedChannelOrphanWrapper.class.getName());

  private final ManagedChannelReference phantom;

  ManagedChannelOrphanWrapper(ManagedChannel delegate) {
    this(delegate, refqueue, refs);
  }

  @VisibleForTesting
  ManagedChannelOrphanWrapper(
      ManagedChannel delegate,
      ReferenceQueue<ManagedChannelOrphanWrapper> refqueue,
      ConcurrentMap<ManagedChannelReference, ManagedChannelReference> refs) {
    super(delegate);
    phantom = new ManagedChannelReference(this, delegate, refqueue, refs);
  }

  @Override
  public ManagedChannel shutdown() {
    phantom.shutdown = true;
    phantom.clear();
    return super.shutdown();
  }

  @Override
  public ManagedChannel shutdownNow() {
    phantom.shutdown = true;
    phantom.clear();
    return super.shutdownNow();
  }

  @VisibleForTesting
  static final class ManagedChannelReference extends WeakReference<ManagedChannelOrphanWrapper> {

    private static final String ALLOCATION_SITE_PROPERTY_NAME =
        "io.grpc.ManagedChannel.enableAllocationTracking";

    private static final boolean ENABLE_ALLOCATION_TRACKING =
        Boolean.parseBoolean(System.getProperty(ALLOCATION_SITE_PROPERTY_NAME, "true"));
    private static final RuntimeException missingCallSite = missingCallSite();

    private final ReferenceQueue<ManagedChannelOrphanWrapper> refqueue;
    private final ConcurrentMap<ManagedChannelReference, ManagedChannelReference> refs;

    private final String channelStr;
    private final Reference<RuntimeException> allocationSite;
    private volatile boolean shutdown;

    ManagedChannelReference(
        ManagedChannelOrphanWrapper orphanable,
        ManagedChannel channel,
        ReferenceQueue<ManagedChannelOrphanWrapper> refqueue,
        ConcurrentMap<ManagedChannelReference, ManagedChannelReference> refs) {
      super(orphanable, refqueue);
      allocationSite = new SoftReference<>(
          ENABLE_ALLOCATION_TRACKING
              ? new RuntimeException("ManagedChannel allocation site")
              : missingCallSite);
      this.channelStr = channel.toString();
      this.refqueue = refqueue;
      this.refs = refs;
      this.refs.put(this, this);
      cleanQueue(refqueue);
    }

    /**
     * This clear() is *not* called automatically by the JVM.  As this is a weak ref, the reference
     * will be cleared automatically by the JVM, but will not be removed from {@link #refs}.
     * We do it here to avoid this ending up on the reference queue.
     */
    @Override
    public void clear() {
      clearInternal();
      // We run this here to periodically clean up the queue if at least some of the channels are
      // being shutdown properly.
      cleanQueue(refqueue);
    }

    // avoid reentrancy
    private void clearInternal() {
      super.clear();
      refs.remove(this);
      allocationSite.clear();
    }

    private static RuntimeException missingCallSite() {
      RuntimeException e = new RuntimeException(
          "ManagedChannel allocation site not recorded.  Set -D"
              + ALLOCATION_SITE_PROPERTY_NAME + "=true to enable it");
      e.setStackTrace(new StackTraceElement[0]);
      return e;
    }

    @VisibleForTesting
    static int cleanQueue(ReferenceQueue<ManagedChannelOrphanWrapper> refqueue) {
      ManagedChannelReference ref;
      int orphanedChannels = 0;
      while ((ref = (ManagedChannelReference) refqueue.poll()) != null) {
        RuntimeException maybeAllocationSite = ref.allocationSite.get();
        ref.clearInternal(); // technically the reference is gone already.
        if (!ref.shutdown) {
          orphanedChannels++;
          Level level = Level.SEVERE;
          if (logger.isLoggable(level)) {
            String fmt =
                "*~*~*~ Channel {0} was not shutdown properly!!! ~*~*~*"
                    + System.getProperty("line.separator")
                    + "    Make sure to call shutdown()/shutdownNow() and wait "
                    + "until awaitTermination() returns true.";
            LogRecord lr = new LogRecord(level, fmt);
            lr.setLoggerName(logger.getName());
            lr.setParameters(new Object[] {ref.channelStr});
            lr.setThrown(maybeAllocationSite);
            logger.log(lr);
          }
        }
      }
      return orphanedChannels;
    }
  }
}
