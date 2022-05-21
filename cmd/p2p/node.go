package p2p

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"flag"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	connmgr "github.com/libp2p/go-libp2p-connmgr"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/routing"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	tls "github.com/libp2p/go-libp2p-tls"
	yamux "github.com/libp2p/go-libp2p-yamux"
	//discovery "github.com/libp2p/go-libp2p-discovery"
	discovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-tcp-transport"
	"github.com/mr-tron/base58"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
	log "github.com/sirupsen/logrus"
)

const service = "manishmeganathan/peerchat"

type Node struct {
	Ctx context.Context

	Host host.Host

	KadDHT *dht.IpfsDHT

	Discovery *discovery.RoutingDiscovery

	PubSub *pubsub.PubSub
}

func Run() {
	// Define input flags
	username := flag.String("user", "", "username to use in the chatroom.")
	chatroom := flag.String("room", "", "chatroom to join.")
	flag.Parse()

	p2pHost := NewNode()

	p2pHost.AdvertiseConnect()

	chatapp, _ := JoinChatRoom(p2pHost, *username, *chatroom)
	log.Infof("Joined the '%s' chatroom as '%s'", chatapp.RoomName, chatapp.UserName)

	// Wait for network setup to complete
	time.Sleep(time.Second * 5)

	// Create the Chat UI
	ui := NewUI(chatapp)
	// Start the UI system
	ui.RunUI()
}

func NewNode() *Node {

	ctx := context.Background()

	p2pHost, kadDHT := setupHost(ctx)

	bootstrapDHT(ctx, p2pHost, kadDHT)

	routingDiscovery := discovery.NewRoutingDiscovery(kadDHT)

	pubsubHandler := setupPubSub(ctx, p2pHost, routingDiscovery)

	return &Node{
		Ctx:       ctx,
		Host:      p2pHost,
		KadDHT:    kadDHT,
		Discovery: routingDiscovery,
		PubSub:    pubsubHandler,
	}
}

func setupHost(ctx context.Context) (host.Host, *dht.IpfsDHT) {

	privateKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)

	if err != nil {
		log.Fatalln("Failed to Generate P2P Identity Configuration!")
	}

	tlsTransport, err := tls.New(privateKey)

	if err != nil {
		log.Fatalln("Failed to Generate P2P Security and Transport Configurations!")
	}

	mutliAddress, err := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/0")

	if err != nil {
		log.Fatalln("Failed to Generate P2P Address Listener Configuration!")
	}

	var kadDHT *dht.IpfsDHT
	routingTable := libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
		kadDHT = setupKadDHT(ctx, h)
		return kadDHT, err
	})

	opts := libp2p.ChainOptions(
		libp2p.Identity(privateKey),
		libp2p.ListenAddrs(mutliAddress),
		libp2p.Security(tls.ID, tlsTransport),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
		libp2p.ConnectionManager(connmgr.NewConnManager(100, 400, time.Minute)),
		libp2p.NATPortMap(),
		routingTable,
		libp2p.EnableAutoRelay(),
	)

	p2pHost, err := libp2p.New(opts)

	return p2pHost, kadDHT
}

func setupKadDHT(ctx context.Context, host host.Host) *dht.IpfsDHT {
	dhtMode := dht.Mode(dht.ModeServer)
	bootstrapPeers := dht.GetDefaultBootstrapPeerAddrInfos()
	dhtPeers := dht.BootstrapPeers(bootstrapPeers...)

	kadDHT, err := dht.New(ctx, host, dhtMode, dhtPeers)
	if err != nil {
		log.Fatalln("Failed to Create the Kademlia DHT!")
	}
	return kadDHT
}

func setupPubSub(ctx context.Context, host host.Host, discovery *discovery.RoutingDiscovery) *pubsub.PubSub {
	pubsubHandler, err := pubsub.NewGossipSub(ctx, host, pubsub.WithDiscovery(discovery))
	if err != nil {
		log.Fatalln("PubSub Handler Creation Failed!")
	}
	return pubsubHandler
}

func bootstrapDHT(ctx context.Context, p2pHost host.Host, kadDHT *dht.IpfsDHT) {
	if err := kadDHT.Bootstrap(ctx); err != nil {
		log.Fatalln("Failed to Bootstrap the Kademlia!")
	}

	log.Info("Set the Kademlia DHT into Bootstrap Mode.")

	var wg sync.WaitGroup
	var connectedBootstrapPeers int
	var totalBootstrapPeers int

	for _, peerAddress := range dht.DefaultBootstrapPeers {
		peerInfo, _ := peer.AddrInfoFromP2pAddr(peerAddress)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p2pHost.Connect(ctx, *peerInfo); err != nil {
				totalBootstrapPeers++
			} else {
				connectedBootstrapPeers++
				totalBootstrapPeers++
			}
		}()
	}

	wg.Wait()

	log.Info("Connected to %d out of %d Bootstrap Peers.", connectedBootstrapPeers, totalBootstrapPeers)
}

func (p2p *Node) AdvertiseConnect() {
	ttl, err := p2p.Discovery.Advertise(p2p.Ctx, service)
	time.Sleep(time.Second * 5)
	log.Debugf("Service Time-to-Live is %s", ttl)
	peerChan, err := p2p.Discovery.FindPeers(p2p.Ctx, service)
	if err != nil {
		log.Fatalln("P2P Peer Discovery Failed!")
	}
	go handlePeerDiscovery(p2p.Host, peerChan)

}

func (p2p *Node) AnnounceConnect() {

	cidValue := generateCID(service)

	if err := p2p.KadDHT.Provide(p2p.Ctx, cidValue, true); err != nil {
		log.Fatalln("Failed to Announce Service CID!")
	}
	// Sleep to give time for the advertisement to propagate
	time.Sleep(time.Second * 5)

	peerChan := p2p.KadDHT.FindProvidersAsync(p2p.Ctx, cidValue, 0)

	go handlePeerDiscovery(p2p.Host, peerChan)
}

func handlePeerDiscovery(p2pHost host.Host, peerChan <-chan peer.AddrInfo) {
	for p := range peerChan {
		if p.ID == p2pHost.ID() {
			continue
		}
		if err := p2pHost.Connect(context.Background(), p); err != nil {
			log.Fatalln("failed to connect to", p.ID, p.Addrs, err)
		}
		log.Info("Successfully connected to peer", p.Addrs)
	}
}

func generateCID(nameString string) cid.Cid {
	hash := sha256.Sum256([]byte(nameString))
	finalHash := append([]byte{0x12, 0x20}, hash[:]...)
	b58string := base58.Encode(finalHash)

	multiHash, err := multihash.FromB58String(string(b58string))
	if err != nil {
		log.Fatalln("Failed to Generate Service CID!")
	}
	cidValue := cid.NewCidV1(12, multiHash)
	return cidValue
}
