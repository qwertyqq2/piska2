package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"

	"github.com/libp2p/go-libp2p/core/protocol"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/multiformats/go-multiaddr"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
)

type addrList []maddr.Multiaddr

func (al *addrList) String() string {
	strs := make([]string, len(*al))
	for i, addr := range *al {
		strs[i] = addr.String()
	}
	return strings.Join(strs, ",")
}

func (al *addrList) Set(value string) error {
	addr, err := maddr.NewMultiaddr(value)
	if err != nil {
		return err
	}
	*al = append(*al, addr)
	return nil
}

func StringsToAddrs(addrStrings []string) (maddrs []maddr.Multiaddr, err error) {
	for _, addrString := range addrStrings {
		addr, err := maddr.NewMultiaddr(addrString)
		if err != nil {
			return maddrs, err
		}
		maddrs = append(maddrs, addr)
	}
	return
}

type Config struct {
	RendezvousString string
	BootstrapPeers   addrList
	ListenAddresses  addrList
	ProtocolID       string
}

func ParseFlags() (Config, error) {
	config := Config{}
	flag.StringVar(&config.RendezvousString, "myrandevy", "meet me here",
		"Unique string to identify group of nodes. Share this with your friends to let them connect with you")
	flag.Var(&config.BootstrapPeers, "peer", "Adds a peer multiaddress to the bootstrap list")
	flag.Var(&config.ListenAddresses, "listen", "Adds a multiaddress to the listen list")
	flag.StringVar(&config.ProtocolID, "pid", "/chat/1.1.0", "Sets a protocol id for stream headers")
	flag.Parse()

	if len(config.BootstrapPeers) == 0 {
		config.BootstrapPeers = dht.DefaultBootstrapPeers
	}

	return config, nil
}

func handleStream(stream network.Stream) {
	log.Println("Got a new stream!")

	rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

	go readData(rw)
	go writeData(rw)

}

func readData(rw *bufio.ReadWriter) {
	for {
		str, err := rw.ReadString('\n')
		if err != nil {
			log.Println("Error reading from buffer")
			break
		}
		if str == "" {
			return
		}
		if str != "\n" {
			log.Printf("\x1b[32m%s\x1b[0m> ", str)
		}
	}
}

func writeData(rw *bufio.ReadWriter) {
	stdReader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		sendData, err := stdReader.ReadString('\n')
		if err != nil {
			log.Println("Error reading from stdin")
			break
		}

		_, err = rw.WriteString(fmt.Sprintf("%s\n", sendData))
		if err != nil {
			log.Println("Error writing to buffer")
			break
		}
		err = rw.Flush()
		if err != nil {
			log.Println("Error flushing buffer")
			break
		}
	}
}

