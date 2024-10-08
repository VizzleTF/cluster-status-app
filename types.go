package main

type WorkloadInfo struct {
    Name      string `json:"name"`
    Kind      string `json:"kind"`
    Ready     string `json:"ready"`
    UpToDate  int32  `json:"upToDate"`
    Available int32  `json:"available"`
}

type HelmRelease struct {
    Name       string         `json:"name"`
    Namespace  string         `json:"namespace"`
    Chart      string         `json:"chart"`
    Version    string         `json:"version"`
    Status     string         `json:"status"`
    Workloads  []WorkloadInfo `json:"workloads"`
}

type NodeStatus struct {
    Name         string   `json:"name"`
    Status       string   `json:"status"`
    Roles        []string `json:"roles"`
    Version      string   `json:"version"`
    InternalIP   string   `json:"internalIP"`
    Age          string   `json:"age"`
    CPUUsage     int64    `json:"cpuUsage"`
    CPULimit     int64    `json:"cpuLimit"`
    MemoryUsage  int64    `json:"memoryUsage"` 
    MemoryLimit  int64    `json:"memoryLimit"` 
    StorageUsage int64    `json:"storageUsage"` 
    StorageLimit int64    `json:"storageLimit"` 
}

type ProxmoxNode struct {
    Name   string `json:"name"`
    Status string `json:"status"`
    Uptime string `json:"uptime"`
}

type PodStatuses struct {
    Running   int `json:"Running"`
    Pending   int `json:"Pending"`
    Failed    int `json:"Failed"`
    Succeeded int `json:"Succeeded"`
    Unknown   int `json:"Unknown"`
}