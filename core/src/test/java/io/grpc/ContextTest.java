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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.common.util.concurrent.MoreExecutors;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * Tests for {@link Context}.
 */
@RunWith(JUnit4.class)
public class ContextTest {

  private static final Context.Key<String> PET = Context.key("pet");
  private static final Context.Key<String> FOOD = Context.keyWithDefault("food", "lasagna");
  private static final Context.Key<String> COLOR = Context.key("color");
  private static final Context.Key<Object> OBSERVED = Context.key("observed");

  private Context listenerNotifedContext;
  private CountDownLatch deadlineLatch = new CountDownLatch(1);
  private Context.CancellationListener cancellationListener = new Context.CancellationListener() {
    @Override
    public void cancelled(Context context) {
      listenerNotifedContext = context;
      deadlineLatch.countDown();
    }
  };

  private CountDownLatch observableLatch = new CountDownLatch(1);
  private Object observed;
  private Runnable runner = new Runnable() {
    @Override
    public void run() {
      observed = OBSERVED.get();
      observableLatch.countDown();
    }
  };

  private ExecutorService executorService = Executors.newCachedThreadPool();

  @Before
  public void setUp() throws Exception {
    // Detach all contexts back to the root.
    while (Context.current() != Context.ROOT) {
      Context.current().detach();
    }
  }

  @After
  public void tearDown() throws Exception {
  }

  @Test
  public void rootIsInitialContext() {
    assertNotNull(Context.ROOT);
    assertTrue(Context.ROOT.isCurrent());
  }

  @Test
  public void rootIsAlwaysBound() {
    Context root = Context.current();
    try {
      root.detach();
    } catch (IllegalStateException ise) {
      // Expected
      assertTrue(Context.ROOT.isCurrent());
      return;
    }
    fail("Attempting to detach root should fail");
  }

  @Test
  public void rootCanBeAttached() {
    Context root = Context.current();
    Context.CancellableContext fork = root.fork();
    fork.attach();
    root.attach();
    assertTrue(root.isCurrent());
    root.detach();
    assertTrue(fork.isCurrent());
    fork.detach();
    assertTrue(root.isCurrent());
  }

  @Test
  public void rootCanNeverHaveAListener() {
    Context root = Context.current();
    root.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertEquals(0, root.listenerCount());
  }

  @Test
  public void attachedCancellableContextCannotBeCastFromCurrent() {
    Context initial = Context.current();
    Context.CancellableContext base = Context.current().withCancellation();
    base.attach();
    assertFalse(Context.current() instanceof Context.CancellableContext);
    assertNotSame(initial, Context.current());
    base.detachAndCancel(null);
    assertTrue(initial.isCurrent());
  }

  @Test
  public void detachingNonCurrentThrowsIllegalStateException() {
    Context base = Context.current().fork();
    try {
      base.detach();
    } catch (IllegalStateException ise) {
      return;
    }
    fail("Expected exception");
  }

  @Test
  public void detachUnwindsAttach() {
    Context base = Context.current().fork();
    Context child = base.withValue(COLOR, "red");
    Context grandchild = child.withValue(COLOR, "blue");
    base.attach();
    base.attach();
    child.attach();
    base.attach();
    grandchild.attach();
    assertTrue(grandchild.isCurrent());
    grandchild.detach();
    assertTrue(base.isCurrent());
    base.detach();
    assertTrue(child.isCurrent());
    child.detach();
    assertTrue(base.isCurrent());
    base.detach();
    assertTrue(base.isCurrent());
    base.detach();
    assertTrue(Context.ROOT.isCurrent());
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

    child.detach();

    // Should have values from base
    assertEquals("dog", PET.get());
    assertEquals("lasagna", FOOD.get());
    assertNull(COLOR.get());

    base.detach();

    assertNull(PET.get());
    assertEquals("lasagna", FOOD.get());
    assertNull(COLOR.get());
  }

  @Test
  public void cancelReturnsFalseIfAlreadyCancelled() {
    Context.CancellableContext base = Context.current().withCancellation();
    assertTrue(base.cancel(null));
    assertTrue(base.isCancelled());
    assertFalse(base.cancel(null));
  }

  @Test
  public void notifyListenerOnCancel() {
    Context.CancellableContext base = Context.current().withCancellation();
    base.attach();
    base.addListener(cancellationListener, MoreExecutors.directExecutor());
    base.detachAndCancel(null);
    assertSame(base, listenerNotifedContext);
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
    assertSame(cause, base.cause());
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertSame(cause, child.cause());
    assertEquals(0, base.listenerCount());
    assertEquals(0, child.listenerCount());
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
    assertSame(cause, base.cause());
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertSame(cause, child.cause());
    assertEquals(0, base.listenerCount());
    assertEquals(0, child.listenerCount());
  }

