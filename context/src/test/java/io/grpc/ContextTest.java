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

import static org.hamcrest.core.IsInstanceOf.instanceOf;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertThat;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import java.util.ArrayDeque;
import java.util.Queue;
import java.util.concurrent.Callable;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executor;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;
import java.util.logging.Handler;
import java.util.logging.Level;
import java.util.logging.LogRecord;
import java.util.logging.Logger;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link Context}.
 */
@RunWith(JUnit4.class)
public class ContextTest {

  private static final Context.Key<String> PET = Context.key("pet");
  private static final Context.Key<String> FOOD = Context.keyWithDefault("food", "lasagna");
  private static final Context.Key<String> COLOR = Context.key("color");
  private static final Context.Key<Object> FAVORITE = Context.key("favorite");
  private static final Context.Key<Integer> LUCKY = Context.key("lucky");

  private Context listenerNotifedContext;
  private CountDownLatch deadlineLatch = new CountDownLatch(1);
  private Context.CancellationListener cancellationListener = new Context.CancellationListener() {
    @Override
    public void cancelled(Context context) {
      listenerNotifedContext = context;
      deadlineLatch.countDown();
    }
  };

  private Context observed;
  private Runnable runner = new Runnable() {
    @Override
    public void run() {
      observed = Context.current();
    }
  };
  private ScheduledExecutorService scheduler = Executors.newSingleThreadScheduledExecutor();

  @Before
  public void setUp() throws Exception {
    Context.ROOT.attach();
  }

  @After
  public void tearDown() throws Exception {
    scheduler.shutdown();
  }

  @Test
  public void defaultContext() throws Exception {
    final SettableFuture<Context> contextOfNewThread = SettableFuture.create();
    Context contextOfThisThread = Context.ROOT.withValue(PET, "dog");
    contextOfThisThread.attach();
    new Thread(new Runnable() {
      @Override
      public void run() {
        contextOfNewThread.set(Context.current());
      }
      }).start();
    assertNotNull(contextOfNewThread.get(5, TimeUnit.SECONDS));
    assertNotSame(contextOfThisThread, contextOfNewThread.get());
    assertSame(contextOfThisThread, Context.current());
  }

  @Test
  public void rootCanBeAttached() {
    Context fork = Context.ROOT.fork();
    fork.attach();
    Context.ROOT.attach();
    assertTrue(Context.ROOT.isCurrent());
    fork.attach();
    assertTrue(fork.isCurrent());
  }

  @Test
  public void rootCanNeverHaveAListener() {
    Context root = Context.current();
    root.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertEquals(0, root.listenerCount());
  }

  @Test
  public void rootIsNotCancelled() {
    assertFalse(Context.ROOT.isCancelled());
    assertNull(Context.ROOT.cancellationCause());
  }

  @Test
  public void attachedCancellableContextCannotBeCastFromCurrent() {
    Context initial = Context.current();
    Context.CancellableContext base = initial.withCancellation();
    base.attach();
    assertFalse(Context.current() instanceof Context.CancellableContext);
    assertNotSame(base, Context.current());
    assertNotSame(initial, Context.current());
    base.detachAndCancel(initial, null);
    assertSame(initial, Context.current());
  }

  @Test
  public void attachingNonCurrentReturnsCurrent() {
    Context initial = Context.current();
    Context base = initial.withValue(PET, "dog");
    assertSame(initial, base.attach());
    assertSame(base, initial.attach());
  }

  @Test
  public void detachingNonCurrentLogsSevereMessage() {
    final AtomicReference<LogRecord> logRef = new AtomicReference<LogRecord>();
    Handler handler = new Handler() {
      @Override
      public void publish(LogRecord record) {
        logRef.set(record);
      }

      @Override
      public void flush() {
      }

      @Override
      public void close() throws SecurityException {
      }
    };
    Logger logger = Logger.getLogger(Context.storage().getClass().getName());
    try {
      logger.addHandler(handler);
      Context initial = Context.current();
      Context base = initial.withValue(PET, "dog");
      // Base is not attached
      base.detach(initial);
      assertSame(initial, Context.current());
      assertNotNull(logRef.get());
      assertEquals(Level.SEVERE, logRef.get().getLevel());
    } finally {
      logger.removeHandler(handler);
    }
  }

