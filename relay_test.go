package main

import (
	"fmt"
	"testing"

	bhost "github.com/libp2p/go-libp2p-blankhost"
	"github.com/libp2p/go-libp2p-core/host"
	swarmt "github.com/libp2p/go-libp2p-swarm/testing"
)

func getNetHosts(t *testing.T, n int) []host.Host {
	var out []host.Host

	for i := 0; i < n; i++ {
		netw := swarmt.GenSwarm(t)
		h := bhost.NewBlankHost(netw)
		out = append(out, h)
	}

	return out
}

func TestRelay(t *testing.T) {
	relay := getNetHosts(t, 10)
	for _, r := range relay {
		fmt.Println(r.Addrs())
	}
}