  @Test
  public void nonCascadingCancellationDoesNotNotifyForked() {
    Context.CancellableContext base = Context.current().withCancellation();
    Context fork = base.fork();
    fork.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertTrue(base.cancel(null));
    assertNull(listenerNotifedContext);
    assertFalse(fork.isCancelled());
    assertEquals(1, fork.listenerCount());
  }

  @Test
  public void testWrapRunnable() throws Exception {
    Context base = Context.current().withValue(OBSERVED, "cat");

    executorService.execute(base.wrap(runner));
    observableLatch.await();
    assertEquals("cat", observed);

    observableLatch = new CountDownLatch(1);
    executorService.execute(Context.current().wrap(runner));
    observableLatch.await();
    assertNull(observed);
  }

  @Test
  public void typicalTryFinallyHandling() throws Exception {
    Context base = Context.current().withValue(COLOR, "blue");
    base.attach();
    try {
      assertTrue(base.isCurrent());
      // Do something
    } finally {
      base.detach();
    }
    assertFalse(base.isCurrent());
  }

  @Test
  public void typicalCancellableTryCatchFinallyHandling() throws Exception {
    Context.CancellableContext base = Context.current().withCancellation();
    base.attach();
    try {
      // Do something
      throw new IllegalStateException("Argh");
    } catch (IllegalStateException ise) {
      base.cancel(ise);
    } finally {
      base.detachAndCancel(null);
    }
    assertTrue(base.isCancelled());
    assertNotNull(base.cause());
  }


  /*
  public void testTryWithResource() throws Exception {
    Context.CancellableContext base = Context.current().withCancellation();

    try (Closeable c = base.attachAsCloseable()) {
      // Do something
      throw new IllegalStateException("Argh");
    } catch (IllegalStateException ise) {
      // Don't capture exception
    }
    assertTrue(base.isCancelled());
    assertNull(base.cause());
  }

  public void testTryWithResource() throws Exception {
    Context.CancellableContext base = Context.current().withCancellation();

    try (Closeable c = base.attachAsCloseable()) {
      // Do something
      throw new IllegalStateException("Argh");
    } catch (IllegalStateException ise) {
      base.cancel(ise);
    }
    assertTrue(base.isCancelled());
    assertNotNull(base.cause());
  }
  */

  @Test
  public void absoluteDeadlineTriggersAndPropagates() throws Exception {
    Context base = Context.current().withDeadlineNanoTime(System.nanoTime()
        + TimeUnit.SECONDS.toNanos(1));
    Context child = base.withValue(FOOD, "lasagna");
    child.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertFalse(base.isCancelled());
    assertFalse(child.isCancelled());
    deadlineLatch.await(2, TimeUnit.SECONDS);
    assertTrue(base.isCancelled());
    assertTrue(base.cause() instanceof TimeoutException);
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertSame(base.cause(), child.cause());
  }

  @Test
  public void relativeDeadlineTriggersAndPropagates() throws Exception {
    Context base = Context.current().withDeadlineAfter(1, TimeUnit.SECONDS);
    Context child = base.withValue(FOOD, "lasagna");
    child.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertFalse(base.isCancelled());
    assertFalse(child.isCancelled());
    deadlineLatch.await(2, TimeUnit.SECONDS);
    assertTrue(base.isCancelled());
    assertTrue(base.cause() instanceof TimeoutException);
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertSame(base.cause(), child.cause());
  }

  @Test
  public void innerDeadlineCompletesBeforeOuter() throws Exception {
    Context base = Context.current().withDeadlineAfter(2, TimeUnit.SECONDS);
    Context child = base.withDeadlineAfter(1, TimeUnit.SECONDS);
    child.addListener(cancellationListener, MoreExecutors.directExecutor());
    assertFalse(base.isCancelled());
    assertFalse(child.isCancelled());
    deadlineLatch.await(2, TimeUnit.SECONDS);
    assertFalse(base.isCancelled());
    assertSame(child, listenerNotifedContext);
    assertTrue(child.isCancelled());
    assertTrue(child.cause() instanceof TimeoutException);

    deadlineLatch = new CountDownLatch(1);
    base.addListener(cancellationListener, MoreExecutors.directExecutor());
    deadlineLatch.await(2, TimeUnit.SECONDS);
    assertTrue(base.isCancelled());
    assertTrue(base.cause() instanceof TimeoutException);
    assertNotSame(base.cause(), child.cause());
  }
}