  @Test
  public void valuesAndOverrides() {
    Context base = Context.current().withValue(PET, "dog");
    Context child = base.withValues(PET, null, FOOD, "cheese");

    base.attach();

    assertEquals("dog", PET.get());
    assertEquals("lasagna", FOOD.get());
    assertNull(COLOR.get());

    child.attach();

    assertNull(PET.get());
    assertEquals("cheese", FOOD.get());
    assertNull(COLOR.get());

    child.detach(base);

    // Should have values from base
    assertEquals("dog", PET.get());
    assertEquals("lasagna", FOOD.get());
    assertNull(COLOR.get());

    base.detach(Context.ROOT);

    assertNull(PET.get());
    assertEquals("lasagna", FOOD.get());
    assertNull(COLOR.get());
  }

  @Test
  public void withValuesThree() {
    Object fav = new Object();
    Context base = Context.current().withValues(PET, "dog", COLOR, "blue");
    Context child = base.withValues(PET, "cat", FOOD, "cheese", FAVORITE, fav);

    child.attach();

    assertEquals("cat", PET.get());
    assertEquals("cheese", FOOD.get());
    assertEquals("blue", COLOR.get());
    assertEquals(fav, FAVORITE.get());

    base.attach();
  }

  @Test
  public void withValuesFour() {
    Object fav = new Object();
    Context base = Context.current().withValues(PET, "dog", COLOR, "blue");
    Context child = base.withValues(PET, "cat", FOOD, "cheese", FAVORITE, fav, LUCKY, 7);

    child.attach();

    assertEquals("cat", PET.get());
    assertEquals("cheese", FOOD.get());
    assertEquals("blue", COLOR.get());
    assertEquals(fav, FAVORITE.get());
    assertEquals(7, (int) LUCKY.get());

    base.attach();
  }

  @Test
  public void cancelReturnsFalseIfAlreadyCancelled() {
    Context.CancellableContext base = Context.current().withCancellation();
    assertTrue(base.cancel(null));
    assertTrue(base.isCancelled());
    assertFalse(base.cancel(null));
  }

  @Test
  public void notifyListenersOnCancel() {
    class SetContextCancellationListener implements Context.CancellationListener {
      private final AtomicReference<Context> observed;

      public SetContextCancellationListener(AtomicReference<Context> observed) {
        this.observed = observed;
      }

      @Override
      public void cancelled(Context context) {
        observed.set(context);
      }
    }

    Context.CancellableContext base = Context.current().withCancellation();
    final AtomicReference<Context> observed1 = new AtomicReference<Context>();
    base.addListener(new SetContextCancellationListener(observed1), MoreExecutors.directExecutor());
    final AtomicReference<Context> observed2 = new AtomicReference<Context>();
    base.addListener(new SetContextCancellationListener(observed2), MoreExecutors.directExecutor());
    assertNull(observed1.get());
    assertNull(observed2.get());
    base.cancel(null);
    assertSame(base, observed1.get());
    assertSame(base, observed2.get());

    final AtomicReference<Context> observed3 = new AtomicReference<Context>();
    base.addListener(new SetContextCancellationListener(observed3), MoreExecutors.directExecutor());
    assertSame(base, observed3.get());
  }

  @Test
  public void exceptionOfExecutorDoesntThrow() {
    final AtomicReference<Throwable> loggedThrowable = new AtomicReference<Throwable>();
    Handler logHandler = new Handler() {
      @Override
      public void publish(LogRecord record) {
        Throwable thrown = record.getThrown();
        if (thrown != null) {
          if (loggedThrowable.get() == null) {
            loggedThrowable.set(thrown);
          } else {
            loggedThrowable.set(new RuntimeException("Too many exceptions", thrown));
          }
        }
      }

      @Override
      public void close() {}

      @Override
      public void flush() {}
    };
    Logger logger = Logger.getLogger(Context.class.getName());
    logger.addHandler(logHandler);
    try {
      Context.CancellableContext base = Context.current().withCancellation();
      final AtomicReference<Runnable> observed1 = new AtomicReference<Runnable>();
      final Error err = new Error();
      base.addListener(cancellationListener, new Executor() {
        @Override
        public void execute(Runnable runnable) {
          observed1.set(runnable);
          throw err;
        }
      });
      assertNull(observed1.get());
      assertNull(loggedThrowable.get());
      base.cancel(null);
      assertNotNull(observed1.get());
      assertSame(err, loggedThrowable.get());

      final Error err2 = new Error();
      loggedThrowable.set(null);
      final AtomicReference<Runnable> observed2 = new AtomicReference<Runnable>();
      base.addListener(cancellationListener, new Executor() {
        @Override
        public void execute(Runnable runnable) {
          observed2.set(runnable);
          throw err2;
        }
      });
      assertNotNull(observed2.get());
      assertSame(err2, loggedThrowable.get());
    } finally {
      logger.removeHandler(logHandler);
    }
  }

