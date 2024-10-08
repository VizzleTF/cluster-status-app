package main

import (
    "encoding/json"
    "fmt"
    "net/http"
)

func statusHandler(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    results := make(map[string]interface{})

    keys := []string{"helm_releases", "node_statuses", "proxmox_nodes", "pod_statuses"}
    
    for _, key := range keys {
        cachedData, err := redisClient.Get(ctx, key).Result()
        if err != nil {
            http.Error(w, fmt.Sprintf("Failed to get %s from cache: %v", key, err), http.StatusInternalServerError)
            return
        }
        
        var data interface{}
        switch key {
        case "helm_releases":
            var helmReleases []HelmRelease
            err = json.Unmarshal([]byte(cachedData), &helmReleases)
            data = helmReleases
        case "node_statuses":
            var nodeStatuses []NodeStatus
            err = json.Unmarshal([]byte(cachedData), &nodeStatuses)
            data = nodeStatuses
        case "proxmox_nodes":
            var proxmoxNodes []ProxmoxNode
            err = json.Unmarshal([]byte(cachedData), &proxmoxNodes)
            data = proxmoxNodes
        case "pod_statuses":
            var podStatuses PodStatuses
            err = json.Unmarshal([]byte(cachedData), &podStatuses)
            data = podStatuses
        }
        
        if err != nil {
            http.Error(w, fmt.Sprintf("Failed to unmarshal %s: %v", key, err), http.StatusInternalServerError)
            return
        }
        
        results[key] = data
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(results)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
    // Проверка состояния всех зависимостей
    if err := checkRedis(); err != nil {
        http.Error(w, fmt.Sprintf("Redis unhealthy: %v", err), http.StatusServiceUnavailable)
        return
    }
    if err := checkKubernetes(); err != nil {
        http.Error(w, fmt.Sprintf("Kubernetes unhealthy: %v", err), http.StatusServiceUnavailable)
        return
    }
    if err := checkProxmox(); err != nil {
        http.Error(w, fmt.Sprintf("Proxmox unhealthy: %v", err), http.StatusServiceUnavailable)
        return
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
    // Проверка готовности приложения к обработке запросов
    if !isAppReady() {
        http.Error(w, "Application is not ready", http.StatusServiceUnavailable)
        return
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte("Ready"))
}

func isAppReady() bool {
    // Проверка готовности всех компонентов приложения
    return checkRedis() == nil &&
           checkKubernetes() == nil &&
           checkProxmox() == nil
}