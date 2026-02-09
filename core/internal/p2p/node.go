// Package p2p implements the libp2p-based networking layer.
package p2p

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"
)

// Protocol IDs
const (
	ProtocolID       = "/ccoin/1.0.0"
	BlockTopic       = "ccoin/blocks"
	TransactionTopic = "ccoin/transactions"
	TaskTopic        = "ccoin/tasks"
)

// Node represents a CCoin P2P network node
type Node struct {
	mu sync.RWMutex

	host      host.Host
	dht       *dht.IpfsDHT
	pubsub    *pubsub.PubSub
	discovery *drouting.RoutingDiscovery

	// Topics
	blockTopic *pubsub.Topic
	txTopic    *pubsub.Topic
	taskTopic  *pubsub.Topic

	// Subscriptions
	blockSub *pubsub.Subscription
	txSub    *pubsub.Subscription
	taskSub  *pubsub.Subscription

	// Handlers
	blockHandler MessageHandler
	txHandler    MessageHandler
	taskHandler  MessageHandler

	// Peer management
	peers    map[peer.ID]*PeerInfo
	maxPeers int

	// State
	ctx    context.Context
	cancel context.CancelFunc
}

// PeerInfo holds information about a connected peer
type PeerInfo struct {
	ID          peer.ID
	Addrs       []multiaddr.Multiaddr
	ConnectedAt time.Time
	LastSeen    time.Time
	Version     string
	Height      uint64
}

// MessageHandler defines the interface for handling incoming messages
type MessageHandler func(ctx context.Context, msg *pubsub.Message) error

// Config holds P2P node configuration
type Config struct {
	ListenAddrs   []string
	BootstrapPeers []string
	PrivateKey    crypto.PrivKey
	MaxPeers      int
	EnableMDNS    bool
}

// DefaultConfig returns default P2P configuration
func DefaultConfig() *Config {
	return &Config{
		ListenAddrs: []string{"/ip4/0.0.0.0/tcp/9000"},
		MaxPeers:    50,
		EnableMDNS:  true,
	}
}

// NewNode creates a new P2P node
func NewNode(ctx context.Context, cfg *Config) (*Node, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	nodeCtx, cancel := context.WithCancel(ctx)

	// Generate key if not provided
	privKey := cfg.PrivateKey
	if privKey == nil {
		var err error
		privKey, _, err = crypto.GenerateKeyPairWithReader(crypto.Ed25519, -1, rand.Reader)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to generate key: %w", err)
		}
	}

	// Parse listen addresses
	listenAddrs := make([]multiaddr.Multiaddr, len(cfg.ListenAddrs))
	for i, addr := range cfg.ListenAddrs {
		ma, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("invalid listen address: %w", err)
		}
		listenAddrs[i] = ma
	}

	// Create libp2p host
	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create host: %w", err)
	}

	// Create DHT for peer discovery
	kadDHT, err := dht.New(nodeCtx, h, dht.Mode(dht.ModeAuto))
	if err != nil {
		h.Close()
		cancel()
		return nil, fmt.Errorf("failed to create DHT: %w", err)
	}

	// Create pubsub with GossipSub
	ps, err := pubsub.NewGossipSub(nodeCtx, h)
	if err != nil {
		kadDHT.Close()
		h.Close()
		cancel()
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	node := &Node{
		host:     h,
		dht:      kadDHT,
		pubsub:   ps,
		peers:    make(map[peer.ID]*PeerInfo),
		maxPeers: cfg.MaxPeers,
		ctx:      nodeCtx,
		cancel:   cancel,
	}

	// Set up connection handler
	h.Network().Notify(&network.NotifyBundle{
		ConnectedF:    node.onPeerConnected,
		DisconnectedF: node.onPeerDisconnected,
	})

	// Bootstrap DHT
	if err := kadDHT.Bootstrap(nodeCtx); err != nil {
		node.Close()
		return nil, fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	// Connect to bootstrap peers
	for _, peerAddr := range cfg.BootstrapPeers {
		if err := node.connectToPeer(peerAddr); err != nil {
			// Log but don't fail on bootstrap connection errors
			fmt.Printf("Warning: failed to connect to bootstrap peer %s: %v\n", peerAddr, err)
		}
	}

	// Set up mDNS for local peer discovery
	if cfg.EnableMDNS {
		if err := node.setupMDNS(); err != nil {
			fmt.Printf("Warning: mDNS setup failed: %v\n", err)
		}
	}

	// Create routing discovery
	node.discovery = drouting.NewRoutingDiscovery(kadDHT)

	// Join topics
	if err := node.joinTopics(); err != nil {
		node.Close()
		return nil, fmt.Errorf("failed to join topics: %w", err)
	}

	return node, nil
}

// joinTopics subscribes to all gossip topics
func (n *Node) joinTopics() error {
	var err error

	// Block topic
	n.blockTopic, err = n.pubsub.Join(BlockTopic)
	if err != nil {
		return fmt.Errorf("failed to join block topic: %w", err)
	}
	n.blockSub, err = n.blockTopic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to blocks: %w", err)
	}

	// Transaction topic
	n.txTopic, err = n.pubsub.Join(TransactionTopic)
	if err != nil {
		return fmt.Errorf("failed to join tx topic: %w", err)
	}
	n.txSub, err = n.txTopic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to transactions: %w", err)
	}

	// Task topic
	n.taskTopic, err = n.pubsub.Join(TaskTopic)
	if err != nil {
		return fmt.Errorf("failed to join task topic: %w", err)
	}
	n.taskSub, err = n.taskTopic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to tasks: %w", err)
	}

	return nil
}

