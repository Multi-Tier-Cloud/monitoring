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
//TODO: try with weights
func FindRtt(target_host string) (float64, float64) {
    client, err := api.NewClient(api.Config{
        Address: "http://10.11.17.24:7777",
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
    //fmt.Printf("%v\n %d\n", list, len(list))

    mean, _ := stats.Mean(list)
    median, _ := stats.Median(list)

    //fmt.Printf("mean is %v median is %v\n", mean, median)

    return mean, median
}