func main() {
	help := flag.Bool("h", false, "Display Help")
	config, err := ParseFlags()
	if err != nil {
		panic(err)
	}

	f, err := os.OpenFile("text.log",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Println(err)
	}
	defer f.Close()
	logger := log.New(f, "log: ", log.LstdFlags)

	if *help {
		fmt.Println("This program demonstrates a simple p2p chat application using libp2p")
		fmt.Println()
		fmt.Println("Usage: Run './chat in two different terminals. Let them connect to the bootstrap nodes, announce themselves and connect to the peers")
		flag.PrintDefaults()
		return
	}

	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	limiterCfg, err := os.Open("limiterCfg.json")
	if err != nil {
		panic(err)
	}
	limiter, err := rcmgr.NewDefaultLimiterFromJSON(limiterCfg)
	if err != nil {
		panic(err)
	}
	rcm, err := rcmgr.NewResourceManager(limiter)
	if err != nil {
		panic(err)
	}
	boostrapInfo := make([]peer.AddrInfo, len(config.BootstrapPeers))
	for i, peerAddr := range config.BootstrapPeers {
		peerinfo, _ := peer.AddrInfoFromP2pAddr(peerAddr)
		boostrapInfo[i] = *peerinfo

	}

	host, err := libp2p.New(
		libp2p.Identity(prvKey),
		libp2p.EnableAutoRelay(
			autorelay.WithStaticRelays(boostrapInfo),
			autorelay.WithCircuitV1Support(),
		),
		libp2p.ListenAddrs([]multiaddr.Multiaddr(config.ListenAddresses)...),
		libp2p.NATPortMap(),
		libp2p.ResourceManager(rcm),
	)
	if err != nil {
		panic(err)
	}
	logger.Println("Host created. We are:", host.ID())
	logger.Printf("'-peer %s/p2p/%s' on another console.\n", host.Addrs()[0].String(), host.ID().Pretty())
	for _, a := range host.Addrs() {
		fmt.Println(a.String())
	}

	log.Println("Handler stream set")
	host.SetStreamHandler(protocol.ID(config.ProtocolID), handleStream)

	ctx := context.Background()
	log.Println("New Dht")
	kademliaDHT, err := dht.New(
		ctx,
		host,
		dht.Mode(dht.ModeServer))
	if err != nil {
		panic(err)
	}

	log.Println("Bootstrapping the DHT")
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		panic(err)
	}

	log.Println("Loading peers...")

	var wg sync.WaitGroup
	log.Println("Boostrap address")
	for _, peerAddr := range config.BootstrapPeers {
		log.Println(peerAddr.String())
	}
	for _, peerAddr := range config.BootstrapPeers {
		peerinfo, _ := peer.AddrInfoFromP2pAddr(peerAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := host.Connect(ctx, *peerinfo); err != nil {
				logger.Println(err)
			} else {
				log.Println("Connection established with bootstrap node:", *peerinfo)
				_, err = client.Reserve(context.Background(), host, *peerinfo)
				if err != nil {
					log.Printf("host failed to receive a relay reservation from relay. %v", err)
				} else {
					log.Println("Connection established with relay node")
				}
			}
		}()
	}
	wg.Wait()
	c, _ := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum([]byte("meet me here"))

	tctx, _ := context.WithTimeout(ctx, time.Second*120)
	if err := kademliaDHT.Provide(tctx, c, true); err != nil {
		panic(err)
	}
	peers, err := kademliaDHT.FindProviders(tctx, c)
	if err != nil {
		panic(err)
	}
	log.Printf("Found %d peers!\n", len(peers))
	for _, p := range peers {
		fmt.Println(p.Addrs)
	}

	log.Println("Announcing ourselves...")
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
	dutil.Advertise(ctx, routingDiscovery, config.RendezvousString)
	log.Println("Successfully announced!")

	log.Println("Searching for other peers...")
	peerChan, err := routingDiscovery.FindPeers(
		ctx,
		config.RendezvousString,
		discovery.Limit(100),
	)
	if err != nil {
		panic(err)
	}

	for peer := range peerChan {
		if peer.ID == host.ID() {
			continue
		}
		log.Println("Found peer:", peer)

		log.Println("Connecting to:", peer)
		stream, err := host.NewStream(network.WithUseTransient(context.Background(), config.ProtocolID), peer.ID, protocol.ID(config.ProtocolID))
		if err != nil {
			log.Println("Connection failed:", err)
			continue
		} else {
			rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

			go writeData(rw)
			go readData(rw)
		}
		log.Println("Connected to:", peer)
	}
	log.Println("Chan closed")
	// var peerInfos []string
	// for _, peerID := range kademliaDHT.RoutingTable().ListPeers() {
	// 	peerInfo := host.Peerstore().PeerInfo(peerID)
	// 	peerInfos = append(peerInfos, peerInfo.Addrs[0].String())
	// }
	// for _, pif := range peerInfos {
	// 	log.Println(pif)
	// }
	log.Println("Wait")
	select {}
}

///QmaMzqsFed4fKe6ZvnvonMJSPtKrgLfzUxukSRabYSZNK6
///QmdT75ooN5fTLB2qrBvZ2nfLPZFS9M3vmCrGZb1i4KwhK6
