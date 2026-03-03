package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
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
		[]string{"container_id", "name", "nodename", "service", "stack"},
	)

	containerMemRSS = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_memory_rss_bytes",
			Help: "RSS memory of a Docker container.",
		},
		[]string{"container_id", "name", "nodename", "service", "stack"},
	)

	containerMemCache = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_memory_cache_bytes",
			Help: "Page cache memory of a Docker container.",
		},
		[]string{"container_id", "name", "nodename", "service", "stack"},
	)

	containerMemLimit = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_memory_limit_bytes",
			Help: "Configured memory limit of a Docker container.",
		},
		[]string{"container_id", "name", "nodename", "service", "stack"},
	)

	containerMemMax = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_memory_max_usage_bytes",
			Help: "Maximum observed memory usage of a Docker container.",
		},
		[]string{"container_id", "name", "nodename", "service", "stack"},
	)

	containerCPU = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_cpu_usage_ratio",
			Help: "Approximate CPU usage ratio over last scrape interval (0..N cores).",
		},
		[]string{"container_id", "name", "nodename", "service", "stack"},
	)

	containerNetRx = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_network_receive_bytes_total",
			Help: "Total received bytes across all interfaces of a Docker container.",
		},
		[]string{"container_id", "name", "nodename", "service", "stack"},
	)

	containerNetTx = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "docker_container_network_transmit_bytes_total",
			Help: "Total transmitted bytes across all interfaces of a Docker container.",
		},
		[]string{"container_id", "name", "nodename", "service", "stack"},
	)
)

func collectLoop(cli *client.Client, nodeName string) {
	log.Println("starting collect loop")

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
			service := ""
			stack := ""

			// swarm service name
			if v, ok := c.Labels["com.docker.swarm.service.name"]; ok {
				service = v
			}
			// docker‑compose service name
			if v, ok := c.Labels["com.docker.compose.service"]; ok && service == "" {
				service = v
			}

			// swarm stack namespace (docker stack deploy)
			if v, ok := c.Labels["com.docker.stack.namespace"]; ok {
				stack = v
			}
			// docker‑compose project name
			if v, ok := c.Labels["com.docker.compose.project"]; ok && stack == "" {
				stack = v
			}

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

			// memory
			memUsage := float64(s.MemoryStats.Usage)
			memRSS := float64(s.MemoryStats.Stats["rss"])
			memCache := float64(s.MemoryStats.Stats["cache"])
			memLimit := float64(s.MemoryStats.Limit)
			memMax := float64(s.MemoryStats.MaxUsage)

			containerMem.WithLabelValues(c.ID, name, nodeName, service, stack).Set(memUsage)
			containerMemRSS.WithLabelValues(c.ID, name, nodeName, service, stack).Set(memRSS)
			containerMemCache.WithLabelValues(c.ID, name, nodeName, service, stack).Set(memCache)
			containerMemLimit.WithLabelValues(c.ID, name, nodeName, service, stack).Set(memLimit)
			containerMemMax.WithLabelValues(c.ID, name, nodeName, service, stack).Set(memMax)

			cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage)
			systemDelta := float64(s.CPUStats.SystemUsage - s.PreCPUStats.SystemUsage)
			cpuRatio := 0.0
			if systemDelta > 0 {
				onlineCPUs := float64(len(s.CPUStats.CPUUsage.PercpuUsage))
				if onlineCPUs == 0 {
					onlineCPUs = 1
				}
				cpuRatio = (cpuDelta / systemDelta) * onlineCPUs
			}
			// cpu
			containerCPU.WithLabelValues(c.ID, name, nodeName, service, stack).Set(cpuRatio)

			rxTotal := 0.0
			txTotal := 0.0
			for _, v := range s.Networks {
				rxTotal += float64(v.RxBytes)
				txTotal += float64(v.TxBytes)
			}
			// network
			containerNetRx.WithLabelValues(c.ID, name, nodeName, service, stack).Set(rxTotal)
			containerNetTx.WithLabelValues(c.ID, name, nodeName, service, stack).Set(txTotal)
		}
	}
}

func main() {
	prometheus.MustRegister(
		containerMem,
		containerMemRSS,
		containerMemCache,
		containerMemLimit,
		containerMemMax,
		containerCPU,
		containerNetRx,
		containerNetTx,
	)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		nodeName = "unknown"
	}
	go collectLoop(cli, nodeName)

	log.Println("starting exporter on :9273")
	http.Handle("/metrics", promhttp.Handler())
	log.Println("listening on :9273")
	log.Fatal(http.ListenAndServe(":9273", nil))
}
