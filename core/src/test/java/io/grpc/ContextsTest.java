/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc;

import static io.grpc.Contexts.interceptCall;
import static io.grpc.Contexts.statusFromCancelled;
import static org.hamcrest.core.IsInstanceOf.instanceOf;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertThat;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import io.grpc.internal.FakeClock;
import io.grpc.internal.NoopServerCall;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link Contexts}.
 */
@RunWith(JUnit4.class)
public class ContextsTest {
  private static Context.Key<Object> contextKey = Context.key("key");
  /** For use in comparing context by reference. */
  private Context uniqueContext = Context.ROOT.withValue(contextKey, new Object());
  @SuppressWarnings("unchecked")
  private ServerCall<Object, Object> call = new NoopServerCall<Object, Object>();
  private Metadata headers = new Metadata();

  @Test
  public void interceptCall_basic() {
    Context origContext = Context.current();
    final Object message = new Object();
    final List<Integer> methodCalls = new ArrayList<>();
    final ServerCall.Listener<Object> listener = new ServerCall.Listener<Object>() {
      @Override public void onMessage(Object messageIn) {
        assertSame(message, messageIn);
        assertSame(uniqueContext, Context.current());
        methodCalls.add(1);
      }

      @Override public void onHalfClose() {
        assertSame(uniqueContext, Context.current());
        methodCalls.add(2);
      }

      @Override public void onCancel() {
        assertSame(uniqueContext, Context.current());
        methodCalls.add(3);
      }

      @Override public void onComplete() {
        assertSame(uniqueContext, Context.current());
        methodCalls.add(4);
      }

      @Override public void onReady() {
        assertSame(uniqueContext, Context.current());
        methodCalls.add(5);
      }
    };
    ServerCall.Listener<Object> wrapped = interceptCall(uniqueContext, call, headers,
        new ServerCallHandler<Object, Object>() {
          @Override
          public ServerCall.Listener<Object> startCall(
              ServerCall<Object, Object> call, Metadata headers) {
            assertSame(ContextsTest.this.call, call);
            assertSame(ContextsTest.this.headers, headers);
            assertSame(uniqueContext, Context.current());
            return listener;
          }
        });
    assertSame(origContext, Context.current());

    wrapped.onMessage(message);
    wrapped.onHalfClose();
    wrapped.onCancel();
    wrapped.onComplete();
    wrapped.onReady();
    assertEquals(Arrays.asList(1, 2, 3, 4, 5), methodCalls);
    assertSame(origContext, Context.current());
  }

  @Test
  public void interceptCall_restoresIfNextThrows() {
    Context origContext = Context.current();
    try {
      interceptCall(uniqueContext, call, headers, new ServerCallHandler<Object, Object>() {
        @Override
        public ServerCall.Listener<Object> startCall(
            ServerCall<Object, Object> call, Metadata headers) {
          throw new RuntimeException();
        }
      });
      fail("Expected exception");
    } catch (RuntimeException expected) {
    }
    assertSame(origContext, Context.current());
  }

  @Test
  public void interceptCall_restoresIfListenerThrows() {
    Context origContext = Context.current();
    final ServerCall.Listener<Object> listener = new ServerCall.Listener<Object>() {
      @Override public void onMessage(Object messageIn) {
        throw new RuntimeException();
      }

      @Override public void onHalfClose() {
        throw new RuntimeException();
      }

      @Override public void onCancel() {
        throw new RuntimeException();
      }

      @Override public void onComplete() {
        throw new RuntimeException();
      }

      @Override public void onReady() {
        throw new RuntimeException();
      }
    };
    ServerCall.Listener<Object> wrapped = interceptCall(uniqueContext, call, headers,
        new ServerCallHandler<Object, Object>() {
          @Override
          public ServerCall.Listener<Object> startCall(
              ServerCall<Object, Object> call, Metadata headers) {
            return listener;
          }
        });

    try {
      wrapped.onMessage(new Object());
      fail("Exception expected");
    } catch (RuntimeException expected) {
    }
    try {
      wrapped.onHalfClose();
      fail("Exception expected");
    } catch (RuntimeException expected) {
    }
    try {
      wrapped.onCancel();
      fail("Exception expected");
    } catch (RuntimeException expected) {
    }
    try {
      wrapped.onComplete();
      fail("Exception expected");
    } catch (RuntimeException expected) {
    }
    try {
      wrapped.onReady();
      fail("Exception expected");
    } catch (RuntimeException expected) {
    }
    assertSame(origContext, Context.current());
  }

