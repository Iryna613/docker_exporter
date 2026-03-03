package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	containerMem = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_memory_usage_bytes",
			Help: "Current memory usage of a Docker container.",
		},
		[]string{"container_id", "name"},
	)
	containerCPU = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_cpu_usage_seconds_total",
			Help: "Total CPU usage seconds of a Docker container.",
		},
		[]string{"container_id", "name"},
	)
)

func collectLoop(cli *client.Client) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ctx := context.Background()
		containers, err := cli.ContainerList(ctx, container.ListOptions{})
		if err != nil {
			log.Println("list containers:", err)
			continue
		}

		for _, c := range containers {
			stats, err := cli.ContainerStatsOneShot(ctx, c.ID)
			if err != nil {
				log.Println("stats:", c.ID, err)
				continue
			}
			var s types.StatsJSON
			if err := json.NewDecoder(stats.Body).Decode(&s); err != nil {
				log.Println("decode:", err)
				stats.Body.Close()
				continue
			}
			stats.Body.Close()

			name := ""
			if len(c.Names) > 0 {
				name = strings.TrimPrefix(c.Names[0], "/")
			}

			// memory usage
			mem := float64(s.MemoryStats.Usage)
			containerMem.WithLabelValues(c.ID, name).Set(mem)

			// cpu usage (total seconds)
			cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage)
			systemDelta := float64(s.CPUStats.SystemUsage - s.PreCPUStats.SystemUsage)
			var cpuTotal float64
			if systemDelta > 0 {
				onlineCPUs := float64(len(s.CPUStats.CPUUsage.PercpuUsage))
				cpuTotal = (cpuDelta / systemDelta) * onlineCPUs
			}
			containerCPU.WithLabelValues(c.ID, name).Add(cpuTotal)
		}
	}
}

func main() {
	prometheus.MustRegister(containerMem, containerCPU)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}

	go collectLoop(cli)

	http.Handle("/metrics", promhttp.Handler())
	log.Println("listening on :9273")
	log.Fatal(http.ListenAndServe(":9273", nil))
}
