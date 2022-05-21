package node

import (
	"context"
	"fmt"
	"io"
	mrand "math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/i500-p2p/config"
	cfg "github.com/i500-p2p/config"
	"github.com/libp2p/go-libp2p"
	connmgr "github.com/libp2p/go-libp2p-connmgr"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	tls "github.com/libp2p/go-libp2p-tls"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	//"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-tcp-transport"
	"github.com/multiformats/go-multiaddr"
	log "github.com/sirupsen/logrus"
)

type Node struct {
	Host   host.Host
	KadDHT *dht.IpfsDHT
}

func Run() {
	ctx := context.Background()

	conf, err := cfg.ParseFlags()
	if err != nil {
		panic(err)
	}

	node, err := NewNode(ctx, conf)

	if err != nil {
		panic(err)
	}

	//go server.RunServer(node.Host.Addrs()[0].String())

	go node.AdvertiseAndFindPeers(ctx, conf)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Debugln("Received signal, shutting down...")

	// shut the node down
	if err := node.Host.Close(); err != nil {
		panic(err)
	}
}

func NewNode(ctx context.Context, config config.Config) (*Node, error) {
	var r io.Reader

	r = mrand.New(mrand.NewSource(config.Seed))

	privateKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		return nil, err
	}

	tlsTransport, err := tls.New(privateKey)

	addr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", config.Port))
	p2pHost, err := libp2p.New(
		libp2p.ListenAddrs(addr),
		libp2p.Identity(privateKey),
		libp2p.NATPortMap(),
		libp2p.EnableAutoRelay(),
		libp2p.Security(tls.ID, tlsTransport),
		libp2p.DefaultMuxers,
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.ConnectionManager(connmgr.NewConnManager(100, 400, time.Minute)),
	)

	if err != nil {
		fmt.Printf("errpr creating p2p host %s", err)
	}

	log.Println("###########")
	log.Println("I am:", p2pHost.ID())
	for _, addr := range p2pHost.Addrs() {
		fmt.Printf("%s: %s/p2p/%s", "I am @:", addr, p2pHost.ID().Pretty())
		fmt.Println()
	}
	log.Println("###########")
	//#########################################
	kadDHT, err := dht.New(ctx, p2pHost)
	if err != nil {
		panic(err)
	}

	// Bootstrap the DHT. In the default configuration, this spawns a Background
	// thread that will refresh the peer table every five minutes.
	log.Println("bootstrapping the DHT")
	if err = kadDHT.Bootstrap(ctx); err != nil {
		return nil, err
	}
	// Let's connect to the bootstrap nodes first. They will tell us about the
	// other nodes in the network.
	var wg sync.WaitGroup
	for _, peerAddr := range config.BootstrapPeers {
		peerInfo, _ := peer.AddrInfoFromP2pAddr(peerAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p2pHost.Connect(ctx, *peerInfo); err != nil {
				log.Println(err)
			} else {
				log.Println("Connection established with bootstrap node:", *peerInfo)
			}
		}()
	}
	wg.Wait()
	return &Node{KadDHT: kadDHT, Host: p2pHost}, nil
}

func (node Node) AdvertiseAndFindPeers(ctx context.Context, cfg config.Config) {
	// We use a rendezvous point "meet me here" to announce our location.
	// This is like telling your friends to meet you at the Eiffel Tower.
	log.Println("Announcing ourselves...")
	routingDiscovery := routing.NewRoutingDiscovery(node.KadDHT)
	dutil.Advertise(ctx, routingDiscovery, cfg.Rendezvous)
	log.Println("Successfully announced!")

	// Now, look for others who have announced
	// This is like your friend telling you the location to meet you.
	for {
		peers, err := routingDiscovery.FindPeers(ctx, cfg.Rendezvous)
		if err != nil {
			log.Printf("error finding peers: ", err)
		}
		for p := range peers {
			if p.ID == node.Host.ID() {
				continue
			}
			if len(p.Addrs) > 0 {
				log.Println("found peers: ")
				for _, addr := range p.Addrs {
					fmt.Printf("%s/p2p/%s", addr, p.ID.Pretty())
					fmt.Println()
					//fmt.Println("--------------NewMultiaddr-----------------")
					//fmt.Println(ma.NewMultiaddr(addr.String()))
					//fmt.Println("--------------AddrInfoFromP2pAddr-----------------")
					//fmt.Println(peer.AddrInfoFromP2pAddr(addr))
				}
				status := node.Host.Network().Connectedness(p.ID)
				if status == network.CanConnect || status == network.NotConnected {
					fmt.Println(p.Addrs[1].String())
					fmt.Println(fmt.Sprintf("%s:%s", strings.Split(p.Addrs[0].String(), "/")[2], strings.Split(p.Addrs[0].String(), "/")[4]))
					if err := node.Host.Connect(ctx, p); err != nil {
						log.Printf("Connection failed:", err)
					} else {
						log.Println("connected to peer: ", p.ID)
						//go client.RunClient(p.Addrs[0].String())
					}
				}
			}

		}
	}
}