  @Test
  public void statusFromCancelled_returnNullIfCtxNotCancelled() {
    Context context = Context.current();
    assertFalse(context.isCancelled());
    assertNull(statusFromCancelled(context));
  }

  @Test
  public void statusFromCancelled_returnStatusAsSetOnCtx() {
    Context.CancellableContext cancellableContext = Context.current().withCancellation();
    cancellableContext.cancel(Status.DEADLINE_EXCEEDED.withDescription("foo bar").asException());
    Status status = statusFromCancelled(cancellableContext);
    assertNotNull(status);
    assertEquals(Status.Code.DEADLINE_EXCEEDED, status.getCode());
    assertEquals("foo bar", status.getDescription());
  }

  @Test
  public void statusFromCancelled_shouldReturnStatusWithCauseAttached() {
    Context.CancellableContext cancellableContext = Context.current().withCancellation();
    Throwable t = new Throwable();
    cancellableContext.cancel(t);
    Status status = statusFromCancelled(cancellableContext);
    assertNotNull(status);
    assertEquals(Status.Code.CANCELLED, status.getCode());
    assertSame(t, status.getCause());
  }

  @Test
  public void statusFromCancelled_TimeoutExceptionShouldMapToDeadlineExceeded() {
    FakeClock fakeClock = new FakeClock();
    Context.CancellableContext cancellableContext = Context.current()
        .withDeadlineAfter(100, TimeUnit.NANOSECONDS, fakeClock.getScheduledExecutorService());
    fakeClock.forwardTime(System.nanoTime(), TimeUnit.NANOSECONDS);
    fakeClock.forwardNanos(100);

    assertTrue(cancellableContext.isCancelled());
    assertThat(cancellableContext.cancellationCause(), instanceOf(TimeoutException.class));

    Status status = statusFromCancelled(cancellableContext);
    assertNotNull(status);
    assertEquals(Status.Code.DEADLINE_EXCEEDED, status.getCode());
    assertEquals("context timed out", status.getDescription());
  }

  @Test
  public void statusFromCancelled_returnCancelledIfCauseIsNull() {
    Context.CancellableContext cancellableContext = Context.current().withCancellation();
    cancellableContext.cancel(null);
    assertTrue(cancellableContext.isCancelled());
    Status status = statusFromCancelled(cancellableContext);
    assertNotNull(status);
    assertEquals(Status.Code.CANCELLED, status.getCode());
  }

  /** This is a whitebox test, to verify a special case of the implementation. */
  @Test
  public void statusFromCancelled_StatusUnknownShouldWork() {
    Context.CancellableContext cancellableContext = Context.current().withCancellation();
    Exception e = Status.UNKNOWN.asException();
    cancellableContext.cancel(e);
    assertTrue(cancellableContext.isCancelled());

    Status status = statusFromCancelled(cancellableContext);
    assertNotNull(status);
    assertEquals(Status.Code.UNKNOWN, status.getCode());
    assertSame(e, status.getCause());
  }

  @Test
  public void statusFromCancelled_shouldThrowIfCtxIsNull() {
    try {
      statusFromCancelled(null);
      fail("NPE expected");
    } catch (NullPointerException npe) {
      assertEquals("context must not be null", npe.getMessage());
    }
  }
}
