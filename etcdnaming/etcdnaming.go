package etcdnaming

import (
	"fmt"
	"golang.org/x/net/context"
	"sync"

	etcdcl "github.com/coreos/etcd/client"
)

type namePair struct {
	key, value string
}

// recvBuffer is an unbounded channel of item.
type recvBuffer struct {
	c        chan *namePair
	mu       sync.Mutex
	stopping bool
	backlog  []*namePair
}

func newRecvBuffer() *recvBuffer {
	b := &recvBuffer{
		c: make(chan *namePair, 1),
	}
	return b
}

func (b *recvBuffer) put(r *namePair) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.backlog = append(b.backlog, r)
	if !b.stopping {
		select {
		case b.c <- b.backlog[0]:
			b.backlog = b.backlog[1:]
		default:
		}
	}
}

func (b *recvBuffer) load() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.backlog) > 0 && !b.stopping {
		select {
		case b.c <- b.backlog[0]:
			b.backlog = b.backlog[1:]
		default:
		}
	}
}

func (b *recvBuffer) get() <-chan *namePair {
	return b.c
}

func (b *recvBuffer) stop() {
	b.mu.Lock()
	b.stopping = true
	close(b.c)
	b.mu.Unlock()
}

type etcdNR struct {
	cfg    etcdcl.Config
	recv   *recvBuffer
	ctx    context.Context
	cancel context.CancelFunc
}

func NewETCDNR(cfg etcdcl.Config) *etcdNR {
	return &etcdNR{
		cfg:  cfg,
		recv: newRecvBuffer(),
		ctx:  context.Background(),
	}
}

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
	cfg := nr.cfg
	c, err := etcdcl.New(cfg)
	if err != nil {
		panic(err)
	}
	kAPI := etcdcl.NewKeysAPI(c)
	resp, err := kAPI.Get(nr.ctx, target, &etcdcl.GetOptions{Recursive: true, Sort: true})
	if err != nil {
		fmt.Printf("non-nil error: %v", err)
	}
	node := resp.Node
	res := make(map[string]string)
	getNode(node, res)
	return res
}

func (nr *etcdNR) Watch(target string) {
	cfg := nr.cfg
	c, err := etcdcl.New(cfg)
	if err != nil {
		panic(err)
	}
	kAPI := etcdcl.NewKeysAPI(c)
	watcher := kAPI.Watcher(target, &etcdcl.WatcherOptions{Recursive: true})
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
		entry := &namePair{key: resp.Node.Key, value: resp.Node.Value}
		nr.recv.put(entry)
	}
}

func (nr *etcdNR) GetUpdate() *namePair {
	select {
	case i := <-nr.recv.get():
		nr.recv.load()
		return i
	}
}

func (nr *etcdNR) Stop() {
	nr.recv.stop()
	nr.cancel()
}
