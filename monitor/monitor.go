package main

import (
    "context"
    "errors"
    "fmt"

    "github.com/libp2p/go-libp2p/p2p/protocol/ping"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
)

func collect(node *p2pnode.Node) {
    for {
        for _, id := range node.Host.Peerstore().Peers() {
            responseChan := ping.Ping(node.Ctx, node.Host, id)
            result := <-responseChan
            if result.RTT == 0 {
                fmt.Println("ID:", id, "Failed to ping, RTT = 0")
                continue
            }
            fmt.Println("ID:", id, "RTT:", result.RTT)
        }
    }
}

func main() {

    ctx := context.Background()

    config := p2pnode.NewConfig()
    // Set no rendezvous (anonymous mode)
    config.Rendezvous = []string{}
    node, err := p2pnode.NewNode(ctx, config)
    if err != nil {
        panic(err)
    }

    collect(&node)
    panic(errors.New("Monitor node exitted monitor loop"))
}
