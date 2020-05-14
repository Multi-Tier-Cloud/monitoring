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
)

var (
    debug = flag.Bool("debug", false, "Debug mode")
)

// Context provided here must already has a deadline set
func pingPeer(ctx context.Context, node *p2pnode.Node, peer peer.ID, pingGaugeVec *prometheus.GaugeVec, host string) {
    // Ping and get result
    responseChan := ping.Ping(ctx, node.Host, peer)
    result := <-responseChan
    if result.Error != nil {
        fmt.Println("ID:", peer, "Failed to ping, error:", result.Error)

        // Delete Gauge object
        ok := pingGaugeVec.DeleteLabelValues(fmt.Sprint(peer), host)
        if !ok {
            fmt.Println("Failed to delete gauge for ID:", peer)
        }
        return
    }

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
    host string, debug bool) {

    // Loop infinitely
    for {
        // Per-round context, used to trigger ping goroutines to stop
        ctx, cancel := context.WithTimeout(node.Ctx, time.Second)

        // Get peer in Peerstore
        for _, id := range node.Host.Peerstore().Peers() {
            if id == node.Host.ID() {
                continue
            }

            go pingPeer(ctx, node, id, pingGaugeVec, host)
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
    collect(&node, pingGaugeVec, *name, *debug)
    panic(errors.New("Monitor node exitted monitor loop"))
}
