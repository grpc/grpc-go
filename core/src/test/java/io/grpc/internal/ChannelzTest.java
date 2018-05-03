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

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.internal.Channelz.id;
import static junit.framework.TestCase.assertTrue;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import com.google.common.util.concurrent.ListenableFuture;
import io.grpc.internal.Channelz.ChannelStats;
import io.grpc.internal.Channelz.RootChannelList;
import io.grpc.internal.Channelz.ServerList;
import io.grpc.internal.Channelz.ServerSocketsList;
import io.grpc.internal.Channelz.ServerStats;
import io.grpc.internal.Channelz.SocketStats;
import io.grpc.internal.Channelz.Tls;
import java.security.cert.Certificate;
import javax.net.ssl.SSLSession;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class ChannelzTest {

  private final Channelz channelz = new Channelz();

  @Test
  public void getRootChannels_empty() {
    RootChannelList rootChannels = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(rootChannels.end);
    assertThat(rootChannels.channels).isEmpty();
  }

  @Test
  public void getRootChannels_onePage() {
    Instrumented<ChannelStats> root1 = create();
    channelz.addRootChannel(root1);
    RootChannelList page = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(page.end);
    assertThat(page.channels).containsExactly(root1);
  }

  @Test
  public void getRootChannels_onePage_multi() {
    Instrumented<ChannelStats> root1 = create();
    Instrumented<ChannelStats> root2 = create();
    channelz.addRootChannel(root1);
    channelz.addRootChannel(root2);
    RootChannelList page = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 2);
    assertTrue(page.end);
    assertThat(page.channels).containsExactly(root1, root2);
  }

  @Test
  public void getRootChannels_paginate() {
    Instrumented<ChannelStats> root1 = create();
    Instrumented<ChannelStats> root2 = create();
    channelz.addRootChannel(root1);
    channelz.addRootChannel(root2);
    RootChannelList page1 = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertFalse(page1.end);
    assertThat(page1.channels).containsExactly(root1);
    RootChannelList page2
        = channelz.getRootChannels(/*fromId=*/ id(root1) + 1, /*maxPageSize=*/ 1);
    assertTrue(page2.end);
    assertThat(page2.channels).containsExactly(root2);
  }

  @Test
  public void getRootChannels_remove() {
    Instrumented<ChannelStats> root1 = create();
    channelz.addRootChannel(root1);
    channelz.removeRootChannel(root1);
    RootChannelList page = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(page.end);
    assertThat(page.channels).isEmpty();
  }

  @Test
  public void getRootChannels_addAfterLastPage() {
    Instrumented<ChannelStats> root1 = create();
    {
      channelz.addRootChannel(root1);
      RootChannelList page1 = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 1);
      assertTrue(page1.end);
      assertThat(page1.channels).containsExactly(root1);
    }

    Instrumented<ChannelStats> root2 = create();
    {
      channelz.addRootChannel(root2);
      RootChannelList page2
          = channelz.getRootChannels(/*fromId=*/ id(root1) + 1, /*maxPageSize=*/ 1);
      assertTrue(page2.end);
      assertThat(page2.channels).containsExactly(root2);
    }
  }

  @Test
  public void getServers_empty() {
    ServerList servers = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(servers.end);
    assertThat(servers.servers).isEmpty();
  }

  @Test
  public void getServers_onePage() {
    Instrumented<ServerStats> server1 = create();
    channelz.addServer(server1);
    ServerList page = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(page.end);
    assertThat(page.servers).containsExactly(server1);
  }

  @Test
  public void getServers_onePage_multi() {
    Instrumented<ServerStats> server1 = create();
    Instrumented<ServerStats> server2 = create();
    channelz.addServer(server1);
    channelz.addServer(server2);
    ServerList page = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 2);
    assertTrue(page.end);
    assertThat(page.servers).containsExactly(server1, server2);
  }

  @Test
  public void getServers_paginate() {
    Instrumented<ServerStats> server1 = create();
    Instrumented<ServerStats> server2 = create();
    channelz.addServer(server1);
    channelz.addServer(server2);
    ServerList page1 = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertFalse(page1.end);
    assertThat(page1.servers).containsExactly(server1);
    ServerList page2
        = channelz.getServers(/*fromId=*/ id(server1) + 1, /*maxPageSize=*/ 1);
    assertTrue(page2.end);
    assertThat(page2.servers).containsExactly(server2);
  }

  @Test
  public void getServers_remove() {
    Instrumented<ServerStats> server1 = create();
    channelz.addServer(server1);
    channelz.removeServer(server1);
    ServerList page = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(page.end);
    assertThat(page.servers).isEmpty();
  }

  @Test
  public void getServers_addAfterLastPage() {
    Instrumented<ServerStats> server1 = create();
    {
      channelz.addServer(server1);
      ServerList page = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 1);
      assertTrue(page.end);
      assertThat(page.servers).containsExactly(server1);
    }

    Instrumented<ServerStats> server2 = create();
    {
      channelz.addServer(server2);
      ServerList page
          = channelz.getServers(/*fromId=*/ id(server1) + 1, /*maxPageSize=*/ 1);
      assertTrue(page.end);
      assertThat(page.servers).containsExactly(server2);
    }
  }

  @Test
  public void getChannel() {
    Instrumented<ChannelStats> root = create();
    assertNull(channelz.getChannel(id(root)));

    channelz.addRootChannel(root);
    assertSame(root, channelz.getChannel(id(root)));
    assertNull(channelz.getSubchannel(id(root)));

    channelz.removeRootChannel(root);
    assertNull(channelz.getRootChannel(id(root)));
  }

  @Test
  public void getSubchannel() {
    Instrumented<ChannelStats> sub = create();
    assertNull(channelz.getSubchannel(id(sub)));

    channelz.addSubchannel(sub);
    assertSame(sub, channelz.getSubchannel(id(sub)));
    assertNull(channelz.getChannel(id(sub)));

    channelz.removeSubchannel(sub);
    assertNull(channelz.getSubchannel(id(sub)));
  }

  @Test
  public void getSocket() {
    Instrumented<SocketStats> socket = create();
    assertNull(channelz.getSocket(id(socket)));

    channelz.addClientSocket(socket);
    assertSame(socket, channelz.getSocket(id(socket)));

    channelz.removeClientSocket(socket);
    assertNull(channelz.getSocket(id(socket)));
  }

  @Test
  public void serverSocket_noServer() {
    assertNull(channelz.getServerSockets(/*serverId=*/ 1, /*fromId=*/0, /*maxPageSize=*/ 1));
  }

  @Test
  public void serverSocket() {
    Instrumented<ServerStats> server = create();
    channelz.addServer(server);

    Instrumented<SocketStats> socket = create();
    assertEmptyServerSocketsPage(id(server), id(socket));

    channelz.addServerSocket(server, socket);
    ServerSocketsList page
        = channelz.getServerSockets(id(server), id(socket), /*maxPageSize=*/ 1);
    assertNotNull(page);
    assertTrue(page.end);
    assertThat(page.sockets).containsExactly(socket);

    channelz.removeServerSocket(server, socket);
    assertEmptyServerSocketsPage(id(server), id(socket));
  }

  @Test
  public void serverSocket_eachServerSeparate() {
    Instrumented<ServerStats> server1 = create();
    Instrumented<ServerStats> server2 = create();

    Instrumented<SocketStats> socket1 = create();
    Instrumented<SocketStats> socket2 = create();

    channelz.addServer(server1);
    channelz.addServer(server2);
    channelz.addServerSocket(server1, socket1);
    channelz.addServerSocket(server2, socket2);

    ServerSocketsList list1
        = channelz.getServerSockets(id(server1), /*fromId=*/ 0, /*maxPageSize=*/ 2);
    assertNotNull(list1);
    assertTrue(list1.end);
    assertThat(list1.sockets).containsExactly(socket1);

    ServerSocketsList list2
        = channelz.getServerSockets(id(server2), /*fromId=*/ 0, /*maxPageSize=*/2);
    assertNotNull(list2);
    assertTrue(list2.end);
    assertThat(list2.sockets).containsExactly(socket2);
  }

  @Test
  public void tlsSecurityInfo() throws Exception {
    Certificate local = io.grpc.internal.testing.TestUtils.loadX509Cert("client.pem");
    Certificate remote = io.grpc.internal.testing.TestUtils.loadX509Cert("server0.pem");
    final SSLSession session = mock(SSLSession.class);
    when(session.getCipherSuite()).thenReturn("TLS_NULL_WITH_NULL_NULL");
    when(session.getLocalCertificates()).thenReturn(new Certificate[]{local});
    when(session.getPeerCertificates()).thenReturn(new Certificate[]{remote});

    Tls tls = new Tls(session);
    assertEquals(local, tls.localCert);
    assertEquals(remote, tls.remoteCert);
    assertEquals("TLS_NULL_WITH_NULL_NULL", tls.cipherSuiteStandardName);
  }

  private void assertEmptyServerSocketsPage(long serverId, long socketId) {
    ServerSocketsList emptyPage
        = channelz.getServerSockets(serverId, socketId, /*maxPageSize=*/ 1);
    assertNotNull(emptyPage);
    assertTrue(emptyPage.end);
    assertThat(emptyPage.sockets).isEmpty();
  }

  private static <T> Instrumented<T> create() {
    return new Instrumented<T>() {
      final LogId id = LogId.allocate("fake-tag");
      @Override
      public ListenableFuture<T> getStats() {
        throw new UnsupportedOperationException();
      }

      @Override
      public LogId getLogId() {
        return id;
      }
    };
  }
}