  @Test
  public void cascadingCancellationNotifiesChild() {
    // Root is not cancellable so we can't cascade from it
    Context.CancellableContext base = Context.current().withCancellation();
    assertEquals(0, base.listenerCount());
    Context child = base.withValue(FOOD, "lasagna");
    assertEquals(0, child.listenerCount());
    child.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertEquals(1, child.listenerCount());
    assertEquals(1, base.listenerCount()); // child is now listening to base
    assertFalse(base.isCancelled());
    assertFalse(child.isCancelled());
    IllegalStateException cause = new IllegalStateException();
    base.cancel(cause);
    assertTrue(base.isCancelled());
    assertSame(cause, base.cancellationCause());
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertSame(cause, child.cancellationCause());
    assertEquals(0, base.listenerCount());
    assertEquals(0, child.listenerCount());
  }

  @Test
  public void cascadingCancellationWithoutListener() {
    Context.CancellableContext base = Context.current().withCancellation();
    Context child = base.withCancellation();
    Throwable t = new Throwable();
    base.cancel(t);
    assertTrue(child.isCancelled());
    assertSame(t, child.cancellationCause());
  }

  // Context#isCurrent() and Context.CancellableContext#isCurrent() are intended
  // to be visible only for testing. The deprecation is meant for users.
  @SuppressWarnings("deprecation")
  @Test
  public void cancellableContextIsAttached() {
    Context.CancellableContext base = Context.current().withValue(FOOD, "fish").withCancellation();
    assertFalse(base.isCurrent());
    base.attach();

    Context attached = Context.current();
    assertSame("fish", FOOD.get());
    assertFalse(attached.isCancelled());
    assertNull(attached.cancellationCause());
    assertTrue(attached.canBeCancelled());
    assertTrue(attached.isCurrent());
    assertTrue(base.isCurrent());

    attached.addListener(cancellationListener, MoreExecutors.directExecutor());
    Throwable t = new Throwable();
    base.cancel(t);
    assertTrue(attached.isCancelled());
    assertSame(t, attached.cancellationCause());
    assertSame(attached, listenerNotifedContext);

    Context.ROOT.attach();
  }

  @Test
  public void cancellableContextCascadesFromCancellableParent() {
    // Root is not cancellable so we can't cascade from it
    Context.CancellableContext base = Context.current().withCancellation();
    Context child = base.withCancellation();
    child.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertFalse(base.isCancelled());
    assertFalse(child.isCancelled());
    IllegalStateException cause = new IllegalStateException();
    base.cancel(cause);
    assertTrue(base.isCancelled());
    assertSame(cause, base.cancellationCause());
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertSame(cause, child.cancellationCause());
    assertEquals(0, base.listenerCount());
    assertEquals(0, child.listenerCount());
  }

  @Test
  public void nonCascadingCancellationDoesNotNotifyForked() {
    Context.CancellableContext base = Context.current().withCancellation();
    Context fork = base.fork();
    fork.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertEquals(0, base.listenerCount());
    assertEquals(0, fork.listenerCount());
    assertTrue(base.cancel(new Throwable()));
    assertNull(listenerNotifedContext);
    assertFalse(fork.isCancelled());
    assertNull(fork.cancellationCause());
  }

  @Test
  public void testWrapRunnable() throws Exception {
    Context base = Context.current().withValue(PET, "cat");
    Context current = Context.current().withValue(PET, "fish");
    current.attach();

    base.wrap(runner).run();
    assertSame(base, observed);
    assertSame(current, Context.current());

    current.wrap(runner).run();
    assertSame(current, observed);
    assertSame(current, Context.current());

    final Error err = new Error();
    try {
      base.wrap(new Runnable() {
        @Override
        public void run() {
          throw err;
        }
      }).run();
      fail("Expected exception");
    } catch (Error ex) {
      assertSame(err, ex);
    }
    assertSame(current, Context.current());

    current.detach(Context.ROOT);
  }

  @Test
  public void testWrapCallable() throws Exception {
    Context base = Context.current().withValue(PET, "cat");
    Context current = Context.current().withValue(PET, "fish");
    current.attach();

    final Object ret = new Object();
    Callable<Object> callable = new Callable<Object>() {
      @Override
      public Object call() {
        runner.run();
        return ret;
      }
    };

    assertSame(ret, base.wrap(callable).call());
    assertSame(base, observed);
    assertSame(current, Context.current());

    assertSame(ret, current.wrap(callable).call());
    assertSame(current, observed);
    assertSame(current, Context.current());

    final Error err = new Error();
    try {
      base.wrap(new Callable<Object>() {
        @Override
        public Object call() {
          throw err;
        }
      }).call();
      fail("Excepted exception");
    } catch (Error ex) {
      assertSame(err, ex);
    }
    assertSame(current, Context.current());

    current.detach(Context.ROOT);
  }

