package main

// Based on github.com/t-lin/ping-exporter

import (
    "context"
    "errors"
    "flag"
    "fmt"
    "net/http"
    "os"
    "time"

    "github.com/libp2p/go-libp2p/p2p/protocol/ping"
    "github.com/libp2p/go-libp2p-core/peer"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/prometheus/client_golang/prometheus/promhttp"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/peerlastseen"
)

var (
    debug = flag.Bool("debug", false, "Debug mode")
    hostname = flag.String("hostname", "", "Name for labelling metrics (defaults to hostname)")
    promEndpoint = flag.String("prom-listen-addr", ":8888", "Listening address/endpoint for Prometheus to scrape")
)

// Context provided here must already has a deadline set
func pingPeer(ctx context.Context, node *p2pnode.Node, peer peer.ID,
        pingGaugeVec *prometheus.GaugeVec,
        pls *peerlastseen.PeerLastSeen) {

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

    // Get Gauge object with peer id as targetHost
    pingGauge := pingGaugeVec.WithLabelValues(fmt.Sprint(peer), *hostname)
    pingGauge.Set(float64(result.RTT) / 1000000) // Convert ns to ms
}

// collect pings all peers in the monitor node's Peerstore to collect
// performance data. For now it prints out what it finds
func collect(node *p2pnode.Node,
        pingGaugeVec *prometheus.GaugeVec,
        callback peerlastseen.PeerLastSeenCB) {

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

            go pingPeer(ctx, node, id, pingGaugeVec, pls)
        }

        <-ctx.Done()
        cancel() // In case anyone isn't listening on Done()
    }
}

func exportEWMAs(node *p2pnode.Node, ewmaGaugeVec *prometheus.GaugeVec) {
    ticker := time.NewTicker(time.Second)

    for {
        <-ticker.C

        for _, id := range node.Host.Network().Peers() {
            if id == node.Host.ID() {
                continue
            }

            // Get Gauge object with peer id as targetHost
            ewmaGauge := ewmaGaugeVec.WithLabelValues(fmt.Sprint(id), *hostname)
            ewmaGauge.Set(float64(node.Host.Peerstore().LatencyEWMA(id)) / 1000000) // Convert ns to ms
        }
    }
}

func main() {
    flag.Parse()

    // Default name as hostname
    if *hostname == "" {
        var err error
        *hostname, err = os.Hostname()
        if err != nil {
            panic(err)
        }
    }

    // Set up Prometheus GaugeVec object for raw RTTs to other peers
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

    // Set up Prometheus GaugeVec object EWMA RTTs to other peers
    // The EWMA calculation is part of libp2p's PeerStore and the
    // smoothing factor is hard-coded to 0.1.
    ewmaGaugeVec := promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "ping_ewma_rtt",
            Help: "Exponentially weighted moving average of ping RTTs over time (ms)",
        },
        []string{
            "targetHost", // Specify ping target
            "host",   // Name of host running ping-exporter
        },
    )

    // Map Prometheus metrics scrape path to handler function
    pMetricsPath := "/metrics" // Make configurable?
    http.Handle(pMetricsPath, promhttp.Handler())

    // Start server in separate goroutine
    go http.ListenAndServe(*promEndpoint, nil)

    // Setup node
    ctx := context.Background()

    config := p2pnode.NewConfig()
    // Set no rendezvous (anonymous mode)
    config.Rendezvous = []string{}
    node, err := p2pnode.NewNode(ctx, config)
    if err != nil {
        panic(err)
    }

    // Define callback for PeerLastSeen
    expireMetrics := func(id peer.ID) {
        // Delete Gauge objects
        ok := pingGaugeVec.DeleteLabelValues(fmt.Sprint(id), *hostname)
        if !ok {
            fmt.Printf("Failed to delete ping gauge for ID:", id)
        }

        ok = ewmaGaugeVec.DeleteLabelValues(fmt.Sprint(id), *hostname)
        if !ok {
            fmt.Printf("Failed to delete EWMA gauge for ID:", id)
        }
    }

    // Start background goroutine to export the EWMA metric in PeerStore
    go exportEWMAs(&node, ewmaGaugeVec)

    // Start pinging (each ping RTT sample will automatically re-calculate
    // the EWMAs in the PeerStore).
    fmt.Println("Starting data collection")
    collect(&node, pingGaugeVec, expireMetrics)
    panic(errors.New("Monitor node exitted monitor loop"))
}