// Start begins processing messages
func (n *Node) Start() {
	go n.processMessages(n.blockSub, n.blockHandler)
	go n.processMessages(n.txSub, n.txHandler)
	go n.processMessages(n.taskSub, n.taskHandler)
	go n.maintainPeers()
}

// processMessages handles incoming messages on a subscription
func (n *Node) processMessages(sub *pubsub.Subscription, handler MessageHandler) {
	for {
		msg, err := sub.Next(n.ctx)
		if err != nil {
			if n.ctx.Err() != nil {
				return // Context cancelled, shutting down
			}
			continue
		}

		// Skip messages from self
		if msg.ReceivedFrom == n.host.ID() {
			continue
		}

		// Update peer last seen
		n.mu.Lock()
		if p, exists := n.peers[msg.ReceivedFrom]; exists {
			p.LastSeen = time.Now()
		}
		n.mu.Unlock()

		// Call handler if set
		if handler != nil {
			if err := handler(n.ctx, msg); err != nil {
				fmt.Printf("Message handler error: %v\n", err)
			}
		}
	}
}

// maintainPeers periodically maintains peer connections
func (n *Node) maintainPeers() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.discoverPeers()
			n.pruneStale()
		}
	}
}

// discoverPeers finds new peers via DHT
func (n *Node) discoverPeers() {
	n.mu.RLock()
	currentPeers := len(n.peers)
	n.mu.RUnlock()

	if currentPeers >= n.maxPeers {
		return
	}

	ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	defer cancel()

	peerChan, err := n.discovery.FindPeers(ctx, "ccoin-network")
	if err != nil {
		return
	}

	for p := range peerChan {
		if p.ID == n.host.ID() {
			continue
		}
		if len(p.Addrs) == 0 {
			continue
		}

		n.mu.RLock()
		_, exists := n.peers[p.ID]
		n.mu.RUnlock()

		if !exists && len(n.peers) < n.maxPeers {
			if err := n.host.Connect(ctx, p); err == nil {
				n.addPeer(p.ID, p.Addrs)
			}
		}
	}
}