  @Test
  public void currentContextExecutor() throws Exception {
    QueuedExecutor queuedExecutor = new QueuedExecutor();
    Executor executor = Context.currentContextExecutor(queuedExecutor);
    Context base = Context.current().withValue(PET, "cat");
    Context previous = base.attach();
    try {
      executor.execute(runner);
    } finally {
      base.detach(previous);
    }
    assertEquals(1, queuedExecutor.runnables.size());
    queuedExecutor.runnables.remove().run();
    assertSame(base, observed);
  }

  @Test
  public void fixedContextExecutor() throws Exception {
    Context base = Context.current().withValue(PET, "cat");
    QueuedExecutor queuedExecutor = new QueuedExecutor();
    base.fixedContextExecutor(queuedExecutor).execute(runner);
    assertEquals(1, queuedExecutor.runnables.size());
    queuedExecutor.runnables.remove().run();
    assertSame(base, observed);
  }

  @Test
  public void typicalTryFinallyHandling() throws Exception {
    Context base = Context.current().withValue(COLOR, "blue");
    Context previous = base.attach();
    try {
      assertTrue(base.isCurrent());
      // Do something
    } finally {
      base.detach(previous);
    }
    assertFalse(base.isCurrent());
  }

  @Test
  public void typicalCancellableTryCatchFinallyHandling() throws Exception {
    Context.CancellableContext base = Context.current().withCancellation();
    Context previous = base.attach();
    try {
      // Do something
      throw new IllegalStateException("Argh");
    } catch (IllegalStateException ise) {
      base.cancel(ise);
    } finally {
      base.detachAndCancel(previous, null);
    }
    assertTrue(base.isCancelled());
    assertNotNull(base.cancellationCause());
  }

  @Test
  public void rootHasNoDeadline() {
    assertNull(Context.ROOT.getDeadline());
  }

  @Test
  public void contextWithDeadlineHasDeadline() {
    Context.CancellableContext cancellableContext =
        Context.ROOT.withDeadlineAfter(1, TimeUnit.SECONDS, scheduler);
    assertNotNull(cancellableContext.getDeadline());
  }

  @Test
  public void earlierParentDeadlineTakesPrecedenceOverLaterChildDeadline() throws Exception {
    final Deadline sooner = Deadline.after(100, TimeUnit.MILLISECONDS);
    final Deadline later = Deadline.after(1, TimeUnit.MINUTES);
    Context.CancellableContext parent = Context.ROOT.withDeadline(sooner, scheduler);
    Context.CancellableContext child = parent.withDeadline(later, scheduler);
    assertSame(parent.getDeadline(), sooner);
    assertSame(child.getDeadline(), sooner);
    final CountDownLatch latch = new CountDownLatch(1);
    final AtomicReference<Exception> error = new AtomicReference<Exception>();
    child.addListener(new Context.CancellationListener() {
      @Override
      public void cancelled(Context context) {
        try {
          assertTrue(sooner.isExpired());
          assertFalse(later.isExpired());
        } catch (Exception e) {
          error.set(e);
        }
        latch.countDown();
      }
    }, MoreExecutors.directExecutor());
    latch.await(3, TimeUnit.SECONDS);
    if (error.get() != null) {
      throw error.get();
    }
  }

  @Test
  public void earlierChldDeadlineTakesPrecedenceOverLaterParentDeadline() {
    Deadline sooner = Deadline.after(1, TimeUnit.HOURS);
    Deadline later = Deadline.after(1, TimeUnit.DAYS);
    Context.CancellableContext parent = Context.ROOT.withDeadline(later, scheduler);
    Context.CancellableContext child = parent.withDeadline(sooner, scheduler);
    assertSame(parent.getDeadline(), later);
    assertSame(child.getDeadline(), sooner);
  }

  @Test
  public void forkingContextDoesNotCarryDeadline() {
    Deadline deadline = Deadline.after(1, TimeUnit.HOURS);
    Context.CancellableContext parent = Context.ROOT.withDeadline(deadline, scheduler);
    Context fork = parent.fork();
    assertNull(fork.getDeadline());
  }

  @Test
  public void cancellationDoesNotExpireDeadline() {
    Deadline deadline = Deadline.after(1, TimeUnit.HOURS);
    Context.CancellableContext parent = Context.ROOT.withDeadline(deadline, scheduler);
    parent.cancel(null);
    assertFalse(deadline.isExpired());
  }

