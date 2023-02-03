package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/libp2p/go-libp2p/core/protocol"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/multiformats/go-multiaddr"
)

func handleStream(stream network.Stream) {
	log.Println("Got a new stream!")

	// Create a buffer stream for non blocking read and write.
	rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

	go readData(rw)
	go writeData(rw)

	// 'stream' will stay open until you close it (or the other side closes it).
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

	if *help {
		fmt.Println("This program demonstrates a simple p2p chat application using libp2p")
		fmt.Println()
		fmt.Println("Usage: Run './chat in two different terminals. Let them connect to the bootstrap nodes, announce themselves and connect to the peers")
		flag.PrintDefaults()
		return
	}

	// libp2p.New constructs a new libp2p Host. Other options can be added
	// here.
	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	host, err := libp2p.New(
		libp2p.Identity(prvKey),
		libp2p.ListenAddrs([]multiaddr.Multiaddr(config.ListenAddresses)...))
	if err != nil {
		panic(err)
	}
	log.Println("Host created. We are:", host.ID())
	log.Printf("'-peer %s/p2p/%s' on another console.\n", host.Addrs()[0].String(), host.ID().Pretty())
	for _, a := range host.Addrs() {
		fmt.Println(a.String())
	}

	// Set a function as stream handler. This function is called when a peer
	// initiates a connection and starts a stream with this peer.
	log.Println("Handler stream set")
	host.SetStreamHandler(protocol.ID(config.ProtocolID), handleStream)
	// Start a DHT, for use in peer discovery. We can't just make a new DHT
	// client because we want each peer to maintain its own local copy of the
	// DHT, so that the bootstrapping node of the DHT can go down without
	// inhibiting future peer discovery.
	ctx := context.Background()
	log.Println("New Dht")
	dhtMode := dht.Mode(dht.ModeServer)
	kademliaDHT, err := dht.New(ctx, host, dhtMode)
	if err != nil {
		panic(err)
	}

	// Bootstrap the DHT. In the default configuration, this spawns a Background
	// thread that will refresh the peer table every five minutes.
	log.Println("Bootstrapping the DHT")
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		panic(err)
	}

	// Let's connect to the bootstrap nodes first. They will tell us about the
	// other nodes in the network.
	var wg sync.WaitGroup
	for _, peerAddr := range config.BootstrapPeers {
		log.Println(peerAddr.String())
	}
	for _, peerAddr := range config.BootstrapPeers {
		peerinfo, _ := peer.AddrInfoFromP2pAddr(peerAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := host.Connect(ctx, *peerinfo); err != nil {
				log.Println(err)
			} else {
				log.Println("Connection established with bootstrap node:", *peerinfo)
				// stream, err := host.NewStream(ctx, peerinfo.ID, protocol.ID(config.ProtocolID))

				// if err != nil {
				// 	log.Println("Connection failed:", err)
				// } else {
				// 	rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

				// 	go writeData(rw)
				// 	go readData(rw)
				// }
				// log.Println("Connected to:", peerinfo.ID.Pretty())
			}
		}()
	}
	wg.Wait()
	time.Sleep(3 * time.Second)

	// We use a rendezvous point "meet me here" to announce our location.
	// This is like telling your friends to meet you at the Eiffel Tower.
	log.Println("Announcing ourselves...")
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
	dutil.Advertise(ctx, routingDiscovery, config.RendezvousString)
	log.Println("Successfully announced!")

	// Now, look for others who have announced
	// This is like your friend telling you the location to meet you.
	log.Println("Searching for other peers...")
	peerChan, err := routingDiscovery.FindPeers(ctx, config.RendezvousString)
	if err != nil {
		panic(err)
	}

	for peer := range peerChan {
		if peer.ID == host.ID() {
			continue
		}
		log.Println("Found peer:", peer)

		log.Println("Connecting to:", peer)
		stream, err := host.NewStream(ctx, peer.ID, protocol.ID(config.ProtocolID))

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

	select {}
}

///ip4/192.168.1.95/tcp/6668/p2p/QmbjSLK9GoZBE5RMadqFc2cWh3pk5cFm3zXuBxjJSGHYyy
