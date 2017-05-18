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

import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.List;
import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;
import javax.annotation.concurrent.NotThreadSafe;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A pluggable component that receives resolved addresses from {@link NameResolver} and provides the
 * channel a usable subchannel when asked.
 *
 * <h3>Overview</h3>
 *
 * <p>A LoadBalancer typically implements three interfaces:
 * <ol>
 *   <li>{@link LoadBalancer} is the main interface.  All methods on it are invoked sequentially
 *       from the Channel Executor.  It receives the results from the {@link NameResolver}, updates
 *       of subchannels' connectivity states, and the channel's request for the LoadBalancer to
 *       shutdown.</li>
 *   <li>{@link SubchannelPicker SubchannelPicker} does the actual load-balancing work.  It selects
 *       a {@link Subchannel Subchannel} for each new RPC.</li>
 *   <li>{@link Factory Factory} creates a new {@link LoadBalancer} instance.
 * </ol>
 *
 * <p>{@link Helper Helper} is implemented by gRPC library and provided to {@link Factory
 * Factory}. It provides functionalities that a {@code LoadBalancer} implementation would typically
 * need.
 *
 * <h3>Channel Executor</h3>
 *
 * <p>Channel Executor is an internal executor of the channel, which is used to serialize all the
 * callback methods on the {@link LoadBalancer} interface, thus the balancer implementation doesn't
 * need to worry about synchronization among them.  However, the actual thread of the Channel
 * Executor is typically the network thread, thus the following rules must be followed to prevent
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
 *   lock, holds the lock in a callback method (e.g., {@link #handleSubchannelState
 *   handleSubchannelState()}) while calling into another class that may involve locks, be cautious
 *   of deadlock.  Generally you wouldn't need any locking in the LoadBalancer.</li>
 *
 * </ol>
 *
 * <p>{@link Helper#runSerialized Helper.runSerialized()} allows you to schedule a task to be run in
 * the Channel Executor.
 *
 * <h3>The canonical implementation pattern</h3>
 *
 * <p>A {@link LoadBalancer} keeps states like the latest addresses from NameResolver, the
 * Subchannel(s) and their latest connectivity states.  These states are mutated within the Channel
 * Executor.
 *
 * <p>A typical {@link SubchannelPicker SubchannelPicker} holds a snapshot of these states.  It may
 * have its own states, e.g., a picker from a round-robin load-balancer may keep a pointer to the
 * next Subchannel, which are typically mutated by multiple threads.  The picker should only mutate
 * its own state, and should not mutate or re-acquire the states of the LoadBalancer.  This way the
 * picker only needs to synchronize its own states, which is typically trivial to implement.
 *
 * <p>When the LoadBalancer states changes, e.g., Subchannels has become or stopped being READY, and
 * we want subsequent RPCs to use the latest list of READY Subchannels, LoadBalancer would create
 * a new picker, which holds a snapshot of the latest Subchannel list.  Refer to the javadoc of
 * {@link #handleSubchannelState handleSubchannelState()} how to do this properly.
 *
 * <p>No synchronization should be necessary between LoadBalancer and its pickers if you follow
 * the pattern above.  It may be possible to implement in a different way, but that would usually
 * result in more complicated threading.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
@NotThreadSafe
public abstract class LoadBalancer {
  /**
   * Handles newly resolved server groups and metadata attributes from name resolution system.
   * {@code servers} contained in {@link ResolvedServerInfoGroup} should be considered equivalent
   * but may be flattened into a single list if needed.
   *
   * <p>Implementations should not modify the given {@code servers}.
   *
   * @deprecated Implement {@link #handleResolvedAddressGroups} instead.  As it is deprecated, the
   *             {@link ResolvedServerInfo}s from the passed-in {@link ResolvedServerInfoGroup}s
   *             lose all their attributes.
   *
   * @param servers the resolved server addresses, never empty.
   * @param attributes extra metadata from naming system.
   */
  @Deprecated
  public void handleResolvedAddresses(
      List<ResolvedServerInfoGroup> servers, Attributes attributes) {
    throw new UnsupportedOperationException("This is deprecated and should not be called");
  }

  /**
   * Handles newly resolved server groups and metadata attributes from name resolution system.
   * {@code servers} contained in {@link EquivalentAddressGroup} should be considered equivalent
   * but may be flattened into a single list if needed.
   *
   * <p>Implementations should not modify the given {@code servers}.
   *
   * @param servers the resolved server addresses, never empty.
   * @param attributes extra metadata from naming system.
   */
  @SuppressWarnings("deprecation")
  public void handleResolvedAddressGroups(
      List<EquivalentAddressGroup> servers, Attributes attributes) {
    ArrayList<ResolvedServerInfoGroup> serverInfoGroups =
        new ArrayList<ResolvedServerInfoGroup>(servers.size());
    for (EquivalentAddressGroup eag : servers) {
      ResolvedServerInfoGroup.Builder serverInfoGroupBuilder =
          ResolvedServerInfoGroup.builder(eag.getAttributes());
      for (SocketAddress addr : eag.getAddresses()) {
        serverInfoGroupBuilder.add(new ResolvedServerInfo(addr));
      }
      serverInfoGroups.add(serverInfoGroupBuilder.build());
    }
    handleResolvedAddresses(serverInfoGroups, attributes);
  }

  /**
   * Handles an error from the name resolution system.
   *
   * @param error a non-OK status
   */
  public abstract void handleNameResolutionError(Status error);

  /**
   * Handles a state change on a Subchannel.
   *
   * <p>The initial state of a Subchannel is IDLE. You won't get a notification for the initial IDLE
   * state.
   *
   * <p>If the new state is not SHUTDOWN, this method should create a new picker and call {@link
   * Helper#updatePicker Helper.updatePicker()}.  Failing to do so may result in unnecessary delays
   * of RPCs. Please refer to {@link PickResult#withSubchannel PickResult.withSubchannel()}'s
   * javadoc for more information.
   *
   * <p>SHUTDOWN can only happen in two cases.  One is that LoadBalancer called {@link
   * Subchannel#shutdown} earlier, thus it should have already discarded this Subchannel.  The other
   * is that Channel is doing a {@link ManagedChannel#shutdownNow forced shutdown} or has already
   * terminated, thus there won't be further requests to LoadBalancer.  Therefore, SHUTDOWN can be
   * safely ignored.
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
     * @param args the pick arguments
     */
    public abstract PickResult pickSubchannel(PickSubchannelArgs args);
  }

  /**
   * Provides arguments for a {@link SubchannelPicker#pickSubchannel(
   * LoadBalancer.PickSubchannelArgs)}.
   */
  public abstract static class PickSubchannelArgs {

    /**
     * Call options.
     */
    public abstract CallOptions getCallOptions();

    /**
     * Headers of the call. {@code pickSubchannel()} may mutate it before before returning.
     */
    public abstract Metadata getHeaders();

    /**
     * Call method.
     */
    public abstract MethodDescriptor<?, ?> getMethodDescriptor();
  }

  /**
   * A balancing decision made by {@link SubchannelPicker SubchannelPicker} for an RPC.
   *
   * <p>The outcome of the decision will be one of the following:
   * <ul>
   *   <li>Proceed: if a Subchannel is provided via {@link #withSubchannel withSubchannel()}, and is
   *       in READY state when the RPC tries to start on it, the RPC will proceed on that
   *       Subchannel.</li>
   *   <li>Error: if an error is provided via {@link #withError withError()}, and the RPC is not
   *       wait-for-ready (i.e., {@link CallOptions#withWaitForReady} was not called), the RPC will
   *       fail immediately with the given error.</li>
   *   <li>Buffer: in all other cases, the RPC will be buffered in the Channel, until the next
   *       picker is provided via {@link Helper#updatePicker Helper.updatePicker()}, when the RPC
   *       will go through the same picking process again.</li>
   * </ul>
   */
  @Immutable
  public static final class PickResult {
    private static final PickResult NO_RESULT = new PickResult(null, null, Status.OK);

    @Nullable private final Subchannel subchannel;
    @Nullable private final ClientStreamTracer.Factory streamTracerFactory;
    // An error to be propagated to the application if subchannel == null
    // Or OK if there is no error.
    // subchannel being null and error being OK means RPC needs to wait
    private final Status status;

    private PickResult(
        @Nullable Subchannel subchannel, @Nullable ClientStreamTracer.Factory streamTracerFactory,
        Status status) {
      this.subchannel = subchannel;
      this.streamTracerFactory = streamTracerFactory;
      this.status = Preconditions.checkNotNull(status, "status");
    }

    /**
     * A decision to proceed the RPC on a Subchannel.
     *
     * <p>Only Subchannels returned by {@link Helper#createSubchannel Helper.createSubchannel()}
     * will work.  DO NOT try to use your own implementations of Subchannels, as they won't work.
     *
     * <p>When the RPC tries to use the return Subchannel, which is briefly after this method
     * returns, the state of the Subchannel will decide where the RPC would go:
     *
     * <ul>
     *   <li>READY: the RPC will proceed on this Subchannel.</li>
     *   <li>IDLE: the RPC will be buffered.  Subchannel will attempt to create connection.</li>
     *   <li>All other states: the RPC will be buffered.</li>
     * </ul>
     *
     * <p><strong>All buffered RPCs will stay buffered</strong> until the next call of {@link
     * Helper#updatePicker Helper.updatePicker()}, which will trigger a new picking process.
     *
     * <p>Note that Subchannel's state may change at the same time the picker is making the
     * decision, which means the decision may be made with (to-be) outdated information.  For
     * example, a picker may return a Subchannel known to be READY, but it has become IDLE when is
     * about to be used by the RPC, which makes the RPC to be buffered.  The LoadBalancer will soon
     * learn about the Subchannels' transition from READY to IDLE, create a new picker and allow the
     * RPC to use another READY transport if there is any.
     *
     * <p>You will want to avoid running into a situation where there are READY Subchannels out
     * there but some RPCs are still buffered for longer than a brief time.
     * <ul>
     *   <li>This can happen if you return Subchannels with states other than READY and IDLE.  For
     *       example, suppose you round-robin on 2 Subchannels, in READY and CONNECTING states
     *       respectively.  If the picker ignores the state and pick them equally, 50% of RPCs will
     *       be stuck in buffered state until both Subchannels are READY.</li>
     *   <li>This can also happen if you don't create a new picker at key state changes of
     *       Subchannels.  Take the above round-robin example again.  Suppose you do pick only READY
     *       and IDLE Subchannels, and initially both Subchannels are READY.  Now one becomes IDLE,
     *       then CONNECTING and stays CONNECTING for a long time.  If you don't create a new picker
     *       in response to the CONNECTING state to exclude that Subchannel, 50% of RPCs will hit it
     *       and be buffered even though the other Subchannel is READY.</li>
     * </ul>
     *
     * <p>In order to prevent unnecessary delay of RPCs, the rules of thumb are:
     * <ol>
     *   <li>The picker should only pick Subchannels that are known as READY or IDLE.  Whether to
     *       pick IDLE Subchannels depends on whether you want Subchannels to connect on-demand or
     *       actively:
     *       <ul>
     *         <li>If you want connect-on-demand, include IDLE Subchannels in your pick results,
     *             because when an RPC tries to use an IDLE Subchannel, the Subchannel will try to
     *             connect.</li>
     *         <li>If you want Subchannels to be always connected even when there is no RPC, you
     *             would call {@link Subchannel#requestConnection Subchannel.requestConnection()}
     *             whenever the Subchannel has transitioned to IDLE, then you don't need to include
     *             IDLE Subchannels in your pick results.</li>
     *       </ul></li>
     *   <li>Always create a new picker and call {@link Helper#updatePicker Helper.updatePicker()}
     *       whenever {@link #handleSubchannelState handleSubchannelState()} is called, unless the
     *       new state is SHUTDOWN. See {@code handleSubchannelState}'s javadoc for more
     *       details.</li>
     * </ol>
     *
     * @param subchannel the picked Subchannel
     * @param streamTracerFactory if not null, will be used to trace the activities of the stream
     *                            created as a result of this pick. Note it's possible that no
     *                            stream is created at all in some cases.
     */
    public static PickResult withSubchannel(
        Subchannel subchannel, @Nullable ClientStreamTracer.Factory streamTracerFactory) {
      return new PickResult(
          Preconditions.checkNotNull(subchannel, "subchannel"), streamTracerFactory, Status.OK);
    }

    /**
     * Equivalent to {@code withSubchannel(subchannel, null)}.
     */
    public static PickResult withSubchannel(Subchannel subchannel) {
      return withSubchannel(subchannel, null);
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
      return new PickResult(null, null, error);
    }

    /**
     * No decision could be made.  The RPC will stay buffered.
     */
    public static PickResult withNoResult() {
      return NO_RESULT;
    }

    /**
     * The Subchannel if this result was created by {@link #withSubchannel withSubchannel()}, or
     * null otherwise.
     */
    @Nullable
    public Subchannel getSubchannel() {
      return subchannel;
    }

    /**
     * The stream tracer factory this result was created with.
     */
    @Nullable
    public ClientStreamTracer.Factory getStreamTracerFactory() {
      return streamTracerFactory;
    }

    /**
     * The status associated with this result.  Non-{@code OK} if created with {@link #withError
     * withError}, or {@code OK} otherwise.
     */
    public Status getStatus() {
      return status;
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("subchannel", subchannel)
          .add("streamTracerFactory", streamTracerFactory)
          .add("status", status)
          .toString();
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(subchannel, status, streamTracerFactory);
    }

    /**
     * Returns true if the {@link Subchannel}, {@link Status}, and
     * {@link ClientStreamTracer.Factory} all match.
     */
    @Override
    public boolean equals(Object other) {
      if (!(other instanceof PickResult)) {
        return false;
      }
      PickResult that = (PickResult) other;
      return Objects.equal(subchannel, that.subchannel) && Objects.equal(status, that.status)
          && Objects.equal(streamTracerFactory, that.streamTracerFactory);
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
     * Subchannel, and can be accessed later through {@link Subchannel#getAttributes
     * Subchannel.getAttributes()}.
     *
     * <p>The LoadBalancer is responsible for closing unused Subchannels, and closing all
     * Subchannels within {@link #shutdown}.
     */
    public abstract Subchannel createSubchannel(EquivalentAddressGroup addrs, Attributes attrs);

    /**
     * Replaces the existing addresses used with {@code subchannel}. This method is superior to
     * {@link #createSubchannel} when the new and old addresses overlap, since the subchannel can
     * continue using an existing connection.
     *
     * @throws IllegalArgumentException if {@code subchannel} was not returned from {@link
     *     #createSubchannel}
     */
    public void updateSubchannelAddresses(Subchannel subchannel, EquivalentAddressGroup addrs) {
      throw new UnsupportedOperationException();
    }

    /**
     * Out-of-band channel for LoadBalancer’s own RPC needs, e.g., talking to an external
     * load-balancer service.
     *
     * <p>The LoadBalancer is responsible for closing unused OOB channels, and closing all OOB
     * channels within {@link #shutdown}.
     */
    public abstract ManagedChannel createOobChannel(EquivalentAddressGroup eag, String authority);

    /**
     * Updates the addresses used for connections in the {@code Channel}. This is supperior to
     * {@link #createOobChannel} when the old and new addresses overlap, since the channel can
     * continue using an existing connection.
     *
     * @throws IllegalArgumentException if {@code channel} was not returned from {@link
     *     #createOobChannel}
     */
    public void updateOobChannelAddresses(ManagedChannel channel, EquivalentAddressGroup eag) {
      throw new UnsupportedOperationException();
    }

    /**
     * Set a new picker to the channel.
     *
     * <p>When a new picker is provided via {@code updatePicker()}, the channel will apply the
     * picker on all buffered RPCs, by calling {@link SubchannelPicker#pickSubchannel(
     * LoadBalancer.PickSubchannelArgs)}.
     *
     * <p>The channel will hold the picker and use it for all RPCs, until {@code updatePicker()} is
     * called again and a new picker replaces the old one.  If {@code updatePicker()} has never been
     * called, the channel will buffer all RPCs until a picker is provided.
     */
    public abstract void updatePicker(SubchannelPicker picker);

    /**
     * Schedule a task to be run in the Channel Executor, which serializes the task with the
     * callback methods on the {@link LoadBalancer} interface.
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
   * #requestConnection requestConnection()} can be used to ask Subchannel to create a transport if
   * there isn't any.
   */
  @ThreadSafe
  public abstract static class Subchannel {
    /**
     * Shuts down the Subchannel.  After this method is called, this Subchannel should no longer
     * be returned by the latest {@link SubchannelPicker picker}, and can be safely discarded.
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
     * The same attributes passed to {@link Helper#createSubchannel Helper.createSubchannel()}.
     * LoadBalancer can use it to attach additional information here, e.g., the shard this
     * Subchannel belongs to.
     */
    public abstract Attributes getAttributes();
  }

  /**
   * Factory to create {@link LoadBalancer} instance.
   */
  @ThreadSafe
  public abstract static class Factory {
    /**
     * Creates a {@link LoadBalancer} that will be used inside a channel.
     */
    public abstract LoadBalancer newLoadBalancer(Helper helper);
  }
}
