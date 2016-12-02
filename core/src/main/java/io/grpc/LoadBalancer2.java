/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc;

import com.google.common.base.Preconditions;

import java.util.List;
import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;
import javax.annotation.concurrent.NotThreadSafe;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A pluggable component that receives resolved addresses from {@link NameResolver} and provides the
 * channel a usable subchannel when asked.  This is the new interface that will replace {@link
 * LoadBalancer}.
 *
 * <p><strong>Warning: this is still in developement.  Consult gRPC team before starting any related
 * work.</strong>
 *
 * <h3>Overview</h3>
 *
 * <p>A LoadBalancer typically implements three interfaces:
 * <ol>
 *   <li>{@link LoadBalancer2} is the main interface.  All methods on it are invoked sequentially
 *       from the Channel Executor.  It receives the results from the {@link NameResolver}, updates
 *       of subchannels' connectivity states, and the channel's request for the LoadBalancer to
 *       shutdown.</li>
 *   <li>{@link SubchannelPicker} does the actual load-balancing work.  It selects a
 *       {@link Subchannel} for each new RPC.</li>
 *   <li>{@link Factory} creates a new {@link LoadBalancer2} instance.
 * </ol>
 *
 * <p>{@link Helper} is implemented by gRPC library and provided to {@link Factory}. It provides
 * functionalities that a {@code LoadBalancer2} implementation would typically need.</li>
 *
 * <h3>Channel Executor</h3>
 *
 * <p>Channel Executor is an internal executor of the channel, which is used to serialize all the
 * callback methods on the {@link LoadBalancer2} interface, thus the balancer implementation doesn't
 * need to worry about synchronization among them.  However, the actual thread of the Channel
 * Executor is typically the network thread, thus following rules must be followed to prevent
 * blocking or even dead-locking in a network
 *
 * <ol>
 *
 *   <li><strong>Never block in Channel Executor</strong>.  The callback methods must return
 *   quickly.  Examples or work that must be avoided: CPU-intensive calculation, waiting on
 *   synchronization primitives, blocking I/O, blocking RPCs, etc.</li>
 *
 *   <li><strong>Avoid calling into other components with lock held</strong>.  Channel Executor may
 *   run callbacks under a lock, e.g., the transport lock of OkHttp.  If your LoadBalancer has a
 *   lock, holds the lock in a callback method (e.g., {@link #handleSubchannelState}) while calling
 *   into another class that may involve locks, be cautious of deadlock.  Generally you wouldn't
 *   need any locking in the LoadBalancer.</li>
 *
 * </ol>
 *
 * <p>{@link Helper#runSerialized} allows you to schedule a task to be run in the Channel Executor.
 *
 * <h3>The canonical implementation pattern</h3>
 *
 * <p>A {@link LoadBalancer2} keeps states like the latest addresses from NameResolver, the
 * Subchannel(s) and their latest connectivity states.  These states are mutated within the Channel
 * Executor.
 *
 * <p>A typical {@link SubchannelPicker} holds a snapshot of these states.  It may have its own
 * states, e.g., a picker from a round-robin load-balancer may keep a pointer to the next
 * Subchannel, which are typically mutated by multiple threads.  The picker should only mutate its
 * own state, and should not mutate or re-acquire the states of the LoadBalancer.  This way
 * the picker only needs to synchronize its own states, which is typically trivial.
 *
 * <p>When the LoadBalancer states changes, e.g., Subchannels has become or stopped being READY, and
 * we want subsequent RPCs to use the latest list of READY Subchannels, LoadBalancer would create
 * a new picker, which holds a snapshot of the latest Subchannel list.
 *
 * <p>No synchronization should be necessary between LoadBalancer and its pickers if you follow
 * the pattern above.  It may be possible to implement in a different way, but that would usually
 * result in more complicated threading.
 */
@Internal
@NotThreadSafe
public abstract class LoadBalancer2 {
  /**
   * Handles newly resolved server groups and metadata attributes from name resolution system.
   * {@code servers} contained in {@link ResolvedServerInfoGroup} should be considered equivalent
   * but may be flattened into a single list if needed.
   *
   * <p>Implementations should not modify the given {@code servers}.
   *
   * @param servers the resolved server addresses, never empty.
   * @param attributes extra metadata from naming system.
   */
  public abstract void handleResolvedAddresses(
      List<ResolvedServerInfoGroup> servers, Attributes attributes);

  /**
   * Handles an error from the name resolution system.
   *
   * @param error a non-OK status
   */
  public abstract void handleNameResolutionError(Status error);

  /**
   * Handles a state change on a Subchannel.
   *
   * @param subchannel the involved Subchannel
   * @param stateInfo the new state
   */
  public abstract void handleSubchannelState(
      Subchannel subchannel, ConnectivityStateInfo stateInfo);

  /**
   * The channel asks the load-balancer to shutdown.  No more callbacks will be called after this
   * method.  The implementation should shutdown all Subchannels and OOB channels, and do any other
   * cleanup as necessary.
   */
  public abstract void shutdown();

  /**
   * The main balancing logic.  It <strong>must be thread-safe</strong>. Typically it should only
   * synchronize on its own state, and avoid synchronizing with the LoadBalancer's state.
   */
  @ThreadSafe
  public abstract static class SubchannelPicker {
    /**
     * Make a balancing decision for a new RPC.
     *
     * @param affinity the affinity attributes provided via {@link CallOptions#withAffinity}
     * @param headers the headers container of the RPC. It can be mutated within this method.
     */
    public abstract PickResult pickSubchannel(Attributes affinity, Metadata headers);
  }

  /**
   * A balancing decision made by {@link SubchannelPicker} for an RPC.
   */
  @Immutable
  public static final class PickResult {
    private static final PickResult NO_RESULT = new PickResult(null, Status.OK);

    // A READY channel, or null
    @Nullable private final Subchannel subchannel;
    // An error to be propagated to the application if subchannel == null
    // Or OK if there is no error.
    // subchannel being null and error being OK means RPC needs to wait
    private final Status status;

    private PickResult(Subchannel subchannel, Status status) {
      this.subchannel = subchannel;
      this.status = Preconditions.checkNotNull(status, "status");
    }

    /**
     * A decision to proceed the RPC on a Subchannel.  The state of the Subchannel is supposed to be
     * {@link ConnectivityState#READY}.  However, since such decisions are racy, a non-READY
     * Subchannel will not fail the RPC, but will only leave it buffered.
     *
     * <p>Only Subchannels returned by {@link Helper#createSubchannel} will work.  DO NOT try to
     * use your own implementations of Subchannels, as they won't work.
     */
    public static PickResult withSubchannel(Subchannel subchannel) {
      return new PickResult(Preconditions.checkNotNull(subchannel, "subchannel"), Status.OK);
    }

    /**
     * A decision to report a connectivity error to the RPC.  If the RPC is {@link
     * CallOptions#withWaitForReady wait-for-ready}, it will stay buffered.  Otherwise, it will fail
     * with the given error.
     *
     * @param error the error status.  Must not be OK.
     */
    public static PickResult withError(Status error) {
      Preconditions.checkArgument(!error.isOk(), "error status shouldn't be OK");
      return new PickResult(null, error);
    }

    /**
     * No decision could be made.  The RPC will stay buffered.
     */
    public static PickResult withNoResult() {
      return NO_RESULT;
    }

    /**
     * The Subchannel if this result was created by {@link #withSubchannel}, or null otherwise.
     */
    @Nullable
    public Subchannel getSubchannel() {
      return subchannel;
    }

    /**
     * The status associated with this result.  Non-{@code OK} if created with {@link #withError},
     * or {@code OK} otherwise.
     */
    public Status getStatus() {
      return status;
    }

    @Override
    public String toString() {
      return "[subchannel=" + subchannel + " status=" + status + "]";
    }
  }

  /**
   * Provides essentials for LoadBalancer implementations.
   */
  @ThreadSafe
  public abstract static class Helper {
    /**
     * Creates a Subchannel, which is a logical connection to the given group of addresses which are
     * considered equivalent.  The {@code attrs} are custom attributes associated with this
     * Subchannel, and can be accessed later through {@link Subchannel#getAttributes}.
     *
     * <p>The LoadBalancer is responsible for closing unused Subchannels, and closing all
     * Subchannels within {@link #shutdown}.
     */
    public abstract Subchannel createSubchannel(EquivalentAddressGroup addrs, Attributes attrs);

    /**
     * Out-of-band channel for LoadBalancer’s own RPC needs, e.g., talking to an external
     * load-balancer service.
     *
     * <p>The LoadBalancer is responsible for closing unused OOB channels, and closing all OOB
     * channels within {@link #shutdown}.
     */
    public abstract ManagedChannel createOobChannel(
        EquivalentAddressGroup eag, String authority);

    /**
     * Set a new picker to the channel.
     *
     * <p>When a new picker is provided via {@link Helper#updatePicker}, the channel will apply the
     * picker on all buffered RPCs, by calling {@link SubchannelPicker#pickSubchannel}.
     *
     * <p>The channel will hold the picker and use it for all RPCs, until {@link #updatePicker} is
     * called again and a new picker replaces the old one.  If {@link #updatePicker} has never been
     * called, the channel will buffer all RPCs until a picker is provided.
     */
    public abstract void updatePicker(SubchannelPicker picker);

    /**
     * Schedule a task to be run in the Channel Executor, which serializes the task with the
     * callback methods on the {@link LoadBalancer2} interface.
     */
    public abstract void runSerialized(Runnable task);

    /**
     * Returns the NameResolver of the channel.
     */
    public abstract NameResolver.Factory getNameResolverFactory();

    /**
     * Returns the authority string of the channel, which is derived from the DNS-style target name.
     */
    public abstract String getAuthority();
  }

  /**
   * A logical connection to a server, or a group of equivalent servers represented by an {@link
   * EquivalentAddressGroup}.
   *
   * <p>It maintains at most one physical connection (aka transport) for sending new RPCs, while
   * also keeps track of previous transports that has been shut down but not terminated yet.
   *
   * <p>If there isn't an active transport yet, and an RPC is assigned to the Subchannel, it will
   * create a new transport.  It won't actively create transports otherwise.  {@link
   * #requestConnection} can be used to ask Subchannel to create a transport if there isn't any.
   */
  @ThreadSafe
  public abstract static class Subchannel {
    /**
     * Shuts down the Subchannel.  No new RPCs will be accepted.
     */
    public abstract void shutdown();

    /**
     * Asks the Subchannel to create a connection (aka transport), if there isn't an active one.
     */
    public abstract void requestConnection();

    /**
     * Returns the addresses that this Subchannel is bound to.
     */
    public abstract EquivalentAddressGroup getAddresses();

    /**
     * The same attributes passed to {@link io.grpc.LoadBalancer2.Helper#createSubchannel}.
     * LoadBalancer can use it to attach additional information here, e.g., the shard this
     * Subchannel belongs to.
     */
    public abstract Attributes getAttributes();
  }

  @ThreadSafe
  public abstract static class Factory {
    /**
     * Creates a {@link LoadBalancer} that will be used inside a channel.
     */
    public abstract LoadBalancer2 newLoadBalancer(Helper helper);
  }
}
