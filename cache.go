package main

import (
    "context"
    "encoding/json"
    "log"
    "sync"
    "time"
)

func updateCacheperiodically() {
    ticker := time.NewTicker(cacheTTL)
    defer ticker.Stop()

    for {
        updateCache()
        <-ticker.C
    }
}

func updateCache() {
    ctx := context.Background()
    var wg sync.WaitGroup
    wg.Add(4)

    go func() {
        defer wg.Done()
        helmReleases, err := getHelmReleases()
        if err != nil {
            log.Printf("Failed to get Helm releases: %v", err)
            return
        }
        cacheData, _ := json.Marshal(helmReleases)
        redisClient.Set(ctx, "helm_releases", cacheData, cacheTTL)
    }()

    go func() {
        defer wg.Done()
        nodeStatuses, err := getNodeStatuses()
        if err != nil {
            log.Printf("Failed to get node statuses: %v", err)
            return
        }
        cacheData, _ := json.Marshal(nodeStatuses)
        redisClient.Set(ctx, "node_statuses", cacheData, cacheTTL)
    }()

    go func() {
        defer wg.Done()
        proxmoxNodes, err := getProxmoxNodes()
        if err != nil {
            log.Printf("Failed to get Proxmox nodes: %v", err)
            return
        }
        cacheData, _ := json.Marshal(proxmoxNodes)
        redisClient.Set(ctx, "proxmox_nodes", cacheData, cacheTTL)
    }()

    go func() {
        defer wg.Done()
        podStatuses, err := getPodStatuses()
        if err != nil {
            log.Printf("Failed to get pod statuses: %v", err)
            return
        }
        cacheData, _ := json.Marshal(podStatuses)
        redisClient.Set(ctx, "pod_statuses", cacheData, cacheTTL)
    }()

    wg.Wait()
    log.Println("Cache updated successfully")
}