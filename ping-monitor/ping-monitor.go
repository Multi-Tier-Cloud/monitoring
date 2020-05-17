package main

// Based on github.com/t-lin/ping-exporter

import (
    "context"
    "errors"
    "flag"
    "fmt"
    "net/http"
    "os"
    "strconv"
    "time"

    "github.com/libp2p/go-libp2p/p2p/protocol/ping"
    "github.com/libp2p/go-libp2p-core/peer"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/prometheus/client_golang/prometheus/promhttp"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/monitoring/peerlastseen"
)

var (
    debug = flag.Bool("debug", false, "Debug mode")
)

// Context provided here must already has a deadline set
func pingPeer(ctx context.Context, node *p2pnode.Node, peer peer.ID,
        pingGaugeVec *prometheus.GaugeVec,
        host string, pls *peerlastseen.PeerLastSeen) {

    // Ping and get result
    responseChan := ping.Ping(ctx, node.Host, peer)
    result := <-responseChan
    if result.Error != nil {
        fmt.Println("ID:", peer, "Failed to ping, error:", result.Error)
        return
    }

    pls.UpdateLastSeen(peer)

    if (*debug) {
        fmt.Println("ID:", peer, "RTT:", result.RTT)
    }

    // Get Gauge object with id as targetHost
    pingGauge := pingGaugeVec.WithLabelValues(fmt.Sprint(peer), host)
    pingGauge.Set(float64(result.RTT) / 1000000) // Convert ns to ms
}

// collect pings all peers in the monitor node's Peerstore to collect
// performance data. For now it prints out what it finds
func collect(node *p2pnode.Node, pingGaugeVec *prometheus.GaugeVec,
    host string) {

    callback := func(id peer.ID) {
        // Delete Gauge object
        ok := pingGaugeVec.DeleteLabelValues(fmt.Sprint(id), host)
        if !ok {
            fmt.Println("Failed to delete gauge for ID:", id)
        }
    }

    // Create PeerLastSeen with timeout of 10 minutes for peers (arbitrary at this point)
    // TODO: Make the timeout configurable
    pls, err := peerlastseen.NewPeerLastSeen(10 * time.Minute, callback)
    if err != nil {
        panic(err)
    }

    // Loop infinitely
    for {
        // Per-round context, used to trigger ping goroutines to stop
        ctx, cancel := context.WithTimeout(node.Ctx, time.Second)

        // Get peer in Peerstore
        for _, id := range node.Host.Network().Peers() {
            if id == node.Host.ID() {
                continue
            }

            go pingPeer(ctx, node, id, pingGaugeVec, host, pls)
        }

        <-ctx.Done()
        cancel() // In case anyone isn't listening on Done()
    }
}

func main() {
    name := flag.String("name", "", "Name for labelling metrics (defaults to hostname)")
    port := flag.Int("port", 8888, "Port to export metrics")
    flag.Parse()

    // Default name as hostname
    if *name == "" {
        var err error
        *name, err = os.Hostname()
        if err != nil {
            panic(err)
        }
    }

    // Prometheus metrics endpoint
    pMetricsPath := "/metrics"
    pListenAddress := ":" + strconv.Itoa(*port)

    // Set up Prometheus GaugeVec object
    pingGaugeVec := promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "ping_rtt",
            Help: "Historical ping RTTs over time (ms)",
        },
        []string{
            "targetHost", // Specify ping target
            "host",   // Name of host running ping-exporter
        },
    )

    // Map Prometheus metrics scrape path to handler function
    http.Handle(pMetricsPath, promhttp.Handler())

    // Start server in separate goroutine
    go http.ListenAndServe(pListenAddress, nil)

    // Setup node
    ctx := context.Background()

    config := p2pnode.NewConfig()
    // Set no rendezvous (anonymous mode)
    config.Rendezvous = []string{}
    node, err := p2pnode.NewNode(ctx, config)
    if err != nil {
        panic(err)
    }

    fmt.Println("Starting data collection")
    collect(&node, pingGaugeVec, *name)
    panic(errors.New("Monitor node exitted monitor loop"))
}
