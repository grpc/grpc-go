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
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * Tests for {@link Contexts}.
 */
@RunWith(JUnit4.class)
public class ContextsTest {

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
        .withDeadlineAfter(100, TimeUnit.MILLISECONDS, fakeClock.scheduledExecutorService);
    fakeClock.forwardTime(System.nanoTime(), TimeUnit.NANOSECONDS);
    fakeClock.forwardMillis(100);

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
