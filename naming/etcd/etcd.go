package etcd

import (
	"fmt"
	"sync"

	etcdcl "github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"google.golang.org/grpc/naming"
)

type kv struct {
	key, value string
}

// recvBuffer is an unbounded channel of *kv.
type recvBuffer struct {
	c        chan *kv
	mu       sync.Mutex
	stopping bool
	backlog  []*kv
}

func newRecvBuffer() *recvBuffer {
	b := &recvBuffer{
		c: make(chan *kv, 1),
	}
	return b
}

func (b *recvBuffer) put(r *kv) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopping {
		return
	}
	b.backlog = append(b.backlog, r)
	select {
	case b.c <- b.backlog[0]:
		b.backlog = b.backlog[1:]
	default:
	}
}

func (b *recvBuffer) load() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopping || len(b.backlog) == 0 {
		return
	}
	select {
	case b.c <- b.backlog[0]:
		b.backlog = b.backlog[1:]
	default:
	}
}

func (b *recvBuffer) get() <-chan *kv {
	return b.c
}

func (b *recvBuffer) stop() {
	b.mu.Lock()
	b.stopping = true
	close(b.c)
	b.mu.Unlock()
}

type etcdNR struct {
	kAPI   etcdcl.KeysAPI
	recv   *recvBuffer
	ctx    context.Context
	cancel context.CancelFunc
}

// NewETCDNR creates an etcd NameResolver
func NewETCDNR(cfg etcdcl.Config) naming.Resolver {
	c, err := etcdcl.New(cfg)
	if err != nil {
		panic(err)
	}
	kAPI := etcdcl.NewKeysAPI(c)
	ctx, cancel := context.WithCancel(context.Background())
	return &etcdNR{
		kAPI:   kAPI,
		recv:   newRecvBuffer(),
		ctx:    ctx,
		cancel: cancel,
	}
}

// getNode builds the resulting key-value map starting from node recursively
func getNode(node *etcdcl.Node, res map[string]string) {
	if !node.Dir {
		res[node.Key] = node.Value
		return
	}
	for _, val := range node.Nodes {
		getNode(val, res)
	}
}

func (nr *etcdNR) Get(target string) map[string]string {
	resp, err := nr.kAPI.Get(nr.ctx, target, &etcdcl.GetOptions{Recursive: true, Sort: true})
	if err != nil {
		fmt.Printf("non-nil error: %v", err)
	}
	res := make(map[string]string)
	getNode(resp.Node, res)
	return res
}

func (nr *etcdNR) Watch(target string) {
	watcher := nr.kAPI.Watcher(target, &etcdcl.WatcherOptions{Recursive: true})
	for {
		ctx, cancel := context.WithCancel(nr.ctx)
		nr.ctx = ctx
		nr.cancel = cancel
		resp, err := watcher.Next(nr.ctx)
		if err != nil {
			fmt.Printf("non-nil error: %v", err)
			break
		}
		if resp.Node.Dir {
			continue
		}
		entry := &kv{key: resp.Node.Key, value: resp.Node.Value}
		nr.recv.put(entry)
	}
}

func (nr *etcdNR) GetUpdate() (string, string) {
	select {
	case i := <-nr.recv.get():
		nr.recv.load()
		if i == nil {
			return "", ""
		}
		// returns key and the corresponding value of the updated kv
		return i.key, i.value
	}
}

func (nr *etcdNR) Stop() {
	nr.recv.stop()
	nr.cancel()
}
