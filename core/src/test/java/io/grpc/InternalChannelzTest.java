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

package io.grpc;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.InternalChannelz.id;
import static junit.framework.TestCase.assertTrue;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import com.google.common.util.concurrent.ListenableFuture;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.RootChannelList;
import io.grpc.InternalChannelz.ServerList;
import io.grpc.InternalChannelz.ServerSocketsList;
import io.grpc.InternalChannelz.ServerStats;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalChannelz.Tls;
import java.security.cert.Certificate;
import javax.net.ssl.SSLSession;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class InternalChannelzTest {

  private final InternalChannelz channelz = new InternalChannelz();

  @Test
  public void getRootChannels_empty() {
    RootChannelList rootChannels = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(rootChannels.end);
    assertThat(rootChannels.channels).isEmpty();
  }

  @Test
  public void getRootChannels_onePage() {
    InternalInstrumented<ChannelStats> root1 = create();
    channelz.addRootChannel(root1);
    RootChannelList page = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(page.end);
    assertThat(page.channels).containsExactly(root1);
  }

  @Test
  public void getRootChannels_onePage_multi() {
    InternalInstrumented<ChannelStats> root1 = create();
    InternalInstrumented<ChannelStats> root2 = create();
    channelz.addRootChannel(root1);
    channelz.addRootChannel(root2);
    RootChannelList page = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 2);
    assertTrue(page.end);
    assertThat(page.channels).containsExactly(root1, root2);
  }

  @Test
  public void getRootChannels_paginate() {
    InternalInstrumented<ChannelStats> root1 = create();
    InternalInstrumented<ChannelStats> root2 = create();
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
    InternalInstrumented<ChannelStats> root1 = create();
    channelz.addRootChannel(root1);
    channelz.removeRootChannel(root1);
    RootChannelList page = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(page.end);
    assertThat(page.channels).isEmpty();
  }

  @Test
  public void getRootChannels_addAfterLastPage() {
    InternalInstrumented<ChannelStats> root1 = create();
    {
      channelz.addRootChannel(root1);
      RootChannelList page1 = channelz.getRootChannels(/*fromId=*/ 0, /*maxPageSize=*/ 1);
      assertTrue(page1.end);
      assertThat(page1.channels).containsExactly(root1);
    }

    InternalInstrumented<ChannelStats> root2 = create();
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
    InternalInstrumented<ServerStats> server1 = create();
    channelz.addServer(server1);
    ServerList page = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(page.end);
    assertThat(page.servers).containsExactly(server1);
  }

  @Test
  public void getServers_onePage_multi() {
    InternalInstrumented<ServerStats> server1 = create();
    InternalInstrumented<ServerStats> server2 = create();
    channelz.addServer(server1);
    channelz.addServer(server2);
    ServerList page = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 2);
    assertTrue(page.end);
    assertThat(page.servers).containsExactly(server1, server2);
  }

  @Test
  public void getServers_paginate() {
    InternalInstrumented<ServerStats> server1 = create();
    InternalInstrumented<ServerStats> server2 = create();
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
    InternalInstrumented<ServerStats> server1 = create();
    channelz.addServer(server1);
    channelz.removeServer(server1);
    ServerList page = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 1);
    assertTrue(page.end);
    assertThat(page.servers).isEmpty();
  }

  @Test
  public void getServers_addAfterLastPage() {
    InternalInstrumented<ServerStats> server1 = create();
    {
      channelz.addServer(server1);
      ServerList page = channelz.getServers(/*fromId=*/ 0, /*maxPageSize=*/ 1);
      assertTrue(page.end);
      assertThat(page.servers).containsExactly(server1);
    }

    InternalInstrumented<ServerStats> server2 = create();
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
    InternalInstrumented<ChannelStats> root = create();
    assertNull(channelz.getChannel(id(root)));

    channelz.addRootChannel(root);
    assertSame(root, channelz.getChannel(id(root)));
    assertNull(channelz.getSubchannel(id(root)));

    channelz.removeRootChannel(root);
    assertNull(channelz.getRootChannel(id(root)));
  }

  @Test
  public void getSubchannel() {
    InternalInstrumented<ChannelStats> sub = create();
    assertNull(channelz.getSubchannel(id(sub)));

    channelz.addSubchannel(sub);
    assertSame(sub, channelz.getSubchannel(id(sub)));
    assertNull(channelz.getChannel(id(sub)));

    channelz.removeSubchannel(sub);
    assertNull(channelz.getSubchannel(id(sub)));
  }

  @Test
  public void getSocket() {
    InternalInstrumented<SocketStats> socket = create();
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
    InternalInstrumented<ServerStats> server = create();
    channelz.addServer(server);

    InternalInstrumented<SocketStats> socket = create();
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
    InternalInstrumented<ServerStats> server1 = create();
    InternalInstrumented<ServerStats> server2 = create();

    InternalInstrumented<SocketStats> socket1 = create();
    InternalInstrumented<SocketStats> socket2 = create();

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

  private static <T> InternalInstrumented<T> create() {
    return new InternalInstrumented<T>() {
      final InternalLogId id = InternalLogId.allocate("fake-type", /*details=*/ null);
      @Override
      public ListenableFuture<T> getStats() {
        throw new UnsupportedOperationException();
      }

      @Override
      public InternalLogId getLogId() {
        return id;
      }
    };
  }
}
