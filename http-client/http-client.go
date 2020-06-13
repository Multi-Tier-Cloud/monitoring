package http_client

import (
    "context"
    "fmt"
    "os"
    "time"
    "strings"
    "strconv"

    "github.com/prometheus/client_golang/api"
    v1 "github.com/prometheus/client_golang/api/prometheus/v1"
    "github.com/montanaflynn/stats"
)

// finds target host's mean and median rtt over last 5 min
// addr is prometheus ip:port, target_host is the p2p hash
//TODO: try with weights
func FindRtt(addr string, target_host string) (float64, float64) {
    client, err := api.NewClient(api.Config{
        Address: "http://"+addr,
    })
    if err != nil {
        fmt.Printf("Error creating client: %v\n", err)
        os.Exit(1)
    }

    v1api := v1.NewAPI(client)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    r := v1.Range{
            Start: time.Now().Add(-time.Minute*5),
            End:   time.Now(),
            Step:  time.Second,
    }

    query := "ping_rtt{targetHost='"+target_host+"'}"

    result, warnings, err := v1api.QueryRange(ctx, query, r)
    if err != nil {
        fmt.Printf("Error querying Prometheus: %v\n", err)
        os.Exit(1)
    }
    if len(warnings) > 0 {
        fmt.Printf("Warnings: %v\n", warnings)
    }

    results := strings.Split(result.String(), "\n")[1:]

    var list []float64
    for _, value := range results {
        time := strings.Split(value, " ")[0]
        rtt, _ := strconv.ParseFloat(time, 64)
        list = append(list, rtt)
    }

    mean, _ := stats.Mean(list)
    median, _ := stats.Median(list)

    return mean, median
}

// finds system cpu usage with 2 points over 15s
// addr is prometheus ip:port, host_machine is the vm name
func FindCpu(addr string, host_machine string) (float64) {
    client, err := api.NewClient(api.Config{
        Address: "http://"+addr,
    })
    if err != nil {
        fmt.Printf("Error creating client: %v\n", err)
        os.Exit(1)
    }

    v1api := v1.NewAPI(client)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    host_machine += "-health"
    query := "100*(1-irate(node_cpu_seconds_total{job='"+host_machine+"',mode='idle'}[15s]))"

    result, warnings, err := v1api.Query(ctx, query, time.Now())
    if err != nil {
        fmt.Printf("Error querying Prometheus: %v\n", err)
        os.Exit(1)
    }
    if len(warnings) > 0 {
        fmt.Printf("Warnings: %v\n", warnings)
    }

    results := strings.Split(result.String(), " ")
    cpu, _ := strconv.ParseFloat(results[len(results)-2], 64)

    return cpu
}

// finds system ram usage avg over 15s
// addr is prometheus ip:port, host_machine is the vm name
func FindMemory(addr string, host_machine string) (float64) {
    client, err := api.NewClient(api.Config{
        Address: "http://"+addr,
    })
    if err != nil {
        fmt.Printf("Error creating client: %v\n", err)
        os.Exit(1)
    }

    v1api := v1.NewAPI(client)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    host_machine += "-health"
    query := "100*(1-((avg_over_time(node_memory_Buffers_bytes{job='"+host_machine+"'}[15s])+avg_over_time(node_memory_MemFree_bytes{job='"+host_machine+"'}[15s])+avg_over_time(node_memory_Cached_bytes{job='"+host_machine+"'}[15s]))/avg_over_time(node_memory_MemTotal_bytes{job='"+host_machine+"'}[15s])))"

    result, warnings, err := v1api.Query(ctx, query, time.Now())
    if err != nil {
        fmt.Printf("Error querying Prometheus: %v\n", err)
        os.Exit(1)
    }
    if len(warnings) > 0 {
        fmt.Printf("Warnings: %v\n", warnings)
    }

    results := strings.Split(result.String(), " ")
    mem, _ := strconv.ParseFloat(results[len(results)-2], 64)

    return mem
}