  @Test
  public void absoluteDeadlineTriggersAndPropagates() throws Exception {
    Context base = Context.current().withDeadline(Deadline.after(1, TimeUnit.SECONDS), scheduler);
    Context child = base.withValue(FOOD, "lasagna");
    child.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertFalse(base.isCancelled());
    assertFalse(child.isCancelled());
    assertTrue(deadlineLatch.await(2, TimeUnit.SECONDS));
    assertTrue(base.isCancelled());
    assertTrue(base.cancellationCause() instanceof TimeoutException);
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertSame(base.cancellationCause(), child.cancellationCause());
  }

  @Test
  public void relativeDeadlineTriggersAndPropagates() throws Exception {
    Context base = Context.current().withDeadline(Deadline.after(1, TimeUnit.SECONDS), scheduler);
    Context child = base.withValue(FOOD, "lasagna");
    child.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertFalse(base.isCancelled());
    assertFalse(child.isCancelled());
    assertTrue(deadlineLatch.await(2, TimeUnit.SECONDS));
    assertTrue(base.isCancelled());
    assertTrue(base.cancellationCause() instanceof TimeoutException);
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertSame(base.cancellationCause(), child.cancellationCause());
  }

  @Test
  public void innerDeadlineCompletesBeforeOuter() throws Exception {
    Context base = Context.current().withDeadline(Deadline.after(2, TimeUnit.SECONDS), scheduler);
    Context child = base.withDeadline(Deadline.after(1, TimeUnit.SECONDS), scheduler);
    child.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertFalse(base.isCancelled());
    assertFalse(child.isCancelled());
    assertTrue(deadlineLatch.await(2, TimeUnit.SECONDS));
    assertFalse(base.isCancelled());
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertTrue(child.cancellationCause() instanceof TimeoutException);

    deadlineLatch = new CountDownLatch(1);
    base.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertTrue(deadlineLatch.await(2, TimeUnit.SECONDS));
    assertTrue(base.isCancelled());
    assertTrue(base.cancellationCause() instanceof TimeoutException);
    assertNotSame(base.cancellationCause(), child.cancellationCause());
  }

  @Test
  public void cancellationCancelsScheduledTask() {
    ScheduledThreadPoolExecutor executor = new ScheduledThreadPoolExecutor(1);
    try {
      assertEquals(0, executor.getQueue().size());
      Context.CancellableContext base
          = Context.current().withDeadline(Deadline.after(1, TimeUnit.DAYS), executor);
      assertEquals(1, executor.getQueue().size());
      base.cancel(null);
      executor.purge();
      assertEquals(0, executor.getQueue().size());
    } finally {
      executor.shutdown();
    }
  }

  private static class QueuedExecutor implements Executor {
    private final Queue<Runnable> runnables = new ArrayDeque<Runnable>();

    @Override
    public void execute(Runnable r) {
      runnables.add(r);
    }
  }

  @Test
  public void childContextListenerNotifiedAfterParentListener() {
    Context.CancellableContext parent = Context.current().withCancellation();
    Context child = parent.withValue(COLOR, "red");
    final AtomicBoolean childAfterParent = new AtomicBoolean();
    final AtomicBoolean parentCalled = new AtomicBoolean();
    child.addListener(new Context.CancellationListener() {
      @Override
      public void cancelled(Context context) {
        if (parentCalled.get()) {
          childAfterParent.set(true);
        }
      }
    }, MoreExecutors.directExecutor());
    parent.addListener(new Context.CancellationListener() {
      @Override
      public void cancelled(Context context) {
        parentCalled.set(true);
      }
    }, MoreExecutors.directExecutor());
    parent.cancel(null);
    assertTrue(parentCalled.get());
    assertTrue(childAfterParent.get());
  }

  @Test
  public void expiredDeadlineShouldCancelContextImmediately() {
    Context parent = Context.current();
    assertFalse(parent.isCancelled());

    Context.CancellableContext context = parent.withDeadlineAfter(0, TimeUnit.SECONDS, scheduler);
    assertTrue(context.isCancelled());
    assertThat(context.cancellationCause(), instanceOf(TimeoutException.class));

    assertFalse(parent.isCancelled());
    Deadline deadline = Deadline.after(-10, TimeUnit.SECONDS);
    assertTrue(deadline.isExpired());
    context = parent.withDeadline(deadline, scheduler);
    assertTrue(context.isCancelled());
    assertThat(context.cancellationCause(), instanceOf(TimeoutException.class));
  }
}