// pruneStale removes stale peer connections
func (n *Node) pruneStale() {
	n.mu.Lock()
	defer n.mu.Unlock()

	staleThreshold := time.Now().Add(-5 * time.Minute)
	for id, p := range n.peers {
		if p.LastSeen.Before(staleThreshold) {
			n.host.Network().ClosePeer(id)
			delete(n.peers, id)
		}
	}
}

// SetBlockHandler sets the handler for incoming blocks
func (n *Node) SetBlockHandler(handler MessageHandler) {
	n.blockHandler = handler
}

// SetTransactionHandler sets the handler for incoming transactions
func (n *Node) SetTransactionHandler(handler MessageHandler) {
	n.txHandler = handler
}

// SetTaskHandler sets the handler for incoming tasks
func (n *Node) SetTaskHandler(handler MessageHandler) {
	n.taskHandler = handler
}

// BroadcastBlock broadcasts a block to the network
func (n *Node) BroadcastBlock(data []byte) error {
	return n.blockTopic.Publish(n.ctx, data)
}

// BroadcastTransaction broadcasts a transaction to the network
func (n *Node) BroadcastTransaction(data []byte) error {
	return n.txTopic.Publish(n.ctx, data)
}

// BroadcastTask broadcasts a task to the network
func (n *Node) BroadcastTask(data []byte) error {
	return n.taskTopic.Publish(n.ctx, data)
}

// connectToPeer connects to a peer given its multiaddress
func (n *Node) connectToPeer(addr string) error {
	ma, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return err
	}

	peerInfo, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	defer cancel()

	if err := n.host.Connect(ctx, *peerInfo); err != nil {
		return err
	}

	n.addPeer(peerInfo.ID, peerInfo.Addrs)
	return nil
}

// addPeer adds a peer to the peer list
func (n *Node) addPeer(id peer.ID, addrs []multiaddr.Multiaddr) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.peers[id] = &PeerInfo{
		ID:          id,
		Addrs:       addrs,
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
	}
}

// onPeerConnected handles new peer connections
func (n *Node) onPeerConnected(_ network.Network, conn network.Conn) {
	id := conn.RemotePeer()
	n.addPeer(id, []multiaddr.Multiaddr{conn.RemoteMultiaddr()})
}

// onPeerDisconnected handles peer disconnections
func (n *Node) onPeerDisconnected(_ network.Network, conn network.Conn) {
	id := conn.RemotePeer()
	n.mu.Lock()
	delete(n.peers, id)
	n.mu.Unlock()
}

// setupMDNS sets up mDNS for local network peer discovery
func (n *Node) setupMDNS() error {
	service := mdns.NewMdnsService(n.host, "ccoin-local", &mdnsNotifee{node: n})
	return service.Start()
}

type mdnsNotifee struct {
	node *Node
}

func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == m.node.host.ID() {
		return
	}
	ctx, cancel := context.WithTimeout(m.node.ctx, 5*time.Second)
	defer cancel()
	m.node.host.Connect(ctx, pi)
}

// ID returns the node's peer ID
func (n *Node) ID() peer.ID {
	return n.host.ID()
}

// Addrs returns the node's listen addresses
func (n *Node) Addrs() []multiaddr.Multiaddr {
	return n.host.Addrs()
}

// PeerCount returns the number of connected peers
func (n *Node) PeerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.peers)
}

// Peers returns information about connected peers
func (n *Node) Peers() []*PeerInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()

	peers := make([]*PeerInfo, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	return peers
}

// Close shuts down the node
func (n *Node) Close() error {
	n.cancel()

	if n.blockSub != nil {
		n.blockSub.Cancel()
	}
	if n.txSub != nil {
		n.txSub.Cancel()
	}
	if n.taskSub != nil {
		n.taskSub.Cancel()
	}

	if n.dht != nil {
		n.dht.Close()
	}

	return n.host.Close()
}

// RegisterProtocol registers a custom protocol handler
func (n *Node) RegisterProtocol(protoID protocol.ID, handler network.StreamHandler) {
	n.host.SetStreamHandler(protoID, handler)
}
