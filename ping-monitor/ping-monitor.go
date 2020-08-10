package main

// Based on github.com/t-lin/ping-exporter

import (
    "context"
    "errors"
    "flag"
    "fmt"
    "log"
    "net/http"
    "os"
    "regexp"
    "strconv"
    "strings"
    "time"

    "github.com/libp2p/go-libp2p/p2p/protocol/ping"
    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/peer"
    "github.com/libp2p/go-libp2p-core/pnet"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/prometheus/client_golang/prometheus/promhttp"

    "github.com/multiformats/go-multiaddr"

    "github.com/PhysarumSM/common/p2pnode"
    "github.com/PhysarumSM/common/peerlastseen"
    "github.com/PhysarumSM/common/util"
)

const defaultKeyFile = "~/.privKeyPing"

var (
    debug = flag.Bool("debug", false, "Debug mode")
    hostname = flag.String("hostname", "", "Name for labelling metrics (defaults to hostname)")
    promEndpoint = flag.String("prom-listen-addr", ":9100", "Listening address/endpoint for Prometheus to scrape")
    p2pEndpoint = flag.String("p2p-listen-addr", ":4001", "Listening address/endpoing for the P2P node")
)

func init() {
    // Set up logging defaults
    log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

// Context provided here must already has a deadline set
func pingPeer(ctx context.Context, node *p2pnode.Node, peer peer.ID,
        pingGaugeVec *prometheus.GaugeVec,
        pls *peerlastseen.PeerLastSeen) {

    // Ping and get result
    responseChan := ping.Ping(ctx, node.Host, peer)
    result := <-responseChan
    if result.Error != nil {
        log.Println("ID:", peer, "Failed to ping, error:", result.Error)
        return
    }

    err := pls.UpdateLastSeen(peer)
    if err != nil {
        log.Printf("WARNING: Unable to update last seen for peer %s\n%v\n", peer, err)
        return
    }

    if (*debug) {
        log.Println("ID:", peer, "RTT:", result.RTT)
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
        log.Fatalln(err)
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

// Validates an IPv4 TCP or UDP endpoint address
func validateEndpoint(ep string) bool {
    epSplit := strings.Split(ep, ":")
    if len(epSplit) != 2 {
        return false
    }

    ip, portStr := epSplit[0], epSplit[1]

    // Validate IP
    // Allow endpoints with just the port number (e.g. ":1234")
    if ip != "" {
        match, err := regexp.Match("[0-9.]", []byte(ip))
        if match != true || err != nil {
            return false
        }

        ipSplit := strings.Split(ip, ".")
        if len(ipSplit) != 4 {
            return false
        }

        for _, octetStr := range ipSplit {
            octet, err := strconv.Atoi(octetStr)
            if octet < 0 || octet > 255 || err != nil {
                return false
            }
        }
    }

    // Validate port number
    port, err := strconv.Atoi(portStr)
    if err != nil {
        return false
    }

    if port < 1 || port > 65535 {
        return false
    }

    return true
}

func tcpEndpoint2MultiaddrStr(ep string) string {
    if !validateEndpoint(ep) {
        return ""
    }

    // Assume all checks on endpoint are done
    epSplit := strings.Split(ep, ":")
    ip, portStr := epSplit[0], epSplit[1]
    if ip == "" {
        ip = "0.0.0.0"
    }

    return fmt.Sprintf("/ip4/%s/tcp/%s", ip, portStr)
}

func main() {
    var err error
    var keyFlags util.KeyFlags
    var bootstraps *[]multiaddr.Multiaddr
    var psk *pnet.PSK
    if keyFlags, err = util.AddKeyFlags(defaultKeyFile); err != nil {
        log.Fatalln(err)
    }
    if bootstraps, err = util.AddBootstrapFlags(); err != nil {
        log.Fatalln(err)
    }
    if psk, err = util.AddPSKFlag(); err != nil {
		log.Fatalln(err)
	}
    flag.Parse()

    // Default name as hostname
    if *hostname == "" {
        *hostname, err = os.Hostname()
        if err != nil {
            log.Fatalln(err)
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
    if !validateEndpoint(*promEndpoint) {
        log.Fatalf("ERROR: Invalid listening address provided (%s)\n", *promEndpoint)
    }
    go http.ListenAndServe(*promEndpoint, nil)

    // Create or load keys
    priv, err := util.CreateOrLoadKey(keyFlags)
    if err != nil {
        log.Fatalln(err)
    }

    // Setup node configuration
    config := p2pnode.NewConfig()
    config.PrivKey = priv
    config.PSK = *psk

    if !validateEndpoint(*p2pEndpoint) {
        log.Fatalf("ERROR: Invalid listening address provided (%s)\n", *p2pEndpoint)
    }
    config.ListenAddrs = append(config.ListenAddrs, tcpEndpoint2MultiaddrStr(*p2pEndpoint))

    if len(*bootstraps) != 0 {
        // This node should connect to a pre-existing bootstrap as part of
        // a larger network. It should also still be a bootstrap itself.
        config.BootstrapPeers = *bootstraps
    }

    // Create new node
    node, err := p2pnode.NewNode(context.Background(), config)
    if err != nil {
        log.Fatalln(err)
    }

    // Print multiaddress (for copying and pasting to other services)
    log.Println("P2P addresses for this node:")
    addrs, err := util.Whoami(node.Host)
    if err != nil {
        log.Fatalln(err)
    }
    for _, addr := range addrs {
        log.Println("\t", addr)
    }

    // Register notification functions (for printing when peers connect
    // or disconnect), mostly for diagnostics purposes
    netCallbacks := network.NotifyBundle{}
    netCallbacks.ConnectedF = func(net network.Network, conn network.Conn) {
        fmt.Printf("\nNew connection to peer %s (%s)\n",
            conn.RemotePeer(), conn.RemoteMultiaddr())
    }
    netCallbacks.DisconnectedF = func(net network.Network, conn network.Conn) {
        fmt.Printf("\nDisconnected from peer %s (%s)\n",
            conn.RemotePeer(), conn.RemoteMultiaddr())
    }

    node.Host.Network().Notify(&netCallbacks)

    // Define callback for PeerLastSeen
    expireMetrics := func(id peer.ID) {
        // Delete Gauge objects
        ok := pingGaugeVec.DeleteLabelValues(fmt.Sprint(id), *hostname)
        if !ok {
            log.Printf("Failed to delete ping gauge for ID:", id)
        }

        ok = ewmaGaugeVec.DeleteLabelValues(fmt.Sprint(id), *hostname)
        if !ok {
            log.Printf("Failed to delete EWMA gauge for ID:", id)
        }
    }

    // Start background goroutine to export the EWMA metric in PeerStore
    go exportEWMAs(&node, ewmaGaugeVec)

    // Start pinging (each ping RTT sample will automatically re-calculate
    // the EWMAs in the PeerStore). The call to collect() should not return.
    log.Println("Starting RTT collection from peers")
    collect(&node, pingGaugeVec, expireMetrics)

    log.Fatalln(errors.New("Monitor node exitted monitor loop"))
}
