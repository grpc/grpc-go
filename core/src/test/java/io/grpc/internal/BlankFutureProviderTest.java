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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.common.base.Supplier;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;

import io.grpc.internal.BlankFutureProvider.FulfillmentBatch;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.ArrayList;
import java.util.concurrent.ExecutionException;

/** Unit tests for {@link BlankFutureProvider}. */
@RunWith(JUnit4.class)
public class BlankFutureProviderTest {

  private final BlankFutureProvider<String> provider = new BlankFutureProvider<String>();

  @Test public void fulfillWithFinishedFutures() throws Exception {
    ListenableFuture<String> f1 = provider.newBlankFuture();
    ListenableFuture<String> f2 = provider.newBlankFuture();
    FulfillmentBatch<String> batch = provider.createFulfillmentBatch();
    ListenableFuture<String> f3 = provider.newBlankFuture();
    batch.link(new Supplier<ListenableFuture<String>>() {
      int index;
      @Override public ListenableFuture<String> get() {
        return Futures.immediateFuture("value" + (index++));
      }
    });
    assertTrue(f1.isDone());
    assertTrue(f2.isDone());
    assertEquals("value0", f1.get());
    assertEquals("value1", f2.get());
    assertFalse(f3.isDone());
  }

  @Test public void fulfillWithLaterSuccessfulFutures() throws Exception {
    ListenableFuture<String> f1 = provider.newBlankFuture();
    ListenableFuture<String> f2 = provider.newBlankFuture();
    FulfillmentBatch<String> batch = provider.createFulfillmentBatch();
    ListenableFuture<String> f3 = provider.newBlankFuture();
    final ArrayList<SettableFuture<String>> fulfillingFutures =
        new ArrayList<SettableFuture<String>>();
    batch.link(new Supplier<ListenableFuture<String>>() {
      @Override public ListenableFuture<String> get() {
        SettableFuture<String> future = SettableFuture.create();
        fulfillingFutures.add(future);
        return future;
      }
    });
    assertFalse(f1.isDone());
    assertFalse(f2.isDone());
    for (int i = 0; i < fulfillingFutures.size(); i++) {
      fulfillingFutures.get(i).set("value" + i);
    }
    assertEquals("value0", f1.get());
    assertEquals("value1", f2.get());
    assertFalse(f3.isDone());
  }

  @Test public void fulfillWithLaterFailedFutures() throws Exception {
    ListenableFuture<String> f1 = provider.newBlankFuture();
    ListenableFuture<String> f2 = provider.newBlankFuture();
    FulfillmentBatch<String> batch = provider.createFulfillmentBatch();
    ListenableFuture<String> f3 = provider.newBlankFuture();
    final ArrayList<SettableFuture<String>> fulfillingFutures =
        new ArrayList<SettableFuture<String>>();
    batch.link(new Supplier<ListenableFuture<String>>() {
      @Override public ListenableFuture<String> get() {
        SettableFuture<String> future = SettableFuture.create();
        fulfillingFutures.add(future);
        return future;
      }
    });
    assertFalse(f1.isDone());
    assertFalse(f2.isDone());
    for (int i = 0; i < fulfillingFutures.size(); i++) {
      fulfillingFutures.get(i).setException(new Exception("simulated" + i));
    }
    assertTrue(f1.isDone());
    assertTrue(f2.isDone());
    Throwable e1 = null;
    Throwable e2 = null;
    try {
      f1.get();
      fail("Should have thrown");
    } catch (ExecutionException e) {
      e1 = e.getCause();
    }
    try {
      f2.get();
      fail("Should have thrown");
    } catch (ExecutionException e) {
      e2 = e.getCause();
    }
    assertEquals("simulated0", e1.getMessage());
    assertEquals("simulated1", e2.getMessage());
    assertFalse(f3.isDone());
  }

  @Test public void fulfillWithError() throws Exception {
    ListenableFuture<String> f1 = provider.newBlankFuture();
    ListenableFuture<String> f2 = provider.newBlankFuture();
    FulfillmentBatch<String> batch = provider.createFulfillmentBatch();
    ListenableFuture<String> f3 = provider.newBlankFuture();
    Exception error = new Exception("simulated");
    batch.fail(error);
    assertTrue(f1.isDone());
    assertTrue(f2.isDone());
    Throwable e1 = null;
    Throwable e2 = null;
    try {
      f1.get();
      fail("Should have thrown");
    } catch (ExecutionException e) {
      e1 = e.getCause();
    }
    try {
      f2.get();
      fail("Should have thrown");
    } catch (ExecutionException e) {
      e2 = e.getCause();
    }
    assertSame(error, e1);
    assertSame(error, e2);
    assertFalse(f3.isDone());
  }

  @Test public void cancellingFutureRemovesItFromSet() {
    SettableFuture<String> f1 = (SettableFuture<String>) provider.newBlankFuture();
    SettableFuture<String> f2 = (SettableFuture<String>) provider.newBlankFuture();
    assertTrue(provider.getBlankFutureSet().contains(f1));
    assertTrue(provider.getBlankFutureSet().contains(f2));
    f1.cancel(false);
    assertTrue(f1.isCancelled());
    assertFalse(provider.getBlankFutureSet().contains(f1));
    assertTrue(provider.getBlankFutureSet().contains(f2));
    assertFalse(f2.isDone());
  }
}
