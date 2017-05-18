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

package io.grpc;

import java.util.ArrayList;
import java.util.concurrent.Callable;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A context propagation mechanism which can carry scoped-values across API boundaries and between
 * threads. Examples of state propagated via context include:
 * <ul>
 *   <li>Security principals and credentials.</li>
 *   <li>Local and distributed tracing information.</li>
 * </ul>
 *
 * <p>A Context object can be {@link #attach attached} to the {@link Storage}, which effectively
 * forms a <b>scope</b> for the context.  The scope is bound to the current thread.  Within a scope,
 * its Context is accessible even across API boundaries, through {@link #current}.  The scope is
 * later exited by {@link #detach detaching} the Context.
 *
 * <p>Context objects are immutable and inherit state from their parent. To add or overwrite the
 * current state a new context object must be created and then attached, replacing the previously
 * bound context. For example:
 *
 * <pre>
 *   Context withCredential = Context.current().withValue(CRED_KEY, cred);
 *   withCredential.run(new Runnable() {
 *     public void run() {
 *        readUserRecords(userId, CRED_KEY.get());
 *     }
 *   });
 * </pre>
 *
 * <p>Contexts are also used to represent a scoped unit of work. When the unit of work is done the
 * context can be cancelled. This cancellation will also cascade to all descendant contexts. You can
 * add a {@link CancellationListener} to a context to be notified when it or one of its ancestors
 * has been cancelled. Cancellation does not release the state stored by a context and it's
 * perfectly valid to {@link #attach()} an already cancelled context to make it current. To cancel a
 * context (and its descendants) you first create a {@link CancellableContext} and when you need to
 * signal cancellation call {@link CancellableContext#cancel} or {@link
 * CancellableContext#detachAndCancel}. For example:
 * <pre>
 *   CancellableContext withCancellation = Context.current().withCancellation();
 *   try {
 *     withCancellation.run(new Runnable() {
 *       public void run() {
 *         while (waitingForData() &amp;&amp; !Context.current().isCancelled()) {}
 *       }
 *     });
 *     doSomeWork();
 *   } catch (Throwable t) {
 *      withCancellation.cancel(t);
 *   }
 * </pre>
 *
 * <p>Contexts can also be created with a timeout relative to the system nano clock which will
 * cause it to automatically cancel at the desired time.
 *
 *
 * <p>Notes and cautions on use:
 * <ul>
 *    <li>Every {@code attach()} should have a {@code detach()} in the same method. And every
 * CancellableContext should be cancelled at some point. Breaking these rules may lead to memory
 * leaks.
 *    <li>While Context objects are immutable they do not place such a restriction on the state
 * they store.</li>
 *    <li>Context is not intended for passing optional parameters to an API and developers should
 * take care to avoid excessive dependence on context when designing an API.</li>
 * </ul>
 */
public class Context {

  private static final Logger log = Logger.getLogger(Context.class.getName());

  private static final Object[][] EMPTY_ENTRIES = new Object[0][2];

  private static final Key<Deadline> DEADLINE_KEY = new Key<Deadline>("deadline");

  /**
   * The logical root context which is the ultimate ancestor of all contexts. This context
   * is not cancellable and so will not cascade cancellation or retain listeners.
   *
   * <p>Never assume this is the default context for new threads, because {@link Storage} may define
   * a default context that is different from ROOT.
   */
  public static final Context ROOT = new Context(null);

  // One and only one of them is non-null
  private static final Storage storage;
  private static final Exception storageInitError;

  static {
    Storage newStorage = null;
    Exception error = null;
    try {
      Class<?> clazz = Class.forName("io.grpc.override.ContextStorageOverride");
      newStorage = (Storage) clazz.getConstructor().newInstance();
    } catch (ClassNotFoundException e) {
      if (log.isLoggable(Level.FINE)) {
        // Avoid writing to logger because custom log handlers may try to use Context, which is
        // problemantic (e.g., NullPointerException) because the Context class has not done loading
        // at this point.  The caveat is that in environments stderr may be disabled, thus this
        // message would go nowhere.
        System.err.println("io.grpc.Context: Storage override doesn't exist. Using default.");
        e.printStackTrace();
      }
      newStorage = new ThreadLocalContextStorage();
    } catch (Exception e) {
      error = e;
    }
    storage = newStorage;
    storageInitError = error;
  }

  // For testing
  static Storage storage() {
    if (storage == null) {
      throw new RuntimeException("Storage override had failed to initialize", storageInitError);
    }
    return storage;
  }

  /**
   * Create a {@link Key} with the given debug name. Multiple different keys may have the same name;
   * the name is intended for debugging purposes and does not impact behavior.
   */
  public static <T> Key<T> key(String name) {
    return new Key<T>(name);
  }

  /**
   * Create a {@link Key} with the given debug name and default value. Multiple different keys may
   * have the same name; the name is intended for debugging purposes and does not impact behavior.
   */
  public static <T> Key<T> keyWithDefault(String name, T defaultValue) {
    return new Key<T>(name, defaultValue);
  }

  /**
   * Return the context associated with the current scope, will never return {@code null}.
   *
   * <p>Will never return {@link CancellableContext} even if one is attached, instead a
   * {@link Context} is returned with the same properties and lifetime. This is to avoid
   * code stealing the ability to cancel arbitrarily.
   */
  public static Context current() {
    Context current = storage().current();
    if (current == null) {
      return ROOT;
    }
    return current;
  }

  private final Context parent;
  private final Object[][] keyValueEntries;
  private final boolean cascadesCancellation;
  private ArrayList<ExecutableListener> listeners;
  private CancellationListener parentListener = new ParentListener();
  private final boolean canBeCancelled;

  /**
   * Construct a context that cannot be cancelled and will not cascade cancellation from its parent.
   */
  private Context(Context parent) {
    this.parent = parent;
    // Not inheriting cancellation implies not inheriting a deadline too.
    keyValueEntries = new Object[][]{{DEADLINE_KEY, null}};
    cascadesCancellation = false;
    canBeCancelled = false;
  }

  /**
   * Construct a context that cannot be cancelled but will cascade cancellation from its parent if
   * it is cancellable.
   */
  private Context(Context parent, Object[][] keyValueEntries) {
    this.parent = parent;
    this.keyValueEntries = keyValueEntries;
    cascadesCancellation = true;
    canBeCancelled = this.parent != null && this.parent.canBeCancelled;
  }

  /**
   * Construct a context that can be cancelled and will cascade cancellation from its parent if
   * it is cancellable.
   */
  private Context(Context parent, Object[][] keyValueEntries, boolean isCancellable) {
    this.parent = parent;
    this.keyValueEntries = keyValueEntries;
    cascadesCancellation = true;
    canBeCancelled = isCancellable;
  }

  /**
   * Create a new context which is independently cancellable and also cascades cancellation from
   * its parent. Callers <em>must</em> ensure that either {@link
   * CancellableContext#cancel(Throwable)} or {@link CancellableContext#detachAndCancel(Context,
   * Throwable)} are called at a later point, in order to allow this context to be garbage
   * collected.
   *
   * <p>Sample usage:
   * <pre>
   *   Context.CancellableContext withCancellation = Context.current().withCancellation();
   *   try {
   *     withCancellation.run(new Runnable() {
   *       public void run() {
   *         Context current = Context.current();
   *         while (!current.isCancelled()) {
   *           keepWorking();
   *         }
   *       }
   *     });
   *   } finally {
   *     withCancellation.cancel(null);
   *   }
   * </pre>
   */
  public CancellableContext withCancellation() {
    return new CancellableContext(this);
  }

  /**
   * Create a new context which will cancel itself after the given {@code duration} from now.
   * The returned context will cascade cancellation of its parent. Callers may explicitly cancel
   * the returned context prior to the deadline just as for {@link #withCancellation()}. If the unit
   * of work completes before the deadline, the context should be explicitly cancelled to allow
   * it to be garbage collected.
   *
   * <p>Sample usage:
   * <pre>
   *   Context.CancellableContext withDeadline = Context.current()
   *       .withDeadlineAfter(5, TimeUnit.SECONDS, scheduler);
   *   try {
   *     withDeadline.run(new Runnable() {
   *       public void run() {
   *         Context current = Context.current();
   *         while (!current.isCancelled()) {
   *           keepWorking();
   *         }
   *       }
   *     });
   *   } finally {
   *     withDeadline.cancel(null);
   *   }
   * </pre>
   */
  public CancellableContext withDeadlineAfter(long duration, TimeUnit unit,
                                              ScheduledExecutorService scheduler) {
    return withDeadline(Deadline.after(duration, unit), scheduler);
  }

  /**
   * Create a new context which will cancel itself at the given {@link Deadline}.
   * The returned context will cascade cancellation of its parent. Callers may explicitly cancel
   * the returned context prior to the deadline just as for {@link #withCancellation()}. If the unit
   * of work completes before the deadline, the context should be explicitly cancelled to allow
   * it to be garbage collected.
   *
   * <p>Sample usage:
   * <pre>
   *   Context.CancellableContext withDeadline = Context.current()
   *      .withDeadline(someReceivedDeadline, scheduler);
   *   try {
   *     withDeadline.run(new Runnable() {
   *       public void run() {
   *         Context current = Context.current();
   *         while (!current.isCancelled() &amp;&amp; moreWorkToDo()) {
   *           keepWorking();
   *         }
   *       }
   *     });
   *   } finally {
   *     withDeadline.cancel(null);
   *   }
   * </pre>
   */
  public CancellableContext withDeadline(Deadline deadline,
      ScheduledExecutorService scheduler) {
    checkNotNull(deadline, "deadline");
    checkNotNull(scheduler, "scheduler");
    return new CancellableContext(this, deadline, scheduler);
  }

  /**
   * Create a new context with the given key value set. The new context will cascade cancellation
   * from its parent.
   *
   <pre>
   *   Context withCredential = Context.current().withValue(CRED_KEY, cred);
   *   withCredential.run(new Runnable() {
   *     public void run() {
   *        readUserRecords(userId, CRED_KEY.get());
   *     }
   *   });
   * </pre>
   *
   */
  public <V> Context withValue(Key<V> k1, V v1) {
    return new Context(this, new Object[][]{{k1, v1}});
  }

  /**
   * Create a new context with the given key value set. The new context will cascade cancellation
   * from its parent.
   */
  public <V1, V2> Context withValues(Key<V1> k1, V1 v1, Key<V2> k2, V2 v2) {
    return new Context(this, new Object[][]{{k1, v1}, {k2, v2}});
  }

  /**
   * Create a new context with the given key value set. The new context will cascade cancellation
   * from its parent.
   */
  public <V1, V2, V3> Context withValues(Key<V1> k1, V1 v1, Key<V2> k2, V2 v2, Key<V3> k3, V3 v3) {
    return new Context(this, new Object[][]{{k1, v1}, {k2, v2}, {k3, v3}});
  }

  /**
   * Create a new context with the given key value set. The new context will cascade cancellation
   * from its parent.
   */
  public <V1, V2, V3, V4> Context withValues(Key<V1> k1, V1 v1, Key<V2> k2, V2 v2,
      Key<V3> k3, V3 v3, Key<V4> k4, V4 v4) {
    return new Context(this, new Object[][]{{k1, v1}, {k2, v2}, {k3, v3}, {k4, v4}});
  }

  /**
   * Create a new context which propagates the values of this context but does not cascade its
   * cancellation.
   */
  public Context fork() {
    return new Context(this);
  }

  boolean canBeCancelled() {
    // A context is cancellable if it cascades from its parent and its parent is
    // cancellable or is itself directly cancellable..
    return canBeCancelled;
  }

  /**
   * Attach this context, thus enter a new scope within which this context is {@link #current}.  The
   * previously current context is returned. It is allowed to attach contexts where {@link
   * #isCancelled()} is {@code true}.
   *
   * <p>Instead of using {@code attach()} and {@link #detach(Context)} most use-cases are better
   * served by using the {@link #run(Runnable)} or {@link #call(java.util.concurrent.Callable)} to
   * execute work immediately within a context's scope. If work needs to be done in other threads it
   * is recommended to use the 'wrap' methods or to use a propagating executor.
   *
   * <p>All calls to {@code attach()} should have a corresponding {@link #detach(Context)} within
   * the same method:
   * <pre>{@code Context previous = someContext.attach();
   * try {
   *   // Do work
   * } finally {
   *   someContext.detach(previous);
   * }}</pre>
   */
  public Context attach() {
    Context previous = current();
    storage().attach(this);
    return previous;
  }

  /**
   * Reverse an {@code attach()}, restoring the previous context and exiting the current scope.
   *
   * <p>This context should be the same context that was previously {@link #attach attached}.  The
   * provided replacement should be what was returned by the same {@link #attach attach()} call.  If
   * an {@code attach()} and a {@code detach()} meet above requirements, they match.
   *
   * <p>It is expected that between any pair of matching {@code attach()} and {@code detach()}, all
   * {@code attach()}es and {@code detach()}es are called in matching pairs.  If this method finds
   * that this context is not {@link #current current}, either you or some code in-between are not
   * detaching correctly, and a SEVERE message will be logged but the context to attach will still
   * be bound.  <strong>Never</strong> use {@code Context.current().detach()}, as this will
   * compromise this error-detecting mechanism.
   */
  public void detach(Context toAttach) {
    checkNotNull(toAttach, "toAttach");
    storage().detach(this, toAttach);
  }

  // Visible for testing
  boolean isCurrent() {
    return current() == this;
  }

  /**
   * Is this context cancelled.
   */
  public boolean isCancelled() {
    if (parent == null || !cascadesCancellation) {
      return false;
    } else {
      return parent.isCancelled();
    }
  }

  /**
   * If a context {@link #isCancelled()} then return the cause of the cancellation or
   * {@code null} if context was cancelled without a cause. If the context is not yet cancelled
   * will always return {@code null}.
   *
   * <p>The cancellation cause is provided for informational purposes only and implementations
   * should generally assume that it has already been handled and logged properly.
   */
  public Throwable cancellationCause() {
    if (parent == null || !cascadesCancellation) {
      return null;
    } else {
      return parent.cancellationCause();
    }
  }

  /**
   * A context may have an associated {@link Deadline} at which it will be automatically cancelled.
   * @return A {@link io.grpc.Deadline} or {@code null} if no deadline is set.
   */
  public Deadline getDeadline() {
    return DEADLINE_KEY.get(this);
  }

  /**
   * Add a listener that will be notified when the context becomes cancelled.
   */
  public void addListener(final CancellationListener cancellationListener,
                          final Executor executor) {
    checkNotNull(cancellationListener, "cancellationListener");
    checkNotNull(executor, "executor");
    if (canBeCancelled) {
      ExecutableListener executableListener =
          new ExecutableListener(executor, cancellationListener);
      synchronized (this) {
        if (isCancelled()) {
          executableListener.deliver();
        } else {
          if (listeners == null) {
            // Now that we have a listener we need to listen to our parent so
            // we can cascade listener notification.
            listeners = new ArrayList<ExecutableListener>();
            listeners.add(executableListener);
            parent.addListener(parentListener, DirectExecutor.INSTANCE);
          } else {
            listeners.add(executableListener);
          }
        }
      }
    }
  }

  /**
   * Remove a {@link CancellationListener}.
   */
  public void removeListener(CancellationListener cancellationListener) {
    if (!canBeCancelled) {
      return;
    }
    synchronized (this) {
      if (listeners != null) {
        for (int i = listeners.size() - 1; i >= 0; i--) {
          if (listeners.get(i).listener == cancellationListener) {
            listeners.remove(i);
            // Just remove the first matching listener, given that we allow duplicate
            // adds we should allow for duplicates after remove.
            break;
          }
        }
        // We have no listeners so no need to listen to our parent
        if (listeners.isEmpty()) {
          parent.removeListener(parentListener);
          listeners = null;
        }
      }
    }
  }

  /**
   * Notify all listeners that this context has been cancelled and immediately release
   * any reference to them so that they may be garbage collected.
   */
  void notifyAndClearListeners() {
    if (!canBeCancelled) {
      return;
    }
    ArrayList<ExecutableListener> tmpListeners;
    synchronized (this) {
      if (listeners == null) {
        return;
      }
      tmpListeners = listeners;
      listeners = null;
    }
    // Deliver events to non-child context listeners before we notify child contexts. We do this
    // to cancel higher level units of work before child units. This allows for a better error
    // handling paradigm where the higher level unit of work knows it is cancelled and so can
    // ignore errors that bubble up as a result of cancellation of lower level units.
    for (int i = 0; i < tmpListeners.size(); i++) {
      if (!(tmpListeners.get(i).listener instanceof ParentListener)) {
        tmpListeners.get(i).deliver();
      }
    }
    for (int i = 0; i < tmpListeners.size(); i++) {
      if (tmpListeners.get(i).listener instanceof ParentListener) {
        tmpListeners.get(i).deliver();
      }
    }
    parent.removeListener(parentListener);
  }

  // Used in tests to ensure that listeners are defined and released when cancellation cascades.
  // It's very important to ensure that we do not accidentally retain listeners.
  int listenerCount() {
    synchronized (this) {
      return listeners == null ? 0 : listeners.size();
    }
  }

  /**
   * Immediately run a {@link Runnable} with this context as the {@link #current} context.
   * @param r {@link Runnable} to run.
   */
  public void run(Runnable r) {
    Context previous = attach();
    try {
      r.run();
    } finally {
      detach(previous);
    }
  }

  /**
   * Immediately call a {@link Callable} with this context as the {@link #current} context.
   * @param c {@link Callable} to call.
   * @return result of call.
   */
  public <V> V call(Callable<V> c) throws Exception {
    Context previous = attach();
    try {
      return c.call();
    } finally {
      detach(previous);
    }
  }

  /**
   * Wrap a {@link Runnable} so that it executes with this context as the {@link #current} context.
   */
  public Runnable wrap(final Runnable r) {
    return new Runnable() {
      @Override
      public void run() {
        Context previous = attach();
        try {
          r.run();
        } finally {
          detach(previous);
        }
      }
    };
  }

  /**
   * Wrap a {@link Callable} so that it executes with this context as the {@link #current} context.
   */
  public <C> Callable<C> wrap(final Callable<C> c) {
    return new Callable<C>() {
      @Override
      public C call() throws Exception {
        Context previous = attach();
        try {
          return c.call();
        } finally {
          detach(previous);
        }
      }
    };
  }

  /**
   * Wrap an {@link Executor} so that it always executes with this context as the {@link #current}
   * context. It is generally expected that {@link #currentContextExecutor(Executor)} would be
   * used more commonly than this method.
   *
   * <p>One scenario in which this executor may be useful is when a single thread is sharding work
   * to multiple threads.
   *
   * @see #currentContextExecutor(Executor)
   */
  public Executor fixedContextExecutor(final Executor e) {
    class FixedContextExecutor implements Executor {
      @Override
      public void execute(Runnable r) {
        e.execute(wrap(r));
      }
    }

    return new FixedContextExecutor();
  }

  /**
   * Create an executor that propagates the {@link #current} context when {@link Executor#execute}
   * is called as the {@link #current} context of the {@code Runnable} scheduled. <em>Note that this
   * is a static method.</em>
   *
   * @see #fixedContextExecutor(Executor)
   */
  public static Executor currentContextExecutor(final Executor e) {
    class CurrentContextExecutor implements Executor {
      @Override
      public void execute(Runnable r) {
        e.execute(Context.current().wrap(r));
      }
    }

    return new CurrentContextExecutor();
  }

  /**
   * Lookup the value for a key in the context inheritance chain.
   */
  private Object lookup(Key<?> key) {
    for (int i = 0; i < keyValueEntries.length; i++) {
      if (key.equals(keyValueEntries[i][0])) {
        return keyValueEntries[i][1];
      }
    }
    if (parent == null) {
      return null;
    }
    return parent.lookup(key);
  }

  /**
   * A context which inherits cancellation from its parent but which can also be independently
   * cancelled and which will propagate cancellation to its descendants. To avoid leaking memory,
   * every CancellableContext must have a defined lifetime, after which it is guaranteed to be
   * cancelled.
   */
  public static final class CancellableContext extends Context {

    private boolean cancelled;
    private Throwable cancellationCause;
    private final Context uncancellableSurrogate;
    private ScheduledFuture<?> pendingDeadline;

    /**
     * If the parent deadline is before the given deadline there is no need to install the value
     * or listen for its expiration as the parent context will already be listening for it.
     */
    private static Object[][] deriveDeadline(Context parent, Deadline deadline) {
      Deadline parentDeadline = DEADLINE_KEY.get(parent);
      return parentDeadline == null || deadline.isBefore(parentDeadline)
          ? new Object[][]{{ DEADLINE_KEY, deadline}} :
          EMPTY_ENTRIES;
    }

    /**
     * Create a cancellable context that does not have a deadline.
     */
    private CancellableContext(Context parent) {
      super(parent, EMPTY_ENTRIES, true);
      // Create a surrogate that inherits from this to attach so that you cannot retrieve a
      // cancellable context from Context.current()
      uncancellableSurrogate = new Context(this, EMPTY_ENTRIES);
    }

    /**
     * Create a cancellable context that has a deadline.
     */
    private CancellableContext(Context parent, Deadline deadline,
        ScheduledExecutorService scheduler) {
      super(parent, deriveDeadline(parent, deadline), true);
      if (DEADLINE_KEY.get(this) == deadline) {
        final TimeoutException cause = new TimeoutException("context timed out");
        if (!deadline.isExpired()) {
          // The parent deadline was after the new deadline so we need to install a listener
          // on the new earlier deadline to trigger expiration for this context.
          pendingDeadline = deadline.runOnExpiration(new Runnable() {
            @Override
            public void run() {
              try {
                cancel(cause);
              } catch (Throwable t) {
                log.log(Level.SEVERE, "Cancel threw an exception, which should not happen", t);
              }
            }
          }, scheduler);
        } else {
          // Cancel immediately if the deadline is already expired.
          cancel(cause);
        }
      }
      uncancellableSurrogate = new Context(this, EMPTY_ENTRIES);
    }


    @Override
    public Context attach() {
      return uncancellableSurrogate.attach();
    }

    @Override
    public void detach(Context toAttach) {
      uncancellableSurrogate.detach(toAttach);
    }

    /**
     * Returns true if the Context is the current context.
     *
     * @deprecated This method violates some GRPC class encapsulation and should not be used.
     *     If you must know whether a Context is the current context, check whether it is the same
     *     object returned by {@link Context#current()}.
     */
    //TODO(spencerfang): The superclass's method is package-private, so this should really match.
    @Override
    @Deprecated
    public boolean isCurrent() {
      return uncancellableSurrogate.isCurrent();
    }

    /**
     * Cancel this context and optionally provide a cause (can be {@code null}) for the
     * cancellation. This will trigger notification of listeners.
     *
     * @return {@code true} if this context cancelled the context and notified listeners,
     *    {@code false} if the context was already cancelled.
     */
    public boolean cancel(Throwable cause) {
      boolean triggeredCancel = false;
      synchronized (this) {
        if (!cancelled) {
          cancelled = true;
          if (pendingDeadline != null) {
            // If we have a scheduled cancellation pending attempt to cancel it.
            pendingDeadline.cancel(false);
            pendingDeadline = null;
          }
          this.cancellationCause = cause;
          triggeredCancel = true;
        }
      }
      if (triggeredCancel) {
        notifyAndClearListeners();
      }
      return triggeredCancel;
    }

    /**
     * Cancel this context and detach it as the current context.
     *
     * @param toAttach context to make current.
     * @param cause of cancellation, can be {@code null}.
     */
    public void detachAndCancel(Context toAttach, Throwable cause) {
      try {
        detach(toAttach);
      } finally {
        cancel(cause);
      }
    }

    @Override
    public boolean isCancelled() {
      synchronized (this) {
        if (cancelled) {
          return true;
        }
      }
      // Detect cancellation of parent in the case where we have no listeners and
      // record it.
      if (super.isCancelled()) {
        cancel(super.cancellationCause());
        return true;
      }
      return false;
    }

    @Override
    public Throwable cancellationCause() {
      if (isCancelled()) {
        return cancellationCause;
      }
      return null;
    }
  }

  /**
   * A listener notified on context cancellation.
   */
  public interface CancellationListener {
    /**
     * @param context the newly cancelled context.
     */
    public void cancelled(Context context);
  }

  /**
   * Key for indexing values stored in a context.
   */
  public static final class Key<T> {

    private final String name;
    private final T defaultValue;

    Key(String name) {
      this(name, null);
    }

    Key(String name, T defaultValue) {
      this.name = checkNotNull(name, "name");
      this.defaultValue = defaultValue;
    }

    /**
     * Get the value from the {@link #current()} context for this key.
     */
    @SuppressWarnings("unchecked")
    public T get() {
      return get(Context.current());
    }

    /**
     * Get the value from the specified context for this key.
     */
    @SuppressWarnings("unchecked")
    public T get(Context context) {
      T value = (T) context.lookup(this);
      return value == null ? defaultValue : value;
    }

    @Override
    public String toString() {
      return name;
    }
  }

  /**
   * Defines the mechanisms for attaching and detaching the "current" context.
   *
   * <p>The default implementation will put the current context in a {@link ThreadLocal}.  If an
   * alternative implementation named {@code io.grpc.override.ContextStorageOverride} exists in the
   * classpath, it will be used instead of the default implementation.
   *
   * <p>This API is <a href="https://github.com/grpc/grpc-java/issues/2462">experimental</a> and
   * subject to change.
   */
  public abstract static class Storage {
    /**
     * Implements {@link io.grpc.Context#attach}.
     *
     * @param toAttach the context to be attached
     */
    public abstract void attach(Context toAttach);

    /**
     * Implements {@link io.grpc.Context#detach}
     *
     * @param toDetach the context to be detached. Should be, or be equivalent to, the current
     *        context of the current scope
     * @param toRestore the context to be the current.  Should be, or be equivalent to, the context
     *        of the outer scope
     */
    public abstract void detach(Context toDetach, Context toRestore);

    /**
     * Implements {@link io.grpc.Context#current}.  Returns the context of the current scope.
     */
    public abstract Context current();
  }

  /**
   * Stores listener and executor pair.
   */
  private class ExecutableListener implements Runnable {
    private final Executor executor;
    private final CancellationListener listener;

    private ExecutableListener(Executor executor, CancellationListener listener) {
      this.executor = executor;
      this.listener = listener;
    }

    private void deliver() {
      try {
        executor.execute(this);
      } catch (Throwable t) {
        log.log(Level.INFO, "Exception notifying context listener", t);
      }
    }

    @Override
    public void run() {
      listener.cancelled(Context.this);
    }
  }

  private class ParentListener implements CancellationListener {
    @Override
    public void cancelled(Context context) {
      if (Context.this instanceof CancellableContext) {
        // Record cancellation with its cancellationCause.
        ((CancellableContext) Context.this).cancel(context.cancellationCause());
      } else {
        notifyAndClearListeners();
      }
    }
  }

  private static <T> T checkNotNull(T reference, Object errorMessage) {
    if (reference == null) {
      throw new NullPointerException(String.valueOf(errorMessage));
    }
    return reference;
  }

  private enum DirectExecutor implements Executor {
    INSTANCE;

    @Override
    public void execute(Runnable command) {
      command.run();
    }

    @Override
    public String toString() {
      return "Context.DirectExecutor";
    }
  }
}
